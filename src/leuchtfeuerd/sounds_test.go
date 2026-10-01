package main

import (
	"bytes"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sineWAV(rate, ch, bits int, secs, hz float64) []byte {
	n := int(float64(rate) * secs)
	d := pcmData{pcmFormat: pcmFormat{Rate: rate, Channels: ch, Bits: bits}, frames: make([][]float64, ch)}
	for c := range d.frames {
		d.frames[c] = make([]float64, n)
		for i := range d.frames[c] {
			d.frames[c][i] = 0.5 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate))
		}
	}
	return d.wav()
}

func TestWAVRoundtripAndConvert(t *testing.T) {
	for _, bits := range []int{8, 16, 24, 32} {
		d, err := parseWAV(sineWAV(44100, 2, bits, 0.5, 440))
		if err != nil || d.Rate != 44100 || d.Channels != 2 || d.Bits != bits || math.Abs(d.seconds()-0.5) > 0.001 {
			t.Fatalf("%d bit: %+v %v", bits, d.pcmFormat, err)
		}
		if pk := maxAbs(d.frames[0]); math.Abs(pk-0.5) > 0.02 {
			t.Fatalf("%d bit: Spitze %.3f", bits, pk)
		}
	}
	d, _ := parseWAV(sineWAV(44100, 2, 16, 1, 440))
	c := d.convert(pcmFormat{Rate: 22050, Channels: 1, Bits: 16})
	if len(c.frames) != 1 || len(c.frames[0]) != 22050 || math.Abs(maxAbs(c.frames[0])-0.5) > 0.02 {
		t.Fatalf("Wandlung: %d Kanäle, %d Werte", len(c.frames), len(c.frames[0]))
	}
	back, err := parseWAV(c.wav())
	if err != nil || back.Rate != 22050 || back.Channels != 1 {
		t.Fatalf("zurückgelesen: %+v %v", back.pcmFormat, err)
	}
	for _, bad := range [][]byte{nil, []byte("RIFF0000WAVEfmt "), []byte("ID3 mp3...")} {
		if _, err := parseWAV(bad); err == nil {
			t.Fatalf("Unsinn gelesen: %q", bad)
		}
	}
}

func maxAbs(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		m = math.Max(m, math.Abs(x))
	}
	return m
}

func TestReplaceVendorAndTones(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	old := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = old; soundRoots = nil })
	sys := t.TempDir()
	soundRoots = []string{sys}
	os.MkdirAll(filepath.Join(sys, "prompts"), 0o755)
	orig := filepath.Join(sys, "prompts", "power_on.wav")
	os.WriteFile(orig, sineWAV(16000, 1, 16, 1.2, 660), 0o644)
	os.WriteFile(filepath.Join(sys, "prompts", "readme.txt"), []byte("x"), 0o644)
	s := newSoundStore()
	mounts := map[string]string{}
	s.mount = func(src, dst string) error { mounts[dst] = src; return nil }
	s.umount = func(dst string) error { delete(mounts, dst); return nil }
	s.mounted = func(dst string) bool { _, ok := mounts[dst]; return ok }
	ta.sounds = s

	list := s.Scan(true)
	if len(list) != 1 || list[0].Name != "power_on.wav" || list[0].Format != (pcmFormat{16000, 1, 16}) || list[0].Seconds != 1.2 {
		t.Fatalf("Suche: %+v", list)
	}
	if err := s.ReplaceVendor(filepath.Join(sys, "nicht-da.wav"), sineWAV(48000, 2, 16, 1, 440)); err == nil {
		t.Fatal("unbekannte Datei ersetzt")
	}
	if err := s.ReplaceVendor(orig, sineWAV(48000, 2, 16, 31, 440)); err == nil {
		t.Fatal("zu lang angenommen")
	}
	// Stereo 48 kHz wird ins Format des Originals gewandelt und eingehängt
	if err := s.ReplaceVendor(orig, sineWAV(48000, 2, 24, 2, 440)); err != nil {
		t.Fatal(err)
	}
	src := mounts[orig]
	b, _ := os.ReadFile(src)
	d, err := parseWAV(b)
	if err != nil || d.pcmFormat != (pcmFormat{16000, 1, 16}) || math.Abs(d.seconds()-2) > 0.01 {
		t.Fatalf("Ersatz: %+v %v", d.pcmFormat, err)
	}
	m, _ := os.ReadFile(filepath.Join(soundDir(), "vendor.map"))
	if !strings.HasPrefix(string(m), orig+"\t") {
		t.Fatalf("Zuordnung: %q", m)
	}
	if l := s.Scan(false); !l[0].Replaced || !l[0].Mounted {
		t.Fatalf("Zustand: %+v", l[0])
	}
	if _, _, err := s.soundSource("vendor:"+orig, true); err == nil {
		t.Fatal("verdecktes Original geliefert")
	}
	if err := s.ResetVendor(orig); err != nil || len(mounts) != 0 {
		t.Fatalf("zurück: %v %v", err, mounts)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("Ersatzdatei bleibt liegen")
	}

	// eigene Töne
	if customTone(toneAlarm) != "" {
		t.Fatal("ohne Datei eine eigene")
	}
	if err := replaceTone("alarm", sineWAV(22050, 1, 16, 3, 500)); err != nil {
		t.Fatal(err)
	}
	if f := customTone(toneAlarm); f == "" || customTone(toneTimer) != "" || toneID(toneBell) != "bell" {
		t.Fatalf("Zuordnung der Töne: %q", f)
	}
	if replaceTone("hupe", sineWAV(22050, 1, 16, 1, 500)) == nil {
		t.Fatal("unbekannter Ton angenommen")
	}
	tl := listTones()
	if !tl[0].Replaced || tl[0].Seconds != 3 || tl[1].Replaced {
		t.Fatalf("Töne: %+v", tl)
	}
	// über die API: hochladen (andere Formate dekodiert GStreamer, hier nachgebildet), anhören, zurück
	oldDec, oldWrap := decodeOther, wrapAuth
	decodeOther = func(in []byte) (pcmData, error) { return parseWAV(sineWAV(48000, 2, 16, 1, 300)) }
	wrapAuth = func(_ *webServer, h http.Handler) http.Handler { return h }
	t.Cleanup(func() { decodeOther, wrapAuth = oldDec, oldWrap })
	ta.sysFn = func() sysStatus { return sysStatus{} }
	w := &webServer{app: ta.app}
	srv := httptest.NewServer(w.routes())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/sounds/upload?target=tone:chime", "audio/mpeg", bytes.NewReader([]byte("ID3\x03 kein echtes mp3")))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("Hochladen: %v %v", resp.Status, err)
	}
	if customTone(toneChime) == "" {
		t.Fatal("Gong nicht ersetzt")
	}
	resp, _ = http.Get(srv.URL + "/api/sounds/file?target=tone:beep")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("Anhören (erzeugt): %s", resp.Status)
	}
	resp, _ = http.Post(srv.URL+"/api/sounds/reset", "application/json", strings.NewReader(`{"target":"tone:chime"}`))
	if resp.StatusCode != 200 || customTone(toneChime) != "" {
		t.Fatal("Zurück zum erzeugten Gong")
	}
	resp, _ = http.Post(srv.URL+"/api/sounds/upload?target=vendor:/etc/passwd", "audio/wav", bytes.NewReader(sineWAV(48000, 1, 16, 1, 300)))
	if resp.StatusCode != 400 {
		t.Fatal("beliebige Datei überdeckbar")
	}
}
