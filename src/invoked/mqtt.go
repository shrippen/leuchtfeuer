package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Home-Assistant-Anbindung über MQTT mit automatischer Erkennung (MQTT Discovery). Home Assistant kennt über MQTT
// keinen Media-Player; angeboten werden deshalb Lautstärke (number), Stumm (switch), Radio-Sender (select),
// Bluetooth-Kopplung (switch), Wecker-/Timer-Tasten (button), Timer (number), Messwerte (sensor) und
// Tastendrücke (event).

type mqttBridge struct {
	app    *app
	mu     sync.Mutex
	c      mqtt.Client
	cfgKey string
	id     string
	base   string
}

func newMQTT(a *app) *mqttBridge {
	m := &mqttBridge{app: a}
	b, _ := os.ReadFile("/sys/class/net/wlan0/address")
	m.id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(string(b)), ":", ""))
	if m.id == "" {
		m.id = "invoke"
	}
	m.base = "invoke/" + m.id
	a.Listen(m.onEvent)
	return m
}

func (m *mqttBridge) connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.c != nil && m.c.IsConnectionOpen()
}

func (m *mqttBridge) Run() {
	for {
		s := m.app.st.Snapshot().MQTT
		key := fmt.Sprintf("%v|%s|%d|%s|%s|%s|%s", s.Enabled, s.Host, s.Port, s.User, s.Pass, s.Discovery, m.app.cfg.Get("DEVICE_NAME", "HK Invoke"))
		m.mu.Lock()
		changed := key != m.cfgKey
		m.mu.Unlock()
		if changed {
			m.disconnect()
			m.mu.Lock()
			m.cfgKey = key
			m.mu.Unlock()
			if s.Enabled && s.Host != "" {
				m.connect(s)
			}
		}
		if m.connected() {
			m.publishState()
		}
		time.Sleep(15 * time.Second)
	}
}

func (m *mqttBridge) disconnect() {
	m.mu.Lock()
	c := m.c
	m.c = nil
	m.mu.Unlock()
	if c != nil {
		m.publish("status", "offline", true)
		c.Disconnect(250)
	}
}

func (m *mqttBridge) publish(sub, payload string, retain bool) {
	m.mu.Lock()
	c := m.c
	m.mu.Unlock()
	if c != nil && c.IsConnectionOpen() {
		c.Publish(m.base+"/"+sub, 0, retain, payload)
	}
}

func (m *mqttBridge) connect(s MQTTSettings) {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", s.Host, s.Port)).
		SetClientID("invoke-"+m.id).
		SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(10*time.Second).
		SetKeepAlive(30*time.Second).
		SetWill(m.base+"/status", "offline", 0, true)
	if s.User != "" {
		opts.SetUsername(s.User).SetPassword(s.Pass)
	}
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("MQTT verbunden (%s:%d)", s.Host, s.Port)
		m.subscribe(c)
		m.discover(s.Discovery)
		m.publish("status", "online", true)
		m.publishState()
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) { log.Printf("MQTT getrennt: %v", err) })
	c := mqtt.NewClient(opts)
	c.Connect() // Wiederholung übernimmt die Bibliothek (ConnectRetry)
	m.mu.Lock()
	m.c = c
	m.mu.Unlock()
}

func (m *mqttBridge) subscribe(c mqtt.Client) {
	c.Subscribe(m.base+"/+/set", 0, func(_ mqtt.Client, msg mqtt.Message) {
		parts := strings.Split(msg.Topic(), "/")
		if len(parts) < 2 {
			return
		}
		m.command(parts[len(parts)-2], strings.TrimSpace(string(msg.Payload())))
	})
	c.Subscribe(m.base+"/+/press", 0, func(_ mqtt.Client, msg mqtt.Message) {
		parts := strings.Split(msg.Topic(), "/")
		m.command(parts[len(parts)-2], "PRESS")
	})
}

func (m *mqttBridge) command(name, payload string) {
	a := m.app
	switch name {
	case "volume":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			a.vol.SetVolume(int(v))
		}
	case "mute":
		a.vol.SetMute(payload == "ON")
	case "radio":
		if payload == "off" || payload == "" {
			a.RadioStop()
			return
		}
		for i, p := range a.st.Snapshot().Radio {
			if p.Name == payload {
				a.RadioPlay(i)
			}
		}
	case "pairing":
		a.h.Publish("invoke.bt.pairing", map[bool]string{true: "open", false: "close"}[payload == "ON"])
	case "timer_minutes":
		if v, err := strconv.ParseFloat(payload, 64); err == nil && v > 0 {
			a.sch.AddTimer("", int(v*60))
		}
	case "alarm_stop":
		a.sch.StopAlarm()
	case "alarm_snooze":
		a.sch.Snooze()
	case "timer_dismiss":
		a.sch.DismissTimer()
	}
	m.publishState()
}

