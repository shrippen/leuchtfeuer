package main

import (
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// wrapAuth schützt die Oberfläche mit Anmeldeseite und Sitzung.
var wrapAuth = func(w *webServer, h http.Handler) http.Handler { return w.auth(h) }

type webServer struct {
	app    *app
	login  *loginState
	tokens *tokenStore
}

func randomPassword() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(rw).Encode(v)
}

func fail(rw http.ResponseWriter, code int, err error) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(code)
	json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

// ---- Status ----

type statusResp struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Volume     int             `json:"volume"`
	Muted      bool            `json:"muted"`
	VolKnown   bool            `json:"volumeKnown"`
	WampOK     bool            `json:"wamp"` // Verbindung zur Hersteller-Software (wie device.linkOK)
	Device     deviceStatus    `json:"device"`
	Output     outputStatus    `json:"output"` // gewählte Ausgabe der Tonkette
	Player     map[string]any  `json:"player"`
	Alarm      alarmState      `json:"alarm"`
	NextAlarm  string          `json:"nextAlarm"`
	NextName   string          `json:"nextAlarmName"`
	Timers     []timerView     `json:"timers"`
	Wifi       wifiStatus      `json:"wifi"`
	Sys        sysStatus       `json:"sys"`
	Bluetooth  btState         `json:"bluetooth"`
	BTMode     string          `json:"btMode"`
	MQTT       bool            `json:"mqtt"`
	Buttons    []buttonEvent   `json:"buttons"`
	Now        string          `json:"now"`
	Timezone   string          `json:"timezone"`
	WebDefault bool            `json:"webDefaultPassword"`
	Demo       bool            `json:"demo"`
	Viz        map[string]bool `json:"viz"` // Leuchtring-Visualizer: Tonabgriff gefunden, zeigt gerade an
	Sources    []sourceInfo    `json:"sources"`
	Active     string          `json:"activeSource"`
	SleepSecs  int             `json:"sleepSecs"`
	Clock      clockStatus     `json:"clock"`
	Update     updateInfo      `json:"update"`
	Holiday    string          `json:"holiday"` // heute Feiertag (Name) in der eingestellten Region
	Voice      voiceStatus     `json:"voice"`
	Briefing   bool            `json:"briefing"` // Briefing läuft
}

type timerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	Total     int    `json:"total"`
}

func (w *webServer) status() statusResp {
	a := w.app
	vol, muted, known := a.vol.Get()
	kind, name, state, title := a.pl.Info()
	set := a.st.Snapshot()
	s := statusResp{
		Name: a.cfg.Get("DEVICE_NAME", hw.DefaultName), Version: a.version, Volume: vol, Muted: muted, VolKnown: known,
		WampOK: hw.linkOK == nil || hw.linkOK(a), Device: deviceStatus{ID: hw.ID, Model: hw.Model, Capabilities: hw.capabilities(), Link: hw.Link, LinkOK: hw.linkOK == nil || hw.linkOK(a)}, Player: map[string]any{"kind": kind, "name": name, "state": state, "title": title},
		Alarm: a.sch.State(), Wifi: a.wifiFn(), Sys: a.sysFn(), Bluetooth: a.btFn(),
		BTMode: a.cfg.Get("BLUETOOTH_PAIRING", "button"), MQTT: a.mqttOK(), Buttons: a.Buttons(),
		Now: a.sch.Now().Format(time.RFC3339), Timezone: set.Timezone, Demo: demoMode,
		Viz:     map[string]bool{"tap": demoMode, "active": false},
		Sources: a.src.List(), Active: a.src.Active(), SleepSecs: a.sch.SleepRemaining(), Clock: a.clkFn(),
		Update: a.updateStatus(), Holiday: holidayName(set.Holidays, a.sch.Now()), Output: a.out.Status(),
	}
	s.Sys.Services = a.svcFn()
	if a.voice != nil {
		s.Voice = a.voice.Status()
	} else {
		s.Voice = voiceStatus{State: "off"}
	}
	if a.brief != nil {
		s.Briefing = a.brief.Running()
	}
	if a.viz != nil {
		s.Viz = a.viz.Status()
	}
	if t, n := a.sch.NextAlarm(); !t.IsZero() {
		s.NextAlarm, s.NextName = t.Format(time.RFC3339), n
	}
	s.Timers = []timerView{} // nie null ausliefern: die Oberfläche geht von Listen aus
	for _, t := range set.Timers {
		rem := int(t.End.Sub(a.clock()).Seconds())
		if rem < 0 {
			rem = 0
		}
		s.Timers = append(s.Timers, timerView{t.ID, t.Name, rem, t.Total})
	}
	return s
}

