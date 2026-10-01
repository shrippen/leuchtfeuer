package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type buttonEvent struct {
	Time  time.Time `json:"time"`
	Name  string    `json:"name"`
	Value string    `json:"value"`
	Do    string    `json:"action"`
}

// Nahtstellen: Die Wecker-/Timer-/Tastenlogik arbeitet gegen diese Schnittstellen und eine austauschbare Uhr,
// damit sie sich ohne Gerät prüfen lässt.
type audioCtl interface {
	OnChange(func()) // nach jeder Änderung (auch am Gerät)
	Get() (vol int, muted, known bool)
	Adjust(delta int) error
	SetVolume(target int) error
	SetMute(m bool) error
	ToggleMute() error
}

type playerCtl interface {
	Info() (kind, name, state, title string)
	PlayURL(kind, name, url string)
	PlayTone(kind, name string, seq []note, repeat bool)
	Stop()
}

type ledAPI interface {
	Animate(name string, repeat bool)
	Off()
}

type app struct {
	cfg     *shellConfig
	st      *store
	h       *hub // Router der Hersteller-Software (nur Invoke, target_invoke.go)
	hOnce   sync.Once
	bus     *localBus // eigene Programme (btagent, castrecv), bus.go
	vol     audioCtl
	pl      playerCtl
	led     ledAPI
	sch     *scheduler
	wifi    *wifiWatch
	mq      *mqttBridge
	viz     *visualizer // nil im Demo-Modus
	src     *sourceHub
	mix     *mixSync // nil im Demo-Modus
	ann     *announcer
	eq      *eqWriter
	brief   *briefing // Morgen-Briefing (briefing.go)
	rb      *radioBrowser
	meas    *measurer   // Raum einmessen (measure.go)
	sounds  *soundStore // Klänge austauschen (sounds.go)
	voice   *voiceSat   // Sprachassistent (voice.go)
	peers   *peerHub    // andere Leuchtfeuer (peers.go)
	logs    *logHub     // Protokolle live, Syslog, Aussetzer (logs.go)
	started time.Time
	version string

	clock  func() time.Time
	sysFn  func() sysStatus
	svcFn  func() []serviceInfo
	clkFn  func() clockStatus
	wifiFn func() wifiStatus
	btFn   func() btState
	mqttOK func() bool

	mu       sync.Mutex
	buttons  []buttonEvent
	lastRadi int
	presses  map[string]*pressState
	listener map[int]func(kind string, data map[string]any)
	nextL    int
}

func newApp(cfg *shellConfig, st *store) *app {
	a := &app{cfg: cfg, st: st, started: time.Now(), version: version}
	a.clock = time.Now
	a.bus = newLocalBus(a)
	a.src = newSourceHub(a)
	a.vol = hw.newVolume(a)
	a.vol.OnChange(func() {
		a.emit("volume", nil)
		if a.bus != nil {
			a.bus.Publish("volume", a.bus.volumeState())
		}
		go a.src.EnforceMax()
	})
	pl := newPlayer(map[string]string{"radio": "leuchtfeuer_radio", "": "leuchtfeuer_music"})
	pl.onChange = func() {
		kind, _, state, title := pl.Info()
		if kind == "radio" {
			a.src.Update("radio", state, map[string]string{"title": title})
		} else {
			a.src.Update("radio", "idle", nil)
		}
		a.emit("player", nil)
	}
	a.pl = pl
	a.ann = newAnnouncer(a, "leuchtfeuer_announce")
	a.led = noLED{}
	if hw.ring != nil {
		a.led = hw.ring.led(a)
	}
	a.rb = newRadioBrowser()
	a.meas = newMeasurer(a)
	a.sounds = newSoundStore()
	a.sch = newScheduler(a)
	a.wifi = newWifiWatch(a)
	a.mq = newMQTT(a)
	a.sysFn = cachedSys(collectSys, 5*time.Second)
	a.svcFn = func() []serviceInfo { return serviceStatus(cfg, time.Now().Unix()) }
	a.clkFn = clockState
	a.wifiFn = a.wifi.Status
	a.btFn = readBT
	a.mqttOK = a.mq.connected
	return a
}

