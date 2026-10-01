package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Auswahllisten für die Oberfläche: was auf dem Gerät oder in Home Assistant wirklich vorhanden ist, damit niemand
// interne Namen (ALSA-Geräte, Entitäten) kennen muss.

type audioInput struct {
	ID          string `json:"id"`             // ALSA-Gerät für arecord
	Name        string `json:"name,omitempty"` // Name der Karte (die Oberfläche setzt die Beschriftung zweisprachig zusammen)
	Card        int    `json:"card"`
	Device      int    `json:"device"`
	Recommended bool   `json:"recommended"` // leuchtfeuer_mic: Vorgabe der Tonkette
}

var pcmLine = regexp.MustCompile(`^(\d+)-(\d+): ([^:]*):.*\bcapture \d+`)
var cardLine = regexp.MustCompile(`^\s*(\d+) \[(\S+)\s*\]: \S+ - (.+)$`)

// audioInputs liest die Aufnahmegeräte aus /proc/asound (procDir). Zuerst steht leuchtfeuer_mic aus der Tonkette des
// Zielgeräts (auf dem Invoke der einzige Weg, den Codec richtig zu lesen), danach die Karten roh. Loopback-Karten
// (snd-aloop) sind keine Mikrofone und fehlen.
func audioInputs(procDir string) []audioInput {
	out := []audioInput{{ID: "leuchtfeuer_mic", Card: -1, Device: -1, Recommended: true}}
	if procDir == "" { // Demo: nur die Tonkette
		return out
	}
	cards := map[string][2]string{} // Nummer -> {ID, Name}
	if b, err := os.ReadFile(filepath.Join(procDir, "cards")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if m := cardLine.FindStringSubmatch(l); m != nil {
				cards[m[1]] = [2]string{m[2], strings.TrimSpace(m[3])}
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(procDir, "pcm"))
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		m := pcmLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		card, _ := strconv.Atoi(m[1])
		dev, _ := strconv.Atoi(m[2])
		c := cards[strconv.Itoa(card)]
		if c[0] == "Loopback" {
			continue
		}
		name := c[1]
		if name == "" {
			name = strings.TrimSpace(m[3])
		}
		out = append(out, audioInput{ID: fmt.Sprintf("plughw:%d,%d", card, dev), Name: name, Card: card, Device: dev})
	}
	return out
}

type haOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// haOptions fragt Home Assistant nach Sprachausgabe-Entitäten und Bereichen (für Briefing und Sprachassistent).
// Braucht Adresse und Token unter Home Assistant; Vorlagen-API (Administrator-Token).
func (a *app) haOptions() (map[string][]haOption, error) {
	set := a.st.Snapshot()
	if demoMode { // keine echten Server: nur was die Demodaten schon eingestellt haben
		return map[string][]haOption{"tts": {{ID: set.HA.TTSEngine, Name: set.HA.TTSEngine}},
			"areas": {{ID: set.Voice.Area, Name: set.Voice.Area}}}, nil
	}
	ha := set.HA
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	get := func(tpl string) ([]haOption, error) {
		s, err := a.brief.haTemplate(ctx, ha, tpl)
		if err != nil {
			return nil, err
		}
		var v []haOption
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("Home Assistant: unerwartete Antwort")
		}
		sort.Slice(v, func(i, j int) bool { return strings.ToLower(v[i].Name) < strings.ToLower(v[j].Name) })
		return v, nil
	}
	tts, err := get(`[{% for s in states.tts %}{"id": {{ s.entity_id | tojson }}, "name": {{ s.name | tojson }}}{{ "," if not loop.last }}{% endfor %}]`)
	if err != nil {
		return nil, err
	}
	areas, err := get(`[{% for a in areas() %}{"id": {{ a | tojson }}, "name": {{ area_name(a) | tojson }}}{{ "," if not loop.last }}{% endfor %}]`)
	if err != nil {
		return nil, err
	}
	return map[string][]haOption{"tts": tts, "areas": areas}, nil
}
