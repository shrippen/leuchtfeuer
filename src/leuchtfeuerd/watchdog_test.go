package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeartbeatFresh(t *testing.T) {
	d := t.TempDir()
	alive, up := filepath.Join(d, "alive"), filepath.Join(d, "uptime")
	os.WriteFile(up, []byte("1000.42 1800.10\n"), 0o644)
	for _, c := range []struct {
		hb   string
		want bool
	}{{"1000\n", true}, {"860", true}, {"849", false}, {"1200", false}, {"", false}, {"x", false}} {
		os.WriteFile(alive, []byte(c.hb), 0o644)
		if got := heartbeatFresh(alive, up); got != c.want {
			t.Errorf("Lebenszeichen %q: %v", c.hb, got)
		}
	}
	os.Remove(alive)
	if heartbeatFresh(alive, up) {
		t.Error("ohne Datei frisch")
	}
}
