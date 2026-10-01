package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Lokaler Bus: Darüber sprechen die eigenen Programme (btagent, castrecv) mit leuchtfeuerd, unabhängig vom
// Zielgerät (bisher lief das über den WAMP-Router der Harman-Software, den es auf anderen Geräten nicht gibt).
// HTTP über den Unix-Socket $LEUCHTFEUER_RUN/leuchtfeuer-bus.sock (nur root), Client: src/lfbus.
//
//	GET  /events  Server-Sent Events (zuerst "volume" mit dem aktuellen Stand):
//	              volume {volume, muted}   Lautstärke geändert (Drehrad, Weboberfläche, andere Quelle)
//	              claim {source}           eine Quelle beginnt zu spielen: die anderen halten an (Quellen-Regel)
//	              bt-pairing {action}      open | close | toggle (Oberfläche, Home Assistant, Aktion)
//	              bt-control {action}      play | pause | stop | next | previous
//	              button {name, value}     Taste am Gerät (nur Geräte mit Tasten)
//	GET  /state   {volume, muted, known}
//	POST /volume  {volume} | {delta} | {muted}
//	POST /source  {name, state, title, artist, album}   Zustand einer Quelle ("Läuft gerade")
//	POST /ring    {animation, repeat}  Animation mit eigenem Namen (bt_open, bt_closed ...); ohne Ring ohne Wirkung

type busEvent struct {
	Name string
	Data any
}

type localBus struct {
	a       *app
	mu      sync.Mutex
	subs    map[int]chan busEvent
	nextSub int
}

func newLocalBus(a *app) *localBus { return &localBus{a: a, subs: map[int]chan busEvent{}} }

// Publish verteilt ein Ereignis an alle verbundenen Programme (langsame verlieren es, statt zu bremsen).
func (b *localBus) Publish(name string, data any) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.subs {
		select {
		case c <- busEvent{name, data}:
		default:
		}
	}
}

func (b *localBus) volumeState() map[string]any {
	v, m, known := b.a.vol.Get()
	return map[string]any{"volume": v, "muted": m, "known": known}
}

func (b *localBus) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", b.events)
	mux.HandleFunc("/state", func(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, b.volumeState()) })
	post := func(path string, f func(r *http.Request) error) {
		mux.HandleFunc(path, func(rw http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(rw, "POST nötig", http.StatusMethodNotAllowed)
				return
			}
			if err := f(r); err != nil {
				fail(rw, 400, err)
				return
			}
			writeJSON(rw, map[string]bool{"ok": true})
		})
	}
	post("/volume", func(r *http.Request) error {
		var v struct {
			Volume *int  `json:"volume"`
			Delta  *int  `json:"delta"`
			Muted  *bool `json:"muted"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		switch {
		case v.Volume != nil:
			return b.a.vol.SetVolume(*v.Volume)
		case v.Delta != nil:
			return b.a.vol.Adjust(*v.Delta)
		case v.Muted != nil:
			return b.a.vol.SetMute(*v.Muted)
		}
		return fmt.Errorf("volume, delta oder muted nötig")
	})
	post("/source", func(r *http.Request) error {
		var v struct{ Name, State, Title, Artist, Album string }
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Name == "" {
			return fmt.Errorf("name nötig")
		}
		b.a.src.Update(v.Name, v.State, map[string]string{"title": v.Title, "artist": v.Artist, "album": v.Album})
		return nil
	})
	post("/ring", func(r *http.Request) error {
		var v struct {
			Animation string
			Repeat    bool
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		b.a.led.Animate(v.Animation, v.Repeat)
		return nil
	})
	return mux
}

func (b *localBus) events(rw http.ResponseWriter, r *http.Request) {
	fl, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "kein Streaming", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-store")
	c := make(chan busEvent, 64)
	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	b.subs[id] = c
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.subs, id); b.mu.Unlock() }()
	send := func(e busEvent) bool {
		j, _ := json.Marshal(e.Data)
		if _, err := fmt.Fprintf(rw, "event: %s\ndata: %s\n\n", e.Name, j); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send(busEvent{"volume", b.volumeState()}) {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-c:
			if !send(e) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(rw, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// Serve lauscht auf dem Unix-Socket (nur root darf ihn öffnen).
func (b *localBus) Serve(path string) {
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("Bus %s: %v", path, err)
		return
	}
	os.Chmod(path, 0o600)
	srv := &http.Server{Handler: b.handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Bus auf %s", path)
	log.Print(srv.Serve(ln))
}
