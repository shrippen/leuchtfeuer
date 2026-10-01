package main

// Leuchtring als Audio-Visualizer.
//
// Ton: Das LADSPA-Plugin leuchtfeuer-viz-tap (device/src/leuchtfeuer-viz-tap.c) sitzt in asound-music.conf in der Kette
// aller Musikdienste und legt eine Mono-Kopie in /dev/shm/leuchtfeuer-viz (Ringpuffer, siehe dort).
// Ring: Der Ring-Controller (MCU) hängt an /dev/i2c-0, Adresse 0x36. Ein Bild ist der Befehl 0e 01 und 13 x RGB
// (die Hersteller-Muster nutzen nur die ersten 12 LEDs). mcu-interface schickt seine Muster genauso, öffnet den
// Bus aber nur für jede Übertragung; der Kern ordnet die Übertragungen nacheinander.
// Solange der Hersteller den Ring braucht (Drehrad, Stumm, Wecker, Timer, Tasten, Ring-Test), schreibt der
// Visualizer nichts.

import (
	"errors"
	"log"
	"math"
	"math/cmplx"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type VizSettings struct {
	Mode       string `json:"mode"`       // off | spectrum | level | pulse | static (gleichmäßiges Licht)
	Color      string `json:"color"`      // rainbow | white | warm | blue | green | red | purple | custom
	RGB        [3]int `json:"rgb"`        // eigene Farbe (Color "custom"), 0 ... 255
	Brightness int    `json:"brightness"` // 5 ... 100 %
	Rotate     int    `json:"rotate"`     // 0 ... 11: LED, bei der Spektrum und Pegel beginnen
	TimerRing  bool   `json:"timerRing"`  // laufender Timer: Restzeit als Füllstand auf dem Ring
}

var vizModes = map[string]bool{"off": true, "spectrum": true, "level": true, "pulse": true, "static": true}

var vizColors = map[string][3]float64{
	"white": {1, 1, 1}, "warm": {1, .55, .16}, "blue": {.16, .43, 1}, "green": {.12, 1, .24},
	"red": {1, .08, .04}, "purple": {.67, .16, 1}, "rainbow": {}, "custom": {},
}

// ringScene: Anzeige mit Vorrang vor dem Visualizer (Lichtwecker, Timer-Fortschritt). ok = jetzt etwas zeigen.
type ringScene func(now time.Time, fr *ringFrame) (ok bool)

const (
	vizLEDs    = 12
	vizFPS     = 25
	vizFFT     = 2048
	vizShmPath = "/dev/shm/leuchtfeuer-viz"
	vizShmN    = 8192
	vizMagic   = 0x315a5649
)

type ringFrame [vizLEDs][3]byte

type ringWriter interface {
	Write(f *ringFrame) error
}

type visualizer struct {
	app  *app
	ring ringWriter
	tap  *vizTap

	mu        sync.Mutex
	holdUntil time.Time
	holdOn    bool // Hersteller-Animation läuft bis Off()

	// Zustand der Berechnung
	vals    [vizLEDs]float64
	refDB   float64 // gleitender Höchstwert (automatische Verstärkung)
	lastPos uint32
	lastNew time.Time
	active  bool // schreibt gerade auf den Ring
	fade    int
	phase   float64
	tapOK   atomic.Bool
	showing atomic.Bool
	scenes  []ringScene
}

func newVisualizer(a *app) *visualizer {
	return &visualizer{app: a, ring: &ringI2C{}, refDB: -30}
}

// Hold: Hersteller zeigt etwas an (Drehrad, Taste, einmalige Animation).
func (v *visualizer) Hold(d time.Duration) {
	v.mu.Lock()
	if t := time.Now().Add(d); t.After(v.holdUntil) {
		v.holdUntil = t
	}
	v.mu.Unlock()
}

func (v *visualizer) HoldOn()  { v.mu.Lock(); v.holdOn = true; v.mu.Unlock() }
func (v *visualizer) HoldOff() { v.mu.Lock(); v.holdOn = false; v.mu.Unlock(); v.Hold(time.Second) }

func (v *visualizer) held() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.holdOn || time.Now().Before(v.holdUntil)
}

// Status für die Weboberfläche
func (v *visualizer) Status() map[string]bool {
	return map[string]bool{"tap": v.tapOK.Load(), "active": v.showing.Load()}
}

