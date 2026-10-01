package main

import (
	"encoding/json"
	"log"
	"sync"

	"leuchtfeuer/lfbus"
)

// Anbindung an leuchtfeuerd über den lokalen Bus (src/lfbus), auf jedem Zielgerät gleich:
//   - Lautstärke und Stumm führt leuchtfeuerd (auf dem Invoke die Hersteller-Software mit Drehrad und LEDs).
//     Ereignis "volume" -> RECEIVER_STATUS an die Sender; Regler am Sender -> POST /volume.
//   - Zustand und Titel gehen als POST /source {name: "cast", ...} an leuchtfeuerd ("Läuft gerade", Home Assistant).
//   - Ereignis "claim": eine andere Quelle beginnt zu spielen, Cast hält an (Quellen-Regel "last").

type uiLink struct {
	c     *lfbus.Client
	mu    sync.Mutex
	vol   int
	mute  bool
	known bool
	last  string
}

var ui = &uiLink{c: lfbus.New(lfbus.DefaultPath())}

func (u *uiLink) start() {
	u.c.On("volume", func(b json.RawMessage) {
		var v lfbus.Volume
		if json.Unmarshal(b, &v) != nil || !v.Known {
			return
		}
		u.mu.Lock()
		changed := !u.known || u.vol != v.Volume || u.mute != v.Muted
		u.vol, u.mute, u.known = v.Volume, v.Muted, true
		u.mu.Unlock()
		if changed {
			broadcastReceiver()
			srv.broadcast(transportID, nsMedia, mediaStatus(0))
		}
	})
	u.c.On("claim", func(b json.RawMessage) {
		var v lfbus.Claim
		if json.Unmarshal(b, &v) == nil && v.Source != "" && v.Source != "cast" {
			pl.pause()
		}
	})
	go u.c.Run()
}

func (u *uiLink) connected() bool { return u.c.Connected() }

func (u *uiLink) get() (int, bool, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.vol, u.mute, u.known && u.c.Connected()
}

func (u *uiLink) setVolume(target int) {
	if err := u.c.Post("/volume", map[string]int{"volume": target}, nil); err != nil {
		log.Printf("Lautstärke: %v", err)
	}
}

func (u *uiLink) setMute(m bool) {
	if err := u.c.Post("/volume", map[string]bool{"muted": m}, nil); err != nil {
		log.Printf("Stumm: %v", err)
	}
}

// reportState meldet Zustand und Titel an leuchtfeuerd (nur bei Änderung).
func (u *uiLink) reportState() {
	st := pl.report()
	key := st["state"].(string) + "|" + st["title"].(string) + "|" + st["artist"].(string)
	u.mu.Lock()
	same := key == u.last
	u.last = key
	u.mu.Unlock()
	if same {
		return
	}
	body := map[string]any{"name": "cast"}
	for k, v := range st {
		body[k] = v
	}
	if err := u.c.Post("/source", body, nil); err != nil {
		log.Printf("Zustand melden: %v", err)
	}
}
