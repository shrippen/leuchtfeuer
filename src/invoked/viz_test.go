package main

import (
	"math"
	"testing"
)

func TestSpectrumBandOfSine(t *testing.T) {
	x := make([]float32, vizFFT)
	for i := range x {
		x[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000))
	}
	b := spectrumBands(x, 48000)
	best := 0
	for i := range b {
		if b[i] > b[best] {
			best = i
		}
	}
	if best != 6 { // 1 kHz liegt im Band 837 ... 1340 Hz
		t.Fatalf("Maximum in Band %d, erwartet 6 (%v)", best, b)
	}
}

func TestRenderLevelSymmetric(t *testing.T) {
	var fr ringFrame
	vals := make([]float64, vizLEDs)
	for i := range vals {
		vals[i] = 0.5
	}
	renderRing(&fr, vals, VizSettings{Mode: "level", Color: "white", Brightness: 100, Rotate: 3}, 0)
	lit := 0
	for i, c := range fr {
		if c != fr[(2*3-1-i+2*vizLEDs)%vizLEDs] { // gespiegelt um die Start-LED 3
			t.Fatalf("nicht symmetrisch: %v", fr)
		}
		if c[0] > 0 {
			lit++
		}
	}
	if lit != 6 {
		t.Fatalf("halber Pegel sollte 6 LEDs zeigen, sind %d: %v", lit, fr)
	}
	if fr[3][0] != 255 || fr[2][0] != 255 {
		t.Fatalf("Start-LED 3 und ihr Spiegel 2 sollten voll leuchten: %v", fr)
	}
}

func TestRenderBrightnessAndOff(t *testing.T) {
	var fr ringFrame
	vals := make([]float64, vizLEDs)
	vals[0] = 1
	renderRing(&fr, vals, VizSettings{Mode: "spectrum", Color: "red", Brightness: 50}, 0)
	if fr[0][0] != 128 || fr[1] != [3]byte{} {
		t.Fatalf("unerwartet: %v", fr)
	}
}