// ---- Einstellungen ----

type deviceSettings struct {
	Name             string `json:"name"`
	SendspinServer   string `json:"sendspinServer"`
	DHCPHostname     string `json:"dhcpHostname"`
	BluetoothPairing string `json:"bluetoothPairing"`
	AirPlay          string `json:"airplay"` // on | off (nur lesend; geschaltet wird über die Dienstgruppe airplay)
	WebPassword      string `json:"webPassword,omitempty"`
}

func (w *webServer) device() deviceSettings {
	c := w.app.cfg
	return deviceSettings{
		Name: c.Get("DEVICE_NAME", hw.DefaultName), SendspinServer: c.Get("SENDSPIN_SERVER", ""),
		DHCPHostname: hostName(c), BluetoothPairing: c.Get("BLUETOOTH_PAIRING", "button"),
		AirPlay: c.Get("AIRPLAY", "on"),
	}
}

func (w *webServer) settings() map[string]any {
	set := w.app.st.Redacted() // Passwörter und Schlüssel nie ausliefern
	if set.Radio == nil {
		set.Radio = []Preset{}
	}
	if set.Alarms == nil {
		set.Alarms = []Alarm{}
	}
	if set.Timers == nil {
		set.Timers = []Timer{}
	}
	if set.Buttons == nil {
		set.Buttons = map[string]map[string]string{}
	}
	return map[string]any{
		"settings": set, "device": w.device(), "actions": actionNames, "holidayRegions": holidayRegions,
		"sourceNames": sourceNames, "version": currentVersion(), "podcasts": podcastPresets, "copySections": copySections,
		"services": serviceDefs(), "groups": serviceGroups(w.app.cfg), "buttonNames": hw.ButtonNames,
	}
}

