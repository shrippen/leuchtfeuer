package main

import (
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Lautstärke-Abgleich und Quellen-Regler (ersetzt volume-sync.sh, das amixer fünfmal je Sekunde aufrief).
//
// Das Drehrad setzt über audio-ui die ALSA-Regler "system"/"voice"; audio-ui dämpft dabei "music" bei jedem Rastschritt
// kurz auf 0. Die Musikdienste spielen deshalb über einen eigenen Softvol-Regler "Leuchtfeuer Music" (asound-music.conf),
// den audio-ui nicht kennt. leuchtfeuerd kopiert "system" dorthin, sobald audio-ui com.harman.volumeChanged meldet, und zur
// Sicherheit alle 5 s (falls jemand den Regler ohne audio-ui ändert).
//
// Jede Quelle spielt über ein eigenes PCM "leuchtfeuer_<quelle>" mit eigenem Softvol-Regler "Quelle <quelle>"
// (asound-music.conf). Darüber kann leuchtfeuerd eine Quelle stummschalten (Quellen-Regel: die neueste Quelle hat Vorrang)
// oder alle Quellen für eine Durchsage absenken (Ducking), ohne die Lautstärke des Geräts anzufassen.
// Weckton, Timer und Durchsage laufen an den Quellen-Reglern vorbei.
//
// Wechsel blenden über (rampSteps x rampStep, etwa eine halbe Sekunde): die verdrängte Quelle wird leiser, statt hart zu
// verstummen, und kommt danach ebenso weich zurück. Der Pegelausgleich je Quelle (Settings.Sources.Limits[].TrimDB)
// senkt laute Quellen dauerhaft ab, damit alle etwa gleich laut sind.

var sourceNames = []string{"spotify", "upnp", "cast", "airplay", "bluetooth", "sendspin", "tidal", "snapcast", "radio"}

type mixerCtl interface {
	Get(ctl string) (int, bool)
	Set(ctl string, v int) error
}

type amixer struct{ card string }

var levelRe = regexp.MustCompile(`(?:Front Left|Mono): (?:Playback )?(\d+)`)

func (m amixer) Get(ctl string) (int, bool) {
	out, err := exec.Command("amixer", "-c", m.card, "sget", ctl).Output()
	if err != nil {
		return 0, false
	}
	if s := levelRe.FindSubmatch(out); s != nil {
		v, _ := strconv.Atoi(string(s[1]))
		return v, true
	}
	return 0, false
}

func (m amixer) Set(ctl string, v int) error {
	return exec.Command("amixer", "-c", m.card, "-q", "sset", ctl, strconv.Itoa(v)).Run()
}

const softvolMax = 255

// softvolForDB: Softvol-Wert für eine Absenkung um db dB (Softvol-Standard: 256 Stufen von -51 dB bis 0 dB).
func softvolForDB(db int) int {
	if db <= 0 {
		return softvolMax
	}
	if db >= 51 {
		return 0
	}
	return softvolMax * (51 - db) / 51
}

type mixSync struct {
	m       mixerCtl
	mu      sync.Mutex
	kick    chan struct{}
	muted   map[string]bool
	duck    int            // dB, 0 = aus
	annPct  int            // Lautstärke der Durchsage in % des Reglerbereichs, 0 = wie "system"
	trim    map[string]int // dauerhafte Absenkung je Quelle in dB
	written map[string]int
	lastAll time.Time
	ready   map[string]bool
	step    time.Duration // Pause zwischen den Überblend-Schritten (0 = ohne Überblenden)
	mirror  string        // Regler der Hersteller-Software, der gespiegelt wird ("" = master gilt)
	master  int           // eigener Wert für "Leuchtfeuer Music" (-1 = noch keiner)
}

const (
	rampSteps = 8
	rampStep  = 60 * time.Millisecond
)

func newMixSync(m mixerCtl) *mixSync {
	return &mixSync{m: m, kick: make(chan struct{}, 1), muted: map[string]bool{}, trim: map[string]int{}, written: map[string]int{},
		ready: map[string]bool{}, step: rampStep, mirror: "system", master: -1}
}

// SetTrims setzt den Pegelausgleich (dB Absenkung je Quelle, 0 ... 20).
func (x *mixSync) SetTrims(t map[string]int) {
	x.mu.Lock()
	ch := len(t) != len(x.trim)
	nt := map[string]int{}
	for k, v := range t {
		if v = clamp(v, 0, 20); v > 0 {
			nt[k] = v
		}
		ch = ch || x.trim[k] != nt[k]
	}
	ch = ch || len(nt) != len(x.trim)
	x.trim = nt
	x.mu.Unlock()
	if ch {
		x.Kick()
	}
}

// Kick löst einen Abgleich aus (gebündelt).
func (x *mixSync) Kick() {
	select {
	case x.kick <- struct{}{}:
	default:
	}
}

func (x *mixSync) SetMuted(src string, m bool) {
	x.mu.Lock()
	ch := x.muted[src] != m
	x.muted[src] = m
	x.mu.Unlock()
	if ch {
		x.Kick()
	}
}

func (x *mixSync) Muted(src string) bool { x.mu.Lock(); defer x.mu.Unlock(); return x.muted[src] }

func (x *mixSync) MutedList() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []string
	for _, s := range sourceNames {
		if x.muted[s] {
			out = append(out, s)
		}
	}
	return out
}

