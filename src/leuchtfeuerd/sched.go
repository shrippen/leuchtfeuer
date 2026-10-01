package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Wecker und Timer. Zeiten gelten in der eingestellten Zeitzone (das Gerät selbst läuft auf Pacific Time).

type alarmRun struct {
	Alarm
	Started   time.Time
	Ringing   bool
	SnoozedTo time.Time
	prevVol   int
	rampStop  chan struct{}
	stream    bool      // spielt einen Stream (Sender oder Adresse)
	streamAt  time.Time // Start des Streams
	fallback  bool      // klingelt mit dem Ersatzton
}

// Wecker-Ersatz: Startet der Stream nicht binnen alarmStreamWait oder endet er, bevor der Wecker gestoppt wurde
// (WLAN weg, Sender tot, Datei zu Ende), klingelt der eingebaute Weckton weiter.
const alarmStreamWait = 15 * time.Second

type scheduler struct {
	app    *app
	mu     sync.Mutex
	fired  map[string]string // Wecker-ID -> "Datum Uhrzeit" der letzten Auslösung
	run    *alarmRun
	tdone  map[string]bool
	tlocal *time.Location

	// Schlummertimer: zu diesem Zeitpunkt leiser werden und alles anhalten
	sleepEnd  time.Time
	sleepBusy bool
}

func newScheduler(a *app) *scheduler {
	return &scheduler{app: a, fired: map[string]string{}, tdone: map[string]bool{}}
}

func (s *scheduler) location() *time.Location {
	tz := s.app.st.Snapshot().Timezone
	if s.tlocal != nil && s.tlocal.String() == tz {
		return s.tlocal
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Printf("Zeitzone %q unbekannt (%v), nehme UTC", tz, err)
		loc = time.UTC
	}
	s.tlocal = loc
	return loc
}

func (s *scheduler) Now() time.Time { return s.app.clock().In(s.location()) }

func dayMatch(days []int, wd time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d == int(wd) {
			return true
		}
	}
	return false
}

func (s *scheduler) Run() {
	t := time.NewTicker(time.Second)
	for range t.C {
		s.tick()
	}
}

func (s *scheduler) tick() {
	now := s.Now()
	set := s.app.st.Snapshot()
	key := now.Format("2006-01-02 15:04")
	hm := now.Format("15:04")
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()

	if run != nil {
		s.checkAlarmStream(run)
		if run.Ringing && run.MaxMins > 0 && s.app.clock().Sub(run.Started) > time.Duration(run.MaxMins)*time.Minute {
			log.Printf("Wecker %q: automatisch gestoppt", run.Name)
			s.StopAlarm()
		} else if !run.Ringing && !run.SnoozedTo.IsZero() && now.After(run.SnoozedTo) {
			s.ring(run.Alarm, true)
		}
	}
	today := now.Format("2006-01-02")
	for _, a := range set.Alarms {
		if a.SkipDate != "" && a.SkipDate < today { // "einmal aussetzen" ist vorbei
			id := a.ID
			s.app.st.Update(func(st *Settings) {
				for i := range st.Alarms {
					if st.Alarms[i].ID == id {
						st.Alarms[i].SkipDate = ""
					}
				}
			})
		}
		if !a.Enabled || a.Time != hm || !s.ringsOn(a, now) {
			continue
		}
		s.mu.Lock()
		already := s.fired[a.ID] == key
		s.fired[a.ID] = key
		s.mu.Unlock()
		if !already {
			s.ring(a, false)
		}
	}
	// Schlummertimer
	s.mu.Lock()
	sleepDue := !s.sleepEnd.IsZero() && !s.app.clock().Before(s.sleepEnd) && !s.sleepBusy
	if sleepDue {
		s.sleepBusy = true
	}
	s.mu.Unlock()
	if sleepDue {
		go s.sleepNow()
	}
	// Timer
	for _, tm := range set.Timers {
		if !tm.End.After(s.app.clock()) {
			s.mu.Lock()
			done := s.tdone[tm.ID]
			s.tdone[tm.ID] = true
			s.mu.Unlock()
			if !done {
				s.fireTimer(tm)
			}
		}
	}
}

