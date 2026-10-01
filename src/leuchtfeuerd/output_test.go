package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cardsFixture = ` 0 [Headphones     ]: bcm2835_headpho - bcm2835 Headphones
                      bcm2835 Headphones
 1 [vc4hdmi        ]: vc4-hdmi - vc4-hdmi
                      vc4-hdmi
`

func TestOutputCardsAndConf(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	dir := t.TempDir()
	ta.out.cardsFile = filepath.Join(dir, "cards")
	os.WriteFile(ta.out.cardsFile, []byte(cardsFixture), 0o644)
	conf := filepath.Join(dir, "output.conf")
	ta.out.confFile = func() string { return conf }
	restarted := make(chan bool, 4)
	ta.out.restart = func() { restarted <- true }

	l := ta.out.List()
	if l.Current != "default" || len(l.Outputs) != 3 || l.Outputs[1].ID != "card:0" || l.Outputs[2].Name != "vc4-hdmi - vc4-hdmi" {
		t.Fatalf("Liste: %+v", l)
	}
	if err := ta.out.Set("card:1"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(conf); !strings.Contains(string(b), `pcm.!leuchtfeuer_sink`) || !strings.Contains(string(b), `"hw:1,0"`) {
		t.Fatalf("output.conf: %s", b)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("Audiodienste nicht neu gestartet")
	}
	if ta.out.Status().ID != "card:1" || ta.st.Snapshot().Output.ID != "card:1" {
		t.Fatalf("Status: %+v", ta.out.Status())
	}
	for _, bad := range []string{"card:7", "card:x", "toaster", "bt:nope", "bt:AA:BB:CC:DD:EE:FF"} {
		if err := ta.out.Set(bad); err == nil {
			t.Errorf("%q angenommen", bad)
		}
	}
	// gleiche Wahl: nichts neu starten
	ta.out.Set("card:1")
	select {
	case <-restarted:
		t.Fatal("unnötiger Neustart")
	case <-time.After(100 * time.Millisecond):
	}
	if err := ta.out.Set("default"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(conf); strings.Contains(string(b), "pcm.!") {
		t.Fatalf("Standard sollte nichts überschreiben: %s", b)
	}
}

func TestOutputBluetooth(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	conf := filepath.Join(t.TempDir(), "output.conf")
	ta.out.confFile = func() string { return conf }
	ta.out.cardsFile = "/nonexistent"
	ta.out.restart = func() {}
	ev := make(chan busEvent, 8)
	ta.bus.mu.Lock()
	ta.bus.subs[99] = ev
	ta.bus.mu.Unlock()

	if err := ta.out.Bluetooth("scan", ""); err == nil {
		t.Fatal("ohne btagent sollte Bluetooth nicht gehen")
	}
	// btagent meldet ein Gerät: gekoppelt, nicht verbunden; ein fremdes, nicht gekoppeltes
	want := ta.out.Report(btReport{Devices: []btDevice{
		{Addr: "AA:BB:CC:DD:EE:01", Name: "Küchenbox", Paired: true},
		{Addr: "AA:BB:CC:DD:EE:02", Name: "Fremd"},
	}})
	if want != "" {
		t.Fatalf("want %q", want)
	}
	l := ta.out.List()
	if !l.Bluetooth.Available || len(l.Bluetooth.Devices) != 2 || len(l.Outputs) != 2 || l.Outputs[1].ID != "bt:AA:BB:CC:DD:EE:01" || l.Outputs[1].Available {
		t.Fatalf("Liste: %+v", l)
	}
	if err := ta.out.Set("bt:AA:BB:CC:DD:EE:02"); err == nil {
		t.Fatal("nicht gekoppelter Lautsprecher angenommen")
	}
	if err := ta.out.Set("bt:aa:bb:cc:dd:ee:01"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(conf); !strings.Contains(string(b), `type bluealsa`) || !strings.Contains(string(b), `device "AA:BB:CC:DD:EE:01"`) {
		t.Fatalf("output.conf: %s", b)
	}
	if e := <-ev; e.Name != "bt-sink" || e.Data.(map[string]string)["action"] != "connect" {
		t.Fatalf("Ereignis %+v", e)
	}
	// btagent erfährt, womit es verbunden sein soll; die Auswahl bleibt, auch wenn es das Gerät nicht mehr meldet
	if w := ta.out.Report(btReport{}); w != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("want %q", w)
	}
	if s := ta.out.Status(); s.ID != "bt:AA:BB:CC:DD:EE:01" || s.OK || s.Name != "Küchenbox" {
		t.Fatalf("Status %+v", s)
	}
	ta.out.Report(btReport{Devices: []btDevice{{Addr: "AA:BB:CC:DD:EE:01", Name: "Küchenbox", Paired: true, Connected: true}}})
	if s := ta.out.Status(); !s.OK {
		t.Fatalf("Status %+v", s)
	}
	if err := ta.out.Bluetooth("connect", "kaputt"); err == nil {
		t.Fatal("ungültige Adresse")
	}
	// Vergessen des gewählten Lautsprechers fällt auf Standard zurück
	if err := ta.out.Bluetooth("forget", "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatal(err)
	}
	if ta.out.Status().ID != "default" {
		t.Fatalf("Rückfall: %+v", ta.out.Status())
	}
}

func TestOutputEnsureConf(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	conf := filepath.Join(t.TempDir(), "output.conf")
	ta.out.confFile = func() string { return conf }
	ta.out.ensureConf()
	if b, err := os.ReadFile(conf); err != nil || strings.Contains(string(b), "pcm.!") {
		t.Fatalf("leere Datei erwartet: %q %v", b, err)
	}
}
