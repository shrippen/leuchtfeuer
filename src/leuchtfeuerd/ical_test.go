package main

import (
	"strings"
	"testing"
	"time"
)

const testICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//DE
BEGIN:VTIMEZONE
TZID:Europe/Berlin
END:VTIMEZONE
BEGIN:VEVENT
UID:einmal
DTSTART;TZID=Europe/Berlin:20261005T090000
DTEND;TZID=Europe/Berlin:20261005T093000
SUMMARY:Zahnarzt\, Kontrolle
BEGIN:VALARM
TRIGGER:-PT15M
SUMMARY:nicht dieser
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:utc
DTSTART:20261005T123000Z
DURATION:PT1H
SUMMARY:Mittag (UTC)
END:VEVENT
BEGIN:VEVENT
UID:team
DTSTART;TZID=W. Europe Standard Time:20260907T100000
DTEND;TZID=W. Europe Standard Time:20260907T110000
RRULE:FREQ=WEEKLY;BYDAY=MO,TH
EXDATE;TZID=Europe/Berlin:20261008T100000
SUMMARY:Team-Runde
END:VEVENT
BEGIN:VEVENT
UID:team
RECURRENCE-ID;TZID=Europe/Berlin:20261012T100000
DTSTART;TZID=Europe/Berlin:20261012T140000
DTEND;TZID=Europe/Berlin:20261012T150000
SUMMARY:Team-Runde (verschoben)
END:VEVENT
BEGIN:VEVENT
UID:bio
DTSTART;VALUE=DATE:20260929
DTEND;VALUE=DATE:20260930
RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=TU
SUMMARY:Biotonne
END:VEVENT
BEGIN:VEVENT
UID:geb
DTSTART;VALUE=DATE:19900513
RRULE:FREQ=YEARLY
SUMMARY:Geburtstag Anna
END:VEVENT
BEGIN:VEVENT
UID:monat
DTSTART;TZID=Europe/Berlin:20260101T190000
RRULE:FREQ=MONTHLY;BYDAY=-1FR;COUNT=12
SUMMARY:Stammtisch
END:VEVENT
BEGIN:VEVENT
UID:abgesagt
DTSTART;TZID=Europe/Berlin:20261005T180000
STATUS:CANCELLED
SUMMARY:Abgesagt
END:VEVENT
BEGIN:VEVENT
UID:urlaub
DTSTART;VALUE=DATE:20261004
DTEND;VALUE=DATE:20261007
SUMMARY:Urlaub
END:VEVENT
BEGIN:VEVENT
UID:taeglich
DTSTART;TZID=Europe/Berlin:20100104T063000
RRULE:FREQ=DAILY;INTERVAL=3
SUMMARY:Pillen
END:VEVENT
END:VCALENDAR
`

func calOn(t *testing.T, day string) []string {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Berlin")
	evs, err := parseICS(strings.ReplaceAll(testICS, "\n", "\r\n"), loc)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := time.ParseInLocation("2006-01-02", day, loc)
	var out []string
	for _, e := range eventsOn(evs, d, loc) {
		if e.AllDay {
			out = append(out, "ganz:"+e.Summary)
		} else {
			out = append(out, e.Start.Format("15:04")+" "+e.Summary)
		}
	}
	return out
}

func TestICSDays(t *testing.T) {
	cases := map[string]string{
		"2026-10-05": "ganz:Urlaub|09:00 Zahnarzt, Kontrolle|10:00 Team-Runde|14:30 Mittag (UTC)",
		"2026-10-08": "",                                      // Donnerstag: Ausnahme
		"2026-10-12": "14:00 Team-Runde (verschoben)",         // Montag: verschoben
		"2026-10-13": "ganz:Biotonne|06:30 Pillen",            // alle 2 Wochen dienstags (29.9., 13.10.)
		"2026-10-06": "ganz:Urlaub",                           // Biotonne nicht (Zwischenwoche)
		"2026-10-07": "06:30 Pillen",                          // Urlaub endet am 7. (exklusiv); Pillen alle 3 Tage seit 2010
		"2026-10-30": "19:00 Stammtisch",                      // letzter Freitag im Oktober
		"2027-05-13": "ganz:Geburtstag Anna|10:00 Team-Runde", // auch ein Donnerstag
		"2027-01-29": "06:30 Pillen",                          // Stammtisch: COUNT=12 vorbei
		"2026-10-15": "10:00 Team-Runde",                      // Donnerstag
	}
	for day, want := range cases {
		if got := strings.Join(calOn(t, day), "|"); got != want {
			t.Errorf("%s: %q, erwartet %q", day, got, want)
		}
	}
}

func TestICSDuration(t *testing.T) {
	if parseICSDuration("PT1H30M") != 90*time.Minute || parseICSDuration("P1D") != 24*time.Hour || parseICSDuration("-PT15M") != -15*time.Minute {
		t.Fatal("Dauer")
	}
	if _, err := parseICS("<html>", time.UTC); err == nil {
		t.Fatal("HTML angenommen")
	}
}
