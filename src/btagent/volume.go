package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Lautstärke: Das Drehrad des Invoke und die Handy-Lautstärke (AVRCP-Absolutvolumen, bei bluez-alsa die
// Eigenschaft Volume des PCM, 0..127 je Kanal) bilden eine gemeinsame Lautstärke. Maßgeblich ist der
// Harman-Dienst audio-ui (Gruppe "music", 0..100 %): Er führt den Zustand, setzt die ALSA-Regler und
// die LEDs. Darum wird nicht der ALSA-Regler geschrieben (audio-ui kennt den Wert sonst nicht und macht
// beim nächsten Schritt am Rad vom alten Stand aus weiter), sondern über WAMP (Router bonefish,
// 127.0.0.1:9999) gerufen: com.harman.volumeAdjust([Differenz]). Das Rad meldet com.harman.volumeChanged.
// bluealsa läuft mit --a2dp-volume und dämpft nicht selbst. Das Gerät ist Master: verbindet sich ein
// Handy, bekommt es den Wert des Geräts.

const wampAddr = "127.0.0.1:9999"

func musicToPhone(n int) int { return (n*127 + 50) / 100 }
func phoneToMusic(p int) int { return (p*100 + 63) / 127 }

type volState struct {
	mu        sync.Mutex
	wamp      *wampClient
	cur       int // Lautstärke der Gruppe "music" in % (-1 = unbekannt)
	lastPhone int // zuletzt bekannter Wert am Handy (0..127, -1 = unbekannt)
	pcm       dbus.ObjectPath
}

var vs = &volState{cur: -1, lastPhone: -1}

// findPCM liefert den Pfad des A2DP-Empfangs-PCM (leer, wenn kein Handy verbunden ist).
func findPCM() dbus.ObjectPath {
	var pcms map[dbus.ObjectPath]map[string]dbus.Variant
	if err := bus.Object("org.bluealsa", "/org/bluealsa").Call("org.bluealsa.Manager1.GetPCMs", 0).Store(&pcms); err != nil {
		return ""
	}
	for p, props := range pcms {
		if t, _ := props["Transport"].Value().(string); t == "A2DP-sink" {
			return p
		}
	}
	return ""
}

func setPhone(pcm dbus.ObjectPath, p int) {
	packed := uint16(p)<<8 | uint16(p)
	err := bus.Object("org.bluealsa", pcm).Call("org.freedesktop.DBus.Properties.Set", 0,
		"org.bluealsa.PCM1", "Volume", dbus.MakeVariant(packed)).Err
	if err != nil {
		log.Printf("Handy-Lautstärke setzen: %v", err)
	}
}

// onWamp verarbeitet Ereignisse des Routers (topic "" = Verbindung verloren).
func (s *volState) onWamp(topic string, args []any) {
	if topic == "" {
		s.mu.Lock()
		s.wamp, s.cur = nil, -1
		s.mu.Unlock()
		return
	}
	if topic == "invoke.bt.pairing" {
		if len(args) >= 1 {
			if a, _ := args[0].(string); a != "" {
				pw.Set(a, *window)
			}
		}
		return
	}
	if topic == "com.harman.test.inputEvent" {
		if len(args) >= 1 {
			if b, _ := args[0].(string); b == "bluetooth" {
				pw.toggle(*window)
			}
		}
		return
	}
	if topic != "com.harman.volumeChanged" || len(args) < 2 {
		return
	}
	if g, _ := args[0].(string); g != "music" {
		return
	}
	n := toInt(args[1])
	s.mu.Lock()
	s.cur = n
	pcm, last := s.pcm, s.lastPhone
	push := pcm != "" && (last < 0 || phoneToMusic(last) != n)
	if push {
		s.lastPhone = musicToPhone(n)
	}
	p := s.lastPhone
	s.mu.Unlock()
	if push {
		log.Printf("Gerät %d %% -> Handy %d/127", n, p)
		setPhone(pcm, p)
	}
}

// onPhone verarbeitet eine Änderung der Handy-Lautstärke.
func (s *volState) onPhone(p int) {
	s.mu.Lock()
	if p == s.lastPhone {
		s.mu.Unlock()
		return // Echo unserer eigenen Änderung
	}
	s.lastPhone = p
	w, cur := s.wamp, s.cur
	s.mu.Unlock()
	target := phoneToMusic(p)
	if w == nil || cur < 0 || target == cur {
		return
	}
	res, err := w.call("com.harman.volumeAdjust", target-cur)
	if err != nil {
		log.Printf("volumeAdjust: %v", err)
		return
	}
	log.Printf("Handy %d/127 -> Gerät %d %% (volumeAdjust %+d, Ergebnis %v)", p, target, target-cur, res)
}

// ensureWamp hält die Verbindung zu bonefish und liest die aktuelle Lautstärke.
func (s *volState) ensureWamp() {
	s.mu.Lock()
	have := s.wamp != nil
	s.mu.Unlock()
	if have {
		return
	}
	w, err := wampConnect(wampAddr, s.onWamp)
	if err != nil {
		return
	}
	if err := w.subscribe("com.harman.volumeChanged"); err != nil {
		w.conn.Close()
		return
	}
	if err := w.subscribe("com.harman.test.inputEvent"); err != nil {
		w.conn.Close()
		return
	}
	if err := w.subscribe("invoke.bt.pairing"); err != nil { // Befehle von invoked (Weboberfläche, Home Assistant)
		w.conn.Close()
		return
	}
	// aktueller Wert: volumeGet liefert ihn als Schlüssel-Wert-Struktur, einfacher ist ein
	// Null-Schritt mit volumeAdjust(0): Ergebnis [Wert, "music"]
	res, err := w.call("com.harman.volumeAdjust", 0)
	if err != nil || len(res) < 1 {
		w.conn.Close()
		return
	}
	s.mu.Lock()
	s.wamp, s.cur = w, toInt(res[0])
	s.mu.Unlock()
	log.Printf("WAMP verbunden, Lautstärke %d %%", s.cur)
}

func volumeLoop() {
	sigs := make(chan *dbus.Signal, 32)
	bus.Signal(sigs)
	if err := bus.AddMatchSignal(dbus.WithMatchSender("org.bluealsa"),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged")); err != nil {
		log.Printf("Match: %v", err)
	}
	tick := time.NewTicker(time.Second)
	for {
		select {
		case sg := <-sigs:
			if len(sg.Body) < 2 || !strings.HasPrefix(string(sg.Path), "/org/bluealsa/") {
				continue
			}
			if iface, _ := sg.Body[0].(string); iface != "org.bluealsa.PCM1" {
				continue
			}
			props, _ := sg.Body[1].(map[string]dbus.Variant)
			if v, ok := props["Volume"]; ok {
				packed, _ := v.Value().(uint16)
				vs.onPhone(int(packed>>8) & 0x7f)
			}
		case <-tick.C:
			vs.ensureWamp()
			now := findPCM()
			vs.mu.Lock()
			changed := now != vs.pcm
			vs.pcm = now
			cur := vs.cur
			if changed {
				vs.lastPhone = -1
			}
			vs.mu.Unlock()
			if changed && now != "" && cur >= 0 {
				// neues Handy: es übernimmt die Lautstärke des Geräts
				p := musicToPhone(cur)
				vs.mu.Lock()
				vs.lastPhone = p
				vs.mu.Unlock()
				log.Printf("Handy verbunden (%s): Gerät %d %% -> Handy %d/127", now, cur, p)
				setPhone(now, p)
			}
		}
	}
}
