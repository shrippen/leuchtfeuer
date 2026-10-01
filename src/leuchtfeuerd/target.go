package main

import (
	"log"
	"strings"
)

// Zielgerät: Was an der Hardware und der Hersteller-Software eines Lautsprechers hängt, steht hier an einer Stelle,
// nicht verstreut im Code. Heute gibt es nur den Harman Kardon Invoke. Ein weiterer Lautsprecher bekommt einen eigenen
// Eintrag; wo er etwas grundsätzlich anders macht (Lautstärke ohne WAMP-Router, Ring ohne I²C), kommen die
// Schnittstellen audioCtl, ledAPI und mixerCtl (app.go, mixer.go) mit eigener Umsetzung dazu.
// Auswahl: TARGET in der Shell-Konfiguration (Standard "invoke").

type wampNames struct {
	VolumeChanged, MuteChanged, InputEvent       string // Ereignisse
	VolumeGet, VolumeAdjust, MuteSet, MuteToggle string // Aufrufe
	LEDAnimate, LEDOff                           string
}

type target struct {
	ID           string
	Manufacturer string
	Model        string
	DefaultName  string            // Gerätename, solange DEVICE_NAME fehlt
	MixerCard    string            // ALSA-Karte der eigenen Softvol-Regler
	SystemCtl    string            // Regler, den die Hersteller-Software beim Drehen setzt (wird nach "Leuchtfeuer Music" kopiert)
	TempPath     string            // SoC-Temperatur in °C
	WifiIface    string            // WLAN-Schnittstelle (wpa_cli, MAC als Gerätekennung, mDNS)
	RingDev      string            // I²C-Gerät des Leuchtring-Controllers ("" = kein Ring)
	RingAddr     int               // I²C-Adresse
	WAMP         wampNames         // Hersteller-Router (audio-ui)
	Animations   map[string]string // eigene Namen -> Ring-Animation der Hersteller-Software
}

var targets = map[string]target{
	"invoke": {
		ID: "invoke", Manufacturer: "Harman Kardon", Model: "Invoke", DefaultName: "HK Invoke",
		MixerCard: "0", SystemCtl: "system", TempPath: "/sys/class/hwmon/hwmon0/device/tsen_temp", WifiIface: "wlan0",
		RingDev: "/dev/i2c-0", RingAddr: 0x36, // 13 LEDs, die Muster nutzen 12 (vizLEDs)
		WAMP: wampNames{
			VolumeChanged: "com.harman.volumeChanged", MuteChanged: "com.harman.musicMuteChanged", InputEvent: "com.harman.test.inputEvent",
			VolumeGet: "com.harman.volumeGet", VolumeAdjust: "com.harman.volumeAdjust", MuteSet: "com.harman.musicMuteSet",
			MuteToggle: "com.harman.musicMuteToggle", LEDAnimate: "com.harman.ledAnimate", LEDOff: "com.harman.ledOff",
		},
		Animations: map[string]string{"alarm": "L_111_c_alarm", "timer": "L_112_c_timer", "success": "L_106_c_success"},
	},
}

// hw: das Zielgerät dieses Laufs.
var hw = targets["invoke"]

func selectTarget(id string) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return
	}
	t, ok := targets[id]
	if !ok {
		log.Printf("Zielgerät %q unbekannt, nehme %q", id, hw.ID)
		return
	}
	hw = t
}

// anim liefert die Ring-Animation des Zielgeräts für einen eigenen Namen ("" = keine).
func anim(name string) string { return hw.Animations[name] }