func (w *webServer) putSettings(section string, r *http.Request) error {
	a := w.app
	switch section {
	case "radio":
		var v []Preset
		if err := decode(r, &v); err != nil {
			return err
		}
		for i := range v {
			v[i].Name, v[i].URL = strings.TrimSpace(v[i].Name), strings.TrimSpace(v[i].URL)
			if v[i].Name == "" || !(strings.HasPrefix(v[i].URL, "http://") || strings.HasPrefix(v[i].URL, "https://")) {
				return fmt.Errorf("Sender %d: Name und http(s)-Adresse nötig", i+1)
			}
		}
		return a.st.Update(func(s *Settings) { s.Radio = v })
	case "alarms":
		var v []Alarm
		if err := decode(r, &v); err != nil {
			return err
		}
		for i := range v {
			if v[i].ID == "" {
				v[i].ID = newID()
			}
			if _, err := time.Parse("15:04", v[i].Time); err != nil {
				return fmt.Errorf("Wecker %d: Uhrzeit HH:MM nötig", i+1)
			}
			v[i].Volume = clamp(v[i].Volume, 0, 100)
			v[i].RampSecs = clamp(v[i].RampSecs, 0, 1800)
			v[i].Snooze = clamp(v[i].Snooze, 0, 60)
			v[i].MaxMins = clamp(v[i].MaxMins, 0, 240)
			v[i].Sunrise = clamp(v[i].Sunrise, 0, 60)
			v[i].FadeOut = clamp(v[i].FadeOut, 0, 120)
			if v[i].SkipDate != "" {
				if _, err := time.Parse("2006-01-02", v[i].SkipDate); err != nil {
					v[i].SkipDate = ""
				}
			}
			if u, ok := strings.CutPrefix(v[i].Source, "url:"); ok && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				return fmt.Errorf("Wecker %d: Adresse muss mit http:// oder https:// beginnen", i+1)
			}
		}
		return a.st.Update(func(s *Settings) { s.Alarms = v })
	case "buttons":
		var v map[string]map[string]string
		if err := decode(r, &v); err != nil {
			return err
		}
		ok := map[string]bool{}
		for _, n := range actionNames {
			ok[n] = true
		}
		for b, m := range v {
			for p, act := range m {
				if !ok[act] {
					return fmt.Errorf("Taste %s/%s: unbekannte Aktion %q", b, p, act)
				}
			}
		}
		return a.st.Update(func(s *Settings) { s.Buttons = v })
	case "mqtt":
		var v MQTTSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.st.Update(func(s *Settings) {
			if v.Pass == "" { // leer = unverändert
				v.Pass = s.MQTT.Pass
			}
			if v.Port == 0 {
				v.Port = 1883
			}
			if v.Discovery == "" {
				v.Discovery = "homeassistant"
			}
			if v.TLS && v.Port == 1883 {
				v.Port = 8883
			}
			s.MQTT = v
		})
	case "wifi":
		var v WifiSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		v.IntervalSec = clamp(v.IntervalSec, 10, 600)
		v.LossPct = clamp(v.LossPct, 1, 100)
		v.RttMs = clamp(v.RttMs, 20, 5000)
		v.PenaltyMins = clamp(v.PenaltyMins, 1, 1440)
		return a.st.Update(func(s *Settings) { s.Wifi = v })
	case "viz":
		var v VizSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		if !vizModes[v.Mode] {
			return fmt.Errorf("unbekannte Anzeige %q", v.Mode)
		}
		if _, ok := vizColors[v.Color]; !ok {
			return fmt.Errorf("unbekannte Farbe %q", v.Color)
		}
		v.Brightness = clamp(v.Brightness, 5, 100)
		v.Rotate = clamp(v.Rotate, 0, vizLEDs-1)
		for k := range v.RGB {
			v.RGB[k] = clamp(v.RGB[k], 0, 255)
		}
		return a.st.Update(func(s *Settings) { s.Viz = v })
	case "sources":
		var v SourceSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Policy != "last" && v.Policy != "mix" {
			return fmt.Errorf("Quellen-Regel last oder mix")
		}
		v.Max = clamp(v.Max, 0, 100)
		v.DuckDB = clamp(v.DuckDB, 0, 40)
		lim := map[string]SourceLimit{}
		for n, l := range v.Limits {
			l.Max, l.Start, l.TrimDB = clamp(l.Max, 0, 100), clamp(l.Start, 0, 100), clamp(l.TrimDB, 0, 20)
			if l.Max > 0 || l.Start > 0 || l.TrimDB > 0 {
				lim[n] = l
			}
		}
		v.Limits = lim
		return a.st.Update(func(s *Settings) { s.Sources = v })
	case "eq":
		var v EqSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		v.Version, v.Bass, v.Treble = 1, clamp(v.Bass, -12, 12), clamp(v.Treble, -12, 12)
		v.Room = cleanPEQ(v.Room)
		return a.st.Update(func(s *Settings) { s.Eq = v })
	case "briefing":
		var v BriefingSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Lang != "en" {
			v.Lang = "de"
		}
		if v.Lat < -90 || v.Lat > 90 || v.Lon < -180 || v.Lon > 180 {
			return fmt.Errorf("Ort: Koordinaten ungültig")
		}
		if len(v.Items) > 20 {
			return fmt.Errorf("höchstens 20 Bausteine")
		}
		for i, it := range v.Items {
			if !briefTypes[it.Type] {
				return fmt.Errorf("Baustein %d: unbekannte Art %q", i+1, it.Type)
			}
			if (it.Type == "calendar" || it.Type == "podcast") && it.On && !(strings.HasPrefix(it.URL, "http://") || strings.HasPrefix(it.URL, "https://") || strings.HasPrefix(it.URL, "webcal://")) {
				return fmt.Errorf("Baustein %d (%s): Adresse nötig", i+1, it.Name)
			}
			v.Items[i].Days = clamp(it.Days, 0, 7)
		}
		if v.TTS != "" && v.TTS != "ha" && v.TTS != "url" {
			return fmt.Errorf("Sprachausgabe: ha, url oder leer")
		}
		if v.TTS == "url" && !strings.Contains(v.TTSURL, "{text}") {
			return fmt.Errorf("Sprachausgabe: Adresse braucht {text}")
		}
		if v.Then != "" && !strings.HasPrefix(v.Then, "radio:") {
			return fmt.Errorf("danach: leer oder radio:<Nummer>")
		}
		return a.st.Update(func(s *Settings) { s.Briefing = v })
	case "homeAssistant":
		var v HASettings
		if err := decode(r, &v); err != nil {
			return err
		}
		v.URL = strings.TrimRight(strings.TrimSpace(v.URL), "/")
		if v.URL != "" && !strings.HasPrefix(v.URL, "http://") && !strings.HasPrefix(v.URL, "https://") {
			return fmt.Errorf("Home Assistant: Adresse mit http:// oder https://")
		}
		return a.st.Update(func(s *Settings) {
			if v.Token == "" { // leer = unverändert
				v.Token = s.HA.Token
			}
			s.HA = v
		})
	case "voice":
		var v VoiceSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Port == 0 {
			v.Port = 10700
		}
		v.Port = clamp(v.Port, 1024, 65535)
		v.DuckDB = clamp(v.DuckDB, 0, 40)
		if v.Mode != "button" {
			v.Mode = "wake"
		}
		v.Mic = strings.TrimSpace(v.Mic)
		if strings.ContainsAny(v.Mic, " \t'\"$`;|&") {
			return fmt.Errorf("Mikrofon: ALSA-Gerätename wie leuchtfeuer_mic oder plughw:2,0")
		}
		if err := a.st.Update(func(s *Settings) { s.Voice = v }); err != nil {
			return err
		}
		if a.voice != nil {
			go a.voice.Apply()
		}
		return nil
	case "holidays":
		var v struct {
			Region string `json:"region"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		ok := v.Region == ""
		for _, h := range holidayRegions {
			ok = ok || h == v.Region
		}
		if !ok {
			return fmt.Errorf("unbekannte Feiertags-Region %q", v.Region)
		}
		return a.st.Update(func(s *Settings) { s.Holidays = v.Region })
	case "update":
		var v UpdateSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.URL != "" && !strings.HasPrefix(v.URL, "https://") {
			return fmt.Errorf("Release-Adresse muss mit https:// beginnen")
		}
		return a.st.Update(func(s *Settings) { s.Update = v })
	case "syslog":
		var v SyslogSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		v.Host = strings.TrimSpace(v.Host)
		if v.Enabled && v.Host == "" {
			return fmt.Errorf("Syslog: Adresse nötig")
		}
		if strings.ContainsAny(v.Host, " /:") && !strings.HasPrefix(v.Host, "[") {
			return fmt.Errorf("Syslog: nur Name oder IP-Adresse, Port extra")
		}
		if v.Port == 0 {
			v.Port = 514
		}
		v.Port = clamp(v.Port, 1, 65535)
		if v.Proto != "tcp" {
			v.Proto = "udp"
		}
		return a.st.Update(func(s *Settings) { s.Syslog = v })
	case "timezone":
		var v struct {
			Timezone string `json:"timezone"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		if _, err := time.LoadLocation(v.Timezone); err != nil {
			return fmt.Errorf("Zeitzone %q unbekannt", v.Timezone)
		}
		return a.st.Update(func(s *Settings) { s.Timezone = v.Timezone })
	case "device":
		var v deviceSettings
		if err := decode(r, &v); err != nil {
			return err
		}
		kv := map[string]string{}
		if v.Name != "" {
			kv["DEVICE_NAME"] = v.Name
		}
		kv["SENDSPIN_SERVER"] = v.SendspinServer
		if v.DHCPHostname != "" {
			kv["DHCP_HOSTNAME"] = v.DHCPHostname
		}
		if v.BluetoothPairing == "button" || v.BluetoothPairing == "always" {
			kv["BLUETOOTH_PAIRING"] = v.BluetoothPairing
		}
		if v.WebPassword != "" {
			if err := checkNewPassword(v.WebPassword); err != nil {
				return err
			}
			h, err := storePassword(a.cfg, v.WebPassword)
			if err != nil {
				return err
			}
			w.login.setHash(h)
		}
		return a.cfg.Set(kv)
	}
	return fmt.Errorf("unbekannter Bereich %q", section)
}

