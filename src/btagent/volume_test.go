package main

import "testing"

// Handy (0..127) und Gerät (0..100 %) müssen sich ohne Drift hin und her abbilden lassen.
func TestVolumeMappingRoundTrip(t *testing.T) {
	for n := 0; n <= 100; n++ {
		if back := phoneToMusic(musicToPhone(n)); back != n {
			t.Errorf("%d %% -> %d/127 -> %d %%", n, musicToPhone(n), back)
		}
	}
	if musicToPhone(0) != 0 || musicToPhone(100) != 127 || phoneToMusic(127) != 100 {
		t.Fatal("Grenzen")
	}
}
