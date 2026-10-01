package main

import (
	"testing"
	"time"
)

func addAlarm(ta *testApp, a Alarm) {
	ta.st.Update(func(s *Settings) { s.Alarms = append(s.Alarms, a) })
}

// 2026-10-01 ist ein Donnerstag.
func TestAlarmRingsOnceAtItsMinute(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 07:29:58")
	addAlarm(ta, Alarm{ID: "a", Name: "Arbeit", Time: "07:30", Days: []int{1, 2, 3, 4, 5}, Enabled: true, Source: "tone"})
	ta.sch.tick()
	if ta.pl.plays != 0 {
		t.Fatal("klingelt zu früh")
	}
	ta.at("2026-10-01 07:30:00")
	ta.sch.tick()
	ta.at("2026-10-01 07:30:30")
	ta.sch.tick()
	if ta.pl.plays != 1 || ta.pl.kind != "alarm" {
		t.Fatalf("erwartet ein Klingeln, plays=%d kind=%q", ta.pl.plays, ta.pl.kind)
	}
	if st := ta.sch.State(); st.State != "ringing" {
		t.Fatalf("Zustand %v", st)
	}
	if ta.src.Active() != "alarm" {
		t.Fatalf("aktive Quelle %q", ta.src.Active())
	}
	ta.sch.StopAlarm()
	if ta.sch.State().State != "idle" || ta.src.Active() != "" {
		t.Fatal("nicht gestoppt")
	}
}

func TestAlarmNotOnWeekendOrSkippedDay(t *testing.T) {
	ta := newTestApp(t, "2026-10-03 07:30:00") // Samstag
	addAlarm(ta, Alarm{ID: "a", Time: "07:30", Days: []int{1, 2, 3, 4, 5}, Enabled: true})
	ta.sch.tick()
	if ta.pl.plays != 0 {
		t.Fatal("klingelt am Samstag")
	}
	next, _ := ta.sch.NextAlarm()
	if want := "2026-10-05 07:30"; next.Format("2006-01-02 15:04") != want {
		t.Fatalf("nächster Wecker %v, erwartet %s", next, want)
	}
	day, err := ta.sch.SkipNext("a", true)
	if err != nil || day != "2026-10-05" {
		t.Fatalf("aussetzen: %q %v", day, err)
	}
	next, _ = ta.sch.NextAlarm()
	if next.Format("2006-01-02") != "2026-10-06" {
		t.Fatalf("nach Aussetzen: %v", next)
	}
	ta.at("2026-10-05 07:30:00")
	ta.sch.tick()
	if ta.pl.plays != 0 {
		t.Fatal("klingelt trotz Aussetzen")
	}
	// am Tag danach wird das Aussetzen vergessen
	ta.at("2026-10-06 06:00:00")
	ta.sch.tick()
	if ta.st.Snapshot().Alarms[0].SkipDate != "" {
		t.Fatal("SkipDate nicht gelöscht")
	}
}

func TestAlarmSkipsHolidays(t *testing.T) {
	ta := newTestApp(t, "2026-10-03 06:00:00") // Tag der Deutschen Einheit, Samstag
	ta.st.Update(func(s *Settings) { s.Holidays = "DE-HH" })
	addAlarm(ta, Alarm{ID: "a", Time: "08:00", Enabled: true, SkipHolidays: true})
	next, _ := ta.sch.NextAlarm()
	if next.Format("2006-01-02") != "2026-10-04" {
		t.Fatalf("nächster Wecker %v", next)
	}
}

func TestAlarmRadioAndURLSource(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 07:30:00")
	ta.st.Update(func(s *Settings) { s.Radio = []Preset{{Name: "A", URL: "http://a"}} })
	addAlarm(ta, Alarm{ID: "a", Time: "07:30", Enabled: true, Source: "radio:0"})
	addAlarm(ta, Alarm{ID: "b", Time: "07:31", Enabled: true, Source: "url:https://server/datei.mp3"})
	ta.sch.tick()
	if ta.pl.url != "http://a" {
		t.Fatalf("Radio: %q", ta.pl.url)
	}
	ta.at("2026-10-01 07:31:00")
	ta.sch.tick()
	if ta.pl.url != "https://server/datei.mp3" {
		t.Fatalf("URL: %q", ta.pl.url)
	}
}

