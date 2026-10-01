package main

// Leuchtring: vorhandene Animationen über audio-ui/mcu-interface (WAMP com.harman.ledAnimate / ledOff).
// Die Muster liegen als Dateien unter /usr/share/lights (je Bild 39 Byte = 13 LEDs x RGB).

type ledHW struct{ h *hub }

func (l *ledHW) Animate(name string, repeat bool) {
	var kw map[string]any
	if repeat {
		kw = map[string]any{"repeat": true}
	}
	go l.h.CallKw("com.harman.ledAnimate", kw, name)
}

func (l *ledHW) Off() { go l.h.Call("com.harman.ledOff") }