func (s *scheduler) ring(a Alarm, resume bool) {
	log.Printf("Wecker %q klingelt", a.Name)
	vol, _, _ := s.app.vol.Get()
	r := &alarmRun{Alarm: a, Started: s.app.clock(), Ringing: true, prevVol: vol, rampStop: make(chan struct{})}
	s.mu.Lock()
	if s.run != nil && s.run.rampStop != nil {
		close(s.run.rampStop)
	}
	s.run = r
	s.mu.Unlock()
	if a.Volume > 0 {
		if a.RampSecs > 0 && !resume {
			go s.ramp(r, a.Volume, a.RampSecs)
		} else {
			s.app.vol.SetVolume(a.Volume)
		}
	}
	s.app.vol.SetMute(false)
	s.app.src.Update("alarm", "playing", map[string]string{"title": a.Name})
	url := ""
	switch {
	case strings.HasPrefix(a.Source, "radio:"):
		idx, _ := strconv.Atoi(strings.TrimPrefix(a.Source, "radio:"))
		set := s.app.st.Snapshot()
		if idx >= 0 && idx < len(set.Radio) {
			url = set.Radio[idx].URL
		}
	case strings.HasPrefix(a.Source, "url:"):
		url = strings.TrimPrefix(a.Source, "url:")
	case a.Source == "briefing":
		if s.app.brief != nil { // das Briefing meldet sein Ende selbst (BriefingDone)
			s.app.brief.StartForAlarm(a.Name)
			url = "-"
		}
	}
	switch url {
	case "-": // Briefing (briefing.go) spielt selbst
	case "":
		s.app.pl.PlayTone("alarm", a.Name, toneAlarm, true)
	default:
		s.mu.Lock()
		r.stream, r.streamAt = true, s.app.clock()
		s.mu.Unlock()
		s.app.pl.PlayURL("alarm", a.Name, url)
	}
	s.app.led.Animate("alarm", true)
	s.app.emit("alarm", map[string]any{"name": a.Name, "state": "ringing"})
}

// checkAlarmStream: Ersatzton, wenn der Weck-Stream nicht startet oder vorzeitig endet.
func (s *scheduler) checkAlarmStream(r *alarmRun) {
	s.mu.Lock()
	need := r.Ringing && r.stream && !r.fallback && s.run == r
	at := r.streamAt
	s.mu.Unlock()
	if !need {
		return
	}
	kind, _, state, _ := s.app.pl.Info()
	why := ""
	switch {
	case kind != "alarm":
		why = "Stream beendet"
	case kind == "alarm" && state != "playing" && s.app.clock().Sub(at) > alarmStreamWait:
		why = "Stream startet nicht"
	}
	if why == "" {
		return
	}
	s.mu.Lock()
	r.fallback = true
	s.mu.Unlock()
	log.Printf("Wecker %q: %s, Ersatzton", r.Name, why)
	s.app.pl.PlayTone("alarm", r.Name, toneAlarm, true)
	s.app.emit("alarm", map[string]any{"name": r.Name, "state": "ringing", "fallback": true})
}

// BriefingDone: Das Weck-Briefing ist zu Ende. Mit einem Anschluss-Sender spielt der als Weckton weiter (mit Ersatz),
// sonst ist der Wecker erledigt. Konnte nichts gespielt werden (kein Netz), klingelt der Weckton.
func (s *scheduler) BriefingDone(played bool, thenURL string) {
	s.mu.Lock()
	r := s.run
	ok := r != nil && r.Ringing
	s.mu.Unlock()
	if !ok {
		return
	}
	switch {
	case !played:
		log.Printf("Wecker %q: Briefing ohne Inhalt, Ersatzton", r.Name)
		s.mu.Lock()
		r.fallback = true
		s.mu.Unlock()
		s.app.pl.PlayTone("alarm", r.Name, toneAlarm, true)
	case thenURL != "":
		s.mu.Lock()
		r.stream, r.streamAt = true, s.app.clock()
		s.mu.Unlock()
		s.app.pl.PlayURL("alarm", r.Name, thenURL)
	default:
		s.stopAlarmNow()
	}
}