func (v *visualizer) Run() {
	h := v.app.h
	h.Subscribe(hw.WAMP.VolumeChanged, func([]any) { v.Hold(2500 * time.Millisecond) })
	h.Subscribe(hw.WAMP.MuteChanged, func([]any) { v.Hold(2500 * time.Millisecond) })
	h.Subscribe(hw.WAMP.InputEvent, func([]any) { v.Hold(3 * time.Second) })
	t := time.NewTicker(time.Second / vizFPS)
	defer t.Stop()
	reopen := time.Time{}
	for now := range t.C {
		if now.After(reopen) {
			v.tap = reopenTap(v.tap)
			v.tapOK.Store(v.tap != nil)
			reopen = now.Add(2 * time.Second)
		}
		v.step(now)
	}
}

func (v *visualizer) step(now time.Time) {
	set := v.app.st.Viz()
	_, muted, _ := v.app.vol.Get()
	playing := false
	if v.tap != nil {
		if p := v.tap.pos(); p != v.lastPos {
			v.lastPos, v.lastNew = p, now
		}
		playing = now.Sub(v.lastNew) < 300*time.Millisecond
	}
	if v.held() {
		v.stop(false)
		return
	}
	var fr ringFrame
	for _, sc := range v.scenes {
		if sc(now, &fr) {
			if err := v.ring.Write(&fr); err == nil {
				v.active = true
				v.showing.Store(false)
			}
			return
		}
	}
	if set.Mode == "static" {
		renderRing(&fr, nil, set, 0)
		if err := v.ring.Write(&fr); err == nil {
			v.active = true
			v.showing.Store(false)
		}
		return
	}
	if set.Mode == "off" || muted {
		v.stop(true)
		return
	}
	if playing {
		v.fade = 0
		v.analyze(set)
	} else {
		if !v.active {
			return
		}
		if v.fade++; v.fade > 12 {
			v.stop(true)
			return
		}
		for i := range v.vals {
			v.vals[i] *= 0.7
		}
	}
	v.phase += 0.004
	renderRing(&fr, v.vals[:], set, v.phase)
	if err := v.ring.Write(&fr); err != nil {
		return
	}
	v.active = true
	v.showing.Store(playing)
}

// stop beendet die Anzeige; blank = Ring ausschalten (nicht, wenn gerade der Hersteller anzeigt).
func (v *visualizer) stop(blank bool) {
	if v.active && (blank || !v.held()) {
		v.ring.Write(&ringFrame{})
	}
	v.active = false
	v.showing.Store(false)
	v.fade = 0
	v.vals = [vizLEDs]float64{}
}

// analyze berechnet aus den letzten Abtastwerten die Werte der 12 LEDs (0 ... 1, geglättet).
func (v *visualizer) analyze(set VizSettings) {
	buf := make([]float32, vizFFT)
	rate := v.tap.last(buf)
	var next [vizLEDs]float64
	switch set.Mode {
	case "level":
		n := rate / vizFPS
		rms := rmsOf(buf[len(buf)-n:])
		l := v.agc(20*math.Log10(rms+1e-9), 30)
		if rms < 2e-4 {
			l = 0
		}
		for i := range next {
			next[i] = l
		}
	default:
		bands := spectrumBands(buf, rate)
		top := -200.0
		for _, b := range bands {
			top = math.Max(top, b)
		}
		v.agcTrack(top)
		silent := rmsOf(buf) < 2e-4
		for i, b := range bands {
			if !silent {
				next[i] = clamp01((b - (v.refDB - 36)) / 36)
			}
		}
		if set.Mode == "pulse" {
			bass := math.Max(math.Max(next[0], next[1]), math.Max(next[2], next[3]))
			for i := range next {
				next[i] = bass
			}
		}
	}
	decay := 0.8
	if set.Mode == "pulse" {
		decay = 0.85
	}
	for i := range v.vals {
		v.vals[i] = math.Max(next[i], v.vals[i]*decay)
	}
}

func (v *visualizer) agcTrack(db float64) {
	v.refDB = math.Max(math.Max(db, v.refDB-0.12), -60) // ~3 dB/s zurück, nie unter -60 dB
}

func (v *visualizer) agc(db, span float64) float64 {
	v.agcTrack(db)
	return clamp01((db - (v.refDB - span)) / span)
}

func rmsOf(x []float32) float64 {
	s := 0.0
	for _, f := range x {
		s += float64(f) * float64(f)
	}
	return math.Sqrt(s / float64(len(x)))
}

func clamp01(x float64) float64 { return math.Min(1, math.Max(0, x)) }

