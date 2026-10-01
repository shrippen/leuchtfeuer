package main

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioInputs(t *testing.T) {
	d := t.TempDir()
	// so sieht /proc/asound auf dem Invoke aus
	os.WriteFile(filepath.Join(d, "cards"), []byte(` 0 [Loopback       ]: Loopback - Loopback
                      Loopback 1
 1 [marvellwm8904  ]: marvell-wm8904 - marvell-wm8904
                      marvell-wm8904
`), 0o644)
	os.WriteFile(filepath.Join(d, "pcm"), []byte(`00-00: Loopback PCM : Loopback PCM : playback 8 : capture 8
00-01: Loopback PCM : Loopback PCM : playback 8 : capture 8
01-00: marvell wm8904-hifi-0 :  : playback 1 : capture 1
01-01: nur Wiedergabe :  : playback 1
`), 0o644)
	got := audioInputs(d)
	if len(got) != 2 {
		t.Fatalf("2 Geräte erwartet (Tonkette + Codec), bekommen %+v", got)
	}
	if got[0].ID != "leuchtfeuer_mic" || !got[0].Recommended {
		t.Errorf("zuerst leuchtfeuer_mic als Vorgabe: %+v", got[0])
	}
	if got[1].ID != "plughw:1,0" || got[1].Name != "marvell-wm8904" || got[1].Card != 1 || got[1].Device != 0 {
		t.Errorf("Codec: %+v", got[1])
	}
	// ohne /proc/asound: nur die Tonkette
	if g := audioInputs(""); len(g) != 1 {
		t.Errorf("leerer Pfad (Demo): %+v", g)
	}
	if g := audioInputs(filepath.Join(d, "fehlt")); len(g) != 1 {
		t.Errorf("ohne proc: %+v", g)
	}
}

func TestLevelS16(t *testing.T) {
	if r, p := levelS16(make([]byte, 3200)); r != -120 || p != -120 {
		t.Errorf("Stille: %v %v", r, p)
	}
	b := make([]byte, 0, 3200)
	for i := 0; i < 1600; i++ { // Rechteck halber Aussteuerung: RMS = Spitze = -6 dBFS
		v := int16(16384)
		if i%2 == 1 {
			v = -16384
		}
		b = append(b, byte(v), byte(uint16(v)>>8))
	}
	if r, p := levelS16(b); r != -6 || p != -6 {
		t.Errorf("halbe Aussteuerung: %v %v", r, p)
	}
}

func TestPreviewSampleIsQuiet(t *testing.T) {
	gen := previewSample(rand.New(rand.NewSource(1)))
	peak := 0.0
	for i := 0; i < 6*48000; i++ {
		peak = math.Max(peak, math.Abs(gen(i)))
	}
	// leise (Testtöne nie laut): Spitze höchstens -12 dBFS
	if peak > 0.26 || peak < 0.05 {
		t.Errorf("Spitze %.3f", peak)
	}
}

func TestSetupDoneMigration(t *testing.T) {
	d := t.TempDir()
	old := filepath.Join(d, "alt.json")
	os.WriteFile(old, []byte(`{"timezone":"Europe/Berlin"}`), 0o600)
	if !loadStore(old).Snapshot().SetupDone {
		t.Error("vorhandene Installation ohne setupDone gilt als eingerichtet")
	}
	explicit := filepath.Join(d, "neu.json")
	os.WriteFile(explicit, []byte(`{"setupDone":false}`), 0o600)
	if loadStore(explicit).Snapshot().SetupDone {
		t.Error("setupDone:false bleibt")
	}
	if loadStore(filepath.Join(d, "fehlt.json")).Snapshot().SetupDone {
		t.Error("frische Installation: Assistent")
	}
}

func TestDiscoverUnknownKind(t *testing.T) {
	if _, err := discover("ftp"); err == nil {
		t.Error("unbekannte Art muss Fehler sein")
	}
}
