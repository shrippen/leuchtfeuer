package main

import (
	"log"
	"sync"

	"leuchtfeuer/wamp"
)

// Anbindung an den Router des Invoke (audio-ui, invoked):
//   - Lautstärke und Stumm laufen über audio-ui (com.harman.volumeAdjust / musicMuteSet) statt direkt über den
//     ALSA-Regler. So bleiben Drehrad, LEDs, Bluetooth und Weboberfläche gleich, und eine Drehung am Rad
//     erscheint sofort im Lautstärkeregler des Senders (RECEIVER_STATUS).
//   - Zustand und Titel gehen als "invoke.source.state" ["cast", {...}] an invoked ("Läuft gerade", Home Assistant).
//   - "invoke.source.claim" [Quelle]: eine andere Quelle beginnt zu spielen, Cast hält an (Quellen-Regel "last").

type uiLink struct {
	h     *wamp.Hub
	mu    sync.Mutex
	vol   int
	mute  bool
	known bool
	last  string
}

var ui = &uiLink{h: wamp.NewHub(wamp.Addr)}

func (u *uiLink) start() {
	u.h.Subscribe("com.harman.volumeChanged", func(a []any) {
		if len(a) >= 2 {
			if g, _ := a[0].(string); g == "music" {
				u.mu.Lock()
				u.vol, u.known = wamp.ToInt(a[1]), true
				u.mu.Unlock()
				broadcastReceiver()
				srv.broadcast(transportID, nsMedia, mediaStatus(0))
			}
		}
	})
	u.h.Subscribe("com.harman.musicMuteChanged", func(a []any) {
		if len(a) >= 1 {
			if m, ok := a[0].(bool); ok {
				u.mu.Lock()
				u.mute = m
				u.mu.Unlock()
				broadcastReceiver()
			}
		}
	})
	u.h.Subscribe("invoke.source.claim", func(a []any) {
		if len(a) >= 1 {
			if src, _ := a[0].(string); src != "" && src != "cast" {
				pl.pause()
			}
		}
	})
	u.h.OnConnect(func() {
		_, kw, err := u.h.CallKw("com.harman.volumeGet", nil)
		if err != nil {
			return
		}
		if m := wamp.ToStrMap(kw["music"]); m != nil {
			u.mu.Lock()
			u.vol, u.mute, u.known = wamp.ToInt(m["volume"]), wamp.ToInt(m["mute"]) != 0, true
			u.mu.Unlock()
		}
	})
	go u.h.Run()
}

func (u *uiLink) connected() bool { return u.h.Connected() }

func (u *uiLink) get() (int, bool, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.vol, u.mute, u.known && u.h.Connected()
}

// setVolume setzt die Lautstärke in % (audio-ui kennt nur Schritte: Differenz zum aktuellen Wert).
func (u *uiLink) setVolume(target int) {
	r, err := u.h.Call("com.harman.volumeAdjust", 0)
	if err != nil || len(r) < 1 {
		return
	}
	if d := target - wamp.ToInt(r[0]); d != 0 {
		if _, err := u.h.Call("com.harman.volumeAdjust", d); err != nil {
			log.Printf("volumeAdjust: %v", err)
		}
	}
}

func (u *uiLink) setMute(m bool) {
	if _, err := u.h.Call("com.harman.musicMuteSet", m); err != nil {
		log.Printf("musicMuteSet: %v", err)
	}
}

// reportState meldet Zustand und Titel an invoked (nur bei Änderung).
func (u *uiLink) reportState() {
	st := pl.report()
	key := st["state"].(string) + "|" + st["title"].(string) + "|" + st["artist"].(string)
	u.mu.Lock()
	same := key == u.last
	u.last = key
	u.mu.Unlock()
	if !same {
		u.h.Publish("invoke.source.state", "cast", st)
	}
}
