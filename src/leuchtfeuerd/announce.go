package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Durchsagen: Home Assistant (TTS, Türklingel), die Weboberfläche oder eine Taste spielen eine Adresse oder einen
// Signalton über das eigene PCM "leuchtfeuer_announce" (an Quellen-Reglern, Klang und Visualizer vorbei). Währenddessen
// senkt leuchtfeuerd alle Quellen um Settings.Sources.DuckDB ab; danach geht die Musik normal weiter.
// Durchsagen laufen nacheinander, höchstens 5 Minuten.

type announceReq struct {
	URL    string `json:"url"`
	Tone   string `json:"tone"`   // chime | bell | beep (statt URL)
	Volume int    `json:"volume"` // % des Reglerbereichs für diese Durchsage, 0 = wie das Gerät
	Name   string `json:"name"`
}

type announcer struct {
	a  *app
	pl *player
	mu sync.Mutex // eine Durchsage nach der anderen
}

func newAnnouncer(a *app, sink string) *announcer {
	return &announcer{a: a, pl: newPlayer(map[string]string{"": sink})}
}

var announceTones = map[string][]note{"chime": toneChime, "bell": toneBell, "beep": toneBeep}

func (an *announcer) check(r announceReq) error {
	if r.URL == "" {
		if _, ok := announceTones[r.Tone]; !ok {
			return fmt.Errorf("Adresse (http/https) oder Ton (chime, bell, beep) nötig")
		}
		return nil
	}
	if !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
		return fmt.Errorf("Adresse muss mit http:// oder https:// beginnen")
	}
	return nil
}

// Play startet eine Durchsage im Hintergrund.
func (an *announcer) Play(r announceReq) error {
	if err := an.check(r); err != nil {
		return err
	}
	go an.run(r)
	return nil
}

func (an *announcer) run(r announceReq) {
	an.mu.Lock()
	defer an.mu.Unlock()
	a := an.a
	name := r.Name
	if name == "" {
		name = map[bool]string{true: r.URL, false: r.Tone}[r.URL != ""]
	}
	logf("Durchsage: %s", name)
	duck := a.st.Snapshot().Sources.DuckDB
	if a.mix != nil {
		a.mix.Duck(duck)
		a.mix.SetAnnounceLevel(r.Volume)
		time.Sleep(150 * time.Millisecond) // Absenkung wirkt, bevor die Durchsage beginnt
	}
	a.src.Update("announce", "playing", map[string]string{"title": name})
	if r.URL != "" {
		an.pl.PlayURL("announce", name, r.URL)
	} else {
		an.pl.PlayTone("announce", name, announceTones[r.Tone], false)
	}
	deadline := time.Now().Add(5 * time.Minute)
	time.Sleep(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if k, _, _, _ := an.pl.Info(); k == "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	an.pl.Stop()
	a.src.Update("announce", "idle", nil)
	if a.mix != nil {
		a.mix.SetAnnounceLevel(0)
		a.mix.Duck(0)
	}
}
