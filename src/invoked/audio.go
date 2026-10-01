package main

import (
	"bufio"
	"encoding/binary"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------------------------------------
// Lautstärke und Stumm über audio-ui (WAMP): audio-ui führt den Zustand, setzt die ALSA-Regler und die LEDs.

type volumeCtl struct {
	h     *hub
	mu    sync.Mutex
	vol   int
	muted bool
	known bool
	onChg func()
}

func newVolumeCtl(h *hub) *volumeCtl {
	v := &volumeCtl{h: h}
	h.Subscribe("com.harman.volumeChanged", func(a []any) {
		if len(a) >= 2 {
			if g, _ := a[0].(string); g == "music" {
				v.set(toInt(a[1]), nil)
			}
		}
	})
	h.Subscribe("com.harman.musicMuteChanged", func(a []any) {
		if len(a) >= 1 {
			if m, ok := a[0].(bool); ok {
				v.set(-1, &m)
			}
		}
	})
	h.OnConnect(func() { v.refresh() })
	return v
}

func (v *volumeCtl) set(vol int, mute *bool) {
	v.mu.Lock()
	if vol >= 0 {
		v.vol = vol
	}
	if mute != nil {
		v.muted = *mute
	}
	v.known = true
	cb := v.onChg
	v.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (v *volumeCtl) Get() (int, bool, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vol, v.muted, v.known
}

// refresh liest den aktuellen Zustand (volumeGet liefert ihn als Schlüssel-Wert-Struktur).
func (v *volumeCtl) refresh() {
	_, kw, err := v.h.CallKw("com.harman.volumeGet", nil)
	if err != nil || kw == nil {
		return
	}
	if m := toStrMap(kw["music"]); m != nil {
		mute := toInt(m["mute"]) != 0
		v.set(toInt(m["volume"]), &mute)
	}
}

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Adjust ändert die Lautstärke um delta Prozentpunkte (audio-ui begrenzt auf 0..100).
func (v *volumeCtl) Adjust(delta int) error {
	r, err := v.h.Call("com.harman.volumeAdjust", delta)
	if err == nil && len(r) >= 1 {
		v.set(toInt(r[0]), nil)
	}
	return err
}

// SetVolume setzt die Lautstärke absolut (Differenz zum aktuellen Wert).
func (v *volumeCtl) SetVolume(target int) error {
	target = clamp(target, 0, 100)
	r, err := v.h.Call("com.harman.volumeAdjust", 0)
	if err != nil || len(r) < 1 {
		return err
	}
	cur := toInt(r[0])
	if cur == target {
		return nil
	}
	return v.Adjust(target - cur)
}

func (v *volumeCtl) SetMute(m bool) error {
	_, err := v.h.Call("com.harman.musicMuteSet", m)
	if err == nil {
		v.set(-1, &m)
	}
	return err
}

func (v *volumeCtl) ToggleMute() error {
	r, err := v.h.Call("com.harman.musicMuteToggle")
	if err == nil && len(r) >= 1 {
		if m, ok := r[0].(bool); ok {
			v.set(-1, &m)
		}
	}
	return err
}

// ---------------------------------------------------------------------------------------------------------
// Wiedergabe: Webradio über GStreamer, Signaltöne selbst erzeugt über aplay. Immer auf ALSA "invoke_music".
//
// Webradio verbindet sich neu, wenn der Stream abbricht (Fehler oder Ende eines Live-Streams): Pause 2, 4, 8, 16, 30 s
// (Zustand "reconnecting"), nach 8 Fehlversuchen in Folge gibt der Player auf. Lief der Stream länger als eine Minute,
// beginnt die Zählung neu. Andere Arten (Weckton-Stream, Durchsage, Datei) spielen genau einmal; den Ersatz beim Wecker
// übernimmt der Wecker selbst (sched.go).

type player struct {
	mu       sync.Mutex
	sinks    map[string]string // Art (radio, alarm, ...) -> ALSA-Gerät; "" = alle übrigen
	cmd      *exec.Cmd
	sess     *playSession
	Kind     string // radio | alarm | timer | ""
	Name     string
	URL      string
	State    string // idle | buffering | playing | reconnecting
	Title    string
	onChange func()

	// austauschbar für Tests
	gst     func(url, sink string) *exec.Cmd
	backoff func(n int) time.Duration
	retries map[string]bool // Arten, die nach einem Abbruch neu verbinden
}

type playSession struct{ stop chan struct{} }

const maxReconnects = 8

func newPlayer(sinks map[string]string) *player {
	return &player{sinks: sinks, State: "idle", gst: gstCommand, backoff: reconnectDelay, retries: map[string]bool{"radio": true}}
}

func gstCommand(url, sink string) *exec.Cmd {
	return exec.Command("gst-launch-1.0", "-e", "-t", "uridecodebin", "uri="+url,
		"!", "audioconvert", "!", "audioresample", "!", "alsasink", "device="+sink)
}

// reconnectDelay: Pause vor dem n-ten Neuverbinden (1, 2, ...).
func reconnectDelay(n int) time.Duration {
	d := 2 * time.Second
	for i := 1; i < n && d < 30*time.Second; i++ {
		d *= 2
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// sink: Webradio spielt über seinen Quellen-Regler ("invoke_radio"), Weckton und Timer daran vorbei.
func (p *player) sink(kind string) string {
	if s, ok := p.sinks[kind]; ok {
		return s
	}
	return p.sinks[""]
}

func (p *player) Info() (kind, name, state, title string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Kind, p.Name, p.State, p.Title
}

func (p *player) changed() {
	if p.onChange != nil {
		p.onChange()
	}
}

func (p *player) stopLocked() {
	if p.sess != nil {
		close(p.sess.stop)
		p.sess = nil
	}
	if p.cmd != nil {
		pid := p.cmd.Process.Pid
		syscall.Kill(-pid, syscall.SIGTERM)
		p.cmd = nil
	}
	p.Kind, p.Name, p.URL, p.State, p.Title = "", "", "", "idle", ""
}

func (p *player) Stop() {
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
	p.changed()
}

// set ändert Zustand/Titel, falls die Sitzung noch die aktuelle ist.
func (p *player) set(s *playSession, f func()) bool {
	p.mu.Lock()
	ok := p.sess == s
	if ok {
		f()
	}
	p.mu.Unlock()
	if ok {
		p.changed()
	}
	return ok
}

// PlayURL spielt einen Stream (Webradio) oder eine Datei ab.
func (p *player) PlayURL(kind, name, url string) {
	p.mu.Lock()
	p.stopLocked()
	s := &playSession{stop: make(chan struct{})}
	p.sess = s
	p.Kind, p.Name, p.URL, p.State, p.Title = kind, name, url, "buffering", ""
	retry := p.retries[kind]
	p.mu.Unlock()
	p.changed()
	go p.runURL(s, kind, url, retry)
}

func (p *player) runURL(s *playSession, kind, url string, retry bool) {
	fails := 0
	for {
		t0 := time.Now()
		res := p.runGst(s, kind, url)
		if res == "stopped" {
			return
		}
		if time.Since(t0) > time.Minute {
			fails = 0
		}
		fails++
		// Datei oder einmalige Wiedergabe: Ende ist Ende. Webradio: Ende oder Fehler -> neu verbinden.
		if !retry || fails > maxReconnects {
			if retry {
				log.Printf("Webradio %s: aufgegeben nach %d Versuchen", url, fails-1)
			}
			p.set(s, func() {
				p.sess, p.cmd = nil, nil
				p.Kind, p.Name, p.URL, p.State, p.Title = "", "", "", "idle", ""
			})
			return
		}
		wait := p.backoff(fails)
		log.Printf("Webradio %s: %s, neuer Versuch in %v", url, res, wait)
		if !p.set(s, func() { p.State, p.cmd = "reconnecting", nil }) {
			return
		}
		select {
		case <-s.stop:
			return
		case <-time.After(wait):
		}
	}
}

// runGst spielt einmal ab: "eos" (Ende), "error" (Abbruch) oder "stopped" (Stop/anderes Stück).
func (p *player) runGst(s *playSession, kind, url string) string {
	p.mu.Lock()
	if p.sess != s {
		p.mu.Unlock()
		return "stopped"
	}
	cmd := p.gst(url, p.sink(kind))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		log.Printf("gst-launch: %v", err)
		return "error"
	}
	p.cmd = cmd
	p.mu.Unlock()
	res := ""
	sc := bufio.NewScanner(out)
	sc.Split(splitLines)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "Setting pipeline to PLAYING"):
			p.set(s, func() { p.State = "playing" })
		case strings.Contains(line, "title=(string)"):
			t := line[strings.Index(line, "title=(string)")+len("title=(string)"):]
			if i := strings.Index(t, ", "); i >= 0 {
				t = t[:i]
			}
			t = strings.Trim(strings.TrimSpace(t), `"\`)
			p.set(s, func() { p.Title = t })
		case strings.Contains(line, "Got EOS"):
			res = "eos"
		case strings.HasPrefix(line, "ERROR:"):
			res = "error"
		}
	}
	err := cmd.Wait()
	select {
	case <-s.stop:
		return "stopped"
	default:
	}
	if res == "" {
		res = map[bool]string{true: "error", false: "eos"}[err != nil]
	}
	return res
}

func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Signaltöne: Folge aus (Frequenz Hz, Dauer ms); Frequenz 0 = Pause.
type note struct{ Hz, Ms int }

var (
	toneAlarm = []note{{880, 180}, {0, 90}, {880, 180}, {0, 90}, {1175, 260}, {0, 900}}
	toneTimer = []note{{988, 140}, {0, 80}, {988, 140}, {0, 80}, {988, 140}, {0, 1000}}
	toneChime = []note{{784, 160}, {0, 40}, {1047, 300}, {0, 200}}
	toneBell  = []note{{659, 450}, {0, 60}, {523, 700}, {0, 300}} // Ding-Dong
	toneBeep  = []note{{1000, 150}, {0, 100}, {1000, 150}, {0, 200}}
)

func synth(n note) []byte {
	const rate = 48000
	samples := rate * n.Ms / 1000
	buf := make([]byte, samples*4)
	for i := 0; i < samples; i++ {
		var v float64
		if n.Hz > 0 {
			t := float64(i) / rate
			env := 1.0
			fade := float64(rate) * 0.008 // 8 ms Ein-/Ausblenden gegen Knacken
			if f := float64(i); f < fade {
				env = f / fade
			} else if f := float64(samples - i); f < fade {
				env = f / fade
			}
			v = 0.5 * env * (math.Sin(2*math.Pi*float64(n.Hz)*t) + 0.25*math.Sin(2*math.Pi*2*float64(n.Hz)*t)) * 0.8
		}
		s := int16(v * 32767)
		binary.LittleEndian.PutUint16(buf[i*4:], uint16(s))
		binary.LittleEndian.PutUint16(buf[i*4+2:], uint16(s))
	}
	return buf
}

// PlayTone spielt die Tonfolge ab; repeat wiederholt sie bis Stop.
func (p *player) PlayTone(kind, name string, seq []note, repeat bool) {
	p.mu.Lock()
	p.stopLocked()
	cmd := exec.Command("aplay", "-q", "-D", p.sink(kind), "-f", "S16_LE", "-r", "48000", "-c", "2", "-t", "raw")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil || cmd.Start() != nil {
		p.mu.Unlock()
		log.Printf("aplay startet nicht")
		return
	}
	sess := &playSession{stop: make(chan struct{})}
	stop := sess.stop
	p.cmd, p.sess, p.Kind, p.Name, p.State = cmd, sess, kind, name, "playing"
	p.mu.Unlock()
	p.changed()
	go func() {
		defer func() {
			in.Close()
			cmd.Wait()
			p.mu.Lock()
			if p.sess == sess {
				p.cmd, p.sess = nil, nil
				p.Kind, p.Name, p.State = "", "", "idle"
			}
			p.mu.Unlock()
			p.changed()
		}()
		for {
			for _, n := range seq {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := in.Write(synth(n)); err != nil {
					return
				}
			}
			if !repeat {
				return
			}
		}
	}()
}
