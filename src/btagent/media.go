package main

import (
	"log"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Wiedergabe am Handy über AVRCP (BlueZ org.bluez.MediaPlayer1): Titel, Interpret, Album und Zustand gehen als
// WAMP-Ereignis "invoke.source.state" ["bluetooth", {state, title, artist, album}] an invoked (Anzeige "Läuft gerade",
// Home Assistant, Quellen-Regel). Befehle kommen über "invoke.bt.control" ("pause" | "play" | "stop" | "next" | "previous").

type mediaState struct {
	mu     sync.Mutex
	player dbus.ObjectPath
	last   map[string]any
}

var media = &mediaState{}

// poll liest den Player des verbundenen Handys und meldet Änderungen.
func (m *mediaState) poll() {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := bus.Object(bluez, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return
	}
	var player dbus.ObjectPath
	state := map[string]any{"state": "idle", "title": "", "artist": "", "album": ""}
	for p, ifs := range objs {
		mp, ok := ifs["org.bluez.MediaPlayer1"]
		if !ok {
			continue
		}
		player = p
		if st, _ := mp["Status"].Value().(string); st != "" {
			state["state"] = st // playing | paused | stopped | forward-seek | ...
		}
		if tr, ok := mp["Track"].Value().(map[string]dbus.Variant); ok {
			for k, key := range map[string]string{"Title": "title", "Artist": "artist", "Album": "album"} {
				if v, _ := tr[k].Value().(string); v != "" {
					state[key] = v
				}
			}
		}
		if state["state"] == "playing" {
			break // mehrere Handys: das spielende zählt
		}
	}
	m.mu.Lock()
	m.player = player
	changed := !sameState(m.last, state)
	if changed {
		m.last = state
	}
	m.mu.Unlock()
	if changed {
		hub.Publish("invoke.source.state", "bluetooth", state)
	}
}

func sameState(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// control schickt einen AVRCP-Befehl an das Handy.
func (m *mediaState) control(action string) {
	method := map[string]string{"pause": "Pause", "play": "Play", "stop": "Stop", "next": "Next", "previous": "Previous"}[action]
	m.mu.Lock()
	p := m.player
	playing := m.last != nil && m.last["state"] == "playing"
	m.mu.Unlock()
	if method == "" || p == "" {
		return
	}
	if action == "pause" && !playing {
		return
	}
	if err := bus.Object(bluez, p).Call("org.bluez.MediaPlayer1."+method, 0).Err; err != nil {
		log.Printf("AVRCP %s: %v", method, err)
		return
	}
	log.Printf("AVRCP %s an %s", method, p)
}
