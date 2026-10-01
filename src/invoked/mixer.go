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
// kurz auf 0. Die Musikdienste spielen deshalb über einen eigenen Softvol-Regler "Invoke Music" (asound-music.conf),
// den audio-ui nicht kennt. invoked kopiert "system" dorthin, sobald audio-ui com.harman.volumeChanged meldet, und zur
// Sicherheit alle 5 s (falls jemand den Regler ohne audio-ui ändert).
//
// Jede Quelle spielt über ein eigenes PCM "invoke_<quelle>" mit eigenem Softvol-Regler "Quelle <quelle>"
// (asound-music.conf). Darüber kann invoked eine Quelle stummschalten (Quellen-Regel: die neueste Quelle hat Vorrang)
// oder alle Quellen für eine Durchsage absenken (Ducking), ohne die Lautstärke des Geräts anzufassen.
// Weckton, Timer und Durchsage laufen an den Quellen-Reglern vorbei.

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
	duck    int // dB, 0 = aus
	annPct  int // Lautstärke der Durchsage in % des Reglerbereichs, 0 = wie "system"
	written map[string]int
	lastAll time.Time
	ready   map[string]bool
}

func newMixSync(m mixerCtl) *mixSync {
	return &mixSync{m: m, kick: make(chan struct{}, 1), muted: map[string]bool{}, written: map[string]int{}, ready: map[string]bool{}}
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
	return softvolForDB(x.duck)
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

func (x *mixSync) sync(full bool) {
	if v, ok := x.m.Get("system"); ok && x.ensure("Invoke Music", "invoke_music") {
		if cur, ok := x.m.Get("Invoke Music"); ok && cur != v {
			x.m.Set("Invoke Music", v)
		}
		x.mu.Lock()
		av := v
		if x.annPct > 0 {
			av = softvolMax * x.annPct / 100
		}
		x.mu.Unlock()
		if x.ensure("Invoke Announce", "invoke_announce") && (full || x.written["Invoke Announce"] != av) {
			x.m.Set("Invoke Announce", av)
			x.written["Invoke Announce"] = av
		}
	}
	for _, s := range sourceNames {
		ctl := "Quelle " + s
		want := x.target(s)
		if !full {
			if w, ok := x.written[ctl]; ok && w == want {
				continue
			}
		}
		if !x.ensure(ctl, "invoke_"+s) {
			continue
		}
		if err := x.m.Set(ctl, want); err == nil {
			x.written[ctl] = want
		}
	}
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