// ramp erhöht die Lautstärke schrittweise bis zum Ziel.
func (s *scheduler) ramp(r *alarmRun, target, secs int) {
	start := 5
	if target < start {
		start = target
	}
	s.app.vol.SetVolume(start)
	steps := target - start
	if steps <= 0 {
		return
	}
	iv := time.Duration(secs) * time.Second / time.Duration(steps)
	if iv < 500*time.Millisecond {
		iv = 500 * time.Millisecond
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for v := start + 1; v <= target; v++ {
		select {
		case <-r.rampStop:
			return
		case <-t.C:
			s.app.vol.SetVolume(v)
		}
	}
}

func (s *scheduler) endRun(restore bool) *alarmRun {
	s.mu.Lock()
	r := s.run
	s.run = nil
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	defer s.app.src.Update("alarm", "idle", nil)
	if s.app.brief != nil {
		s.app.brief.Stop()
	}
	if r.rampStop != nil {
		select {
		case <-r.rampStop:
		default:
			close(r.rampStop)
		}
	}
	if k, _, _, _ := s.app.pl.Info(); k == "alarm" || k == "briefing" {
		s.app.pl.Stop()
	}
	s.app.led.Off()
	if restore && r.Volume > 0 && r.prevVol > 0 {
		s.app.vol.SetVolume(r.prevVol)
	}
	return r
}

// StopAlarm beendet den Wecker; mit "Ausblenden" (FadeOut) wird er vorher über einige Sekunden leiser.
func (s *scheduler) StopAlarm() bool {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r != nil && r.Ringing && r.FadeOut > 0 {
		if r.rampStop != nil {
			select {
			case <-r.rampStop:
			default:
				close(r.rampStop)
			}
			r.rampStop = nil
		}
		go func() {
			s.fade(r.FadeOut)
			s.mu.Lock()
			same := s.run == r
			s.mu.Unlock()
			if same {
				s.stopAlarmNow()
			}
		}()
		return true
	}
	return s.stopAlarmNow()
}

// fade senkt die Lautstärke über secs Sekunden auf 0 (ohne sie zu speichern).
func (s *scheduler) fade(secs int) {
	v, _, _ := s.app.vol.Get()
	if v <= 0 || secs <= 0 {
		return
	}
	steps := v
	if steps > 20 {
		steps = 20
	}
	iv := time.Duration(secs) * time.Second / time.Duration(steps)
	for i := 1; i <= steps; i++ {
		time.Sleep(iv)
		s.app.vol.SetVolume(v - v*i/steps)
	}
}

func (s *scheduler) stopAlarmNow() bool {
	r := s.endRun(true)
	if r != nil {
		s.app.emit("alarm", map[string]any{"name": r.Name, "state": "stopped"})
	}
	return r != nil
}

// Snooze stoppt den Ton und lässt den Wecker nach Snooze-Minuten erneut klingeln.
func (s *scheduler) Snooze() bool {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil || !r.Ringing {
		return false
	}
	mins := r.Snooze
	if mins <= 0 {
		mins = 9
	}
	s.mu.Lock() // erst den Zustand, dann den Ton: sonst hielte die Prüfung das Ende für einen Abbruch
	r.Ringing = false
	r.fallback = false
	r.SnoozedTo = s.Now().Add(time.Duration(mins) * time.Minute)
	s.mu.Unlock()
	if s.app.brief != nil {
		s.app.brief.Stop()
	}
	if k, _, _, _ := s.app.pl.Info(); k == "alarm" || k == "briefing" {
		s.app.pl.Stop()
	}
	s.app.led.Off()
	s.app.src.Update("alarm", "idle", nil)
	s.app.emit("alarm", map[string]any{"name": r.Name, "state": "snoozed"})
	return true
}

type alarmState struct {
	Active  bool      `json:"active"`
	Name    string    `json:"name"`
	State   string    `json:"state"` // ringing | snoozed | idle
	SnoozeT time.Time `json:"snoozeUntil,omitempty"`
}

func (s *scheduler) State() alarmState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return alarmState{State: "idle"}
	}
	st := "ringing"
	if !s.run.Ringing {
		st = "snoozed"
	}
	return alarmState{Active: true, Name: s.run.Name, State: st, SnoozeT: s.run.SnoozedTo}
}

