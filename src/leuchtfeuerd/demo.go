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
	"slices"
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

	// Was die Oberfläche sonst noch zeigt (world: speaker)
	DemoTime string `json:"demo_time"`
	Config   struct {
		BluetoothPairing string `json:"bluetooth_pairing"`
		SendspinPort     int    `json:"sendspin_port"`
		Airplay          string `json:"airplay"`
		WebPassword      string `json:"web_password"`
	} `json:"config"`
	Briefing struct {
		Place   string  `json:"place"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
		TTS     string  `json:"tts"`
		Then    string  `json:"then"`
		Podcast int     `json:"podcast"`
		Items   []struct {
			Type string `json:"type"`
			Name loc    `json:"name"`
			URL  string `json:"url"`
			Days int    `json:"days"`
			Text loc    `json:"text"`
		} `json:"items"`
	} `json:"briefing"`
	HA struct {
		URL       string `json:"url"`
		Token     string `json:"token"`
		TTSEngine string `json:"tts_engine"`
	} `json:"home_assistant"`
	Voice struct {
		Port   int    `json:"port"`
		Mic    string `json:"mic"`
		Mode   string `json:"mode"`
		Area   loc    `json:"area"`
		DuckDB int    `json:"duck_db"`
		Heard  loc    `json:"heard"`
		Answer loc    `json:"answer"`
	} `json:"voice"`
	Eq struct {
		Version  int  `json:"version"`
		Bass     int  `json:"bass"`
		Loudness bool `json:"loudness"`
		RoomOn   bool `json:"room_on"`
		Room     []struct {
			Hz float64 `json:"hz"`
			DB float64 `json:"db"`
			Q  float64 `json:"q"`
		} `json:"room"`
	} `json:"eq"`
	SourceLimits map[string]struct {
		Max    int `json:"max"`
		TrimDB int `json:"trim_db"`
		Start  int `json:"start"`
	} `json:"source_limits"`
	Syslog struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		Proto string `json:"proto"`
	} `json:"syslog"`
	System struct {
		Load1      float64 `json:"load1"`
		MemTotalMB int     `json:"mem_total_mb"`
		MemFreeMB  int     `json:"mem_free_mb"`
		DataFreeMB int     `json:"data_free_mb"`
		DataSizeMB int     `json:"data_size_mb"`
		ExtraHours int     `json:"uptime_extra_hours"`
	} `json:"system"`
	Clock struct {
		OffsetMs      int64  `json:"offset_ms"`
		CheckedMinAgo int    `json:"checked_min_ago"`
		Server        string `json:"server"`
		SyncHoursAgo  int    `json:"last_sync_hours_ago"`
	} `json:"clock"`
	HiddenGroups []string `json:"hidden_service_groups"`
	AudioCards   []struct {
		ID   string `json:"id"`
		Name loc    `json:"name"`
	} `json:"audio_cards"`
	BTDevices []struct {
		Addr      string `json:"addr"`
		Name      string `json:"name"`
		Paired    bool   `json:"paired"`
		Connected bool   `json:"connected"`
		RSSI      int    `json:"rssi"`
	} `json:"bluetooth_devices"`
	Peers []struct {
		Name    loc    `json:"name"`
		URL     string `json:"url"`
		Online  bool   `json:"online"`
		Volume  int    `json:"volume"`
		TempC   int    `json:"temp_c"`
		Playing string `json:"playing"`
		Error   string `json:"error"`
	} `json:"peers"`
	FoundPeers []struct {
		ID   string `json:"id"`
		Name loc    `json:"name"`
		URL  string `json:"url"`
	} `json:"found_peers"`
	Logs []struct {
		Ago    int    `json:"ago_secs"`
		Source string `json:"source"`
		Text   loc    `json:"text"`
	} `json:"logs"`
	SSHKeys      []string           `json:"ssh_keys"`
	VendorSounds map[string]float64 `json:"vendor_sounds"`
	Tokens       []struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	} `json:"tokens"`
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
	now, err := time.ParseInLocation("2006-01-02 15:04", day+" "+sp.DemoTime, tz)
	if err != nil {
		log.Fatal(err)
	}
	dir, _ := os.MkdirTemp("", "leuchtfeuerd-demo")
	cfgPath := dir + "/config"
	c := sp.Config
	os.WriteFile(cfgPath, []byte(fmt.Sprintf("DEVICE_NAME=%q\nDHCP_HOSTNAME=%q\nBLUETOOTH_PAIRING=%q\nSENDSPIN_SERVER=\"%s:%d\"\nAIRPLAY=%q\nWEB_PASSWORD=%q\n",
		sp.Name, sp.Hostname, c.BluetoothPairing, sp.MQTT.Host, c.SendspinPort, c.Airplay, c.WebPassword)), 0o600)
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
		br := sp.Briefing
		s.Briefing = BriefingSettings{Lang: lang, Place: br.Place, Lat: br.Lat, Lon: br.Lon, TTS: br.TTS, Then: br.Then}
		for _, it := range br.Items {
			item := BriefItem{Type: it.Type, On: true, Name: it.Name.in(lang), URL: it.URL, Days: it.Days, Text: it.Text.in(lang)}
			if it.Type == "podcast" {
				item.Name, item.URL = podcastPresets[br.Podcast].Name, podcastPresets[br.Podcast].URL
			}
			s.Briefing.Items = append(s.Briefing.Items, item)
		}
		s.SetupDone = true // der Assistent ist über #/setup erreichbar
		s.HA = HASettings{URL: sp.HA.URL, Token: sp.HA.Token, TTSEngine: sp.HA.TTSEngine}
		v := sp.Voice
		s.Voice = VoiceSettings{Enabled: true, Port: v.Port, Mic: v.Mic, Mode: v.Mode, Area: v.Area.in(lang), DuckDB: v.DuckDB}
		s.Eq = EqSettings{Version: sp.Eq.Version, Bass: sp.Eq.Bass, Loudness: sp.Eq.Loudness, RoomOn: sp.Eq.RoomOn}
		for _, b := range sp.Eq.Room {
			s.Eq.Room = append(s.Eq.Room, PEQBand{Hz: b.Hz, DB: b.DB, Q: b.Q})
		}
		s.Sources.Limits = map[string]SourceLimit{}
		for src, l := range sp.SourceLimits {
			s.Sources.Limits[src] = SourceLimit{Max: l.Max, TrimDB: l.TrimDB, Start: l.Start}
		}
		s.Syslog = SyslogSettings{Enabled: true, Host: sp.Syslog.Host, Port: sp.Syslog.Port, Proto: sp.Syslog.Proto}
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
		y := sp.System
		return sysStatus{TempC: float64(sp.TempC), UptimeSecs: sp.UpDays*86400 + y.ExtraHours*3600, Load1: y.Load1,
			MemTotalMB: y.MemTotalMB, MemFreeMB: y.MemFreeMB, DataFreeMB: y.DataFreeMB, DataSizeMB: y.DataSizeMB}
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
			if slices.Contains(sp.HiddenGroups, d.Group) {
				continue
			}
			out = append(out, serviceInfo{serviceDef: d, Enabled: true, Running: running[d.Process] || (d.Group == "core")})
		}
		return out
	}
	a.clkFn = func() clockStatus {
		k := sp.Clock
		return clockStatus{OffsetMs: k.OffsetMs, Checked: now.Add(-time.Duration(k.CheckedMinAgo) * time.Minute), Server: k.Server,
			LastSync: now.Add(-time.Duration(k.SyncHoursAgo) * time.Hour), Synced: true}
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
	var cards strings.Builder
	for i, c := range sp.AudioCards { // im Format von /proc/asound/cards
		name := c.Name.in(lang)
		fmt.Fprintf(&cards, "%2d [%-15s]: %s - %s\n                      %s\n", i, c.ID, c.ID, name, name)
	}
	os.WriteFile(a.out.cardsFile, []byte(cards.String()), 0o644)
	a.out.confFile = func() string { return filepath.Join(dir, "output.conf") }
	a.out.restart = func() {}
	go func() {
		for {
			var devices []btDevice
			for _, d := range sp.BTDevices {
				devices = append(devices, btDevice{Addr: d.Addr, Name: d.Name, Paired: d.Paired, Connected: d.Connected, RSSI: d.RSSI})
			}
			a.out.Report(btReport{Devices: devices})
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
	a.voice.heard, a.voice.answer = sp.Voice.Heard.in(lang), sp.Voice.Answer.in(lang)
	// andere Lautsprecher
	a.peers = newPeerHub(a)
	var peers []Peer
	var statuses []peerStatus
	for _, p := range sp.Peers {
		peer := Peer{Name: p.Name.in(lang), URL: p.URL}
		peers = append(peers, peer)
		st := peerStatus{Peer: peer, Online: p.Online, Volume: p.Volume, TempC: p.TempC, Playing: p.Playing}
		if p.Online {
			st.Version = currentVersion()
		}
		if p.Error != "" {
			st.Error = peer.Name + ": " + p.Error
		}
		statuses = append(statuses, st)
	}
	st.Update(func(s *Settings) { s.Peers = peers })
	a.peers.statusFn = func() []peerStatus { return statuses }
	a.peers.found = map[string]foundPeer{}
	for _, f := range sp.FoundPeers {
		a.peers.found[f.ID] = foundPeer{Name: f.Name.in(lang), URL: f.URL, Version: currentVersion(), ID: f.ID}
	}
	// Protokolle, Schlüssel
	dataDir, logDir, hookLog = dir, dir+"/log", dir+"/hook.log"
	os.MkdirAll(logDir, 0o755)
	a.logs = newLogHub(nil)
	for _, l := range sp.Logs {
		os.WriteFile(logDir+"/"+l.Source+".log", nil, 0o644)
		a.logs.publish(logLine{now.Add(-time.Duration(l.Ago) * time.Second), l.Source, l.Text.in(lang)})
	}
	os.WriteFile(dir+"/authorized_keys", []byte(strings.Join(sp.SSHKeys, "\n")+"\n"), 0o600)
	// Klänge: nachgebildete Hersteller-Klänge, ein eigener Weckton
	sysRoot, _ := os.MkdirTemp("", "leuchtfeuer-demo-system") // nicht unter dem Datenverzeichnis: das überspringt die Suche
	sys := sysRoot + "/usr/share/harman/prompts"
	os.MkdirAll(sys, 0o755)
	for n, secs := range sp.VendorSounds {
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
	for _, t := range sp.Tokens {
		w.tokens.Create(t.Name, t.Scope)
	}
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
