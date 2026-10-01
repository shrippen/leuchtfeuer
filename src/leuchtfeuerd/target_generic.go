package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// generic: ein gewöhnliches Linux mit ALSA (Raspberry Pi, alter Rechner, Container), ohne Hersteller-Software,
// Tasten oder Leuchtring. leuchtfeuerd führt die Lautstärke selbst: Sie ist der Softvol-Regler "Leuchtfeuer Music"
// der eigenen Tonkette (0 ... 100 % linear in dB über die 51 dB des Reglers, 0 % = stumm) und wird in
// volume.json gespeichert. Ausgabe, Karte und WLAN-Schnittstelle kommen aus der Konfiguration
// (targets/generic/asound-target.conf, ALSA_CARD, WIFI_IFACE).

func init() {
	registerTarget(target{
		ID: "generic", Manufacturer: "Leuchtfeuer", Model: "Linux", DefaultName: "Leuchtfeuer",
		MixerCard: "0", TempPath: "/sys/class/thermal/thermal_zone0/temp", TempDiv: 1000, WifiIface: "wlan0",
		newVolume: func(a *app) audioCtl { return newSoftVolume(a, filepath.Join(dataDir, "volume.json")) },
		start:     func(a *app) { a.vol.(*softVolume).apply() },
	})
}

type softVolume struct {
	a     *app
	path  string
	mu    sync.Mutex
	Vol   int  `json:"volume"`
	Muted bool `json:"muted"`
	onChg func()
}

func newSoftVolume(a *app, path string) *softVolume {
	v := &softVolume{a: a, path: path, Vol: 30}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, v)
	}
	v.Vol = clamp(v.Vol, 0, 100)
	return v
}

func (v *softVolume) OnChange(f func()) { v.mu.Lock(); v.onChg = f; v.mu.Unlock() }

func (v *softVolume) Get() (int, bool, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.Vol, v.Muted, true
}

// level: Wert des Softvol-Reglers (0 ... 255).
func (v *softVolume) level() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Muted || v.Vol <= 0 {
		return 0
	}
	return softvolMax * v.Vol / 100
}

// apply schreibt den Regler (über den Abgleich, der ihn auch anlegt) und meldet die Änderung.
func (v *softVolume) apply() {
	if v.a.mix != nil {
		v.a.mix.SetMaster(v.level())
	}
	v.mu.Lock()
	b, _ := json.Marshal(v)
	cb := v.onChg
	v.mu.Unlock()
	tmp := v.path + ".new"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, v.path)
	}
	if cb != nil {
		cb()
	}
}

func (v *softVolume) change(f func()) error {
	v.mu.Lock()
	f()
	v.Vol = clamp(v.Vol, 0, 100)
	v.mu.Unlock()
	v.apply()
	return nil
}

func (v *softVolume) Adjust(d int) error    { return v.change(func() { v.Vol += d }) }
func (v *softVolume) SetVolume(t int) error { return v.change(func() { v.Vol = t }) }
func (v *softVolume) SetMute(m bool) error  { return v.change(func() { v.Muted = m }) }
func (v *softVolume) ToggleMute() error     { return v.change(func() { v.Muted = !v.Muted }) }