// spectrumBands: 12 Bänder von 50 Hz bis 14 kHz (logarithmisch), Leistung in dB.
func spectrumBands(x []float32, rate int) [vizLEDs]float64 {
	n := len(x)
	c := make([]complex128, n)
	for i, f := range x {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		c[i] = complex(float64(f)*w, 0)
	}
	fft(c)
	var out [vizLEDs]float64
	binHz := float64(rate) / float64(n)
	for b := 0; b < vizLEDs; b++ {
		lo := 50 * math.Pow(280, float64(b)/vizLEDs)
		hi := 50 * math.Pow(280, float64(b+1)/vizLEDs)
		i0, i1 := int(lo/binHz), int(hi/binHz)
		if i1 <= i0 {
			i1 = i0 + 1
		}
		p := 0.0
		for i := i0; i < i1 && i < n/2; i++ {
			p += real(c[i])*real(c[i]) + imag(c[i])*imag(c[i])
		}
		out[b] = 10*math.Log10(p/float64(n)+1e-12) + float64(b) // Höhen um 1 dB je Band anheben (Musik fällt nach oben ab)
	}
	return out
}

// fft: iterative Radix-2-FFT an Ort und Stelle (len(a) Zweierpotenz).
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for l := 2; l <= n; l <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(l)))
		for i := 0; i < n; i += l {
			wk := complex(1, 0)
			for k := 0; k < l/2; k++ {
				u, t := a[i+k], a[i+k+l/2]*wk
				a[i+k], a[i+k+l/2] = u+t, u-t
				wk *= w
			}
		}
	}
}

// ---- Szenen ----

// sunriseColor: Farbe des Lichtweckers bei Fortschritt p (0 ... 1): tiefrot -> orange -> warmweiß, immer heller.
func sunriseColor(p float64) [3]byte {
	p = clamp01(p)
	stops := [][3]float64{{60, 2, 0}, {255, 60, 0}, {255, 150, 40}, {255, 210, 150}}
	x := p * float64(len(stops)-1)
	i := int(x)
	if i >= len(stops)-1 {
		i = len(stops) - 2
	}
	f := x - float64(i)
	g := 0.03 + 0.97*p*p // Helligkeit wie ein Sonnenaufgang: lange dunkel, dann schnell
	var out [3]byte
	for k := 0; k < 3; k++ {
		out[k] = byte(math.Round((stops[i][k]*(1-f) + stops[i+1][k]*f) * g))
	}
	return out
}

// sunriseScene: vor einem Wecker mit Lichtwecker den Ring langsam aufhellen.
func (a *app) sunriseScene(now time.Time, fr *ringFrame) bool {
	p, ok := a.sch.SunriseProgress()
	if !ok {
		return false
	}
	c := sunriseColor(p)
	for i := range fr {
		fr[i] = c
	}
	return true
}

// timerScene: Restzeit des nächsten Timers als Füllstand (im Uhrzeigersinn ab der Start-LED), sanft gedimmt.
func (a *app) timerScene(now time.Time, fr *ringFrame) bool {
	set := a.st.Snapshot()
	if !set.Viz.TimerRing || len(set.Timers) == 0 {
		return false
	}
	t := set.Timers[0]
	for _, x := range set.Timers {
		if x.End.Before(t.End) {
			t = x
		}
	}
	rem := t.End.Sub(a.clock()).Seconds()
	if rem <= 0 || t.Total <= 0 {
		return false
	}
	frac := clamp01(rem / float64(t.Total))
	scale := float64(clampInt(set.Viz.Brightness, 5, 100)) / 100 * 0.5
	lit := frac * vizLEDs
	for i := 0; i < vizLEDs; i++ {
		x := clamp01(lit - float64(i))
		p := ((i+set.Viz.Rotate)%vizLEDs + vizLEDs) % vizLEDs
		col := [3]float64{.16, .7, 1}
		if frac < 0.1 {
			col = [3]float64{1, .45, .05} // letzte 10 %: orange
		}
		for k := 0; k < 3; k++ {
			fr[p][k] = byte(math.Round(255 * scale * x * col[k]))
		}
	}
	return true
}

// renderRing setzt die Werte (0 ... 1) als Farben auf die LEDs.
func renderRing(fr *ringFrame, vals []float64, set VizSettings, phase float64) {
	scale := float64(clampInt(set.Brightness, 5, 100)) / 100
	put := func(led int, rgb [3]float64, x float64) {
		if x < 0.03 {
			return
		}
		g := x * x // grobe Gamma-Korrektur: das Auge empfindet LED-Helligkeit nicht linear
		p := ((led+set.Rotate)%vizLEDs + vizLEDs) % vizLEDs
		for k := 0; k < 3; k++ {
			fr[p][k] = byte(math.Round(255 * scale * g * rgb[k]))
		}
	}
	col := func(i int) [3]float64 {
		if set.Color == "custom" {
			return [3]float64{float64(set.RGB[0]) / 255, float64(set.RGB[1]) / 255, float64(set.RGB[2]) / 255}
		}
		if c, ok := vizColors[set.Color]; ok && set.Color != "rainbow" {
			return c
		}
		return hue(float64(i)/vizLEDs*0.8 + phase)
	}
	switch set.Mode {
	case "static":
		for i := 0; i < vizLEDs; i++ {
			c := col(i)
			if set.Color == "rainbow" || set.Color == "" {
				c = hue(float64(i) / vizLEDs)
			}
			put(i, c, 1)
		}
	case "level": // symmetrisch von der Start-LED aus, wie ein Pegelmesser
		l := vals[0] * vizLEDs / 2
		for i := 0; i < vizLEDs/2; i++ {
			x := clamp01(l - float64(i))
			c := col(i)
			if set.Color == "rainbow" || set.Color == "" {
				c = hue(0.33 - 0.33*float64(i)/(vizLEDs/2-1)) // grün -> gelb -> rot
			}
			put(i, c, x)
			put(vizLEDs-1-i, c, x)
		}
	default:
		for i := 0; i < vizLEDs; i++ {
			put(i, col(i), vals[i])
		}
	}
}

