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
}

type scheduler struct {
	app    *app
	mu     sync.Mutex
	fired  map[string]string // Wecker-ID -> "Datum Uhrzeit" der letzten Auslösung
	run    *alarmRun
	tdone  map[string]bool
	tlocal *time.Location
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
		if run.Ringing && run.MaxMins > 0 && s.app.clock().Sub(run.Started) > time.Duration(run.MaxMins)*time.Minute {
			log.Printf("Wecker %q: automatisch gestoppt", run.Name)
			s.StopAlarm()
		} else if !run.Ringing && !run.SnoozedTo.IsZero() && now.After(run.SnoozedTo) {
			s.ring(run.Alarm, true)
		}
	}
	for _, a := range set.Alarms {
		if !a.Enabled || a.Time != hm || !dayMatch(a.Days, now.Weekday()) {
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
	if strings.HasPrefix(a.Source, "radio:") {
		idx, _ := strconv.Atoi(strings.TrimPrefix(a.Source, "radio:"))
		set := s.app.st.Snapshot()
		if idx >= 0 && idx < len(set.Radio) {
			s.app.pl.PlayURL("alarm", a.Name, set.Radio[idx].URL)
		} else {
			s.app.pl.PlayTone("alarm", a.Name, toneAlarm, true)
		}
	} else {
		s.app.pl.PlayTone("alarm", a.Name, toneAlarm, true)
	}
	s.app.led.Animate("L_111_c_alarm", true)
	s.app.emit("alarm", map[string]any{"name": a.Name, "state": "ringing"})
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
	if r.rampStop != nil {
		select {
		case <-r.rampStop:
		default:
			close(r.rampStop)
		}
	}
	if k, _, _, _ := s.app.pl.Info(); k == "alarm" {
		s.app.pl.Stop()
	}
	s.app.led.Off()
	if restore && r.Volume > 0 && r.prevVol > 0 {
		s.app.vol.SetVolume(r.prevVol)
	}
	return r
}

func (s *scheduler) StopAlarm() bool {
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
	if k, _, _, _ := s.app.pl.Info(); k == "alarm" {
		s.app.pl.Stop()
	}
	s.app.led.Off()
	s.mu.Lock()
	r.Ringing = false
	r.SnoozedTo = s.Now().Add(time.Duration(mins) * time.Minute)
	s.mu.Unlock()
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

// NextAlarm liefert den nächsten Weckzeitpunkt (Nullwert, wenn keiner aktiv ist).
func (s *scheduler) NextAlarm() (time.Time, string) {
	now := s.Now()
	set := s.app.st.Snapshot()
	var best time.Time
	var name string
	for _, a := range set.Alarms {
		if !a.Enabled {
			continue
		}
		hm := strings.Split(a.Time, ":")
		if len(hm) != 2 {
			continue
		}
		h, _ := strconv.Atoi(hm[0])
		m, _ := strconv.Atoi(hm[1])
		for d := 0; d < 8; d++ {
			t := time.Date(now.Year(), now.Month(), now.Day()+d, h, m, 0, 0, now.Location())
			if t.After(now) && dayMatch(a.Days, t.Weekday()) {
				if best.IsZero() || t.Before(best) {
					best, name = t, a.Name
				}
				break
			}
		}
	}
	return best, name
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
	s.app.led.Animate("L_112_c_timer", true)
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
