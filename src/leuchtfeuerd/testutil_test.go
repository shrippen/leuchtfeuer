package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Nachbildungen für Tests ohne Gerät.

type fakeVol struct {
	mu    sync.Mutex
	vol   int
	muted bool
	sets  []int
}

func (f *fakeVol) OnChange(func()) {}

func (f *fakeVol) Get() (int, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vol, f.muted, true
}
func (f *fakeVol) Adjust(d int) error {
	f.mu.Lock()
	f.vol = clamp(f.vol+d, 0, 100)
	f.mu.Unlock()
	return nil
}
func (f *fakeVol) SetVolume(v int) error {
	f.mu.Lock()
	f.vol = clamp(v, 0, 100)
	f.sets = append(f.sets, f.vol)
	f.mu.Unlock()
	return nil
}
func (f *fakeVol) SetMute(m bool) error { f.mu.Lock(); f.muted = m; f.mu.Unlock(); return nil }
func (f *fakeVol) ToggleMute() error    { f.mu.Lock(); f.muted = !f.muted; f.mu.Unlock(); return nil }
func (f *fakeVol) level() int           { f.mu.Lock(); defer f.mu.Unlock(); return f.vol }

type fakePlayer struct {
	mu                       sync.Mutex
	kind, name, state, title string
	url                      string
	plays                    int
}

func (p *fakePlayer) Info() (string, string, string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kind, p.name, p.state, p.title
}
func (p *fakePlayer) PlayURL(kind, name, url string) {
	p.mu.Lock()
	p.kind, p.name, p.state, p.url = kind, name, "playing", url
	p.plays++
	p.mu.Unlock()
}
func (p *fakePlayer) PlayTone(kind, name string, _ []note, _ bool) {
	p.mu.Lock()
	p.kind, p.name, p.state, p.url = kind, name, "playing", ""
	p.plays++
	p.mu.Unlock()
}
func (p *fakePlayer) Stop() { p.mu.Lock(); p.kind, p.name, p.state = "", "", "idle"; p.mu.Unlock() }

type fakeLED struct {
	mu    sync.Mutex
	anims []string
}

func (l *fakeLED) Animate(n string, _ bool) { l.mu.Lock(); l.anims = append(l.anims, n); l.mu.Unlock() }
func (l *fakeLED) Off()                     {}

type fakeMixer struct {
	mu  sync.Mutex
	ctl map[string]int
}

func (m *fakeMixer) Get(c string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.ctl[c]
	return v, ok
}
func (m *fakeMixer) Set(c string, v int) error {
	m.mu.Lock()
	m.ctl[c] = v
	m.mu.Unlock()
	return nil
}

type testApp struct {
	*app
	vol *fakeVol
	pl  *fakePlayer
	led *fakeLED
	now time.Time
}

// newTestApp: leuchtfeuerd-Kern mit Nachbildungen und einer beweglichen Uhr (Europe/Berlin).
func newTestApp(t *testing.T, start string) *testApp {
	t.Helper()
	dir := t.TempDir()
	cfg := &shellConfig{path: filepath.Join(dir, "config")}
	cfg.Set(map[string]string{"DEVICE_NAME": "Test"})
	st := loadStore(filepath.Join(dir, "leuchtfeuerd.json"))
	loc, _ := time.LoadLocation("Europe/Berlin")
	now, err := time.ParseInLocation("2006-01-02 15:04:05", start, loc)
	if err != nil {
		t.Fatal(err)
	}
	ta := &testApp{vol: &fakeVol{vol: 30}, pl: &fakePlayer{}, led: &fakeLED{}, now: now}
	a := &app{cfg: cfg, st: st}
	a.bus = newLocalBus(a)
	a.clock = func() time.Time { return ta.now }
	a.vol, a.pl, a.led = ta.vol, ta.pl, ta.led
	a.src = newSourceHub(a)
	a.src.procRoot = filepath.Join(dir, "proc")
	a.mix = newMixSync(&fakeMixer{ctl: map[string]int{}})
	a.ann = newAnnouncer(a, "null")
	a.sch = newScheduler(a)
	ta.app = a
	return ta
}

func (ta *testApp) at(s string) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	ta.now, _ = time.ParseInLocation("2006-01-02 15:04:05", s, loc)
}