// hue: Farbton 0 ... 1 -> RGB mit voller Sättigung.
func hue(h float64) [3]float64 {
	h = math.Mod(h, 1)
	if h < 0 {
		h++
	}
	f := func(n float64) float64 {
		k := math.Mod(n+h*6, 6)
		return 1 - math.Max(0, math.Min(math.Min(k, 4-k), 1))
	}
	return [3]float64{f(5), f(3), f(1)}
}

func clampInt(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// ---- Ringpuffer des Tonabgriffs ----

type vizTap struct {
	data []byte
	ino  uint64
}

// reopenTap öffnet den Puffer neu, wenn er fehlt oder ersetzt wurde (z. B. nach einem Neustart).
func reopenTap(t *vizTap) *vizTap {
	var st syscall.Stat_t
	if err := syscall.Stat(vizShmPath, &st); err != nil {
		if t != nil {
			t.close()
		}
		return nil
	}
	if t != nil && t.ino == st.Ino {
		return t
	}
	if t != nil {
		t.close()
	}
	f, err := os.Open(vizShmPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	size := 16 + vizShmN*4
	if st.Size < int64(size) {
		return nil
	}
	d, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil
	}
	nt := &vizTap{data: d, ino: st.Ino}
	if atomic.LoadUint32((*uint32)(unsafe.Pointer(&d[0]))) != vizMagic {
		nt.close()
		return nil
	}
	return nt
}

func (t *vizTap) close() { syscall.Munmap(t.data) }

func (t *vizTap) pos() uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&t.data[8]))) }

// last füllt dst mit den neuesten Werten und liefert die Abtastrate.
func (t *vizTap) last(dst []float32) int {
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&t.data[16])), vizShmN)
	end := t.pos()
	for i := range dst {
		dst[i] = samples[(end-uint32(len(dst))+uint32(i))&(vizShmN-1)]
	}
	rate := int(atomic.LoadUint32((*uint32)(unsafe.Pointer(&t.data[4]))))
	if rate < 8000 || rate > 192000 {
		rate = 48000
	}
	return rate
}

// ---- Ring über I2C ----

type ringI2C struct {
	f      *os.File
	errLog time.Time
}

const (
	i2cSlave      = 0x0703
	i2cSlaveForce = 0x0706
)

func (r *ringI2C) Write(fr *ringFrame) error {
	if r.f == nil {
		f, err := os.OpenFile(hw.RingDev, os.O_RDWR, 0)
		if err != nil {
			return r.fail(err)
		}
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), i2cSlave, uintptr(hw.RingAddr)); e != 0 {
			if _, _, e = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), i2cSlaveForce, uintptr(hw.RingAddr)); e != 0 {
				f.Close()
				return r.fail(e)
			}
		}
		r.f = f
	}
	var b [2 + 13*3]byte
	b[0], b[1] = 0x0e, 0x01
	for i := range fr {
		copy(b[2+i*3:], fr[i][:])
	}
	if _, err := r.f.Write(b[:]); err != nil {
		r.f.Close()
		r.f = nil
		return r.fail(err)
	}
	return nil
}

func (r *ringI2C) fail(err error) error {
	if time.Since(r.errLog) > time.Minute {
		r.errLog = time.Now()
		log.Printf("Leuchtring (I2C): %v", err)
	}
	return errors.Join(errors.New("Leuchtring"), err)
}

// holdLED hält den Visualizer an, solange eine Hersteller-Animation läuft.
type holdLED struct {
	ledAPI
	v *visualizer
}

func (l holdLED) Animate(name string, repeat bool) {
	if repeat {
		l.v.HoldOn()
	} else {
		l.v.Hold(4 * time.Second)
	}
	l.ledAPI.Animate(name, repeat)
}

func (l holdLED) Off() { l.v.HoldOff(); l.ledAPI.Off() }