func (m *mqttBridge) onEvent(kind string, data map[string]any) {
	if !m.connected() {
		return
	}
	switch kind {
	case "button":
		b, _ := json.Marshal(map[string]any{"event_type": data["button"], "value": data["value"]})
		m.publish("button", string(b), false)
	case "alarm", "timer":
		b, _ := json.Marshal(data)
		m.publish(kind, string(b), false)
		m.publishState()
	default:
		m.publishState()
	}
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (m *mqttBridge) publishState() {
	if !m.connected() {
		return
	}
	a := m.app
	vol, muted, _ := a.vol.Get()
	m.publish("volume", strconv.Itoa(vol), true)
	m.publish("mute", onOff(muted), true)
	kind, name, state, title := a.pl.Info()
	radio := "off"
	if kind == "radio" {
		radio = name
	}
	m.publish("radio", radio, true)
	m.publish("player", map[bool]string{true: kind + ": " + name, false: "idle"}[kind != ""], true)
	m.publish("title", title, true)
	_ = state
	ws := a.wifiFn()
	m.publish("wifi_rssi", strconv.Itoa(ws.RSSI), true)
	m.publish("wifi_loss", strconv.Itoa(ws.LossPct), true)
	sys := a.sysFn()
	m.publish("temperature", strconv.FormatFloat(sys.TempC, 'f', 0, 64), true)
	m.publish("uptime", strconv.Itoa(sys.UptimeSecs), true)
	if t, _ := a.sch.NextAlarm(); !t.IsZero() {
		m.publish("next_alarm", t.Format(time.RFC3339), true)
	} else {
		m.publish("next_alarm", "unknown", true)
	}
	m.publish("alarm_state", a.sch.State().State, true)
	m.publish("pairing", onOff(a.btFn().Open), true)
	m.publish("timers", strconv.Itoa(len(a.st.Snapshot().Timers)), true)
}

func (m *mqttBridge) discover(prefix string) {
	a := m.app
	dev := map[string]any{
		"identifiers":  []string{"invoke_" + m.id},
		"name":         a.cfg.Get("DEVICE_NAME", "HK Invoke"),
		"manufacturer": "Harman Kardon",
		"model":        "Invoke (Leuchtfeuer)",
		"sw_version":   a.version,
	}
	put := func(comp, obj string, cfg map[string]any) {
		cfg["unique_id"] = "invoke_" + m.id + "_" + obj
		cfg["object_id"] = "invoke_" + obj
		cfg["device"] = dev
		cfg["availability_topic"] = m.base + "/status"
		b, _ := json.Marshal(cfg)
		m.mu.Lock()
		c := m.c
		m.mu.Unlock()
		if c != nil {
			c.Publish(fmt.Sprintf("%s/%s/invoke_%s/%s/config", prefix, comp, m.id, obj), 0, true, b)
		}
	}
	T := func(s string) string { return m.base + "/" + s }
	stations := []string{"off"}
	for _, p := range a.st.Snapshot().Radio {
		stations = append(stations, p.Name)
	}
	put("number", "volume", map[string]any{"name": "Lautstärke", "state_topic": T("volume"), "command_topic": T("volume/set"),
		"min": 0, "max": 100, "step": 1, "icon": "mdi:volume-high", "unit_of_measurement": "%"})
	put("switch", "mute", map[string]any{"name": "Stumm", "state_topic": T("mute"), "command_topic": T("mute/set"), "icon": "mdi:volume-off"})
	put("select", "radio", map[string]any{"name": "Webradio", "state_topic": T("radio"), "command_topic": T("radio/set"), "options": stations, "icon": "mdi:radio"})
	put("sensor", "player", map[string]any{"name": "Wiedergabe", "state_topic": T("player"), "icon": "mdi:play-circle"})
	put("sensor", "title", map[string]any{"name": "Titel", "state_topic": T("title"), "icon": "mdi:music-note"})
	put("switch", "pairing", map[string]any{"name": "Bluetooth-Kopplung offen", "state_topic": T("pairing"), "command_topic": T("pairing/set"), "icon": "mdi:bluetooth-connect"})
	put("number", "timer_minutes", map[string]any{"name": "Timer starten", "command_topic": T("timer_minutes/set"), "min": 1, "max": 600, "step": 1, "unit_of_measurement": "min", "icon": "mdi:timer-outline", "mode": "box"})
	put("sensor", "timers", map[string]any{"name": "Laufende Timer", "state_topic": T("timers"), "icon": "mdi:timer-sand"})
	put("button", "alarm_stop", map[string]any{"name": "Wecker stoppen", "command_topic": T("alarm_stop/press"), "icon": "mdi:alarm-off"})
	put("button", "alarm_snooze", map[string]any{"name": "Wecker schlummern", "command_topic": T("alarm_snooze/press"), "icon": "mdi:alarm-snooze"})
	put("button", "timer_dismiss", map[string]any{"name": "Timer beenden", "command_topic": T("timer_dismiss/press"), "icon": "mdi:timer-off"})
	put("sensor", "next_alarm", map[string]any{"name": "Nächster Wecker", "state_topic": T("next_alarm"), "device_class": "timestamp", "icon": "mdi:alarm"})
	put("sensor", "alarm_state", map[string]any{"name": "Wecker-Zustand", "state_topic": T("alarm_state"), "icon": "mdi:alarm-light"})
	put("sensor", "wifi_rssi", map[string]any{"name": "WLAN-Signal", "state_topic": T("wifi_rssi"), "device_class": "signal_strength", "unit_of_measurement": "dBm", "entity_category": "diagnostic"})
	put("sensor", "wifi_loss", map[string]any{"name": "WLAN-Paketverlust", "state_topic": T("wifi_loss"), "unit_of_measurement": "%", "icon": "mdi:wifi-alert", "entity_category": "diagnostic"})
	put("sensor", "temperature", map[string]any{"name": "Temperatur", "state_topic": T("temperature"), "device_class": "temperature", "unit_of_measurement": "°C", "entity_category": "diagnostic"})
	put("sensor", "uptime", map[string]any{"name": "Laufzeit", "state_topic": T("uptime"), "device_class": "duration", "unit_of_measurement": "s", "entity_category": "diagnostic"})
	put("event", "button", map[string]any{"name": "Taste", "state_topic": T("button"), "event_types": []string{"mic", "bluetooth", "volumeup", "volumedown", "reset", "play", "mute"}})
}
