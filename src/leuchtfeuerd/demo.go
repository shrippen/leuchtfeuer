//go:build demo

// Demo-Modus: nur ein internes Werkzeug zum Erstellen von Screenshots mit den gemeinsamen Demodaten
// („Studio Weber“, demo/world.json). Dieser Build entsteht nur mit dem Tag "demo" und wird nie ausgeliefert;
// tools/build-leuchtfeuerd.sh bricht ab, wenn die Marke unten im Release-Programm steht.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const demoMode = true

// Marke, nach der der Release-Build sucht.
var demoMarker = "LEUCHTFEUER-DEMO-BUILD"

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

func (d *demoVol) OnChange(func()) {}

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
	// Demo stellt einen Invoke dar; die Hersteller-Software gilt als verbunden
	hw = targets["invoke"]
	hw.linkOK = func(*app) bool { return true }
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
	dir, _ := os.MkdirTemp("", "leuchtfeuerd-demo")
	cfgPath := dir + "/config"
	os.WriteFile(cfgPath, []byte(fmt.Sprintf("DEVICE_NAME=%q\nDHCP_HOSTNAME=%q\nBLUETOOTH_PAIRING=\"button\"\nSENDSPIN_SERVER=\"%s:8927\"\nAIRPLAY=\"on\"\nWEB_PASSWORD=\"demo\"\n",
		sp.Name, sp.Hostname, sp.MQTT.Host)), 0o600)
	st := loadStore(dir + "/leuchtfeuerd.json")
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
		de := lang == "de"
		pick := func(en, d string) string {
			if de {
				return d
			}
			return en
		}
		s.Briefing = BriefingSettings{Lang: lang, Place: "Hamburg", Lat: 53.55, Lon: 9.99, TTS: "ha", Then: "radio:0", Items: []BriefItem{
			{Type: "greeting", On: true}, {Type: "weather", On: true}, {Type: "warnings", On: true},
			{Type: "calendar", On: true, Name: pick("Studio", "Studio"), URL: "https://cloud.weber-studio.example/remote.php/dav/public-calendars/studio?export", Days: 0},
			{Type: "calendar", On: true, Name: pick("Waste collection", "Müllabfuhr"), URL: "https://www.stadtreinigung.example/abfuhr.ics", Days: 1},
			{Type: "ha", On: true, Text: pick("Travel time to the studio: {{ states('sensor.travel_time') }} minutes.", "Fahrzeit ins Studio: {{ states('sensor.fahrzeit') }} Minuten.")},
			{Type: "podcast", On: true, Name: podcastPresets[0].Name, URL: podcastPresets[0].URL},
		}}
		s.HA = HASettings{URL: "http://homeassistant.local:8123", Token: "x", TTSEngine: "tts.piper"}
		s.Voice = VoiceSettings{Enabled: true, Port: 10700, Mic: "leuchtfeuer_mic", Mode: "wake", Area: pick("Studio", "Studio"), DuckDB: 20}
		s.Eq = EqSettings{Version: 1, Bass: 2, Loudness: true, RoomOn: true, Room: []PEQBand{{Hz: 52, DB: -6.4, Q: 4.6}, {Hz: 118, DB: -3.8, Q: 3.2}}}
		s.Sources.Limits = map[string]SourceLimit{"bluetooth": {Max: 80, TrimDB: 4}, "radio": {Start: 25}}
		s.Syslog = SyslogSettings{Enabled: true, Host: "192.168.178.20", Port: 514, Proto: "udp"}
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
		return sysStatus{TempC: float64(sp.TempC), UptimeSecs: sp.UpDays*86400 + 5*3600, Load1: 0.31, MemTotalMB: 462, MemFreeMB: 398, DataFreeMB: 68, DataSizeMB: 123}
	}
	// Dienste aus den Kopfzeilen im Repo wie auf dem Invoke: gemeinsame und gerätespezifische
	// (demo/start.sh startet im Hauptverzeichnis)
	servicesDir, runDir = filepath.Join(dir, "services"), dir
	os.MkdirAll(servicesDir, 0o755)
	for _, pat := range []string{"device/leuchtfeuer/services/*.sh", "targets/invoke/services/*.sh"} {
		files, _ := filepath.Glob(pat)
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil {
				os.WriteFile(filepath.Join(servicesDir, filepath.Base(f)), b, 0o755)
			}
		}
	}
	a.svcFn = func() []serviceInfo {
		var out []serviceInfo
		for _, d := range serviceDefs() {
			if d.Group == "tidal" || d.Group == "snapcast" {
				continue
			}
			out = append(out, serviceInfo{serviceDef: d, Enabled: true, Running: running[d.Process] || (d.Group == "core")})
		}
		return out
	}
	a.clkFn = func() clockStatus {
		return clockStatus{OffsetMs: 38, Checked: now.Add(-12 * time.Minute), Server: "pool.ntp.org", LastSync: now.Add(-3 * time.Hour), Synced: true}
	}
	a.src.Update("radio", "playing", map[string]string{"title": sp.Playing.Title.in(lang)})
	wr := sp.WifiRaw
	a.wifiFn = func() wifiStatus {
		return wifiStatus{SSID: fmt.Sprint(wr["ssid"]), BSSID: fmt.Sprint(wr["bssid"]), FreqMHz: toInt(wr["freq"]), RSSI: toInt(wr["rssi"]), LinkMbps: toInt(wr["link_mbps"]),
			State: "COMPLETED", Gateway: fmt.Sprint(wr["gateway"]), LossPct: toInt(wr["loss_pct"]), RttMs: float64(toInt(wr["rtt_ms"])), Good: true,
			LastCheck: now, Log: toStrings(wr["log"])}
	}
	a.btFn = func() btState { return btState{} }
	// Ausgabe: zwei Soundkarten und zwei Bluetooth-Lautsprecher (einer gekoppelt)
	a.out.cardsFile = filepath.Join(dir, "cards")
	os.WriteFile(a.out.cardsFile, []byte(" 0 [wm8904         ]: wm8904 - HK Invoke DSP\n                      HK Invoke DSP\n 1 [HDMI           ]: HDMI - HDMI Ausgang\n                      HDMI Ausgang\n"), 0o644)
	a.out.confFile = func() string { return filepath.Join(dir, "output.conf") }
	a.out.restart = func() {}
	go func() {
		for {
			a.out.Report(btReport{Devices: []btDevice{
				{Addr: "00:1A:7D:DA:71:13", Name: "JBL Flip 6", Paired: true, Connected: true},
				{Addr: "F4:6D:04:12:34:56", Name: "Bose SoundLink", RSSI: -58},
			}})
			time.Sleep(5 * time.Second)
		}
	}()
	a.mqttOK = func() bool { return true }
	for _, e := range sp.ButtonLog {
		a.buttons = append(a.buttons, buttonEvent{Time: now.Add(-time.Duration(e.Ago) * time.Second), Name: e.Name, Value: e.Value, Do: e.Action})
	}
	wrapAuth = func(_ *webServer, h http.Handler) http.Handler { return h }
	a.ann = newAnnouncer(a, "null")
	a.brief = newBriefing(a)
	// Sprachassistent: mit Home Assistant verbunden, letzte Frage
	a.voice = newVoice(a)
	a.voice.active, a.voice.state = &wyConn{}, "idle"
	if lang == "de" {
		a.voice.heard, a.voice.answer = "Wie warm ist es im Studio?", "Im Studio sind es 21,5 Grad."
	} else {
		a.voice.heard, a.voice.answer = "How warm is it in the studio?", "It is 21.5 degrees in the studio."
	}
	// andere Lautsprecher
	a.peers = newPeerHub(a)
	st.Update(func(s *Settings) {
		s.Peers = []Peer{{Name: "Küche", URL: "http://invoke-kueche.lan"}, {Name: "Lager", URL: "https://invoke-lager.lan"}}
	})
	a.peers.statusFn = func() []peerStatus {
		return []peerStatus{
			{Peer: Peer{Name: "Küche", URL: "http://invoke-kueche.lan"}, Online: true, Version: currentVersion(), Volume: 22, TempC: 61, Playing: "spotify: Nils Frahm – Says"},
			{Peer: Peer{Name: "Lager", URL: "https://invoke-lager.lan"}, Error: "Lager: dial tcp 192.168.178.47:443: i/o timeout"},
		}
	}
	a.peers.found = map[string]foundPeer{"aabbccddeeff": {Name: "Empfang", URL: "http://192.168.178.53", Version: currentVersion(), ID: "aabbccddeeff"}}
	// Protokolle, Schlüssel
	dataDir, logDir, hookLog = dir, dir+"/log", dir+"/hook.log"
	os.MkdirAll(logDir, 0o755)
	for _, n := range []string{"leuchtfeuerd", "librespot", "shairport", "bluetooth-4-aplay"} {
		os.WriteFile(logDir+"/"+n+".log", nil, 0o644)
	}
	a.logs = newLogHub(nil)
	for _, l := range []logLine{
		{now.Add(-95 * time.Second), "hook", "Dienst librespot gestartet (pid 2817)"},
		{now.Add(-80 * time.Second), "librespot", "[INFO librespot_playback::player] Loading <Says> with Spotify URI <spotify:track:1C1Z1Ry9Lo6pKyx1Rj5DnN>"},
		{now.Add(-62 * time.Second), "leuchtfeuerd", "Quelle radio stumm (andere Quelle hat Vorrang)"},
		{now.Add(-41 * time.Second), "bluetooth-4-aplay", "underrun!!! (at least 3.412 ms long)"},
		{now.Add(-12 * time.Second), "leuchtfeuerd", "Webradio http://stream.example/radio: error, neuer Versuch in 2s"},
		{now.Add(-9 * time.Second), "leuchtfeuerd", "Briefing für 06:45 vorbereitet (4 Abschnitte)"},
	} {
		a.logs.publish(l)
	}
	os.WriteFile(dir+"/authorized_keys", []byte(demoKeys), 0o600)
	// Klänge: nachgebildete Hersteller-Klänge, ein eigener Weckton
	sysRoot, _ := os.MkdirTemp("", "leuchtfeuer-demo-system") // nicht unter dem Datenverzeichnis: das überspringt die Suche
	sys := sysRoot + "/usr/share/harman/prompts"
	os.MkdirAll(sys, 0o755)
	for n, secs := range map[string]float64{"power_on.wav": 2.4, "network_error.wav": 1.1, "bt_connected.wav": 0.8, "setup_mode.wav": 1.6} {
		frames := make([]float64, int(16000*secs))
		os.WriteFile(sys+"/"+n, pcmData{pcmFormat: pcmFormat{16000, 1, 16}, frames: [][]float64{frames}}.wav(), 0o644)
	}
	soundRoots = []string{sysRoot + "/usr/share"}
	a.sounds.mount = func(string, string) error { return nil }
	a.sounds.umount = func(string) error { return nil }
	a.sounds.mounted = func(string) bool { return true }
	replaceTone("alarm", pcmData{pcmFormat: pcmFormat{48000, 2, 16}, frames: [][]float64{make([]float64, 48000*4), make([]float64, 48000*4)}}.wav())
	a.sounds.ReplaceVendor(sys+"/power_on.wav", pcmData{pcmFormat: pcmFormat{48000, 2, 16}, frames: [][]float64{make([]float64, 48000*3), make([]float64, 48000*3)}}.wav())
	w := &webServer{app: a, login: newLoginState(""), tokens: loadTokens(dir + "/tokens.json")}
	w.tokens.Create("Home Assistant", "full")
	w.tokens.Create("Prometheus", "read")
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

// Beispielschlüssel im richtigen Format (Zufallsbytes, keine echten Schlüssel)
const demoKeys = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKLvR49qGA+o0okpI4l2wDksoS1Vptf26qrGl9IK/0uh anna@studio-mac\n" +
	"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFcHtOPWPaWZCXPIgw0tkATbFTkDx1MQn982m5FMFiI7 backup@nas\n"