// emit meldet ein Ereignis an alle Zuhörer (MQTT, ...).
func (a *app) emit(kind string, data map[string]any) {
	a.mu.Lock()
	ls := make([]func(string, map[string]any), 0, len(a.listener))
	for i := 0; i < a.nextL; i++ {
		if f, ok := a.listener[i]; ok {
			ls = append(ls, f)
		}
	}
	a.mu.Unlock()
	for _, f := range ls {
		f(kind, data)
	}
}

// Listen meldet einen Zuhörer an; die zurückgegebene Funktion meldet ihn wieder ab.
func (a *app) Listen(f func(kind string, data map[string]any)) func() {
	a.mu.Lock()
	if a.listener == nil {
		a.listener = map[int]func(string, map[string]any){}
	}
	id := a.nextL
	a.nextL++
	a.listener[id] = f
	a.mu.Unlock()
	return func() { a.mu.Lock(); delete(a.listener, id); a.mu.Unlock() }
}

// ---- Webradio ----

func (a *app) RadioPlay(idx int) error {
	set := a.st.Snapshot()
	if idx < 0 || idx >= len(set.Radio) {
		return fmt.Errorf("Sender %d gibt es nicht", idx)
	}
	a.mu.Lock()
	a.lastRadi = idx
	a.mu.Unlock()
	a.vol.SetMute(false)
	a.pl.PlayURL("radio", set.Radio[idx].Name, set.Radio[idx].URL)
	if a.rb != nil {
		a.rb.Click(set.Radio[idx].UUID)
	}
	return nil
}

// PlayStream spielt eine Adresse als Webradio, ohne sie zu speichern (Home Assistant, Vorhören in der Sendersuche).
func (a *app) PlayStream(name, url, uuid string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("Adresse muss mit http:// oder https:// beginnen")
	}
	if strings.TrimSpace(name) == "" {
		name = url
	}
	a.vol.SetMute(false)
	a.pl.PlayURL("radio", name, url)
	if a.rb != nil {
		a.rb.Click(uuid)
	}
	return nil
}

func (a *app) RadioStop() {
	if k, _, _, _ := a.pl.Info(); k == "radio" {
		a.pl.Stop()
	}
}

func (a *app) RadioIndex() int { a.mu.Lock(); defer a.mu.Unlock(); return a.lastRadi }

// ---- Tasten ----

// onButton: Tastendruck des Geräts (Treiber im Zielgerät).
func (a *app) onButton(name, val string) {
	if a.bus != nil {
		a.bus.Publish("button", map[string]string{"name": name, "value": val})
	}
	set := a.st.Snapshot()
	// Mehrfachdruck: Sind "double"/"triple" belegt, wartet ein kurzer Druck pressWindow auf weitere
	if val == "short" && (set.Buttons[name]["double"] != "" || set.Buttons[name]["triple"] != "") {
		a.multiPress(name)
		return
	}
	a.runButton(name, val, set)
}

// pressWindow: so lange nach einem kurzen Druck auf den nächsten warten (Doppel-/Dreifachdruck).
var pressWindow = 450 * time.Millisecond

func (a *app) multiPress(name string) {
	a.mu.Lock()
	if a.presses == nil {
		a.presses = map[string]*pressState{}
	}
	p := a.presses[name]
	if p == nil {
		p = &pressState{}
		a.presses[name] = p
	}
	p.n++
	if p.t != nil {
		p.t.Stop()
	}
	p.t = time.AfterFunc(pressWindow, func() {
		a.mu.Lock()
		n := p.n
		p.n, p.t = 0, nil
		a.mu.Unlock()
		val := map[int]string{1: "short", 2: "double"}[n]
		if n >= 3 {
			val = "triple"
		}
		a.runButton(name, val, a.st.Snapshot())
	})
	a.mu.Unlock()
}

type pressState struct {
	n int
	t *time.Timer
}

