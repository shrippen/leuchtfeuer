package main

import (
	"log"
	"os"
	"runtime"
	"strings"
)

// Zielgeräte. Leuchtfeuer selbst (Wecker, Radio, Quellen, Klang, Briefing, Weboberfläche, Home Assistant ...) kennt
// kein bestimmtes Gerät; ein Zielgerät liefert nur, was es hat:
//
//   - Pflicht: Lautstärke und Stumm (audioCtl). Entweder führt die Hersteller-Software sie (Invoke: audio-ui,
//     Drehrad) und leuchtfeuerd spiegelt ihren Regler auf "Leuchtfeuer Music" (MirrorCtl), oder leuchtfeuerd führt
//     sie selbst und schreibt "Leuchtfeuer Music" direkt (generic).
//   - Erweiterung Tasten: Das Gerät meldet Tastendrücke (a.onButton); Tastenbelegung und Mehrfachdruck sind allgemein.
//   - Erweiterung Leuchtring: Bilder schreiben (Visualizer, Lichtwecker, Timer, Sprachassistent) und
//     Animationen mit eigenen Namen (alarm, timer, success, bt_open, bt_closed) abspielen.
//
// Die Oberfläche, MQTT und die API zeigen nur, was das Gerät kann (Status: device.capabilities).
// Auswahl: LEUCHTFEUER_TARGET (setzt der Hook aus targets/<ziel>/target.sh), sonst TARGET in der Shell-Konfiguration. Ein neues Gerät: target_<name>.go mit einem Eintrag in targets
// (Anleitung: docs/TARGETS.md).

type target struct {
	ID           string
	Manufacturer string
	Model        string
	DefaultName  string   // Gerätename, solange DEVICE_NAME fehlt
	DefaultHost  string   // Name im Netz, solange DHCP_HOSTNAME fehlt ("" = Rechnername)
	MixerCard    string   // ALSA-Karte der eigenen Softvol-Regler
	MirrorCtl    string   // Regler der Hersteller-Software, der nach "Leuchtfeuer Music" kopiert wird ("" = keiner)
	TempPath     string   // Temperatur (Datei mit Zahl)
	TempDiv      float64  // Teiler auf °C (1 oder 1000 für Milligrad)
	WifiIface    string   // WLAN-Schnittstelle (wpa_cli, Gerätekennung, mDNS)
	SoundDirs    []string // Klänge der Hersteller-Software (sounds.go); leer = keine
	Link         string   // Verbindung zur Hersteller-Software für die Statusanzeige ("" = keine)
	ButtonNames  []string // Namen der Tasten (Home Assistant: Ereignis-Typen)
	Package      string   // Endung des Release-Pakets (Updates): leuchtfeuer-<Version>-<Package>.tar.gz

	newVolume func(a *app) audioCtl // Lautstärke (Pflicht)
	start     func(a *app)          // Treiber starten (Ereignisse, Tasten); nil = nichts
	linkOK    func(a *app) bool     // Verbindung zur Hersteller-Software steht
	buttons   bool                  // meldet Tasten
	ring      *ringSpec             // Leuchtring; nil = keiner
}

type ringSpec struct {
	writer func() ringWriter   // einzelne Bilder (Visualizer, Szenen)
	led    func(a *app) ledAPI // Animationen mit eigenen Namen
}

var targets = map[string]target{}

// defaultTarget: ohne TARGET in der Konfiguration (ältere Installationen kennen den Schalter nicht).
const defaultTarget = "invoke"

func registerTarget(t target) {
	targets[t.ID] = t
	if t.ID == defaultTarget {
		hw = t
	}
}

// packageArch: Architektur im Paketnamen (generic: generic-amd64, generic-arm64, generic-armv7).
func packageArch() string {
	if runtime.GOARCH == "arm" {
		return "armv7"
	}
	return runtime.GOARCH
}

// hw: das Zielgerät dieses Laufs.
var hw target

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

// capabilities: Erweiterungen des Geräts (für Oberfläche, MQTT, API).
func (t target) capabilities() []string {
	c := []string{}
	if t.buttons {
		c = append(c, "buttons")
	}
	if t.ring != nil {
		c = append(c, "ring")
	}
	if len(t.SoundDirs) > 0 {
		c = append(c, "vendorSounds")
	}
	if t.MirrorCtl != "" {
		c = append(c, "vendorVolume")
	}
	return c
}

func (t target) has(c string) bool {
	for _, x := range t.capabilities() {
		if x == c {
			return true
		}
	}
	return false
}

type deviceStatus struct {
	ID           string   `json:"id"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
	Link         string   `json:"link"`   // z. B. "audio-ui (WAMP)"; leer = keine Hersteller-Software
	LinkOK       bool     `json:"linkOK"` // verbunden
}

// noLED: Gerät ohne Leuchtring.
type noLED struct{}

func (noLED) Animate(string, bool) {}
func (noLED) Off()                 {}

// hostName: Name im Netz (DHCP, Zertifikat, Syslog). DHCP_HOSTNAME, sonst Vorgabe des Ziels, sonst der Rechnername.
func hostName(c *shellConfig) string {
	if n := c.Get("DHCP_HOSTNAME", ""); n != "" {
		return n
	}
	if hw.DefaultHost != "" {
		return hw.DefaultHost
	}
	if n, err := os.Hostname(); err == nil && n != "" {
		return n
	}
	return "leuchtfeuer"
}
