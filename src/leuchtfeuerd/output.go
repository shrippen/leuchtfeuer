package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Ausgabe: wohin die Tonkette am Ende spielt (asound-music.conf: leuchtfeuer_sink).
//
//	default      der Ausgang des Zielgeräts (Invoke: DSP, generic: ALSA_CARD)
//	card:<n>     eine andere Soundkarte der Hardware (dmix, 48 kHz)
//	bt:<MAC>     ein Bluetooth-Lautsprecher (bluez-alsa als A2DP-Quelle; btagent koppelt und verbindet)
//
// Die Wahl steht in den Einstellungen (output) und als Definition "pcm.!leuchtfeuer_sink" in $LEUCHTFEUER_DIR/output.conf,
// die asound-music.conf einbindet. ALSA liest sie beim Start eines Programms; nach dem Wechsel startet leuchtfeuerd
// deshalb die Audiodienste neu (der Hook startet sie sofort wieder, SIGUSR1) und hält die eigene Wiedergabe an.

type OutputSettings struct {
	ID   string `json:"id"`   // "" = default
	Name string `json:"name"` // Anzeigename des Lautsprechers (für bt:), falls btagent ihn gerade nicht meldet
}

type outputInfo struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // default | card | bluetooth
	Name      string `json:"name"`
	Available bool   `json:"available"` // Karte da / Lautsprecher verbunden
	Current   bool   `json:"current"`
}

// btDevice: ein Gerät, das btagent meldet (nur solche mit A2DP-Senke).
type btDevice struct {
	Addr      string `json:"addr"`
	Name      string `json:"name"`
	Paired    bool   `json:"paired"`
	Connected bool   `json:"connected"`
	RSSI      int    `json:"rssi,omitempty"` // nur während der Suche
}

type btReport struct {
	Devices  []btDevice `json:"devices"`
	Scanning bool       `json:"scanning"`
	Busy     string     `json:"busy,omitempty"`  // Adresse, mit der btagent gerade koppelt/verbindet
	Error    string     `json:"error,omitempty"` // letzter Fehler
}

type outputResp struct {
	Current   string       `json:"current"`
	Outputs   []outputInfo `json:"outputs"`
	Bluetooth struct {
		Available bool       `json:"available"` // btagent meldet sich
		Scanning  bool       `json:"scanning"`
		Busy      string     `json:"busy,omitempty"`
		Error     string     `json:"error,omitempty"`
		Devices   []btDevice `json:"devices"`
	} `json:"bluetooth"`
}

type outputMgr struct {
	a      *app
	mu     sync.Mutex
	bt     btReport
	btSeen time.Time
	// für Tests ersetzbar
	cardsFile string
	confFile  func() string
	restart   func() // Audiodienste neu starten
}

var macRe = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)

func newOutputMgr(a *app) *outputMgr {
	m := &outputMgr{a: a, cardsFile: "/proc/asound/cards"}
	m.confFile = func() string { return filepath.Join(dataDir, "output.conf") }
	m.restart = m.restartAudio
	return m
}

var cardRe = regexp.MustCompile(`^\s*(\d+)\s+\[([^\]]*?)\s*\]:\s*(.*)$`)

// cards liest die Soundkarten der Hardware.
func (m *outputMgr) cards() []outputInfo {
	b, err := os.ReadFile(m.cardsFile)
	if err != nil {
		return nil
	}
	var out []outputInfo
	for _, l := range strings.Split(string(b), "\n") {
		if g := cardRe.FindStringSubmatch(l); g != nil {
			name := strings.TrimSpace(g[3])
			if name == "" {
				name = g[2]
			}
			out = append(out, outputInfo{ID: "card:" + g[1], Kind: "card", Name: name, Available: true})
		}
	}
	return out
}

// Report nimmt den Zustand von btagent entgegen und liefert die gewünschte Verbindung zurück ("" = keine).
func (m *outputMgr) Report(r btReport) (want string) {
	m.mu.Lock()
	changed := fmt.Sprint(m.bt) != fmt.Sprint(r) || time.Since(m.btSeen) > time.Minute
	m.bt, m.btSeen = r, time.Now()
	m.mu.Unlock()
	if changed {
		m.a.emit("output", nil)
	}
	if addr, ok := strings.CutPrefix(m.a.st.Snapshot().Output.ID, "bt:"); ok {
		return addr
	}
	return ""
}

