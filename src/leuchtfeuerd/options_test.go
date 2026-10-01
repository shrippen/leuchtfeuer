package main

import (
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
