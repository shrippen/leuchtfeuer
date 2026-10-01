package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPITokensAndOrigin(t *testing.T) {
	ts := loadTokens(filepath.Join(t.TempDir(), "tokens.json"))
	full, _, err := ts.Create("Home Assistant", "full")
	if err != nil {
		t.Fatal(err)
	}
	ro, info, _ := ts.Create("Prometheus", "read")
	if _, _, err := ts.Create("", "full"); err == nil {
		t.Fatal("leerer Name angenommen")
	}
	if !strings.HasPrefix(full, "lf_") || len(full) != 67 {
		t.Fatalf("Format: %s", full)
	}
	b, _ := os.ReadFile(ts.path)
	if strings.Contains(string(b), full[3:]) {
		t.Fatal("Schlüssel im Klartext gespeichert")
	}
	w := &webServer{login: newLoginState(hashPassword("geheim1")), tokens: ts}
	srv := httptest.NewServer(w.auth(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write([]byte("ok")) })))
	defer srv.Close()
	do := func(method, path, tok string, hdr map[string]string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s %s: keine Sicherheits-Header", method, path)
		}
		return r.StatusCode
	}
	cases := []struct {
		method, path, tok string
		want              int
	}{
		{"GET", "/api/status", full, 200},
		{"POST", "/api/volume", full, 200},
		{"GET", "/metrics", ro, 200},
		{"GET", "/api/status", ro, 200},
		{"POST", "/api/volume", ro, 403},
		{"GET", "/api/tokens", full, 403},    // Zugangsverwaltung nur mit Sitzung
		{"POST", "/api/ssh-keys", full, 403}, // dito
		{"PUT", "/api/settings/device", full, 403},
		{"GET", "/", full, 403}, // Seiten nicht mit Schlüssel
		{"GET", "/api/status", "lf_" + strings.Repeat("0", 64), 401},
		{"GET", "/metrics", "", 401},
	}
	for _, c := range cases {
		if got := do(c.method, c.path, c.tok, nil); got != c.want {
			t.Errorf("%s %s (%.6s): %d statt %d", c.method, c.path, c.tok, got, c.want)
		}
	}
	// fremde Herkunft: auch die Anmeldung wird abgewiesen
	if got := do("POST", "/api/login", "", map[string]string{"Origin": "http://evil.example"}); got != 403 {
		t.Errorf("fremde Herkunft: %d", got)
	}
	if got := do("POST", "/api/login", "", map[string]string{"Origin": srv.URL}); got != 401 {
		t.Errorf("eigene Herkunft (falsches Passwort erwartet): %d", got)
	}
	if err := ts.Delete(info.ID); err != nil {
		t.Fatal(err)
	}
	if got := do("GET", "/api/status", ro, nil); got != 401 {
		t.Errorf("gelöschter Schlüssel gilt noch: %d", got)
	}
	if ts2 := loadTokens(ts.path); len(ts2.List()) != 1 || ts2.Check(full) != "full" {
		t.Fatal("Speichern/Laden")
	}
}

func TestSameOrigin(t *testing.T) {
	mk := func(method, origin, ref string) *http.Request {
		r := httptest.NewRequest(method, "http://invoke.lan/api/x", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if ref != "" {
			r.Header.Set("Referer", ref)
		}
		return r
	}
	ok := []*http.Request{mk("GET", "http://evil", ""), mk("POST", "", ""), mk("POST", "http://invoke.lan", ""), mk("POST", "", "http://invoke.lan/#/settings")}
	bad := []*http.Request{mk("POST", "http://evil", ""), mk("PUT", "null", "http://evil/x"), mk("POST", "", "http://invoke.lan.evil/")}
	for i, r := range ok {
		if !sameOrigin(r) {
			t.Errorf("ok %d abgelehnt", i)
		}
	}
	for i, r := range bad {
		if sameOrigin(r) {
			t.Errorf("bad %d angenommen", i)
		}
	}
}

// ed25519-Schlüssel im richtigen Format (Zufallsbytes, nur für Tests)
const testKey1 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFEIQ6gZp2urAOdx/BZsINQbuMBnPVIMOqHCgpa/wSQq test@one"
const testKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDBnS+EMpEuYH5nB8p7YHoS7dE3bLiss+P7WTAZpzDkc test@two"

const testKey1FP = "SHA256:z29VaDZyW6IYInRApqnt5SRv9XIeVAIautzIsE+idto" // wie ssh-keygen -l

func TestSSHKeys(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = old })
	os.WriteFile(authorizedKeysPath(), []byte("# Kommentar\n"+testKey1+"\n"), 0o600)
	k, err := parseSSHKey(testKey1)
	if err != nil || k.Type != "ssh-ed25519" || k.Comment != "test@one" || k.Fingerprint != testKey1FP {
		t.Fatalf("%+v %v", k, err)
	}
	for _, bad := range []string{"ssh-ed25519 kein-base64", "ssh-dss AAAAB3NzaC1kc3M= x", "ssh-rsa AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl x"} {
		if _, err := parseSSHKey(bad); err == nil {
			t.Errorf("angenommen: %s", bad)
		}
	}
	if _, err := addSSHKey(testKey1); err == nil {
		t.Fatal("doppelt eingetragen")
	}
	if _, err := addSSHKey(testKey2 + "\n" + testKey1); err == nil {
		t.Fatal("zwei Zeilen angenommen")
	}
	if _, err := addSSHKey(testKey2); err != nil {
		t.Fatal(err)
	}
	if l, _ := listSSHKeys(); len(l) != 2 {
		t.Fatalf("Liste: %v", l)
	}
	if err := deleteSSHKey(k.Fingerprint); err != nil {
		t.Fatal(err)
	}
	l, _ := listSSHKeys()
	if len(l) != 1 || l[0].Comment != "test@two" {
		t.Fatalf("nach Löschen: %v", l)
	}
	if err := deleteSSHKey(l[0].Fingerprint); err == nil {
		t.Fatal("letzter Schlüssel gelöscht")
	}
	b, _ := os.ReadFile(authorizedKeysPath())
	if !strings.HasPrefix(string(b), "# Kommentar\n") {
		t.Fatalf("Kommentar verloren: %q", b)
	}
}