func (m *outputMgr) btAvailable() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.btSeen.IsZero() && time.Since(m.btSeen) < 15*time.Second
}

// List: alle wählbaren Ausgaben und der Zustand der Bluetooth-Suche.
func (m *outputMgr) List() outputResp {
	cur := m.a.st.Snapshot().Output
	var r outputResp
	r.Current = cur.ID
	if r.Current == "" {
		r.Current = "default"
	}
	r.Outputs = append(r.Outputs, outputInfo{ID: "default", Kind: "default", Name: "Standard", Available: true})
	r.Outputs = append(r.Outputs, m.cards()...)
	m.mu.Lock()
	bt := m.bt
	m.mu.Unlock()
	r.Bluetooth.Available = m.btAvailable()
	r.Bluetooth.Scanning, r.Bluetooth.Busy, r.Bluetooth.Error = bt.Scanning, bt.Busy, bt.Error
	r.Bluetooth.Devices = bt.Devices
	if r.Bluetooth.Devices == nil {
		r.Bluetooth.Devices = []btDevice{}
	}
	seen := map[string]bool{}
	for _, d := range bt.Devices {
		if !d.Paired && !d.Connected {
			continue // nur gekoppelte sind wählbar; neue erst koppeln
		}
		seen["bt:"+d.Addr] = true
		r.Outputs = append(r.Outputs, outputInfo{ID: "bt:" + d.Addr, Kind: "bluetooth", Name: d.Name, Available: d.Connected})
	}
	if strings.HasPrefix(cur.ID, "bt:") && !seen[cur.ID] { // gewählt, aber btagent meldet es nicht
		r.Outputs = append(r.Outputs, outputInfo{ID: cur.ID, Kind: "bluetooth", Name: cur.Name})
	}
	for i := range r.Outputs {
		r.Outputs[i].Current = r.Outputs[i].ID == r.Current
	}
	return r
}

// confFor: Inhalt von output.conf für eine Ausgabe.
func confFor(id string) string {
	const head = "# von leuchtfeuerd geschrieben (Weboberfläche > Einstellungen > Ausgabe), nicht von Hand ändern\n"
	switch {
	case id == "" || id == "default":
		return head
	case strings.HasPrefix(id, "card:"):
		n := strings.TrimPrefix(id, "card:")
		return head + fmt.Sprintf(`pcm.!leuchtfeuer_sink {
    type plug
    slave.pcm {
        type dmix
        ipc_key 4731
        ipc_perm 0660
        slave {
            pcm "hw:%s,0"
            rate 48000
            format S16_LE
            period_size 1024
            buffer_size 8192
        }
    }
}
`, n)
	case strings.HasPrefix(id, "bt:"):
		return head + fmt.Sprintf(`pcm.!leuchtfeuer_sink {
    type plug
    slave.pcm {
        type bluealsa
        device "%s"
        profile "a2dp"
    }
}
`, strings.ToUpper(strings.TrimPrefix(id, "bt:")))
	}
	return head
}