// Duck senkt alle Quellen um db dB ab (0 = wieder normal).
func (x *mixSync) Duck(db int) {
	x.mu.Lock()
	x.duck = db
	x.mu.Unlock()
	x.Kick()
}

// SetAnnounceLevel setzt die Lautstärke der nächsten Durchsage (0 = wie das Gerät).
func (x *mixSync) SetAnnounceLevel(pct int) {
	x.mu.Lock()
	x.annPct = clamp(pct, 0, 100)
	x.mu.Unlock()
	x.Kick()
}

// target liefert den gewünschten Wert des Quellen-Reglers.
func (x *mixSync) target(src string) int {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.muted[src] {
		return 0
	}
	return softvolForDB(x.duck + x.trim[src])
}

// ensure legt die Softvol-Regler an: ALSA erzeugt sie erst beim ersten Öffnen des PCM.
func (x *mixSync) ensure(ctl, pcm string) bool {
	if x.ready[ctl] {
		return true
	}
	if _, ok := x.m.Get(ctl); !ok {
		exec.Command("aplay", "-q", "-D", pcm, "-d", "1", "-f", "S16_LE", "-r", "48000", "-c", "2", "/dev/zero").Run()
		if _, ok = x.m.Get(ctl); !ok {
			return false
		}
	}
	x.ready[ctl] = true
	return true
}

// SetMaster: Gerät ohne Hersteller-Lautstärke (generic): leuchtfeuerd setzt "Leuchtfeuer Music" selbst (0 ... 255).
func (x *mixSync) SetMaster(v int) {
	x.mu.Lock()
	x.master = clamp(v, 0, softvolMax)
	x.mu.Unlock()
	x.Kick()
}

// reference: Wert für "Leuchtfeuer Music" - der gespiegelte Regler der Hersteller-Software oder der eigene.
func (x *mixSync) reference() (int, bool) {
	if x.mirror != "" {
		return x.m.Get(x.mirror)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.master, x.master >= 0
}

func (x *mixSync) sync(full bool) {
	if v, ok := x.reference(); ok && x.ensure("Leuchtfeuer Music", "leuchtfeuer_music") {
		if cur, ok := x.m.Get("Leuchtfeuer Music"); ok && cur != v {
			x.m.Set("Leuchtfeuer Music", v)
		}
		x.mu.Lock()
		av := v
		if x.annPct > 0 {
			av = softvolMax * x.annPct / 100
		}
		x.mu.Unlock()
		if x.ensure("Leuchtfeuer Announce", "leuchtfeuer_announce") && (full || x.written["Leuchtfeuer Announce"] != av) {
			x.m.Set("Leuchtfeuer Announce", av)
			x.written["Leuchtfeuer Announce"] = av
		}
	}
	type ramp struct {
		ctl      string
		from, to int
	}
	var ramps []ramp
	for _, s := range sourceNames {
		ctl := "Quelle " + s
		want := x.target(s)
		w, known := x.written[ctl]
		if !full && known && w == want {
			continue
		}
		if !x.ensure(ctl, "leuchtfeuer_"+s) {
			continue
		}
		if known && x.step > 0 && abs(want-w) > 16 {
			ramps = append(ramps, ramp{ctl, w, want})
			continue
		}
		if err := x.m.Set(ctl, want); err == nil {
			x.written[ctl] = want
		}
	}
	// Überblenden: alle betroffenen Regler gemeinsam in gleichen Schritten (linear in dB)
	for i := 1; i <= rampSteps && len(ramps) > 0; i++ {
		for _, r := range ramps {
			v := r.from + (r.to-r.from)*i/rampSteps
			if x.m.Set(r.ctl, v) == nil {
				x.written[r.ctl] = v
			}
		}
		if i < rampSteps {
			time.Sleep(x.step)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Run gleicht bei jedem Anstoß und alle 5 s ab; alle 60 s werden alle Quellen-Regler neu geschrieben.
func (x *mixSync) Run() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	x.sync(true)
	x.lastAll = time.Now()
	for {
		select {
		case <-x.kick:
			time.Sleep(30 * time.Millisecond) // Rastschritte bündeln
		case <-t.C:
		}
		full := time.Since(x.lastAll) > time.Minute
		if full {
			x.lastAll = time.Now()
		}
		x.sync(full)
	}
}

func logf(format string, args ...any) { log.Printf(format, args...) }