// ringsOn: klingelt der Wecker am Tag von t (Wochentag, einmal aussetzen, Feiertage)?
func (s *scheduler) ringsOn(a Alarm, t time.Time) bool {
	if !dayMatch(a.Days, t.Weekday()) {
		return false
	}
	if a.SkipDate != "" && a.SkipDate == t.Format("2006-01-02") {
		return false
	}
	if a.SkipHolidays && holidayName(s.app.st.Snapshot().Holidays, t) != "" {
		return false
	}
	return true
}

// nextOccurrence liefert den nächsten Weckzeitpunkt eines Weckers nach now (Nullwert: keiner in 2 Wochen).
func (s *scheduler) nextOccurrence(a Alarm, now time.Time) time.Time {
	hm := strings.Split(a.Time, ":")
	if len(hm) != 2 {
		return time.Time{}
	}
	h, _ := strconv.Atoi(hm[0])
	m, _ := strconv.Atoi(hm[1])
	for d := 0; d < 15; d++ {
		t := time.Date(now.Year(), now.Month(), now.Day()+d, h, m, 0, 0, now.Location())
		if t.After(now) && s.ringsOn(a, t) {
			return t
		}
	}
	return time.Time{}
}

// NextAlarm liefert den nächsten Weckzeitpunkt (Nullwert, wenn keiner aktiv ist).
func (s *scheduler) NextAlarm() (time.Time, string) {
	now := s.Now()
	var best time.Time
	var name string
	for _, a := range s.app.st.Snapshot().Alarms {
		if !a.Enabled {
			continue
		}
		if t := s.nextOccurrence(a, now); !t.IsZero() && (best.IsZero() || t.Before(best)) {
			best, name = t, a.Name
		}
	}
	return best, name
}

// SkipNext lässt den nächsten Termin eines Weckers einmal ausfallen (oder hebt das wieder auf).
func (s *scheduler) SkipNext(id string, skip bool) (string, error) {
	now := s.Now()
	var day string
	err := s.app.st.Update(func(st *Settings) {
		for i := range st.Alarms {
			if st.Alarms[i].ID != id {
				continue
			}
			if !skip {
				st.Alarms[i].SkipDate = ""
				return
			}
			a := st.Alarms[i]
			a.SkipDate = ""
			if t := s.nextOccurrence(a, now); !t.IsZero() {
				day = t.Format("2006-01-02")
				st.Alarms[i].SkipDate = day
			}
		}
	})
	if err == nil && skip && day == "" {
		return "", fmt.Errorf("kein nächster Termin")
	}
	s.app.emit("settings", nil)
	return day, err
}

// SunriseProgress: läuft gerade ein Lichtwecker? p = 0 ... 1 bis zur Weckzeit.
func (s *scheduler) SunriseProgress() (float64, bool) {
	now := s.Now()
	s.mu.Lock()
	ringing := s.run != nil
	s.mu.Unlock()
	if ringing {
		return 0, false
	}
	best, ok := 0.0, false
	for _, a := range s.app.st.Snapshot().Alarms {
		if !a.Enabled || a.Sunrise <= 0 {
			continue
		}
		t := s.nextOccurrence(a, now.Add(-time.Second))
		if t.IsZero() {
			continue
		}
		win := time.Duration(a.Sunrise) * time.Minute
		if left := t.Sub(now); left >= 0 && left <= win {
			p := 1 - float64(left)/float64(win)
			if !ok || p > best {
				best, ok = p, true
			}
		}
	}
	return best, ok
}

