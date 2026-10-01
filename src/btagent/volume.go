package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"leuchtfeuer/wamp"
)

// Lautstärke: Das Drehrad des Invoke und die Handy-Lautstärke (AVRCP-Absolutvolumen, bei bluez-alsa die
// Eigenschaft Volume des PCM, 0..127 je Kanal) bilden eine gemeinsame Lautstärke. Maßgeblich ist der
// Harman-Dienst audio-ui (Gruppe "music", 0..100 %): Er führt den Zustand, setzt die ALSA-Regler und
// die LEDs. Darum wird nicht der ALSA-Regler geschrieben (audio-ui kennt den Wert sonst nicht und macht
// beim nächsten Schritt am Rad vom alten Stand aus weiter), sondern über WAMP (Router bonefish,
// 127.0.0.1:9999) gerufen: com.harman.volumeAdjust([Differenz]). Das Rad meldet com.harman.volumeChanged.
// bluealsa läuft mit --a2dp-volume und dämpft nicht selbst. Das Gerät ist Master: verbindet sich ein
// Handy, bekommt es den Wert des Geräts.

func musicToPhone(n int) int { return (n*127 + 50) / 100 }
func phoneToMusic(p int) int { return (p*100 + 63) / 127 }

type volState struct {
	mu        sync.Mutex
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

// hub: Verbindung zum Router des Invoke (bonefish), verbindet selbst neu.
var hub = wamp.NewHub(wamp.Addr)

// setupWamp meldet die Ereignisse an; nach jeder Verbindung wird die aktuelle Lautstärke gelesen.
func setupWamp() {
	hub.Subscribe("com.harman.volumeChanged", vs.onVolume)
	hub.Subscribe("com.harman.test.inputEvent", func(args []any) {
		if len(args) >= 1 {
			if b, _ := args[0].(string); b == "bluetooth" {
				pw.toggle(*window)
			}
		}
	})
	// Befehle von invoked (Weboberfläche, Home Assistant)
	hub.Subscribe("invoke.bt.pairing", func(args []any) {
		if len(args) >= 1 {
			if a, _ := args[0].(string); a != "" {
				pw.Set(a, *window)
			}
		}
	})
	hub.Subscribe("invoke.bt.control", func(args []any) {
		if len(args) >= 1 {
			if a, _ := args[0].(string); a != "" {
				media.control(a)
			}
		}
	})
	// Eine andere Quelle beginnt zu spielen (invoked, Quellen-Regel "last"): Handy anhalten.
	hub.Subscribe("invoke.source.claim", func(args []any) {
		if len(args) >= 1 {
			if src, _ := args[0].(string); src != "" && src != "bluetooth" {
				media.control("pause")
			}
		}
	})
	hub.OnConnect(func() {
		// aktueller Wert: ein Null-Schritt mit volumeAdjust(0) liefert [Wert, "music"]
		res, err := hub.Call("com.harman.volumeAdjust", 0)
		if err != nil || len(res) < 1 {
			return
		}
		vs.mu.Lock()
		vs.cur = wamp.ToInt(res[0])
		vs.mu.Unlock()
		log.Printf("Lautstärke %d %%", wamp.ToInt(res[0]))
	})
	go hub.Run()
}

// onVolume: das Drehrad (oder invoked) hat die Lautstärke geändert.
func (s *volState) onVolume(args []any) {
	if len(args) < 2 {
		return
	}
	if g, _ := args[0].(string); g != "music" {
		return
	}
	n := wamp.ToInt(args[1])
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
	cur := s.cur
	s.mu.Unlock()
	target := phoneToMusic(p)
	if !hub.Connected() || cur < 0 || target == cur {
		return
	}
	res, err := hub.Call("com.harman.volumeAdjust", target-cur)
	if err != nil {
		log.Printf("volumeAdjust: %v", err)
		return
	}
	log.Printf("Handy %d/127 -> Gerät %d %% (volumeAdjust %+d, Ergebnis %v)", p, target, target-cur, res)
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