func TestSnoozeOverMidnight(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 23:55:00")
	addAlarm(ta, Alarm{ID: "a", Time: "23:55", Enabled: true, Snooze: 9})
	ta.sch.tick()
	if !ta.sch.Snooze() {
		t.Fatal("Schlummern geht nicht")
	}
	ta.at("2026-10-02 00:03:00")
	ta.sch.tick()
	if ta.pl.plays != 1 {
		t.Fatal("zu früh wieder geklingelt")
	}
	ta.at("2026-10-02 00:04:01")
	ta.sch.tick()
	if ta.pl.plays != 2 || ta.sch.State().State != "ringing" {
		t.Fatalf("nach dem Schlummern nicht wieder geklingelt (plays=%d)", ta.pl.plays)
	}
}

func TestDaylightSavingEnd(t *testing.T) {
	// 2026-10-25: die Uhr springt um 03:00 auf 02:00 zurück; ein Wecker um 02:30 darf nur einmal klingeln.
	ta := newTestApp(t, "2026-10-25 02:30:00")
	addAlarm(ta, Alarm{ID: "a", Time: "02:30", Enabled: true})
	ta.sch.tick()
	ta.sch.StopAlarm()
	ta.now = ta.now.Add(time.Hour) // zweites 02:30 (Winterzeit)
	if ta.now.Format("15:04") != "02:30" {
		t.Skipf("Zeitzonendaten ohne Umstellung: %v", ta.now)
	}
	ta.sch.tick()
	if ta.pl.plays != 1 {
		t.Fatalf("klingelt bei der Zeitumstellung %d-mal", ta.pl.plays)
	}
}

func TestTimerFiresAndIsRemoved(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 12:00:00")
	ta.sch.AddTimer("Tee", 180)
	ta.at("2026-10-01 12:02:59")
	ta.sch.tick()
	if ta.pl.plays != 0 {
		t.Fatal("Timer zu früh")
	}
	ta.at("2026-10-01 12:03:00")
	ta.sch.tick()
	if ta.pl.kind != "timer" || len(ta.st.Snapshot().Timers) != 0 {
		t.Fatalf("Timer: kind=%q übrig=%d", ta.pl.kind, len(ta.st.Snapshot().Timers))
	}
}

func TestSunriseProgress(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 06:59:00")
	addAlarm(ta, Alarm{ID: "a", Time: "07:30", Enabled: true, Sunrise: 20})
	if _, ok := ta.sch.SunriseProgress(); ok {
		t.Fatal("Lichtwecker zu früh")
	}
	ta.at("2026-10-01 07:20:00")
	p, ok := ta.sch.SunriseProgress()
	if !ok || p < 0.49 || p > 0.51 {
		t.Fatalf("Fortschritt %v %v", p, ok)
	}
	c0, c1 := sunriseColor(0.1), sunriseColor(0.9)
	if int(c1[0])+int(c1[1])+int(c1[2]) <= int(c0[0])+int(c0[1])+int(c0[2]) {
		t.Fatal("Lichtwecker wird nicht heller")
	}
}

func TestSleepTimerFadesAndStops(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 22:00:00")
	ta.pl.PlayURL("radio", "A", "http://a")
	ta.src.Update("radio", "playing", nil)
	ta.sch.SetSleep(30)
	if r := ta.sch.SleepRemaining(); r != 1800 {
		t.Fatalf("Restzeit %d", r)
	}
	ta.at("2026-10-01 22:30:00")
	ta.sch.mu.Lock()
	ta.sch.sleepBusy = true
	ta.sch.mu.Unlock()
	// sleepNow ohne 30 s Ausblenden prüfen: Lautstärke 0 überspringt die Stufen
	ta.vol.SetVolume(0)
	ta.sch.sleepNow()
	if k, _, _, _ := ta.pl.Info(); k != "" {
		t.Fatal("Radio läuft noch")
	}
	if ta.sch.SleepRemaining() != 0 {
		t.Fatal("Schlummertimer nicht beendet")
	}
}
