package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	// bekannter Wert (RFC 7914, PBKDF2-HMAC-SHA256, "passwd"/"salt", 1 Runde)
	got := pbkdf2SHA256([]byte("passwd"), []byte("salt"), 1, 16)
	if h := strings.ToLower(string([]byte(func() []byte {
		b := make([]byte, 0)
		for _, c := range got {
			b = append(b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&15])
		}
		return b
	}()))); h != "55ac046e56e3089fec1691c22544b605" {
		t.Fatalf("PBKDF2 weicht ab: %s", h)
	}
	h := hashPassword("geheim!")
	if strings.Contains(h, "geheim") || !verifyPassword("geheim!", h) || verifyPassword("falsch", h) || verifyPassword("geheim!", "x") {
		t.Fatal("Hash/Prüfung falsch")
	}
	if hashPassword("geheim!") == h {
		t.Fatal("Salz fehlt")
	}
}

func TestStoreAndLogin(t *testing.T) {
	f := t.TempDir() + "/config"
	os.WriteFile(f, []byte("DEVICE_NAME=\"x\"\nWEB_PASSWORD=\"alt123\"\n"), 0o600)
	cfg := &shellConfig{path: f}
	h, err := storePassword(cfg, "pa$$ w\"ort")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if strings.Contains(string(b), "WEB_PASSWORD=") || strings.Contains(string(b), "pa$$") || cfg.Get("WEB_PASSWORD_HASH", "") != h {
		t.Fatalf("Config falsch: %s", b)
	}
	w := &webServer{login: newLoginState(h)}
	srv := httptest.NewServer(w.auth(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write([]byte("geheim")) })))
	defer srv.Close()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if r, _ := c.Get(srv.URL + "/api/status"); r.StatusCode != 401 {
		t.Fatalf("API ohne Sitzung: %d", r.StatusCode)
	}
	if r, _ := c.Get(srv.URL + "/"); r.StatusCode != 302 {
		t.Fatalf("Seite ohne Sitzung: %d", r.StatusCode)
	}
	if r, _ := c.Post(srv.URL+"/api/login", "application/json", strings.NewReader(`{"password":"pa$$ w\"ort"}`)); r.StatusCode != 200 || len(r.Cookies()) == 0 {
		t.Fatalf("Login: %d", r.StatusCode)
	} else {
		req, _ := http.NewRequest("GET", srv.URL+"/api/status", nil)
		req.AddCookie(r.Cookies()[0])
		if r2, _ := c.Do(req); r2.StatusCode != 200 {
			t.Fatalf("mit Sitzung: %d", r2.StatusCode)
		}
		w.login.setHash(h) // Passwortwechsel beendet Sitzungen
		if r2, _ := c.Do(req); r2.StatusCode != 401 {
			t.Fatalf("Sitzung blieb gültig: %d", r2.StatusCode)
		}
	}
}
