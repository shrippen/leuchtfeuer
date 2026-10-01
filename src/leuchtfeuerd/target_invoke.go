package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
)

// Harman Kardon Invoke: Lautstärke, Stumm, Tasten und Ring-Animationen führt die Hersteller-Software audio-ui; sie
// ist über ihren WAMP-Router (bonefish, 127.0.0.1:9999) erreichbar. Das Drehrad setzt den ALSA-Regler "system", den
// leuchtfeuerd auf "Leuchtfeuer Music" spiegelt. Der Leuchtring hängt als MCU an /dev/i2c-0 (Adresse 0x36); einzelne
// Bilder schreibt leuchtfeuerd direkt (0e 01 + 13 x RGB), Animationen spielt mcu-interface aus /usr/share/lights.

var invokeWAMP = struct {
	VolumeChanged, MuteChanged, InputEvent       string
	VolumeGet, VolumeAdjust, MuteSet, MuteToggle string
	LEDAnimate, LEDOff                           string
}{
	VolumeChanged: "com.harman.volumeChanged", MuteChanged: "com.harman.musicMuteChanged", InputEvent: "com.harman.test.inputEvent",
	VolumeGet: "com.harman.volumeGet", VolumeAdjust: "com.harman.volumeAdjust", MuteSet: "com.harman.musicMuteSet",
	MuteToggle: "com.harman.musicMuteToggle", LEDAnimate: "com.harman.ledAnimate", LEDOff: "com.harman.ledOff",
}

// eigene Animationsnamen -> Muster der Hersteller-Software
var invokeAnimations = map[string]string{
	"alarm": "L_111_c_alarm", "timer": "L_112_c_timer", "success": "L_106_c_success",
	"bt_open": "L_106_c_success", "bt_closed": "L_312_d_shorttap",
}

const (
	invokeRingDev  = "/dev/i2c-0"
	invokeRingAddr = 0x36
)

func init() {
	registerTarget(target{
		ID: "invoke", Manufacturer: "Harman Kardon", Model: "Invoke", DefaultName: "HK Invoke",
		MixerCard: "0", MirrorCtl: "system", TempPath: "/sys/class/hwmon/hwmon0/device/tsen_temp", TempDiv: 1, WifiIface: "wlan0",
		SoundDirs:   []string{"/usr/share", "/usr/local/share", "/etc", "/opt"},
		Link:        "audio-ui (WAMP)",
		ButtonNames: []string{"mic", "bluetooth", "volumeup", "volumedown", "reset", "play", "mute"},
		newVolume:   func(a *app) audioCtl { return newVolumeCtl(a.wamp()) },
		start:       startInvoke,
		linkOK:      func(a *app) bool { return a.wamp().Connected() },
		buttons:     true,
		ring: &ringSpec{
			writer: func() ringWriter { return &ringI2C{} },
			led:    func(a *app) ledAPI { return &ledHW{h: a.wamp()} },
		},
	})
}

// wamp: der Router der Hersteller-Software (nur auf dem Invoke; einmal angelegt).
func (a *app) wamp() *hub {
	a.hOnce.Do(func() { a.h = newHub() })
	return a.h
}

func startInvoke(a *app) {
	h := a.wamp()
	h.Subscribe(invokeWAMP.InputEvent, func(args []any) {
		if len(args) < 1 {
			return
		}
		name, _ := args[0].(string)
		val := ""
		if len(args) > 1 {
			val = fmt.Sprint(args[1])
		}
		a.onButton(name, val)
	})
	if a.mix != nil {
		h.Subscribe(invokeWAMP.VolumeChanged, func([]any) { a.mix.Kick() })
		h.Subscribe(invokeWAMP.MuteChanged, func([]any) { a.mix.Kick() })
	}
	go h.Run()
}

// ---- Lautstärke über audio-ui ----

type volumeCtl struct {
	h     *hub
	mu    sync.Mutex
	vol   int
	muted bool
	known bool
	onChg func()
}

func newVolumeCtl(h *hub) *volumeCtl {
	v := &volumeCtl{h: h}
	h.Subscribe(invokeWAMP.VolumeChanged, func(a []any) {
		if len(a) >= 2 {
			if g, _ := a[0].(string); g == "music" {
				v.set(toInt(a[1]), nil)
			}
		}
	})
	h.Subscribe(invokeWAMP.MuteChanged, func(a []any) {
		if len(a) >= 1 {
			if m, ok := a[0].(bool); ok {
				v.set(-1, &m)
			}
		}
	})
	h.OnConnect(func() { v.refresh() })
	return v
}

