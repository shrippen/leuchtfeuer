package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTargetsAndCapabilities(t *testing.T) {
	inv, gen := targets["invoke"], targets["generic"]
	if hw.ID != "invoke" {
		t.Fatalf("Standard-Zielgerät %q", hw.ID)
	}
	if c := strings.Join(inv.capabilities(), ","); c != "buttons,ring,vendorSounds,vendorVolume" {
		t.Fatalf("Invoke kann: %s", c)
	}
	if c := gen.capabilities(); len(c) != 0 {
		t.Fatalf("generic kann: %v", c)
	}
	old := hw
	t.Cleanup(func() { hw = old })
	selectTarget("GENERIC")
	if hw.ID != "generic" || hw.has("ring") {
		t.Fatalf("Auswahl: %+v", hw.ID)
	}
	selectTarget("toaster") // unbekannt: bleibt
	if hw.ID != "generic" {
		t.Fatal("unbekanntes Ziel übernommen")
	}
}

// generic: leuchtfeuerd führt die Lautstärke selbst ("Leuchtfeuer Music"), gespeichert über Neustarts.
func TestGenericSoftVolume(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	m := &fakeMixer{ctl: map[string]int{"Leuchtfeuer Music": 0, "Leuchtfeuer Announce": 0}}
	ta.mix = newMixSync(m)
	ta.mix.mirror = ""
	path := filepath.Join(t.TempDir(), "volume.json")
	v := newSoftVolume(ta.app, path)
	changed := 0
	v.OnChange(func() { changed++ })
	if vol, muted, known := v.Get(); vol != 30 || muted || !known {
		t.Fatalf("Start: %d %v %v", vol, muted, known)
	}
	v.SetVolume(80)
	ta.mix.sync(true)
	if m.ctl["Leuchtfeuer Music"] != 204 || m.ctl["Leuchtfeuer Announce"] != 204 {
		t.Fatalf("Regler: %v", m.ctl)
	}
	v.Adjust(30) // begrenzt auf 100
	v.ToggleMute()
	ta.mix.sync(false)
	if vol, muted, _ := v.Get(); vol != 100 || !muted || m.ctl["Leuchtfeuer Music"] != 0 || changed != 3 {
		t.Fatalf("stumm: %d %v %v %d", vol, muted, m.ctl, changed)
	}
	if w := newSoftVolume(ta.app, path); w.Vol != 100 || !w.Muted {
		t.Fatalf("nicht gespeichert: %+v", w)
	}
	// Invoke-Weg: der Regler der Hersteller-Software wird gespiegelt
	m2 := &fakeMixer{ctl: map[string]int{"system": 120, "Leuchtfeuer Music": 0, "Leuchtfeuer Announce": 0}}
	x := newMixSync(m2)
	x.sync(true)
	if m2.ctl["Leuchtfeuer Music"] != 120 {
		t.Fatalf("Spiegel: %v", m2.ctl)
	}
}

// Lokaler Bus: Ereignisse (Lautstärke zuerst, dann Vorrang, Tasten) und Befehle (Lautstärke, Quelle, Ring).
func TestLocalBus(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	led := &fakeLED{}
	ta.app.led = led
	sock := filepath.Join(t.TempDir(), "bus.sock")
	go ta.bus.Serve(sock)
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		if resp, err = hc.Get("http://bus/events"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	next := func() (string, map[string]any) {
		t.Helper()
		var ev string
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimRight(line, "\n")
			if strings.HasPrefix(line, "event: ") {
				ev = line[7:]
			} else if strings.HasPrefix(line, "data: ") {
				var d map[string]any
				json.Unmarshal([]byte(line[6:]), &d)
				return ev, d
			}
		}
	}
	if ev, d := next(); ev != "volume" || d["volume"] != float64(30) {
		t.Fatalf("erstes Ereignis: %s %v", ev, d)
	}
	post := func(path, body string) {
		t.Helper()
		r, err := hc.Post("http://bus"+path, "application/json", strings.NewReader(body))
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("%s: %v %v", path, r.Status, err)
		}
	}
	post("/volume", `{"volume":55}`)
	if ta.vol.level() != 55 {
		t.Fatalf("Lautstärke %d", ta.vol.level())
	}
	post("/source", `{"name":"cast","state":"playing","title":"Lied"}`)
	if ta.src.Active() != "cast" {
		t.Fatalf("Quelle: %q", ta.src.Active())
	}
	if ev, d := next(); ev != "claim" || d["source"] != "cast" {
		t.Fatalf("Vorrang: %s %v", ev, d)
	}
	ta.onButton("bluetooth", "short")
	if ev, d := next(); ev != "button" || d["name"] != "bluetooth" {
		t.Fatalf("Taste: %s %v", ev, d)
	}
	post("/ring", `{"animation":"bt_open"}`)
	if len(led.anims) != 1 || led.anims[0] != "bt_open" {
		t.Fatalf("Ring: %v", led.anims)
	}
}
