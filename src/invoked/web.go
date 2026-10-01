package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// wrapAuth schützt die Oberfläche mit Anmeldung (Benutzer admin).
var wrapAuth = func(w *webServer, h http.Handler) http.Handler { return w.auth(h) }

type webServer struct {
	app  *app
	pass string
}

func randomPassword() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (w *webServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte("admin")) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(w.pass)) != 1 {
			rw.Header().Set("WWW-Authenticate", `Basic realm="HK Invoke"`)
			http.Error(rw, "Anmeldung nötig (Benutzer admin, Passwort in /data/invoke/config: WEB_PASSWORD)", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
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
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Volume     int            `json:"volume"`
	Muted      bool           `json:"muted"`
	VolKnown   bool           `json:"volumeKnown"`
	WampOK     bool           `json:"wamp"`
	Player     map[string]any `json:"player"`
	Alarm      alarmState     `json:"alarm"`
	NextAlarm  string         `json:"nextAlarm"`
	NextName   string         `json:"nextAlarmName"`
	Timers     []timerView    `json:"timers"`
	Wifi       wifiStatus     `json:"wifi"`
	Sys        sysStatus      `json:"sys"`
	Bluetooth  btState        `json:"bluetooth"`
	BTMode     string         `json:"btMode"`
	MQTT       bool           `json:"mqtt"`
	Buttons    []buttonEvent  `json:"buttons"`
	Now        string         `json:"now"`
	Timezone   string         `json:"timezone"`
	WebDefault bool           `json:"webDefaultPassword"`
	Demo       bool           `json:"demo"`
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
	AirPlay          string `json:"airplay"` // on | off
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
		"settings": set, "device": w.device(), "actions": actionNames,
		"services": []string{"librespot", "gmrender", "sendspin", "castrecv", "shairport", "tidal-3-connect", "bluetooth-1-bluetoothd", "bluetooth-2-agent", "bluetooth-3-bluealsa", "bluetooth-4-aplay", "volume-sync", "invoked"},
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
		if v.AirPlay == "on" || v.AirPlay == "off" {
			kv["AIRPLAY"] = v.AirPlay
		}
		if v.WebPassword != "" {
			if len(v.WebPassword) < 6 {
				return fmt.Errorf("Passwort: mindestens 6 Zeichen")
			}
			kv["WEB_PASSWORD"] = v.WebPassword
			w.pass = v.WebPassword
		}
		return a.cfg.Set(kv)
	}
	return fmt.Errorf("unbekannter Bereich %q", section)
}

// restartService beendet den Dienst; hook.sh startet ihn innerhalb von 30 s neu.
func restartService(name string) error {
	ok := map[string]bool{"librespot": true, "gmrender": true, "sendspin": true, "castrecv": true, "shairport": true,
		"tidal-3-connect": true, "bluetooth-1-bluetoothd": true, "bluetooth-2-agent": true, "bluetooth-3-bluealsa": true,
		"bluetooth-4-aplay": true, "volume-sync": true, "invoked": true}
	if !ok[name] {
		return fmt.Errorf("unbekannter Dienst")
	}
	b, err := os.ReadFile("/run/invoke-svc-" + name + ".pid")
	if err != nil {
		return fmt.Errorf("Dienst läuft nicht (kein PID)")
	}
	return exec.Command("kill", strings.TrimSpace(string(b))).Run()
}

func tailLog(name string, lines int) (string, error) {
	ok := map[string]bool{"librespot": true, "gmrender": true, "sendspin": true, "castrecv": true, "shairport": true,
		"tidal-3-connect": true, "bluetooth-1-bluetoothd": true, "bluetooth-2-agent": true, "bluetooth-3-bluealsa": true,
		"bluetooth-4-aplay": true, "volume-sync": true, "invoked": true, "hook": true}
	if !ok[name] {
		return "", fmt.Errorf("unbekanntes Protokoll")
	}
	path := "/data/invoke/log/" + name + ".log"
	if name == "hook" {
		path = "/data/invoke/hook.log"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	l := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(l) > lines {
		l = l[len(l)-lines:]
	}
	// lange dmix-Debugzeilen der ALSA-Bibliothek ausblenden
	var out []string
	for _, x := range l {
		if !strings.HasPrefix(x, "dmix<") && len(x) < 400 {
			out = append(out, x)
		}
	}
	return strings.Join(out, "\n"), nil
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
	return wrapAuth(w, mux)
}

func (w *webServer) Run(addr string) {
	srv := &http.Server{Addr: addr, Handler: w.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Weboberfläche auf %s (Benutzer admin)", addr)
	log.Fatal(srv.ListenAndServe())
}