func (v *volumeCtl) OnChange(f func()) { v.mu.Lock(); v.onChg = f; v.mu.Unlock() }

func (v *volumeCtl) set(vol int, mute *bool) {
	v.mu.Lock()
	if vol >= 0 {
		v.vol = vol
	}
	if mute != nil {
		v.muted = *mute
	}
	v.known = true
	cb := v.onChg
	v.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (v *volumeCtl) Get() (int, bool, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vol, v.muted, v.known
}

// refresh liest den aktuellen Zustand (volumeGet liefert ihn als Schlüssel-Wert-Struktur).
func (v *volumeCtl) refresh() {
	_, kw, err := v.h.CallKw(invokeWAMP.VolumeGet, nil)
	if err != nil || kw == nil {
		return
	}
	if m := toStrMap(kw["music"]); m != nil {
		mute := toInt(m["mute"]) != 0
		v.set(toInt(m["volume"]), &mute)
	}
}

// Adjust ändert die Lautstärke um delta Prozentpunkte (audio-ui begrenzt auf 0..100).
func (v *volumeCtl) Adjust(delta int) error {
	r, err := v.h.Call(invokeWAMP.VolumeAdjust, delta)
	if err == nil && len(r) >= 1 {
		v.set(toInt(r[0]), nil)
	}
	return err
}

// SetVolume setzt die Lautstärke absolut (Differenz zum aktuellen Wert).
func (v *volumeCtl) SetVolume(target int) error {
	target = clamp(target, 0, 100)
	r, err := v.h.Call(invokeWAMP.VolumeAdjust, 0)
	if err != nil || len(r) < 1 {
		return err
	}
	cur := toInt(r[0])
	if cur == target {
		return nil
	}
	return v.Adjust(target - cur)
}

func (v *volumeCtl) SetMute(m bool) error {
	_, err := v.h.Call(invokeWAMP.MuteSet, m)
	if err == nil {
		v.set(-1, &m)
	}
	return err
}

func (v *volumeCtl) ToggleMute() error {
	r, err := v.h.Call(invokeWAMP.MuteToggle)
	if err == nil && len(r) >= 1 {
		if m, ok := r[0].(bool); ok {
			v.set(-1, &m)
		}
	}
	return err
}

// ---- Ring-Animationen über audio-ui/mcu-interface ----

type ledHW struct{ h *hub }

func (l *ledHW) Animate(name string, repeat bool) {
	p, ok := invokeAnimations[name]
	if !ok {
		return
	}
	var kw map[string]any
	if repeat {
		kw = map[string]any{"repeat": true}
	}
	go l.h.CallKw(invokeWAMP.LEDAnimate, kw, p)
}

func (l *ledHW) Off() { go l.h.Call(invokeWAMP.LEDOff) }

// ---- einzelne Ring-Bilder über I²C ----

type ringI2C struct {
	f      *os.File
	errLog time.Time
}

const (
	i2cSlave      = 0x0703
	i2cSlaveForce = 0x0706
)

func (r *ringI2C) Write(fr *ringFrame) error {
	if r.f == nil {
		f, err := os.OpenFile(invokeRingDev, os.O_RDWR, 0)
		if err != nil {
			return r.fail(err)
		}
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), i2cSlave, invokeRingAddr); e != 0 {
			if _, _, e = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), i2cSlaveForce, invokeRingAddr); e != 0 {
				f.Close()
				return r.fail(e)
			}
		}
		r.f = f
	}
	var b [2 + 13*3]byte // 13 LEDs, die Muster nutzen 12 (vizLEDs)
	b[0], b[1] = 0x0e, 0x01
	for i := range fr {
		copy(b[2+i*3:], fr[i][:])
	}
	if _, err := r.f.Write(b[:]); err != nil {
		r.f.Close()
		r.f = nil
		return r.fail(err)
	}
	return nil
}

func (r *ringI2C) fail(err error) error {
	if time.Since(r.errLog) > time.Minute {
		r.errLog = time.Now()
		log.Printf("Leuchtring (I2C): %v", err)
	}
	return errors.Join(errors.New("Leuchtring"), err)
}