func (w *webServer) routes() http.Handler {
	a := w.app
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/status", func(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, w.status()) })
	mux.HandleFunc("/api/settings", func(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, w.settings()) })
	mux.HandleFunc("/api/settings/", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(rw, "PUT nötig", http.StatusMethodNotAllowed)
			return
		}
		if err := w.putSettings(strings.TrimPrefix(r.URL.Path, "/api/settings/"), r); err != nil {
			fail(rw, 400, err)
			return
		}
		a.emit("settings", nil)
		writeJSON(rw, map[string]bool{"ok": true})
	})
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
	post("/api/volume", func(r *http.Request) error {
		var v struct {
			Volume *int `json:"volume"`
			Delta  *int `json:"delta"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Volume != nil {
			return a.vol.SetVolume(*v.Volume)
		}
		if v.Delta != nil {
			return a.vol.Adjust(*v.Delta)
		}
		return fmt.Errorf("volume oder delta nötig")
	})
	post("/api/mute", func(r *http.Request) error {
		var v struct {
			Muted bool `json:"muted"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.vol.SetMute(v.Muted)
	})
	post("/api/radio/play", func(r *http.Request) error {
		var v struct {
			Index int `json:"index"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.RadioPlay(v.Index)
	})
	post("/api/radio/url", func(r *http.Request) error { // beliebigen Stream als Webradio (Home Assistant, Sendersuche)
		var v struct{ Name, URL, UUID string }
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.PlayStream(v.Name, v.URL, v.UUID)
	})
	post("/api/radio/stop", func(r *http.Request) error { // stoppt auch ein laufendes Briefing
		if a.brief != nil {
			a.brief.Stop()
		}
		a.pl.Stop()
		return nil
	})
	post("/api/alarm/stop", func(r *http.Request) error { a.sch.StopAlarm(); return nil })
	post("/api/alarm/snooze", func(r *http.Request) error { a.sch.Snooze(); return nil })
	post("/api/timers", func(r *http.Request) error {
		var v struct {
			Name    string `json:"name"`
			Seconds int    `json:"seconds"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Seconds < 1 || v.Seconds > 24*3600 {
			return fmt.Errorf("Dauer 1 s bis 24 h")
		}
		a.sch.AddTimer(v.Name, v.Seconds)
		return nil
	})
	post("/api/timers/cancel", func(r *http.Request) error {
		var v struct {
			ID string `json:"id"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		a.sch.CancelTimer(v.ID)
		return nil
	})
	post("/api/action", func(r *http.Request) error {
		var v struct {
			Action string `json:"action"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.Do(v.Action)
	})
	post("/api/bluetooth/pairing", func(r *http.Request) error {
		var v struct {
			Action string `json:"action"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		if v.Action != "open" && v.Action != "close" && v.Action != "toggle" {
			return fmt.Errorf("open, close oder toggle")
		}
		a.bus.Publish("bt-pairing", map[string]string{"action": v.Action})
		return nil
	})
	post("/api/services/restart", func(r *http.Request) error {
		var v struct {
			Name string `json:"name"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return restartService(v.Name)
	})
	mux.HandleFunc("/metrics", w.metrics)
	getJ := func(path string, f func(r *http.Request) (any, error)) {
		mux.HandleFunc(path, func(rw http.ResponseWriter, r *http.Request) {
			v, err := f(r)
			if err != nil {
				fail(rw, 400, err)
				return
			}
			writeJSON(rw, v)
		})
	}
	// Sendersuche
	getJ("/api/radio/search", func(r *http.Request) (any, error) {
		return a.rb.Search(r.URL.Query().Get("q"), r.URL.Query().Get("country"))
	})
	// Klänge austauschen
	getJ("/api/sounds", func(r *http.Request) (any, error) {
		return map[string]any{"tones": listTones(), "vendor": a.sounds.Scan(r.URL.Query().Get("rescan") == "1")}, nil
	})
	post("/api/sounds/upload", func(r *http.Request) error {
		t := r.URL.Query().Get("target")
		b, err := readUpload(r.Body)
		if err != nil {
			return err
		}
		if len(b) == 0 {
			return fmt.Errorf("keine Datei")
		}
		if id, ok := strings.CutPrefix(t, "tone:"); ok {
			return replaceTone(id, b)
		}
		if p, ok := strings.CutPrefix(t, "vendor:"); ok {
			return a.sounds.ReplaceVendor(p, b)
		}
		return fmt.Errorf("Ziel tone:<Name> oder vendor:<Pfad>")
	})
	post("/api/sounds/reset", func(r *http.Request) error {
		var v struct{ Target string }
		if err := decode(r, &v); err != nil {
			return err
		}
		if id, ok := strings.CutPrefix(v.Target, "tone:"); ok {
			return resetTone(id)
		}
		if p, ok := strings.CutPrefix(v.Target, "vendor:"); ok {
			return a.sounds.ResetVendor(p)
		}
		return fmt.Errorf("Ziel tone:<Name> oder vendor:<Pfad>")
	})
	post("/api/sounds/play", func(r *http.Request) error { // auf dem Lautsprecher anhören (über die Durchsage)
		var v struct {
			Target   string
			Original bool
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		name, b, err := a.sounds.soundSource(v.Target, v.Original)
		if err != nil {
			return err
		}
		f, err := os.CreateTemp("", "lf-preview-*.wav")
		if err != nil {
			return err
		}
		f.Write(b)
		f.Close()
		time.AfterFunc(5*time.Minute, func() { os.Remove(f.Name()) })
		return a.ann.Play(announceReq{file: f.Name(), Name: name})
	})
	mux.HandleFunc("/api/sounds/file", func(rw http.ResponseWriter, r *http.Request) { // im Browser anhören
		name, b, err := a.sounds.soundSource(r.URL.Query().Get("target"), r.URL.Query().Get("original") == "1")
		if err != nil {
			fail(rw, 400, err)
			return
		}
		rw.Header().Set("Content-Type", "audio/wav")
		rw.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
		rw.Header().Set("Cache-Control", "no-store")
		rw.Write(b)
	})
	// Raum einmessen
	post("/api/measure/start", func(r *http.Request) error {
		var v struct{ Seconds int }
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.meas.Start(v.Seconds)
	})
	post("/api/measure/stop", func(r *http.Request) error { a.meas.Stop(); return nil })
	// Briefing
	getJ("/api/briefing/preview", func(r *http.Request) (any, error) {
		if a.brief == nil {
			return nil, fmt.Errorf("Briefing nicht verfügbar")
		}
		return a.brief.Build(a.sch.Now(), false), nil
	})
	post("/api/briefing/start", func(r *http.Request) error {
		if a.brief == nil {
			return fmt.Errorf("Briefing nicht verfügbar")
		}
		return a.brief.Start("api")
	})
	post("/api/briefing/stop", func(r *http.Request) error {
		if a.brief != nil {
			a.brief.Stop()
		}
		if k, _, _, _ := a.pl.Info(); k == "briefing" {
			a.pl.Stop()
		}
		return nil
	})
	getJ("/api/briefing/geocode", func(r *http.Request) (any, error) {
		return a.brief.Geocode(r.URL.Query().Get("q"), r.URL.Query().Get("lang"))
	})
	getJ("/api/briefing/pollen-regions", func(r *http.Request) (any, error) { return a.brief.PollenRegions() })
	// Auswahllisten der Oberfläche (options.go)
	getJ("/api/audio/inputs", func(r *http.Request) (any, error) {
		if demoMode { // nie die Soundkarten des Rechners zeigen, auf dem die Demo läuft
			return audioInputs(""), nil
		}
		return audioInputs("/proc/asound"), nil
	})
	getJ("/api/ha/options", func(r *http.Request) (any, error) { return a.haOptions() })
	// Sprachassistent
	post("/api/voice/listen", func(r *http.Request) error {
		if a.voice == nil {
			return fmt.Errorf("Sprachassistent nicht verfügbar")
		}
		return a.voice.PushToTalk()
	})
	// andere Leuchtfeuer
	getJ("/api/peers", func(r *http.Request) (any, error) {
		if a.peers == nil {
			return map[string]any{"peers": []peerStatus{}, "found": []foundPeer{}}, nil
		}
		return map[string]any{"peers": a.peers.Status(), "found": a.peers.Found()}, nil
	})
	post("/api/peers/add", func(r *http.Request) error {
		var v struct{ Name, URL, Token string }
		if err := decode(r, &v); err != nil {
			return err
		}
		_, err := a.peers.Add(v.Name, v.URL, v.Token)
		return err
	})
	post("/api/peers/delete", func(r *http.Request) error {
		var v struct{ URL string }
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.peers.Delete(v.URL)
	})
	post("/api/peers/copy", func(r *http.Request) error {
		var v struct {
			URL      string
			Sections []string
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.peers.Copy(v.URL, v.Sections)
	})
	post("/api/peers/update", func(r *http.Request) error {
		var v struct{ URL string }
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.peers.Update(v.URL)
	})
	post("/api/peers/action", func(r *http.Request) error {
		var v struct{ URL, Action string }
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.peers.Action(v.URL, v.Action)
	})
	mux.HandleFunc("/api/logs/stream", func(rw http.ResponseWriter, r *http.Request) {
		if a.logs == nil {
			fail(rw, 503, fmt.Errorf("Protokolle nicht verfügbar"))
			return
		}
		a.logs.stream(rw, r)
	})
	mux.HandleFunc("/api/logs/names", func(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, logNames()) })
	mux.HandleFunc("/api/tokens", func(rw http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(rw, w.tokens.List())
		case http.MethodPost:
			var v struct{ Name, Scope string }
			if err := decode(r, &v); err != nil {
				fail(rw, 400, err)
				return
			}
			tok, t, err := w.tokens.Create(v.Name, v.Scope)
			if err != nil {
				fail(rw, 400, err)
				return
			}
			log.Printf("API-Schlüssel %q (%s) angelegt", t.Name, t.Scope)
			writeJSON(rw, map[string]any{"token": tok, "info": t})
		default:
			http.Error(rw, "GET oder POST", http.StatusMethodNotAllowed)
		}
	})
	post("/api/tokens/delete", func(r *http.Request) error {
		var v struct{ ID string }
		if err := decode(r, &v); err != nil {
			return err
		}
		return w.tokens.Delete(v.ID)
	})
	mux.HandleFunc("/api/ssh-keys", func(rw http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			k, err := listSSHKeys()
			if err != nil {
				fail(rw, 500, err)
				return
			}
			writeJSON(rw, k)
		case http.MethodPost:
			var v struct{ Key string }
			if err := decode(r, &v); err != nil {
				fail(rw, 400, err)
				return
			}
			k, err := addSSHKey(v.Key)
			if err != nil {
				fail(rw, 400, err)
				return
			}
			log.Printf("SSH-Schlüssel %s (%s) eingetragen", k.Fingerprint, k.Comment)
			writeJSON(rw, k)
		default:
			http.Error(rw, "GET oder POST", http.StatusMethodNotAllowed)
		}
	})
	post("/api/ssh-keys/delete", func(r *http.Request) error {
		var v struct{ Fingerprint string }
		if err := decode(r, &v); err != nil {
			return err
		}
		if err := deleteSSHKey(v.Fingerprint); err != nil {
			return err
		}
		log.Printf("SSH-Schlüssel %s entfernt", v.Fingerprint)
		return nil
	})
	mux.HandleFunc("/api/logs", func(rw http.ResponseWriter, r *http.Request) {
		t, err := tailLog(r.URL.Query().Get("name"), 200)
		if err != nil {
			fail(rw, 400, err)
			return
		}
		writeJSON(rw, map[string]string{"log": t})
	})
	mux.HandleFunc("/api/wifi/scan", func(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, a.wifi.Scan()) })
	post("/api/wifi/roam", func(r *http.Request) error {
		var v struct {
			BSSID string `json:"bssid"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.wifi.RoamTo(v.BSSID)
	})
	mux.HandleFunc("/api/led/test", func(rw http.ResponseWriter, r *http.Request) {
		a.led.Animate("success", false)
		writeJSON(rw, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/events", w.events)
	// Ausgabe der Tonkette (output.go)
	mux.HandleFunc("/api/outputs", func(rw http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(rw, a.out.List())
		case http.MethodPut:
			var v struct {
				ID string `json:"id"`
			}
			if err := decode(r, &v); err != nil {
				fail(rw, 400, err)
				return
			}
			if err := a.out.Set(v.ID); err != nil {
				fail(rw, 400, err)
				return
			}
			writeJSON(rw, a.out.List())
		default:
			http.Error(rw, "GET oder PUT nötig", http.StatusMethodNotAllowed)
		}
	})
	post("/api/outputs/bluetooth", func(r *http.Request) error {
		var v struct {
			Action string `json:"action"`
			Addr   string `json:"addr"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.out.Bluetooth(v.Action, v.Addr)
	})
	post("/api/services/group", func(r *http.Request) error {
		var v struct {
			Group   string `json:"group"`
			Enabled bool   `json:"enabled"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		if err := setGroup(a.cfg, v.Group, v.Enabled); err != nil {
			return err
		}
		a.emit("settings", nil)
		return nil
	})
	post("/api/announce", func(r *http.Request) error {
		var v announceReq
		if err := decode(r, &v); err != nil {
			return err
		}
		return a.ann.Play(v)
	})
	post("/api/sleep", func(r *http.Request) error {
		var v struct {
			Minutes int `json:"minutes"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		a.sch.SetSleep(clamp(v.Minutes, 0, 600))
		return nil
	})
	post("/api/alarms/skip", func(r *http.Request) error {
		var v struct {
			ID   string `json:"id"`
			Skip bool   `json:"skip"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		_, err := a.sch.SkipNext(v.ID, v.Skip)
		return err
	})
	post("/api/bluetooth/control", func(r *http.Request) error {
		var v struct {
			Action string `json:"action"`
		}
		if err := decode(r, &v); err != nil {
			return err
		}
		switch v.Action {
		case "play", "pause", "stop", "next", "previous":
			a.bus.Publish("bt-control", map[string]string{"action": v.Action})
			return nil
		}
		return fmt.Errorf("play, pause, stop, next oder previous")
	})
	mux.HandleFunc("/api/backup", func(rw http.ResponseWriter, r *http.Request) {
		sendTarGz(rw, fileName("leuchtfeuer-sicherung", a.cfg.Get("DEVICE_NAME", hw.DefaultName)), writeBackup)
	})
	mux.HandleFunc("/api/diag", func(rw http.ResponseWriter, r *http.Request) {
		sendTarGz(rw, fileName("leuchtfeuer-diagnose", a.cfg.Get("DEVICE_NAME", hw.DefaultName)), w.writeDiag)
	})
	post("/api/restore", func(r *http.Request) error {
		n, err := restoreBackup(http.MaxBytesReader(nil, r.Body, 60<<20))
		if err != nil {
			return err
		}
		log.Printf("Sicherung zurückgespielt (%d Dateien), Dienste starten neu", n)
		restartAllServices()
		return nil
	})
	mux.HandleFunc("/api/update", func(rw http.ResponseWriter, r *http.Request) {
		info, err := a.checkUpdate()
		if err != nil {
			info.Message = err.Error()
		}
		writeJSON(rw, info)
	})
	post("/api/update/install", func(r *http.Request) error { return a.startUpdate() })
	post("/api/update/upload", func(r *http.Request) error {
		r.Body = http.MaxBytesReader(nil, r.Body, 310<<20)
		return a.uploadUpdate(r)
	})
	post("/api/update/rollback", func(r *http.Request) error { return rollbackUpdate() })
	return wrapAuth(w, mux)
}

// events: Server-Sent Events statt Abfrage alle 2 s. Bei jeder Änderung (Lautstärke, Quelle, Wecker, Einstellungen ...)
// und sonst alle 10 s kommt der vollständige Status; "settings" meldet geänderte Einstellungen. Timer zählt die
// Oberfläche selbst herunter.
func (w *webServer) events(rw http.ResponseWriter, r *http.Request) {
	fl, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "kein Streaming", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Accel-Buffering", "no")
	kick := make(chan string, 8)
	stop := w.app.Listen(func(kind string, _ map[string]any) {
		select {
		case kick <- kind:
		default:
		}
	})
	defer stop()
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(rw, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send("status", w.status()) {
		return
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case kind := <-kick:
			time.Sleep(150 * time.Millisecond) // Ereignisse bündeln
			settings := kind == "settings"
		drain:
			for {
				select {
				case k := <-kick:
					settings = settings || k == "settings"
				default:
					break drain
				}
			}
			if settings && !send("settings", map[string]bool{"changed": true}) {
				return
			}
			if !send("status", w.status()) {
				return
			}
		case <-t.C:
			if !send("status", w.status()) {
				return
			}
		}
	}
}

// Run startet die Weboberfläche; mit WEB_TLS="on" zusätzlich HTTPS (HTTP leitet dann um).
func (w *webServer) Run(addr string) {
	h := w.routes()
	cfg := w.app.cfg
	if cfg.Get("WEB_TLS", "") == "on" {
		port := cfg.Get("WEB_TLS_PORT", "443")
		host := hostName(cfg)
		cert, err := ensureCert(dataDir, host)
		if err == nil {
			w.login.secure = true
			go func() {
				srv := &http.Server{Addr: addr, Handler: redirectHTTPS(port), ReadHeaderTimeout: 10 * time.Second}
				log.Printf("HTTP auf %s leitet auf HTTPS um", addr)
				log.Print(srv.ListenAndServe())
			}()
			srv := &http.Server{Addr: ":" + port, Handler: h, ReadHeaderTimeout: 10 * time.Second,
				TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}}
			log.Printf("Weboberfläche auf :%s (HTTPS)", port)
			log.Fatal(srv.ListenAndServeTLS("", ""))
		}
		log.Printf("HTTPS: %v - weiter nur mit HTTP", err)
	}
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Weboberfläche auf %s ", addr)
	log.Fatal(srv.ListenAndServe())
}