func (a *app) runButton(name, val string, set Settings) {
	action := set.Buttons[name][val]
	if action == "" && val != "double" && val != "triple" {
		action = set.Buttons[name]["short"]
	}
	// Drehrad und Bluetooth-Knopf bearbeiten audio-ui bzw. btagent selbst
	if name == "volumeup" || name == "volumedown" || name == "bluetooth" {
		action = ""
	}
	a.mu.Lock()
	a.buttons = append(a.buttons, buttonEvent{time.Now(), name, val, action})
	if len(a.buttons) > 30 {
		a.buttons = a.buttons[len(a.buttons)-30:]
	}
	a.mu.Unlock()
	a.emit("button", map[string]any{"button": name, "value": val})
	if action != "" && action != "none" {
		log.Printf("Taste %s/%s -> %s", name, val, action)
		if err := a.Do(action); err != nil {
			log.Printf("Aktion %s: %v", action, err)
		}
	}
}

func (a *app) Buttons() []buttonEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]buttonEvent{}, a.buttons...)
}

// Do führt eine benannte Aktion aus (Tasten, Weboberfläche, Home Assistant).
func (a *app) Do(action string) error {
	switch action {
	case "none", "":
		return nil
	case "mute_toggle":
		return a.vol.ToggleMute()
	case "volume_up":
		return a.vol.Adjust(5)
	case "volume_down":
		return a.vol.Adjust(-5)
	case "radio_toggle":
		if k, _, _, _ := a.pl.Info(); k == "radio" {
			a.RadioStop()
			return nil
		}
		return a.RadioPlay(a.RadioIndex())
	case "radio_next":
		n := len(a.st.Snapshot().Radio)
		if n == 0 {
			return nil
		}
		return a.RadioPlay((a.RadioIndex() + 1) % n)
	case "alarm_stop":
		a.sch.StopAlarm()
	case "alarm_snooze":
		a.sch.Snooze()
	case "timer_dismiss":
		a.sch.DismissTimer()
	case "timers_cancel":
		a.sch.CancelTimer("*")
	case "stop_all":
		if a.brief != nil {
			a.brief.Stop()
		}
		a.sch.StopAlarm()
		a.sch.DismissTimer()
		a.RadioStop()
	case "smart": // klingelnder Wecker: schlummern; klingelnder Timer: beenden; sonst Stumm umschalten
		if a.sch.Snooze() || a.sch.DismissTimer() {
			return nil
		}
		return a.vol.ToggleMute()
	case "bt_pairing":
		a.bus.Publish("bt-pairing", map[string]string{"action": "toggle"})
	case "sleep_toggle": // Schlummertimer 30 Minuten an / aus
		if a.sch.SleepRemaining() > 0 {
			a.sch.SetSleep(0)
		} else {
			a.sch.SetSleep(30)
		}
	case "chime":
		return a.ann.Play(announceReq{Tone: "chime"})
	case "radio_1", "radio_2", "radio_3", "radio_4", "radio_5":
		return a.RadioPlay(int(action[len(action)-1] - '1'))
	case "briefing":
		if a.brief == nil {
			return fmt.Errorf("Briefing nicht verfügbar")
		}
		return a.brief.Start("button")
	case "voice":
		if a.voice == nil {
			return fmt.Errorf("Sprachassistent nicht eingerichtet")
		}
		return a.voice.PushToTalk()
	case "voice_mute":
		if a.voice == nil {
			return fmt.Errorf("Sprachassistent nicht eingerichtet")
		}
		return a.voice.ToggleMute()
	default:
		return fmt.Errorf("unbekannte Aktion %q", action)
	}
	return nil
}

var actionNames = []string{"none", "smart", "mute_toggle", "volume_up", "volume_down", "radio_toggle", "radio_next",
	"alarm_stop", "alarm_snooze", "timer_dismiss", "timers_cancel", "stop_all", "bt_pairing", "sleep_toggle", "chime",
	"radio_1", "radio_2", "radio_3", "radio_4", "radio_5", "briefing", "voice", "voice_mute"}
