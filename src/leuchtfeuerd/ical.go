package main

import (
	"bufio"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kalender (iCalendar, RFC 5545) für das Briefing: Termine eines Tages aus einer ICS-Adresse (Google "Privatadresse
// im iCal-Format", Nextcloud, Outlook, Abfallkalender der Stadt ...). Unterstützt: ganztägige und zeitgebundene
// Termine, TZID (IANA und die üblichen Windows-Namen), Wiederholungen (RRULE: FREQ DAILY/WEEKLY/MONTHLY/YEARLY,
// INTERVAL, COUNT, UNTIL, BYDAY, BYMONTHDAY, BYMONTH), Ausnahmen (EXDATE), geänderte Einzeltermine (RECURRENCE-ID)
// und abgesagte Termine (STATUS:CANCELLED).

type calEvent struct {
	Summary string
	Start   time.Time
	End     time.Time
	AllDay  bool
}

type icsEvent struct {
	uid, summary, status string
	start, end           time.Time
	dur                  time.Duration
	allDay               bool
	rrule                map[string]string
	exdates              map[int64]bool
	recurID              time.Time
	hasRecur             bool
}

// Windows-Zeitzonen (Outlook/Exchange) -> IANA, die häufigsten.
var windowsTZ = map[string]string{
	"W. Europe Standard Time": "Europe/Berlin", "Central Europe Standard Time": "Europe/Budapest",
	"Romance Standard Time": "Europe/Paris", "Central European Standard Time": "Europe/Warsaw",
	"GMT Standard Time": "Europe/London", "UTC": "UTC", "Eastern Standard Time": "America/New_York",
	"Pacific Standard Time": "America/Los_Angeles", "Central Standard Time": "America/Chicago",
}

func icsLocation(tzid string, def *time.Location) *time.Location {
	tzid = strings.Trim(tzid, `"`)
	if tzid == "" {
		return def
	}
	if w, ok := windowsTZ[tzid]; ok {
		tzid = w
	}
	if l, err := time.LoadLocation(tzid); err == nil {
		return l
	}
	// "/mozilla.org/20050126_1/Europe/Berlin" und Ähnliches
	if i := strings.Index(tzid, "/"); i >= 0 {
		for p := tzid; strings.Contains(p, "/"); p = p[strings.Index(p, "/")+1:] {
			if l, err := time.LoadLocation(p); err == nil {
				return l
			}
		}
	}
	return def
}

// icsTime liest DTSTART/DTEND/EXDATE/RECURRENCE-ID: "20261001T090000Z", "...T090000" (TZID oder lokal), "20261001".
func icsTime(params map[string]string, v string, def *time.Location) (time.Time, bool, error) {
	v = strings.TrimSpace(v)
	if params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, def)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	t, err := time.ParseInLocation("20060102T150405", v, icsLocation(params["TZID"], def))
	return t, false, err
}

func icsUnescape(s string) string {
	r := strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(s))
}

// parseICSDuration: "PT1H30M", "P1D", "-PT15M" ...
func parseICSDuration(s string) time.Duration {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	s = strings.TrimPrefix(s, "P")
	var d time.Duration
	num := ""
	inTime := false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			num += string(c)
		case c == 'T':
			inTime = true
		default:
			n, _ := strconv.Atoi(num)
			num = ""
			switch {
			case c == 'W':
				d += time.Duration(n) * 7 * 24 * time.Hour
			case c == 'D':
				d += time.Duration(n) * 24 * time.Hour
			case c == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case c == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case c == 'S' && inTime:
				d += time.Duration(n) * time.Second
			}
		}
	}
	if neg {
		d = -d
	}
	return d
}

