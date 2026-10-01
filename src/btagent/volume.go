package main

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"leuchtfeuer/lfbus"
)

// Lautstärke: Die Lautstärke des Geräts und die Handy-Lautstärke (AVRCP-Absolutvolumen, bei bluez-alsa die
// Eigenschaft Volume des PCM, 0..127 je Kanal) bilden eine gemeinsame Lautstärke. Maßgeblich ist leuchtfeuerd
// (0..100 %; auf dem Invoke führt sie die Hersteller-Software, Drehrad und LEDs inklusive): Änderungen kommen als
// Ereignis "volume" über den lokalen Bus, Änderungen am Handy gehen als POST /volume zurück.
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

// lf: lokaler Bus von leuchtfeuerd (src/lfbus); verbindet selbst neu.
var lf = lfbus.New(lfbus.DefaultPath())

// setupBus meldet die Ereignisse an: Lautstärke, Bluetooth-Taste, Befehle der Weboberfläche, Vorrang anderer Quellen.
func setupBus() {
	lf.On("volume", func(b json.RawMessage) {
		var v lfbus.Volume
		if json.Unmarshal(b, &v) == nil && v.Known {
			vs.onVolume(v.Volume)
		}
	})
	lf.On("button", func(b json.RawMessage) {
		var v lfbus.Button
		if json.Unmarshal(b, &v) == nil && v.Name == "bluetooth" {
			pw.toggle(*window)
		}
	})
	// Befehle von leuchtfeuerd (Weboberfläche, Home Assistant)
	lf.On("bt-pairing", func(b json.RawMessage) {
		var v lfbus.Action
		if json.Unmarshal(b, &v) == nil && v.Action != "" {
			pw.Set(v.Action, *window)
		}
	})
	lf.On("bt-control", func(b json.RawMessage) {
		var v lfbus.Action
		if json.Unmarshal(b, &v) == nil && v.Action != "" {
			media.control(v.Action)
		}
	})
	// Eine andere Quelle beginnt zu spielen (Quellen-Regel "last"): Handy anhalten.
	lf.On("claim", func(b json.RawMessage) {
		var v lfbus.Claim
		if json.Unmarshal(b, &v) == nil && v.Source != "" && v.Source != "bluetooth" {
			media.control("pause")
		}
	})
	go lf.Run()
}

// onVolume: das Gerät (Drehrad, Weboberfläche ...) hat die Lautstärke geändert.
func (s *volState) onVolume(n int) {
	s.mu.Lock()
	first := s.cur < 0
	s.cur = n
	pcm, last := s.pcm, s.lastPhone
	push := pcm != "" && (last < 0 || phoneToMusic(last) != n)
	if push {
		s.lastPhone = musicToPhone(n)
	}
	p := s.lastPhone
	s.mu.Unlock()
	if first {
		log.Printf("Lautstärke %d %%", n)
	}
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
	if !lf.Connected() || cur < 0 || target == cur {
		return
	}
	if err := lf.Post("/volume", map[string]int{"volume": target}, nil); err != nil {
		log.Printf("Lautstärke setzen: %v", err)
		return
	}
	log.Printf("Handy %d/127 -> Gerät %d %%", p, target)
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
