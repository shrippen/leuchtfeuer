package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Quellen: wer gerade spielt, mit Titel, für "Läuft gerade", Home Assistant und die Quellen-Regel.
//
// Woher die Zustände kommen:
//   - Spotify (librespot --onevent) und AirPlay (shairport-sync sessioncontrol): `invoked -source-event ...` schickt
//     das Ereignis über /run/invoke-events.sock (events.go); AirPlay-Titel kommen aus der Metadaten-Pipe (airplay.go).
//   - Bluetooth (btagent, AVRCP) und Cast (castrecv): WAMP-Ereignis "invoke.source.state".
//   - Webradio: der eigene Player.
//   - UPnP, Sendspin, Tidal, Snapcast melden nichts: sie gelten als spielend, solange ihr Prozess das ALSA-Gerät offen
//     hat (Suche in /proc/*/fd nach /dev/snd/pcm*p, Zuordnung über die Prozess-Eltern zum Dienst).
//
// Quellen-Regel "last" (Standard): Beginnt eine Quelle zu spielen, haben die anderen Pause. Bluetooth und Cast halten
// selbst an ("invoke.source.claim"), das Webradio stoppt, die übrigen werden über ihren Quellen-Regler stummgeschaltet
// (sie lassen sich von außen nicht anhalten). Endet die neue Quelle, kommen die stummgeschalteten nach 5 s wieder.
// "mix": alle spielen gleichzeitig (altes Verhalten).
//
// Grenzen: höchste Lautstärke gesamt und je Quelle, Startlautstärke je Quelle (Settings.Sources).

type sourceInfo struct {
	Name   string    `json:"name"`
	State  string    `json:"state"` // playing | paused | idle
	Title  string    `json:"title"`
	Artist string    `json:"artist"`
	Album  string    `json:"album"`
	Since  time.Time `json:"since"`
	Muted  bool      `json:"muted"`
}

type SourceLimit struct {
	Max   int `json:"max"`   // höchste Lautstärke in %, 0 = keine Grenze
	Start int `json:"start"` // Lautstärke beim Start der Quelle, 0 = unverändert
}

type SourceSettings struct {
	Policy string                 `json:"policy"` // last | mix
	Max    int                    `json:"max"`    // höchste Lautstärke für alle Quellen, 0 = keine Grenze
	Limits map[string]SourceLimit `json:"limits"`
	DuckDB int                    `json:"duckDB"` // Absenkung der Quellen während einer Durchsage
}

// Quellen, die von außen nicht anhaltbar sind und nur über ihren PCM-Zugriff erkannt werden.
var pcmOnlySources = map[string]bool{"upnp": true, "sendspin": true, "tidal": true, "snapcast": true}

type sourceHub struct {
	a           *app
	mu          sync.Mutex
	s           map[string]*sourceInfo
	claimer     string
	releaseAt   time.Time
	spotifyConn time.Time
	procRoot    string
}

func newSourceHub(a *app) *sourceHub {
	return &sourceHub{a: a, s: map[string]*sourceInfo{}, procRoot: "/proc"}
}

func (h *sourceHub) settings() SourceSettings { return h.a.st.Snapshot().Sources }

// List liefert alle bekannten Quellen (spielende zuerst).
func (h *sourceHub) List() []sourceInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []sourceInfo
	for _, st := range []string{"playing", "paused"} {
		for _, n := range append(append([]string{}, sourceNames...), "alarm", "announce") {
			if s := h.s[n]; s != nil && s.State == st {
				c := *s
				c.Muted = h.a.mix != nil && h.a.mix.Muted(n)
				out = append(out, c)
			}
		}
	}
	if out == nil {
		out = []sourceInfo{}
	}
	return out
}

// Active liefert die vorrangige spielende Quelle ("" = keine).
func (h *sourceHub) Active() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.claimer != "" && h.s[h.claimer] != nil && h.s[h.claimer].State == "playing" {
		return h.claimer
	}
	var best string
	var since time.Time
	for n, s := range h.s {
		if s.State == "playing" && (best == "" || s.Since.After(since)) {
			best, since = n, s.Since
		}
	}
	return best
}