// parseICS liest alle VEVENTs.
func parseICS(data string, def *time.Location) ([]icsEvent, error) {
	// Zeilen entfalten (Folgezeilen beginnen mit Leerzeichen/Tab)
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 256<<10), 1<<20)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	if !strings.Contains(data, "BEGIN:VCALENDAR") {
		return nil, fmt.Errorf("kein iCalendar (BEGIN:VCALENDAR fehlt)")
	}
	var out []icsEvent
	var ev *icsEvent
	depth := 0 // VALARM u. Ä. in VEVENT überspringen
	for _, l := range lines {
		name, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		parts := strings.Split(name, ";")
		key := strings.ToUpper(parts[0])
		params := map[string]string{}
		for _, p := range parts[1:] {
			if k, v, ok := strings.Cut(p, "="); ok {
				params[strings.ToUpper(k)] = v
			}
		}
		switch {
		case key == "BEGIN" && val == "VEVENT":
			ev = &icsEvent{exdates: map[int64]bool{}}
			depth = 0
			continue
		case key == "BEGIN" && ev != nil:
			depth++
			continue
		case key == "END" && val == "VEVENT" && ev != nil:
			if !ev.start.IsZero() {
				out = append(out, *ev)
			}
			ev = nil
			continue
		case key == "END" && ev != nil:
			depth--
			continue
		}
		if ev == nil || depth > 0 {
			continue
		}
		switch key {
		case "UID":
			ev.uid = val
		case "SUMMARY":
			ev.summary = icsUnescape(val)
		case "STATUS":
			ev.status = strings.ToUpper(val)
		case "DTSTART":
			t, allDay, err := icsTime(params, val, def)
			if err == nil {
				ev.start, ev.allDay = t, allDay
			}
		case "DTEND":
			if t, _, err := icsTime(params, val, def); err == nil {
				ev.end = t
			}
		case "DURATION":
			ev.dur = parseICSDuration(val)
		case "RRULE":
			ev.rrule = map[string]string{}
			for _, p := range strings.Split(val, ";") {
				if k, v, ok := strings.Cut(p, "="); ok {
					ev.rrule[strings.ToUpper(k)] = strings.ToUpper(v)
				}
			}
		case "EXDATE":
			for _, v := range strings.Split(val, ",") {
				if t, allDay, err := icsTime(params, v, def); err == nil {
					ev.exdates[exKey(t, allDay)] = true
				}
			}
		case "RECURRENCE-ID":
			if t, _, err := icsTime(params, val, def); err == nil {
				ev.recurID, ev.hasRecur = t, true
			}
		}
	}
	return out, nil
}

// exKey: Ausnahme vergleichen (ganztägig nur nach Datum).
func exKey(t time.Time, allDay bool) int64 {
	if allDay {
		y, m, d := t.Date()
		return int64(y*10000 + int(m)*100 + d)
	}
	return t.Unix()
}

var icsDays = map[string]time.Weekday{"SU": 0, "MO": 1, "TU": 2, "WE": 3, "TH": 4, "FR": 5, "SA": 6}