// ---- Schlummertimer ----

// SetSleep startet den Schlummertimer (Minuten; 0 = aus).
func (s *scheduler) SetSleep(mins int) {
	s.mu.Lock()
	if mins <= 0 {
		s.sleepEnd = time.Time{}
	} else {
		s.sleepEnd = s.app.clock().Add(time.Duration(mins) * time.Minute)
	}
	s.mu.Unlock()
	s.app.emit("sleep", nil)
}

// SleepRemaining: Sekunden bis zum Schlummer-Ende (0 = aus).
func (s *scheduler) SleepRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sleepEnd.IsZero() {
		return 0
	}
	r := int(s.sleepEnd.Sub(s.app.clock()).Seconds())
	if r < 1 {
		r = 1
	}
	return r
}

// sleepNow: 30 s ausblenden, alles anhalten, alte Lautstärke wiederherstellen (für das nächste Mal).
func (s *scheduler) sleepNow() {
	log.Printf("Schlummertimer abgelaufen: ausblenden und anhalten")
	v, _, _ := s.app.vol.Get()
	s.fade(30)
	s.app.src.StopAll()
	time.Sleep(time.Second)
	if v > 0 {
		s.app.vol.SetVolume(v)
	}
	s.mu.Lock()
	s.sleepEnd, s.sleepBusy = time.Time{}, false
	s.mu.Unlock()
	s.app.emit("sleep", nil)
}

// ---- Timer ----

func newID() string { return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffffff) }

func (s *scheduler) AddTimer(name string, secs int) Timer {
	if name == "" {
		name = fmt.Sprintf("%d:%02d", secs/60, secs%60)
	}
	t := Timer{ID: newID(), Name: name, End: s.app.clock().Add(time.Duration(secs) * time.Second), Total: secs}
	s.app.st.Update(func(st *Settings) { st.Timers = append(st.Timers, t) })
	s.app.emit("timer", map[string]any{"name": name, "state": "started"})
	return t
}

func (s *scheduler) CancelTimer(id string) {
	s.app.st.Update(func(st *Settings) {
		var keep []Timer
		for _, t := range st.Timers {
			if t.ID != id && id != "*" {
				keep = append(keep, t)
			}
		}
		st.Timers = keep
	})
	if k, _, _, _ := s.app.pl.Info(); k == "timer" {
		s.app.pl.Stop()
		s.app.led.Off()
	}
}

func (s *scheduler) fireTimer(t Timer) {
	log.Printf("Timer %q abgelaufen", t.Name)
	s.app.pl.PlayTone("timer", t.Name, toneTimer, true)
	s.app.led.Animate("timer", true)
	s.app.emit("timer", map[string]any{"name": t.Name, "state": "finished"})
	// Ton nach 60 s von selbst beenden; Timer aus der Liste nehmen
	go func() {
		time.Sleep(60 * time.Second)
		if k, n, _, _ := s.app.pl.Info(); k == "timer" && n == t.Name {
			s.app.pl.Stop()
			s.app.led.Off()
		}
	}()
	s.app.st.Update(func(st *Settings) {
		var keep []Timer
		for _, x := range st.Timers {
			if x.ID != t.ID {
				keep = append(keep, x)
			}
		}
		st.Timers = keep
	})
}

// DismissTimer beendet einen klingelnden Timer.
func (s *scheduler) DismissTimer() bool {
	if k, _, _, _ := s.app.pl.Info(); k == "timer" {
		s.app.pl.Stop()
		s.app.led.Off()
		return true
	}
	return false
}