// Update setzt Zustand und (falls angegeben) Titel einer Quelle.
func (h *sourceHub) Update(name, state string, meta map[string]string) {
	if state == "" {
		state = "idle"
	}
	switch state {
	case "playing", "paused", "idle":
	case "buffering", "forward-seek", "reverse-seek":
		state = "playing"
	default:
		state = "idle"
	}
	h.mu.Lock()
	s := h.s[name]
	if s == nil {
		s = &sourceInfo{Name: name, State: "idle"}
		h.s[name] = s
	}
	prev := s.State
	s.State = state
	changed := prev != state
	for k, v := range meta {
		switch k {
		case "title":
			changed = changed || s.Title != v
			s.Title = v
		case "artist":
			changed = changed || s.Artist != v
			s.Artist = v
		case "album":
			s.Album = v
		}
	}
	if state == "idle" && meta == nil {
		s.Title, s.Artist, s.Album = "", "", ""
	}
	if prev != "playing" && state == "playing" {
		s.Since = h.a.clock()
	}
	h.mu.Unlock()
	if prev != "playing" && state == "playing" {
		h.onStart(name)
	} else if prev == "playing" && state != "playing" {
		h.onStop(name)
	}
	if changed {
		h.a.emit("source", map[string]any{"name": name, "state": state})
	}
}

func (h *sourceHub) onStart(name string) {
	set := h.settings()
	if l, ok := set.Limits[name]; ok && l.Start > 0 {
		go h.a.vol.SetVolume(l.Start)
	}
	if h.a.mix != nil {
		h.a.mix.SetMuted(name, false)
	}
	if set.Policy != "mix" && name != "announce" {
		h.mu.Lock()
		h.claimer, h.releaseAt = name, time.Time{}
		var others []string
		for n, s := range h.s {
			if n != name && s.State == "playing" {
				others = append(others, n)
			}
		}
		h.mu.Unlock()
		h.a.h.Publish("invoke.source.claim", name) // Bluetooth und Cast halten selbst an
		for _, o := range others {
			h.yield(o)
		}
	}
	go h.EnforceMax()
}

// yield: Quelle o tritt zurück, weil eine andere begonnen hat.
func (h *sourceHub) yield(o string) {
	switch o {
	case "radio":
		h.a.RadioStop()
	case "bluetooth", "cast", "alarm", "announce":
		// halten selbst an bzw. haben Vorrang
	default:
		if h.a.mix != nil {
			logf("Quelle %s stumm (andere Quelle hat Vorrang)", o)
			h.a.mix.SetMuted(o, true)
		}
	}
}

func (h *sourceHub) onStop(name string) {
	h.mu.Lock()
	if name == h.claimer {
		h.releaseAt = h.a.clock().Add(5 * time.Second)
	}
	h.mu.Unlock()
}

// tick: nach Ende der vorrangigen Quelle die stummgeschalteten Quellen wieder freigeben.
func (h *sourceHub) tick() {
	h.mu.Lock()
	rel := !h.releaseAt.IsZero() && h.a.clock().After(h.releaseAt)
	if rel {
		if c := h.s[h.claimer]; c != nil && c.State == "playing" {
			rel = false
		}
		h.releaseAt = time.Time{}
		if rel {
			h.claimer = ""
		}
	}
	h.mu.Unlock()
	if rel && h.a.mix != nil {
		for _, n := range h.a.mix.MutedList() {
			logf("Quelle %s wieder frei", n)
			h.a.mix.SetMuted(n, false)
		}
	}
}

// StopAll hält alles an (Schlummertimer): Webradio stoppt, Bluetooth und Cast pausieren, der Rest wird stumm, bis er
// das nächste Mal zu spielen beginnt.
func (h *sourceHub) StopAll() {
	h.a.RadioStop()
	h.a.h.Publish("invoke.source.claim", "sleep")
	h.mu.Lock()
	var playing []string
	for n, s := range h.s {
		if s.State == "playing" {
			playing = append(playing, n)
		}
	}
	h.claimer, h.releaseAt = "", time.Time{}
	h.mu.Unlock()
	for _, n := range playing {
		if n != "radio" && n != "bluetooth" && n != "cast" && h.a.mix != nil {
			h.a.mix.SetMuted(n, true)
		}
	}
}

// EnforceMax senkt die Lautstärke auf die Grenze der spielenden Quelle bzw. die Gesamtgrenze.
func (h *sourceHub) EnforceMax() {
	set := h.settings()
	max := set.Max
	if l, ok := set.Limits[h.Active()]; ok && l.Max > 0 && (max == 0 || l.Max < max) {
		max = l.Max
	}
	if max <= 0 || max >= 100 {
		return
	}
	if v, _, known := h.a.vol.Get(); known && v > max {
		logf("Lautstärke %d %% über der Grenze, auf %d %%", v, max)
		h.a.vol.SetVolume(max)
	}
}

// ---- Ereignisse der Dienste ----

// spotifyEvent verarbeitet ein librespot-Ereignis (Umgebung von --onevent).
func (h *sourceHub) spotifyEvent(env map[string]string) {
	switch env["PLAYER_EVENT"] {
	case "session_connected":
		h.mu.Lock()
		h.spotifyConn = h.a.clock()
		h.mu.Unlock()
	case "session_disconnected", "stopped":
		h.Update("spotify", "idle", nil)
	case "playing":
		h.Update("spotify", "playing", map[string]string{})
	case "paused":
		h.Update("spotify", "paused", map[string]string{})
	case "track_changed":
		h.mu.Lock()
		st := "idle"
		if s := h.s["spotify"]; s != nil {
			st = s.State
		}
		h.mu.Unlock()
		var artists []string
		for _, x := range strings.Split(env["ARTISTS"], "\n") {
			if x = strings.TrimSpace(x); x != "" {
				artists = append(artists, x)
			}
		}
		h.Update("spotify", st, map[string]string{"title": env["NAME"], "artist": strings.Join(artists, ", "), "album": env["ALBUM"]})
	case "volume_changed":
		v, err := strconv.Atoi(env["VOLUME"])
		h.mu.Lock()
		fresh := h.spotifyConn.IsZero() || h.a.clock().Sub(h.spotifyConn) < 3*time.Second
		h.mu.Unlock()
		if err == nil && !fresh { // die gespeicherte Lautstärke beim Verbinden übernimmt das Gerät nicht
			h.a.vol.SetVolume((v*100 + 32767) / 65535)
		}
	}
}

// airplayEvent verarbeitet shairport-sync-Ereignisse: "playing", "idle", "volume <dB>[,...]".
func (h *sourceHub) airplayEvent(args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "playing":
		h.Update("airplay", "playing", map[string]string{})
	case "idle":
		h.Update("airplay", "idle", nil)
	case "volume":
		if len(args) < 2 {
			return
		}
		db, err := strconv.ParseFloat(strings.Split(args[1], ",")[0], 64)
		if err != nil {
			return
		}
		h.a.vol.SetVolume(airplayPercent(db))
	}
}

// airplayPercent: AirPlay-Lautstärke (-30 ... 0 dB, -144 = stumm) in % (linear in dB wie der Regler am iPhone).
func airplayPercent(db float64) int {
	if db <= -30 {
		return 0
	}
	if db >= 0 {
		return 100
	}
	return int((db+30)/30*100 + 0.5)
}

// ---- Erkennung über das ALSA-Gerät (UPnP, Sendspin, Tidal, Snapcast) ----

// pcmGroups liefert die Dienstgruppen, deren Prozesse gerade ein Wiedergabegerät offen haben.
func (h *sourceHub) pcmGroups() map[string]bool {
	svcPid := map[int]string{} // PID des Dienstskripts -> Gruppe
	for _, d := range serviceDefs() {
		if p := readPid(d.Name); p > 0 {
			svcPid[p] = d.Group
		}
	}
	open := map[string]bool{}
	ents, _ := os.ReadDir(h.procRoot)
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(h.procRoot, e.Name(), "fd"))
		if err != nil {
			continue
		}
		has := false
		for _, fd := range fds {
			t, err := os.Readlink(filepath.Join(h.procRoot, e.Name(), "fd", fd.Name()))
			if err == nil && strings.HasPrefix(t, "/dev/snd/pcm") && strings.HasSuffix(t, "p") {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		// über die Eltern zum Dienstskript (die Dienste enden mit exec: meist ist es der Prozess selbst)
		for p, i := pid, 0; p > 1 && i < 8; i++ {
			if g, ok := svcPid[p]; ok {
				open[g] = true
				break
			}
			p = h.ppid(p)
		}
	}
	return open
}

func (h *sourceHub) ppid(pid int) int {
	b, err := os.ReadFile(filepath.Join(h.procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0
	}
	p, _ := strconv.Atoi(f[1])
	return p
}

// Run prüft alle 2 s die Geräte-Zugriffe und gibt Quellen nach Ende der vorrangigen frei.
func (h *sourceHub) Run() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for range t.C {
		open := h.pcmGroups()
		for g := range pcmOnlySources {
			h.mu.Lock()
			cur := "idle"
			if s := h.s[g]; s != nil {
				cur = s.State
			}
			h.mu.Unlock()
			want := map[bool]string{true: "playing", false: "idle"}[open[g]]
			if cur != want {
				h.Update(g, want, nil)
			}
		}
		h.tick()
	}
}
