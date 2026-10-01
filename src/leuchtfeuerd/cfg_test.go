package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Was leuchtfeuerd in die Shell-Konfiguration schreibt, muss sh genau so wieder einlesen (Sonderzeichen inklusive).
func TestShellConfigRoundTripThroughSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("kein sh")
	}
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("# Kommentar bleibt\nDEVICE_NAME=\"Alt\"\n\nOTHER='x'\n"), 0o600)
	c := &shellConfig{path: p}
	values := map[string]string{
		"DEVICE_NAME":     `Wohn"zimmer $HOME ` + "`id`" + ` \ ende`,
		"SENDSPIN_SERVER": "10.0.0.2:8927",
		"NEU":             "zeile1\nzeile2",
	}
	if err := c.Set(values); err != nil {
		t.Fatal(err)
	}
	for k, want := range values {
		want = strings.ReplaceAll(want, "\n", " ")
		out, err := exec.Command("sh", "-c", `. "$1"; eval "printf '%s' \"\${$2}\""`, "sh", p, k).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != want {
			t.Errorf("%s: sh liest %q, erwartet %q", k, out, want)
		}
		if got := c.Get(k, ""); got != strings.ReplaceAll(values[k], "\n", " ") && k != "DEVICE_NAME" && k != "NEU" {
			t.Errorf("%s: Get %q", k, got)
		}
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "# Kommentar bleibt") || !strings.Contains(string(b), "OTHER='x'") {
		t.Fatalf("fremde Zeilen verloren:\n%s", b)
	}
	c.Delete("NEU")
	if c.Get("NEU", "weg") != "weg" {
		t.Fatal("Delete")
	}
}

func TestHolidays(t *testing.T) {
	cases := []struct {
		region, day, want string
	}{
		{"DE", "2026-04-03", "Karfreitag"},
		{"DE", "2026-04-06", "Ostermontag"},
		{"DE", "2026-05-14", "Christi Himmelfahrt"},
		{"DE", "2026-05-25", "Pfingstmontag"},
		{"DE", "2026-06-04", ""},
		{"DE-BY", "2026-06-04", "Fronleichnam"},
		{"DE-HH", "2026-10-31", "Reformationstag"},
		{"DE-BY", "2026-10-31", ""},
		{"DE-SN", "2026-11-18", "Buß- und Bettag"},
		{"DE", "2026-12-26", "2. Weihnachtstag"},
		{"", "2026-12-25", ""},
	}
	for _, c := range cases {
		tm, _ := parseDayTime(c.day)
		if got := holidayName(c.region, tm); got != c.want {
			t.Errorf("%s %s: %q, erwartet %q", c.region, c.day, got, c.want)
		}
	}
	if e := easter(2027); e.Format("01-02") != "03-28" {
		t.Fatalf("Ostern 2027: %v", e)
	}
}
