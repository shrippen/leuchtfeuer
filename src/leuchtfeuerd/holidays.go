package main

import "time"

// Gesetzliche Feiertage in Deutschland, bundesweit ("DE") oder mit den Feiertagen eines Landes ("DE-BY" ...).
// Für Wecker mit "An Feiertagen nicht klingeln". Quelle der Regeln: Feiertagsgesetze der Länder (Stand 2026).

var holidayRegions = []string{"DE", "DE-BW", "DE-BY", "DE-BE", "DE-BB", "DE-HB", "DE-HH", "DE-HE", "DE-MV", "DE-NI", "DE-NW",
	"DE-RP", "DE-SL", "DE-SN", "DE-ST", "DE-SH", "DE-TH"}

// easter liefert den Ostersonntag (gregorianisch, Gauß/Anonymous-Algorithmus).
func easter(y int) time.Time {
	a := y % 19
	b, c := y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// holidayName liefert den Namen des Feiertags am Tag d in der Region ("" = kein Feiertag).
func holidayName(region string, d time.Time) string {
	if region == "" {
		return ""
	}
	y, m, day := d.Date()
	date := time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	es := easter(y)
	rel := int(date.Sub(es).Hours() / 24)
	in := func(states ...string) bool {
		for _, s := range states {
			if region == "DE-"+s {
				return true
			}
		}
		return false
	}
	switch {
	case m == 1 && day == 1:
		return "Neujahr"
	case m == 1 && day == 6 && in("BW", "BY", "ST"):
		return "Heilige Drei Könige"
	case m == 3 && day == 8 && in("BE", "MV"):
		return "Internationaler Frauentag"
	case rel == -2:
		return "Karfreitag"
	case rel == 0 && in("BB"):
		return "Ostersonntag"
	case rel == 1:
		return "Ostermontag"
	case m == 5 && day == 1:
		return "Tag der Arbeit"
	case rel == 39:
		return "Christi Himmelfahrt"
	case rel == 49 && in("BB"):
		return "Pfingstsonntag"
	case rel == 50:
		return "Pfingstmontag"
	case rel == 60 && in("BW", "BY", "HE", "NW", "RP", "SL"):
		return "Fronleichnam"
	case m == 8 && day == 15 && in("SL"):
		return "Mariä Himmelfahrt"
	case m == 9 && day == 20 && in("TH"):
		return "Weltkindertag"
	case m == 10 && day == 3:
		return "Tag der Deutschen Einheit"
	case m == 10 && day == 31 && in("BB", "HB", "HH", "MV", "NI", "SN", "ST", "SH", "TH"):
		return "Reformationstag"
	case m == 11 && day == 1 && in("BW", "BY", "NW", "RP", "SL"):
		return "Allerheiligen"
	case m == 11 && in("SN") && date.Weekday() == time.Wednesday && day >= 16 && day <= 22:
		return "Buß- und Bettag"
	case m == 12 && day == 25:
		return "1. Weihnachtstag"
	case m == 12 && day == 26:
		return "2. Weihnachtstag"
	}
	return ""
}

func parseDayTime(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }
