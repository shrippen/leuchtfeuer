package main

import (
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Webradio verbindet nach einem Abbruch neu; eine einmalige Wiedergabe endet.
func TestPlayerReconnects(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	p := newPlayer(map[string]string{"": "null"})
	p.backoff = func(int) time.Duration { return 10 * time.Millisecond }
	p.gst = func(url, sink string) *exec.Cmd {
		mu.Lock()
		runs++
		n := runs
		mu.Unlock()
		if n < 3 { // zweimal Abbruch, dann läuft der Stream
			return exec.Command("sh", "-c", "echo 'Setting pipeline to PLAYING ...'; echo 'ERROR: from element /GstPipeline:pipeline0/GstURIDecodeBin'; exit 1")
		}
		return exec.Command("sh", "-c", "echo 'Setting pipeline to PLAYING ...'; echo 'title=(string)\"Song A\", artist'; sleep 5")
	}
	p.PlayURL("radio", "Test", "http://x")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if k, _, st, title := p.Info(); k == "radio" && st == "playing" && title == "Song A" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	k, _, st, title := p.Info()
	mu.Lock()
	n := runs
	mu.Unlock()
	if k != "radio" || st != "playing" || title != "Song A" || n != 3 {
		t.Fatalf("kind=%q state=%q title=%q runs=%d", k, st, title, n)
	}
	p.Stop()
	if k, _, st, _ := p.Info(); k != "" || st != "idle" {
		t.Fatalf("nach Stop: %q %q", k, st)
	}

	// Durchsage/Datei: kein Neuverbinden
	q := newPlayer(map[string]string{"": "null"})
	q.gst = func(url, sink string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo 'Got EOS from element'; exit 0")
	}
	q.PlayURL("announce", "x", "http://y")
	time.Sleep(300 * time.Millisecond)
	if k, _, st, _ := q.Info(); k != "" || st != "idle" {
		t.Fatalf("Datei läuft weiter: %q %q", k, st)
	}
}

func TestPlayerGivesUp(t *testing.T) {
	p := newPlayer(map[string]string{"": "null"})
	p.backoff = func(int) time.Duration { return time.Millisecond }
	p.gst = func(url, sink string) *exec.Cmd { return exec.Command("sh", "-c", "exit 1") }
	p.PlayURL("radio", "Tot", "http://dead")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if k, _, _, _ := p.Info(); k == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Player gibt nicht auf")
}

func TestReconnectDelay(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if d := reconnectDelay(i + 1); d != w {
			t.Fatalf("Versuch %d: %v statt %v", i+1, d, w)
		}
	}
}

// Wecker mit Sender: startet der Stream nicht oder endet er, klingelt der Weckton.
func TestAlarmStreamFallback(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 06:59:50")
	ta.st.Update(func(s *Settings) {
		s.Alarms = []Alarm{{ID: "a", Name: "Radio", Time: "07:00", Enabled: true, Source: "radio:0"}}
	})
	ta.at("2026-10-05 07:00:00")
	ta.sch.tick()
	if ta.pl.url == "" {
		t.Fatal("Sender spielt nicht")
	}
	// Stream hängt beim Puffern
	ta.pl.mu.Lock()
	ta.pl.state = "buffering"
	ta.pl.mu.Unlock()
	ta.at("2026-10-05 07:00:10")
	ta.sch.tick()
	if ta.pl.url == "" {
		t.Fatal("zu früh auf Ersatz umgeschaltet")
	}
	ta.at("2026-10-05 07:00:16")
	ta.sch.tick()
	if ta.pl.url != "" || ta.pl.kind != "alarm" {
		t.Fatalf("kein Ersatzton: %+v", ta.pl)
	}

	// zweiter Fall: Stream bricht ab (Player leer), während der Wecker klingelt
	ta.sch.StopAlarm()
	ta.st.Update(func(s *Settings) { s.Alarms[0].Time = "07:05" })
	ta.at("2026-10-05 07:05:00")
	ta.sch.tick()
	if ta.pl.url == "" {
		t.Fatal("Sender spielt nicht (2)")
	}
	ta.pl.Stop()
	ta.at("2026-10-05 07:05:02")
	ta.sch.tick()
	if ta.pl.kind != "alarm" || ta.pl.url != "" {
		t.Fatalf("kein Ersatzton nach Abbruch: %+v", ta.pl)
	}

	// Schlummern zählt nicht als Abbruch
	ta.sch.StopAlarm()
	ta.st.Update(func(s *Settings) { s.Alarms[0].Time = "07:10" })
	ta.at("2026-10-05 07:10:00")
	ta.sch.tick()
	ta.sch.Snooze()
	ta.at("2026-10-05 07:10:03")
	ta.sch.tick()
	if ta.pl.kind != "" {
		t.Fatalf("Schlummern löst Ersatzton aus: %+v", ta.pl)
	}
}

type recMixer struct {
	fakeMixer
	log []string
}

func (m *recMixer) Set(c string, v int) error {
	m.mu.Lock()
	m.log = append(m.log, c)
	m.mu.Unlock()
	return m.fakeMixer.Set(c, v)
}

// Quellenwechsel blendet über; Pegelausgleich senkt eine Quelle dauerhaft ab.
func TestMixerRampAndTrim(t *testing.T) {
	m := &recMixer{fakeMixer: fakeMixer{ctl: map[string]int{"system": 200, "Invoke Music": 200, "Invoke Announce": 200}}}
	for _, s := range sourceNames {
		m.ctl["Quelle "+s] = 255
	}
	x := newMixSync(m)
	x.step = time.Millisecond
	x.sync(true)
	m.log = nil
	x.SetMuted("spotify", true)
	x.sync(false)
	n := 0
	for _, c := range m.log {
		if c == "Quelle spotify" {
			n++
		}
	}
	if n != rampSteps || m.ctl["Quelle spotify"] != 0 {
		t.Fatalf("Überblenden: %d Schritte, Endwert %d", n, m.ctl["Quelle spotify"])
	}
	x.SetMuted("spotify", false)
	x.SetTrims(map[string]int{"bluetooth": 6, "radio": 99})
	x.sync(false)
	if m.ctl["Quelle spotify"] != 255 || m.ctl["Quelle bluetooth"] != softvolForDB(6) || m.ctl["Quelle radio"] != softvolForDB(20) {
		t.Fatalf("Pegel: %v", m.ctl)
	}
	// Ausgleich und Durchsage addieren sich
	x.Duck(10)
	x.sync(false)
	if m.ctl["Quelle bluetooth"] != softvolForDB(16) {
		t.Fatalf("Ausgleich + Absenkung: %d", m.ctl["Quelle bluetooth"])
	}
	set := SourceSettings{Limits: map[string]SourceLimit{"cast": {TrimDB: 4}, "upnp": {Max: 50}}}
	if tr := set.Trims(); len(tr) != 1 || tr["cast"] != 4 {
		t.Fatalf("Trims: %v", tr)
	}
}