// Set wählt die Ausgabe: schreibt output.conf, merkt sie, startet Audiodienste neu.
func (m *outputMgr) Set(id string) error {
	id = strings.TrimSpace(id)
	name := ""
	switch {
	case id == "" || id == "default":
		id = ""
	case strings.HasPrefix(id, "card:"):
		ok := false
		for _, c := range m.cards() {
			ok = ok || c.ID == id
		}
		if !ok {
			return fmt.Errorf("Soundkarte nicht gefunden")
		}
	case strings.HasPrefix(id, "bt:"):
		addr := strings.ToUpper(strings.TrimPrefix(id, "bt:"))
		if !macRe.MatchString(addr) {
			return fmt.Errorf("ungültige Bluetooth-Adresse")
		}
		id = "bt:" + addr
		found := false
		m.mu.Lock()
		for _, d := range m.bt.Devices {
			if strings.EqualFold(d.Addr, addr) && (d.Paired || d.Connected) {
				found, name = true, d.Name
			}
		}
		m.mu.Unlock()
		if !found {
			return fmt.Errorf("Lautsprecher nicht gekoppelt (erst suchen und koppeln)")
		}
	default:
		return fmt.Errorf("unbekannte Ausgabe")
	}
	cur := m.a.st.Snapshot().Output
	if cur.ID == id {
		return nil
	}
	if err := writeFileAtomic(m.confFile(), []byte(confFor(id))); err != nil {
		return err
	}
	if err := m.a.st.Update(func(s *Settings) { s.Output = OutputSettings{ID: id, Name: name} }); err != nil {
		return err
	}
	if addr, ok := strings.CutPrefix(id, "bt:"); ok {
		m.a.bus.Publish("bt-sink", map[string]string{"action": "connect", "addr": addr})
	}
	m.a.pl.Stop() // laufende Wiedergabe von leuchtfeuerd selbst (Radio, Wecker) spielt sonst auf der alten Ausgabe zu Ende
	go m.restart()
	m.a.emit("output", nil)
	return nil
}

// Bluetooth: Befehle der Weboberfläche an btagent (scan | stop | connect | disconnect | forget).
func (m *outputMgr) Bluetooth(action, addr string) error {
	switch action {
	case "scan", "stop":
	case "connect", "disconnect", "forget":
		if !macRe.MatchString(addr) {
			return fmt.Errorf("ungültige Bluetooth-Adresse")
		}
	default:
		return fmt.Errorf("unbekannte Aktion")
	}
	if !m.btAvailable() {
		return fmt.Errorf("Bluetooth ist nicht verfügbar (Dienst Bluetooth-Agent aus oder nicht installiert)")
	}
	if action == "forget" && strings.EqualFold(m.a.st.Snapshot().Output.ID, "bt:"+addr) {
		m.Set("default")
	}
	m.a.bus.Publish("bt-sink", map[string]string{"action": action, "addr": strings.ToUpper(addr)})
	return nil
}

// Dienste, die ALSA nicht selbst benutzen, bleiben stehen.
var keepRunning = map[string]bool{"leuchtfeuerd": true, "bluetoothd": true, "btagent": true, "bluealsa": true, "dbus-daemon": true, "avahi-daemon": true}

// restartAudio beendet die laufenden Audiodienste; der Hook startet sie sofort neu (SIGUSR1) und zählt das nicht als Ausfall.
func (m *outputMgr) restartAudio() {
	n := 0
	for _, d := range serviceDefs() {
		if keepRunning[d.Process] || d.Group == "core" || !pidAlive(readPid(d.Name)) {
			continue
		}
		if restartService(d.Name) == nil {
			n++
		}
	}
	if b, err := os.ReadFile(filepath.Join(runDir, "leuchtfeuer-hook.pid")); err == nil {
		var pid int
		fmt.Sscan(string(b), &pid)
		if pid > 0 {
			syscall.Kill(pid, syscall.SIGUSR1)
		}
	}
	log.Printf("Ausgabe gewechselt: %d Audiodienste neu gestartet", n)
}

// ensureConf legt output.conf an (leer oder passend zur gespeicherten Wahl); asound-music.conf bindet sie ein.
func (m *outputMgr) ensureConf() {
	want := confFor(m.a.st.Snapshot().Output.ID)
	if b, err := os.ReadFile(m.confFile()); err != nil || string(b) != want {
		if err := writeFileAtomic(m.confFile(), []byte(want)); err != nil {
			log.Printf("output.conf: %v", err)
		}
	}
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// outputStatus: kurz für /api/status (Übersicht der Weboberfläche).
type outputStatus struct {
	ID   string `json:"id"` // default | card:<n> | bt:<MAC>
	Name string `json:"name"`
	OK   bool   `json:"ok"` // Karte da / Lautsprecher verbunden
}

func (m *outputMgr) Status() outputStatus {
	for _, o := range m.List().Outputs {
		if o.Current {
			return outputStatus{ID: o.ID, Name: o.Name, OK: o.Available}
		}
	}
	return outputStatus{ID: "default", Name: "Standard", OK: true}
}
