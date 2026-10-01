package main

import (
	"fmt"
	"log"
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
	h       *hub
	vol     audioCtl
	pl      playerCtl
	led     ledAPI
	sch     *scheduler
	wifi    *wifiWatch
	mq      *mqttBridge
	viz     *visualizer // nil im Demo-Modus
	started time.Time
	version string

	clock  func() time.Time
	sysFn  func() sysStatus
	wifiFn func() wifiStatus
	btFn   func() btState
	mqttOK func() bool
	wampOK func() bool

	mu       sync.Mutex
	buttons  []buttonEvent
	lastRadi int
	listener []func(kind string, data map[string]any)
}

func newApp(cfg *shellConfig, st *store) *app {
	a := &app{cfg: cfg, st: st, started: time.Now(), version: version}
	a.clock = time.Now
	a.h = newHub()
	vc := newVolumeCtl(a.h)
	vc.onChg = func() { a.emit("volume", nil) }
	a.vol = vc
	pl := newPlayer("invoke_music")
	pl.onChange = func() { a.emit("player", nil) }
	a.pl = pl
	a.led = &ledHW{h: a.h}
	a.sch = newScheduler(a)
	a.wifi = newWifiWatch(a)
	a.mq = newMQTT(a)
	a.sysFn = collectSys
	a.wifiFn = a.wifi.Status
	a.btFn = readBT
	a.mqttOK = a.mq.connected
	a.wampOK = a.h.Connected
	return a
}

// emit meldet ein Ereignis an alle Zuhörer (MQTT, ...).
func (a *app) emit(kind string, data map[string]any) {
	a.mu.Lock()
	ls := append([]func(string, map[string]any){}, a.listener...)
	a.mu.Unlock()
	for _, f := range ls {
		f(kind, data)
	}
}

func (a *app) Listen(f func(kind string, data map[string]any)) {
	a.mu.Lock()
	a.listener = append(a.listener, f)
	a.mu.Unlock()
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
	return nil
}

func (a *app) RadioStop() {
	if k, _, _, _ := a.pl.Info(); k == "radio" {
		a.pl.Stop()
	}
}

func (a *app) RadioIndex() int { a.mu.Lock(); defer a.mu.Unlock(); return a.lastRadi }

// ---- Tasten ----

func (a *app) onButton(args []any) {
	if len(args) < 1 {
		return
	}
	name, _ := args[0].(string)
	val := ""
	if len(args) > 1 {
		val = fmt.Sprint(args[1])
	}
	set := a.st.Snapshot()
	action := set.Buttons[name][val]
	if action == "" {
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
		a.sch.StopAlarm()
		a.sch.DismissTimer()
		a.RadioStop()
	case "smart": // klingelnder Wecker: schlummern; klingelnder Timer: beenden; sonst Stumm umschalten
		if a.sch.Snooze() || a.sch.DismissTimer() {
			return nil
		}
		return a.vol.ToggleMute()
	case "bt_pairing":
		a.h.Publish("invoke.bt.pairing", "toggle")
	default:
		return fmt.Errorf("unbekannte Aktion %q", action)
	}
	return nil
}

var actionNames = []string{"none", "smart", "mute_toggle", "volume_up", "volume_down", "radio_toggle", "radio_next",
	"alarm_stop", "alarm_snooze", "timer_dismiss", "timers_cancel", "stop_all", "bt_pairing"}
