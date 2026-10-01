//go:build demo

// Demo-Modus: nur ein internes Werkzeug zum Erstellen von Screenshots mit den gemeinsamen Demodaten
// („Studio Weber“, demo/world.json). Dieser Build entsteht nur mit dem Tag "demo" und wird nie ausgeliefert;
// tools/build-invoked.sh bricht ab, wenn die Marke unten im Release-Programm steht.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const demoMode = true

// Marke, nach der der Release-Build sucht.
var demoMarker = "INVOKE-DEMO-BUILD"

var demoWorld = flag.String("demo", "", "Demodaten (demo/world.json) laden und ohne Gerät starten")

func demoRequested() bool { return *demoWorld != "" }

type loc struct{ DE, EN string }

func (l *loc) UnmarshalJSON(b []byte) error {
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		l.DE, l.EN = m["de"], m["en"]
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	l.DE, l.EN = s, s
	return nil
}
func (l loc) in(lang string) string {
	if lang == "de" && l.DE != "" {
		return l.DE
	}
	return l.EN
}

type demoAlarm struct {
	Name     loc    `json:"name"`
	Time     string `json:"time"`
	Days     []int  `json:"days"`
	Enabled  bool   `json:"enabled"`
	Source   string `json:"source"`
	Volume   int    `json:"volume"`
	RampSecs int    `json:"ramp_secs"`
	Snooze   int    `json:"snooze_min"`
	MaxMins  int    `json:"max_mins"`
}

type demoSpeaker struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Volume   int    `json:"volume"`
	Muted    bool   `json:"muted"`
	TempC    int    `json:"temp_c"`
	UpDays   int    `json:"uptime_days"`
	Wifi     struct {
		SSID, BSSID, Gateway string
		Freq, RSSI, LinkMbps int      `json:"-"`
		Log                  []string `json:"log"`
	} `json:"-"`
	WifiRaw map[string]any `json:"wifi"`
	Radio   []Preset       `json:"radio"`
	Playing struct {
		Radio int `json:"radio"`
		Title loc `json:"title"`
	} `json:"playing"`
	Alarms []demoAlarm `json:"alarms"`
	Timers []struct {
		Name    loc `json:"name"`
		Total   int `json:"total_secs"`
		Elapsed int `json:"elapsed_secs"`
	} `json:"timers"`
	Buttons   map[string]map[string]string `json:"buttons"`
	ButtonLog []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Ago    int    `json:"ago_secs"`
		Action string `json:"action"`
	} `json:"button_log"`
	MQTT struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		User      string `json:"user"`
		Discovery string `json:"discovery"`
	} `json:"mqtt"`
	Services []string `json:"services_running"`
}

type demoVol struct {
	mu    sync.Mutex
	vol   int
	muted bool
}

func (d *demoVol) Get() (int, bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.vol, d.muted, true
}
func (d *demoVol) Adjust(x int) error {
	d.mu.Lock()
	d.vol = clamp(d.vol+x, 0, 100)
	d.mu.Unlock()
	return nil
}
func (d *demoVol) SetVolume(v int) error {
	d.mu.Lock()
	d.vol = clamp(v, 0, 100)
	d.mu.Unlock()
	return nil
}
func (d *demoVol) SetMute(m bool) error { d.mu.Lock(); d.muted = m; d.mu.Unlock(); return nil }
func (d *demoVol) ToggleMute() error    { d.mu.Lock(); d.muted = !d.muted; d.mu.Unlock(); return nil }

type demoPlayer struct {
	mu                       sync.Mutex
	kind, name, state, title string
}

func (p *demoPlayer) Info() (string, string, string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kind, p.name, p.state, p.title
}
func (p *demoPlayer) PlayURL(kind, name, url string) {
	p.mu.Lock()
	p.kind, p.name, p.state, p.title = kind, name, "playing", ""
	p.mu.Unlock()
}
func (p *demoPlayer) PlayTone(kind, name string, _ []note, _ bool) {
	p.mu.Lock()
	p.kind, p.name, p.state, p.title = kind, name, "playing", ""
	p.mu.Unlock()
}
func (p *demoPlayer) Stop() {
	p.mu.Lock()
	p.kind, p.name, p.state, p.title = "", "", "idle", ""
	p.mu.Unlock()
}

type demoLED struct{}

func (demoLED) Animate(string, bool) {}
func (demoLED) Off()                 {}

