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
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// wrapAuth schützt die Oberfläche mit Anmeldeseite und Sitzung.
var wrapAuth = func(w *webServer, h http.Handler) http.Handler { return w.auth(h) }

type webServer struct {
	app   *app
	login *loginState
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
	WampOK     bool            `json:"wamp"`
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
		Name: a.cfg.Get("DEVICE_NAME", "HK Invoke"), Version: a.version, Volume: vol, Muted: muted, VolKnown: known,
		WampOK: a.wampOK(), Player: map[string]any{"kind": kind, "name": name, "state": state, "title": title},
		Alarm: a.sch.State(), Wifi: a.wifiFn(), Sys: a.sysFn(), Bluetooth: a.btFn(),
		BTMode: a.cfg.Get("BLUETOOTH_PAIRING", "button"), MQTT: a.mqttOK(), Buttons: a.Buttons(),
		Now: a.sch.Now().Format(time.RFC3339), Timezone: set.Timezone, Demo: demoMode,
		Viz:     map[string]bool{"tap": demoMode, "active": false},
		Sources: a.src.List(), Active: a.src.Active(), SleepSecs: a.sch.SleepRemaining(), Clock: a.clkFn(),
		Update: a.updateStatus(), Holiday: holidayName(set.Holidays, a.sch.Now()),
	}
	s.Sys.Services = a.svcFn()
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
		Name: c.Get("DEVICE_NAME", "HK Invoke"), SendspinServer: c.Get("SENDSPIN_SERVER", ""),
		DHCPHostname: c.Get("DHCP_HOSTNAME", "invoke"), BluetoothPairing: c.Get("BLUETOOTH_PAIRING", "button"),
		AirPlay: c.Get("AIRPLAY", "on"),
	}
}

func (w *webServer) settings() map[string]any {
	set := w.app.st.Snapshot()
	set.MQTT.Pass = "" // Passwörter nie ausliefern
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
		"sourceNames": sourceNames, "version": currentVersion(),
		"services": serviceDefs(), "groups": serviceGroups(w.app.cfg),
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
			l.Max, l.Start = clamp(l.Max, 0, 100), clamp(l.Start, 0, 100)
			if l.Max > 0 || l.Start > 0 {
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
		return a.st.Update(func(s *Settings) { s.Eq = v })
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
	post("/api/radio/stop", func(r *http.Request) error { a.pl.Stop(); return nil })
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
		a.h.Publish("invoke.bt.pairing", v.Action)
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
		a.led.Animate("L_106_c_success", false)
		writeJSON(rw, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/events", w.events)
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
			a.h.Publish("invoke.bt.control", v.Action)
			return nil
		}
		return fmt.Errorf("play, pause, stop, next oder previous")
	})
	mux.HandleFunc("/api/backup", func(rw http.ResponseWriter, r *http.Request) {
		sendTarGz(rw, fileName("leuchtfeuer-sicherung", a.cfg.Get("DEVICE_NAME", "invoke")), writeBackup)
	})
	mux.HandleFunc("/api/diag", func(rw http.ResponseWriter, r *http.Request) {
		sendTarGz(rw, fileName("leuchtfeuer-diagnose", a.cfg.Get("DEVICE_NAME", "invoke")), w.writeDiag)
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
		host := cfg.Get("DHCP_HOSTNAME", "invoke")
		cert, err := ensureCert(invokeDir, host)
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