// occurrences liefert die Anfangszeiten eines Termins im Zeitraum [from, to).
func (e icsEvent) occurrences(from, to time.Time) []time.Time {
	dur := e.dur
	if !e.end.IsZero() {
		dur = e.end.Sub(e.start)
	}
	if dur <= 0 && e.allDay {
		dur = 24 * time.Hour
	}
	overlaps := func(s time.Time) bool { return s.Before(to) && s.Add(dur).After(from) || s.Equal(from) }
	if e.rrule == nil {
		if overlaps(e.start) {
			return []time.Time{e.start}
		}
		return nil
	}
	r := e.rrule
	interval, _ := strconv.Atoi(r["INTERVAL"])
	if interval < 1 {
		interval = 1
	}
	count, _ := strconv.Atoi(r["COUNT"])
	var until time.Time
	if u := r["UNTIL"]; u != "" {
		if t, _, err := icsTime(map[string]string{}, u, e.start.Location()); err == nil {
			until = t
			if len(u) == 8 { // Datum: einschließlich des ganzen Tages
				until = until.Add(24*time.Hour - time.Second)
			}
		}
	}
	var byDay []string
	if r["BYDAY"] != "" {
		byDay = strings.Split(r["BYDAY"], ",")
	}
	var byMonthDay, byMonth []int
	for _, s := range strings.Split(r["BYMONTHDAY"], ",") {
		if n, err := strconv.Atoi(s); err == nil {
			byMonthDay = append(byMonthDay, n)
		}
	}
	for _, s := range strings.Split(r["BYMONTH"], ",") {
		if n, err := strconv.Atoi(s); err == nil {
			byMonth = append(byMonth, n)
		}
	}
	loc := e.start.Location()
	h, mi, se := e.start.Clock()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, h, mi, se, 0, loc) }
	// Kandidaten einer Periode (Tag, Woche, Monat, Jahr), sortiert
	period := func(p time.Time) []time.Time {
		var c []time.Time
		switch r["FREQ"] {
		case "DAILY":
			c = []time.Time{p}
		case "WEEKLY":
			if len(byDay) == 0 {
				c = []time.Time{p}
				break
			}
			// Woche ab Montag (WKST=MO, Standard)
			ws := p.AddDate(0, 0, -((int(p.Weekday()) + 6) % 7))
			for i := 0; i < 7; i++ {
				d := ws.AddDate(0, 0, i)
				for _, bd := range byDay {
					if wd, ok := icsDays[bd[len(bd)-2:]]; ok && wd == d.Weekday() {
						c = append(c, at(d.Year(), d.Month(), d.Day()))
					}
				}
			}
		case "MONTHLY", "YEARLY":
			months := []time.Month{p.Month()}
			if r["FREQ"] == "YEARLY" {
				months = []time.Month{e.start.Month()}
			}
			if r["FREQ"] == "YEARLY" && len(byMonth) > 0 {
				months = nil
				for _, m := range byMonth {
					months = append(months, time.Month(m))
				}
			}
			for _, mo := range months {
				first := time.Date(p.Year(), mo, 1, 0, 0, 0, 0, loc)
				days := first.AddDate(0, 1, -1).Day()
				switch {
				case len(byMonthDay) > 0:
					for _, md := range byMonthDay {
						if md < 0 {
							md = days + md + 1
						}
						if md >= 1 && md <= days {
							c = append(c, at(p.Year(), mo, md))
						}
					}
				case len(byDay) > 0:
					for _, bd := range byDay {
						wd, ok := icsDays[bd[len(bd)-2:]]
						if !ok {
							continue
						}
						n, _ := strconv.Atoi(bd[:len(bd)-2]) // 0 = jeder
						var hits []int
						for d := 1; d <= days; d++ {
							if time.Date(p.Year(), mo, d, 0, 0, 0, 0, loc).Weekday() == wd {
								hits = append(hits, d)
							}
						}
						switch {
						case n == 0:
							for _, d := range hits {
								c = append(c, at(p.Year(), mo, d))
							}
						case n > 0 && n <= len(hits):
							c = append(c, at(p.Year(), mo, hits[n-1]))
						case n < 0 && -n <= len(hits):
							c = append(c, at(p.Year(), mo, hits[len(hits)+n]))
						}
					}
				default:
					if d := e.start.Day(); d <= days {
						c = append(c, at(p.Year(), mo, d))
					}
				}
			}
		default:
			return nil
		}
		sort.Slice(c, func(i, j int) bool { return c[i].Before(c[j]) })
		return c
	}
	step := func(p time.Time) time.Time {
		switch r["FREQ"] {
		case "DAILY":
			return p.AddDate(0, 0, interval)
		case "WEEKLY":
			return p.AddDate(0, 0, 7*interval)
		case "MONTHLY":
			return time.Date(p.Year(), p.Month()+time.Month(interval), 1, h, mi, se, 0, loc)
		default:
			return time.Date(p.Year()+interval, p.Month(), 1, h, mi, se, 0, loc)
		}
	}
	var out []time.Time
	n := 0
	p := e.start
	if r["FREQ"] == "MONTHLY" || r["FREQ"] == "YEARLY" {
		p = time.Date(e.start.Year(), e.start.Month(), 1, h, mi, se, 0, loc)
		if r["FREQ"] == "YEARLY" {
			p = time.Date(e.start.Year(), 1, 1, h, mi, se, 0, loc)
		}
	}
	// ohne COUNT: Perioden weit vor dem Zeitraum überspringen (tägliche Termine seit Jahren)
	if count == 0 {
		for i := 0; i < 200000; i++ {
			next := step(p)
			if !next.Before(from.AddDate(0, 0, -40)) {
				break
			}
			p = next
		}
	}
	for i := 0; i < 5000 && p.Before(to); i++ {
		for _, t := range period(p) {
			if t.Before(e.start) {
				continue
			}
			if (!until.IsZero() && t.After(until)) || (count > 0 && n >= count) {
				return out
			}
			n++
			if !e.exdates[exKey(t, e.allDay)] && overlaps(t) {
				out = append(out, t)
			}
		}
		p = step(p)
	}
	return out
}

// eventsOn liefert die Termine eines Tages (Zeitzone loc), sortiert: ganztägige zuerst, dann nach Uhrzeit.
func eventsOn(evs []icsEvent, day time.Time, loc *time.Location) []calEvent {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	// geänderte Einzeltermine: UID + ursprünglicher Zeitpunkt
	moved := map[string]bool{}
	for _, e := range evs {
		if e.hasRecur {
			moved[e.uid+"|"+fmt.Sprint(e.recurID.Unix())] = true
		}
	}
	var out []calEvent
	for _, e := range evs {
		if e.status == "CANCELLED" && !e.hasRecur {
			continue
		}
		dur := e.dur
		if !e.end.IsZero() {
			dur = e.end.Sub(e.start)
		}
		if e.allDay {
			// ganztägig: Datum in der Zeitzone des Tages (unabhängig von der Zeitzone der Datei)
			for _, t := range e.occurrences(from.AddDate(0, 0, -1), to.AddDate(0, 0, 1)) {
				s := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
				d := dur
				if d <= 0 {
					d = 24 * time.Hour
				}
				if s.Before(to) && s.Add(d).After(from) {
					out = append(out, calEvent{Summary: e.summary, Start: s, End: s.Add(d), AllDay: true})
				}
			}
			continue
		}
		for _, t := range e.occurrences(from, to) {
			if e.rrule != nil && moved[e.uid+"|"+fmt.Sprint(t.Unix())] {
				continue
			}
			if e.hasRecur && e.status == "CANCELLED" {
				continue
			}
			out = append(out, calEvent{Summary: e.summary, Start: t.In(loc), End: t.Add(dur).In(loc)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AllDay != out[j].AllDay {
			return out[i].AllDay
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}