func runDemo(listen string) {
	log.Printf("%s", demoMarker)
	b, err := os.ReadFile(*demoWorld)
	if err != nil {
		log.Fatal(err)
	}
	var world struct {
		Today    string      `json:"screenshot_today"`
		Timezone string      `json:"timezone"`
		Speaker  demoSpeaker `json:"speaker"`
	}
	if err := json.Unmarshal(b, &world); err != nil {
		log.Fatal(err)
	}
	sp := world.Speaker
	lang := strings.ToLower(os.Getenv("DEMO_LANG"))
	if lang != "de" {
		lang = "en"
	}
	day := os.Getenv("DEMO_TODAY")
	if day == "" {
		day = world.Today
	}
	tz, _ := time.LoadLocation(world.Timezone)
	if tz == nil {
		tz = time.UTC
	}
	now, err := time.ParseInLocation("2006-01-02 15:04", day+" 18:20", tz)
	if err != nil {
		log.Fatal(err)
	}
	dir, _ := os.MkdirTemp("", "invoked-demo")
	cfgPath := dir + "/config"
	os.WriteFile(cfgPath, []byte(fmt.Sprintf("DEVICE_NAME=%q\nDHCP_HOSTNAME=%q\nBLUETOOTH_PAIRING=\"button\"\nSENDSPIN_SERVER=\"%s:8927\"\nAIRPLAY=\"on\"\nWEB_PASSWORD=\"demo\"\n",
		sp.Name, sp.Hostname, sp.MQTT.Host)), 0o600)
	st := loadStore(dir + "/invoked.json")
	st.Update(func(s *Settings) {
		s.Timezone = world.Timezone
		s.Radio = sp.Radio
		s.Buttons = sp.Buttons
		s.MQTT = MQTTSettings{Enabled: true, Host: sp.MQTT.Host, Port: sp.MQTT.Port, User: sp.MQTT.User, Discovery: sp.MQTT.Discovery}
		s.Alarms = nil
		for i, a := range sp.Alarms {
			s.Alarms = append(s.Alarms, Alarm{ID: fmt.Sprintf("a%d", i+1), Name: a.Name.in(lang), Time: a.Time, Days: a.Days, Enabled: a.Enabled,
				Source: a.Source, Volume: a.Volume, RampSecs: a.RampSecs, Snooze: a.Snooze, MaxMins: a.MaxMins})
		}
		s.Timers = nil
		for i, t := range sp.Timers {
			s.Timers = append(s.Timers, Timer{ID: fmt.Sprintf("t%d", i+1), Name: t.Name.in(lang), Total: t.Total,
				End: now.Add(time.Duration(t.Total-t.Elapsed) * time.Second)})
		}
	})
	a := newApp(&shellConfig{path: cfgPath}, st)
	a.clock = func() time.Time { return now }
	a.vol = &demoVol{vol: sp.Volume, muted: sp.Muted}
	a.pl = &demoPlayer{kind: "radio", name: sp.Radio[sp.Playing.Radio].Name, state: "playing", title: sp.Playing.Title.in(lang)}
	a.led = demoLED{}
	running := map[string]bool{}
	for _, n := range sp.Services {
		running[n] = true
	}
	a.sysFn = func() sysStatus {
		s := sysStatus{TempC: float64(sp.TempC), UptimeSecs: sp.UpDays*86400 + 5*3600, Load1: 0.31, MemTotalMB: 462, MemFreeMB: 398, DataFreeMB: 68, DataSizeMB: 123}
		for _, w := range watched {
			s.Services = append(s.Services, serviceInfo{w.Name, running[w.Match]})
		}
		return s
	}
	wr := sp.WifiRaw
	a.wifiFn = func() wifiStatus {
		return wifiStatus{SSID: fmt.Sprint(wr["ssid"]), BSSID: fmt.Sprint(wr["bssid"]), FreqMHz: toInt(wr["freq"]), RSSI: toInt(wr["rssi"]), LinkMbps: toInt(wr["link_mbps"]),
			State: "COMPLETED", Gateway: fmt.Sprint(wr["gateway"]), LossPct: toInt(wr["loss_pct"]), RttMs: float64(toInt(wr["rtt_ms"])), Good: true,
			LastCheck: now, Log: toStrings(wr["log"])}
	}
	a.btFn = func() btState { return btState{} }
	a.mqttOK = func() bool { return true }
	a.wampOK = func() bool { return true }
	for _, e := range sp.ButtonLog {
		a.buttons = append(a.buttons, buttonEvent{Time: now.Add(-time.Duration(e.Ago) * time.Second), Name: e.Name, Value: e.Value, Do: e.Action})
	}
	wrapAuth = func(_ *webServer, h http.Handler) http.Handler { return h }
	w := &webServer{app: a, pass: "demo"}
	log.Printf("Demo (%s) auf %s", lang, listen)
	w.Run(listen)
}

func toStrings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, fmt.Sprint(x))
		}
	}
	return out
}
