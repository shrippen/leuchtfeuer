package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Einstellungen von invoked (Weboberfläche, Home Assistant, Wecker, Tasten ...), als JSON auf /data.
// Gemeinsame Schlüssel der Dienst-Skripte (Gerätename, Sendspin-Server, Bluetooth-Modus ...) stehen in der
// Shell-Konfiguration, siehe cfg.go.

type Preset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Alarm struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Time     string `json:"time"` // "07:30"
	Days     []int  `json:"days"` // 0 = Sonntag ... 6 = Samstag; leer = täglich
	Enabled  bool   `json:"enabled"`
	Source   string `json:"source"` // "tone" oder "radio:<Index>"
	Volume   int    `json:"volume"` // Ziel-Lautstärke in %, 0 = unverändert
	RampSecs int    `json:"rampSecs"`
	Snooze   int    `json:"snoozeMin"`
	MaxMins  int    `json:"maxMins"` // automatisch stoppen
}

type Timer struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	End   time.Time `json:"end"`
	Total int       `json:"total"` // Sekunden
}

type MQTTSettings struct {
	Enabled   bool   `json:"enabled"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Pass      string `json:"pass"`
	Discovery string `json:"discovery"` // Präfix der HA-Erkennung, Standard "homeassistant"
}

type WifiSettings struct {
	Enabled     bool `json:"enabled"`
	IntervalSec int  `json:"intervalSec"`
	LossPct     int  `json:"lossPct"`     // ab diesem Paketverlust gilt die Verbindung als schlecht
	RttMs       int  `json:"rttMs"`       // ab dieser mittleren Laufzeit
	Prefer5GHz  bool `json:"prefer5GHz"`  // 5-GHz-Kandidaten zuerst
	PenaltyMins int  `json:"penaltyMins"` // so lange meidet er einen als schlecht erkannten Access Point
	DryRun      bool `json:"dryRun"`      // nur protokollieren, nicht wechseln
}

type Settings struct {
	Timezone string                       `json:"timezone"`
	Radio    []Preset                     `json:"radio"`
	Alarms   []Alarm                      `json:"alarms"`
	Timers   []Timer                      `json:"timers"`
	Buttons  map[string]map[string]string `json:"buttons"` // Taste -> Druckart -> Aktion
	MQTT     MQTTSettings                 `json:"mqtt"`
	Wifi     WifiSettings                 `json:"wifi"`
}

func defaultSettings() Settings {
	return Settings{
		Timezone: "Europe/Berlin",
		Radio: []Preset{
			{"SomaFM Groove Salad", "http://ice1.somafm.com/groovesalad-128-mp3"},
			{"Radio Paradise", "http://stream.radioparadise.com/mp3-128"},
			{"Deutschlandfunk", "https://st01.sslstream.dlf.de/dlf/01/128/mp3/stream.mp3"},
		},
		Buttons: map[string]map[string]string{
			"mic": {"short": "mute_toggle"},
		},
		MQTT: MQTTSettings{Port: 1883, Discovery: "homeassistant"},
		Wifi: WifiSettings{Enabled: true, IntervalSec: 20, LossPct: 20, RttMs: 150, Prefer5GHz: false, PenaltyMins: 30},
	}
}

type store struct {
	mu   sync.Mutex
	path string
	S    Settings
}

func loadStore(path string) *store {
	st := &store{path: path, S: defaultSettings()}
	if b, err := os.ReadFile(path); err == nil {
		var s Settings
		if json.Unmarshal(b, &s) == nil {
			st.S = mergeDefaults(s)
		}
	}
	return st
}

// mergeDefaults füllt Lücken einer älteren Datei mit Standardwerten.
func mergeDefaults(s Settings) Settings {
	d := defaultSettings()
	if s.Timezone == "" {
		s.Timezone = d.Timezone
	}
	if s.Radio == nil {
		s.Radio = d.Radio
	}
	if s.Buttons == nil {
		s.Buttons = d.Buttons
	}
	if s.MQTT.Port == 0 {
		s.MQTT.Port = d.MQTT.Port
	}
	if s.MQTT.Discovery == "" {
		s.MQTT.Discovery = d.MQTT.Discovery
	}
	if s.Wifi.IntervalSec == 0 {
		s.Wifi = d.Wifi
	}
	return s
}

// Update wendet f unter Sperre an und speichert danach atomar.
func (st *store) Update(f func(s *Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	f(&st.S)
	b, err := json.MarshalIndent(st.S, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Snapshot liefert eine Kopie (JSON-Rundlauf, damit Slices/Maps nicht geteilt werden).
func (st *store) Snapshot() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	b, _ := json.Marshal(st.S)
	var s Settings
	json.Unmarshal(b, &s)
	return s
}