func TestLogHubTailTruncateAndSyslog(t *testing.T) {
	dir := t.TempDir()
	oldL, oldH := logDir, hookLog
	logDir, hookLog = dir, filepath.Join(dir, "hook.log")
	t.Cleanup(func() { logDir, hookLog = oldL, oldH })
	f := filepath.Join(dir, "librespot.log")
	os.WriteFile(f, []byte("alt\n"), 0o644)
	ta := newTestApp(t, "2026-10-05 07:00:00")
	// Syslog-Empfänger (UDP)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	ta.st.Update(func(s *Settings) {
		s.Syslog = SyslogSettings{Enabled: true, Host: "127.0.0.1", Port: port, Proto: "udp"}
	})
	h := newLogHub(ta.app)
	h.pos["librespot"] = 4 // wie nach Run(): Altes überspringen
	c, stop := h.subscribe()
	defer stop()
	fh, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString("underrun!!! (at least 12 ms long)\nhalbe")
	h.poll()
	select {
	case l := <-c:
		if l.Service != "librespot" || !strings.HasPrefix(l.Text, "underrun") {
			t.Fatalf("%+v", l)
		}
	case <-time.After(time.Second):
		t.Fatal("keine Zeile")
	}
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := pc.ReadFrom(buf)
	if err != nil || !bytes.HasPrefix(buf[:n], []byte("<132>1 ")) || !bytes.Contains(buf[:n], []byte("leuchtfeuer-librespot - - - underrun")) {
		t.Fatalf("Syslog: %q %v", buf[:n], err)
	}
	fh.WriteString(" Zeile\n")
	h.poll()
	if l := <-c; l.Text != "halbe Zeile" {
		t.Fatalf("Zeilenrest: %q", l.Text)
	}
	// gekürzt (copytruncate): von vorne lesen
	fh.Close()
	os.WriteFile(f, []byte("neu\n"), 0o644)
	h.poll()
	if l := <-c; l.Text != "neu" {
		t.Fatalf("nach Kürzen: %q", l.Text)
	}
	if xr, _ := h.Counters(); xr["librespot"] != 1 {
		t.Fatalf("Aussetzer: %v", xr)
	}
	if syslogSeverity("FEHLER: x") != 3 || syslogSeverity("alles gut") != 6 {
		t.Fatal("Schweregrad")
	}
}

func TestMetricsFormat(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	ta.sysFn = func() sysStatus { return sysStatus{TempC: 61, UptimeSecs: 99} }
	ta.svcFn = func() []serviceInfo {
		return []serviceInfo{{serviceDef: serviceDef{Name: "librespot"}, Running: true, Enabled: true, Restarts: 2}}
	}
	ta.clkFn = func() clockStatus { return clockStatus{} }
	ta.wifiFn = func() wifiStatus { return wifiStatus{RSSI: -60, Good: true} }
	ta.btFn = func() btState { return btState{} }
	ta.mqttOK = func() bool { return false }
	ta.logs = newLogHub(nil)
	ta.logs.publish(logLine{Service: "bluetooth-4-aplay", Text: "underrun!!!"})
	w := &webServer{app: ta.app}
	var b bytes.Buffer
	w.writeMetrics(&b)
	out := b.String()
	for _, want := range []string{
		"# TYPE leuchtfeuer_temperature_celsius gauge\nleuchtfeuer_temperature_celsius 61\n",
		`leuchtfeuer_service_restarts_total{service="librespot"} 2`,
		`leuchtfeuer_audio_underruns_total{service="bluetooth-4-aplay"} 1`,
		"leuchtfeuer_wifi_rssi_dbm -60",
		`leuchtfeuer_source_playing{source="spotify"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fehlt: %s", want)
		}
	}
	if strings.Count(out, "# TYPE leuchtfeuer_source_playing") != 1 {
		t.Error("TYPE doppelt")
	}
	_ = io.Discard
}
