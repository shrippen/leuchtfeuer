package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libdns/libdns"
)

func TestHTTPSConfigValidation(t *testing.T) {
	none := httpsSettings{}
	ok := httpsSettings{Mode: "acme", Port: 443, Domain: "Lautsprecher.Example.de", DNS: "cloudflare", Token: "t"}
	kv, err := httpsConfig(ok, none)
	if err != nil {
		t.Fatal(err)
	}
	if kv["WEB_TLS"] != "acme" || kv["ACME_DOMAIN"] != "lautsprecher.example.de" || kv["ACME_DNS_TOKEN"] != "t" || kv["WEB_TLS_PORT"] != "443" {
		t.Fatalf("Schlüssel falsch: %v", kv)
	}
	bad := map[string]httpsSettings{
		"Art":            {Mode: "x", Port: 443},
		"Port":           {Mode: "on", Port: 0},
		"Domain":         {Mode: "acme", Port: 443, Domain: "lautsprecher", DNS: "cloudflare", Token: "t"},
		"Domain-IP":      {Mode: "acme", Port: 443, Domain: "192.168.1.5", DNS: "cloudflare", Token: "t"},
		"Anbieter":       {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "route53", Token: "t"},
		"Token":          {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "desec"},
		"netcup-Nr":      {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "netcup", Token: "k", NetcupPassword: "p"},
		"netcup-Pass":    {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "netcup", Token: "k", NetcupCustomer: "1"},
		"porkbun-Secret": {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "porkbun", Token: "k"},
		"namecheap-User": {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "namecheap", Token: "k"},
		"acmedns-http":   {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "acmedns", Token: "k", User: "u", Server: "http://x.example", Subdomain: "s"},
		"acmedns-Sub":    {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "acmedns", Token: "k", User: "u", Server: "https://x.example"},
		"E-Mail":         {Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "desec", Token: "t", Email: "kein at"},
		"Zeilenumbruch":  {Mode: "acme", Port: 443, Domain: "a.example.de\nX=1", DNS: "desec", Token: "t"},
	}
	for name, v := range bad {
		if _, err := httpsConfig(v, none); err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
	for _, v := range []httpsSettings{
		{Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "gandi", Token: "t"},
		{Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "porkbun", Token: "k", Secret: "s"},
		{Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "namecheap", Token: "k", User: "u"},
		{Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "acmedns", Token: "k", User: "u", Server: "https://x.example", Subdomain: "s"},
	} {
		if _, err := httpsConfig(v, none); err != nil {
			t.Errorf("%s: %v", v.DNS, err)
		}
	}
	// leeres Token behält das gespeicherte, aber nur beim selben Anbieter
	old := httpsSettings{DNS: "hetzner", TokenSet: true}
	keep := httpsSettings{Mode: "acme", Port: 443, Domain: "a.example.de", DNS: "hetzner"}
	if kv, err := httpsConfig(keep, old); err != nil || kv["ACME_DNS_TOKEN"] != "" {
		t.Fatalf("Token behalten: %v %v", kv, err)
	}
	keep.DNS = "desec"
	if _, err := httpsConfig(keep, old); err == nil {
		t.Fatal("Anbieterwechsel ohne Token muss scheitern")
	}
	if kv, err := httpsConfig(httpsSettings{Mode: "off"}, none); err != nil || kv["WEB_TLS"] != "" {
		t.Fatalf("aus: %v %v", kv, err)
	}
}

func TestHTTPSFallbackToOwnCert(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	// Let's Encrypt ohne Zugangsdaten: Fehler wird angezeigt, HTTPS läuft mit dem eigenen Zertifikat
	os.WriteFile(cfgPath, []byte("WEB_TLS=\"acme\"\nACME_DOMAIN=\"a.example.de\"\nACME_DNS=\"desec\"\nDHCP_HOSTNAME=\"test\"\n"), 0o600)
	m, err := newHTTPS(&shellConfig{path: cfgPath}, dir)
	if err != nil || m == nil {
		t.Fatalf("newHTTPS: %v", err)
	}
	st := m.Status()
	if st.Mode != "acme" || st.State != "error" || !strings.Contains(st.Error, "Zugangsdaten") {
		t.Fatalf("Status: %+v", st)
	}
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.example.de"})
	if err != nil || c != &m.self {
		t.Fatal("ohne Let's-Encrypt-Zertifikat muss das eigene gelten")
	}
	// gültige Angaben: certmagic eingerichtet, fremde Namen bekommen das eigene Zertifikat
	os.WriteFile(cfgPath, []byte("WEB_TLS=\"acme\"\nACME_DOMAIN=\"a.example.de\"\nACME_DNS=\"desec\"\nACME_DNS_TOKEN=\"t\"\n"), 0o600)
	m, _ = newHTTPS(&shellConfig{path: cfgPath}, dir)
	if m.magic == nil {
		t.Fatalf("certmagic nicht eingerichtet: %+v", m.Status())
	}
	if c, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "192.168.1.5"}); c != &m.self {
		t.Fatal("IP-Adresse: eigenes Zertifikat erwartet")
	}
	if c, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.example.de"}); c != &m.self {
		t.Fatal("noch nicht abgeholt: eigenes Zertifikat erwartet")
	}
	// aus
	os.WriteFile(cfgPath, []byte("WEB_TLS=\"\"\n"), 0o600)
	if m, _ := newHTTPS(&shellConfig{path: cfgPath}, dir); m != nil {
		t.Fatal("HTTPS aus: kein Verwalter erwartet")
	}
}

func TestHetznerDNS(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			rw.WriteHeader(401)
			io.WriteString(rw, `{"error":{"code":"unauthorized","message":"unable to authenticate"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/add_records"):
			io.WriteString(rw, `{"action":{"id":7,"status":"running"}}`)
		case r.URL.Path == "/actions":
			io.WriteString(rw, `{"actions":[{"id":7,"status":"success"}]}`)
		case strings.HasSuffix(r.URL.Path, "/actions/remove_records"):
			rw.WriteHeader(404)
			io.WriteString(rw, `{"error":{"code":"not_found","message":"rrset not found"}}`)
		}
	}))
	defer srv.Close()
	p := &hetznerDNS{Token: "tok", BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rec := libdns.TXT{Name: "_acme-challenge.lautsprecher", Text: "abc-123", TTL: 30 * time.Second}
	got, err := p.AppendRecords(ctx, "example.de.", []libdns.Record{rec})
	if err != nil || len(got) != 1 {
		t.Fatalf("AppendRecords: %v %v", got, err)
	}
	if !strings.HasPrefix(calls[0], "POST /zones/example.de/rrsets/_acme-challenge.lautsprecher/TXT/actions/add_records ") {
		t.Fatalf("Pfad: %s", calls[0])
	}
	var body struct {
		Records []hzRecord `json:"records"`
		TTL     int        `json:"ttl"`
	}
	json.Unmarshal([]byte(calls[0][strings.Index(calls[0], "{"):]), &body)
	if len(body.Records) != 1 || body.Records[0].Value != `"abc-123"` || body.TTL != 60 {
		t.Fatalf("Anfrage: %+v", body)
	}
	if !strings.HasPrefix(calls[1], "GET /actions?id=7") {
		t.Fatalf("auf die Aktion warten: %v", calls)
	}
	// schon entfernt (404): kein Fehler
	if _, err := p.DeleteRecords(ctx, "example.de.", []libdns.Record{rec}); err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}
	if _, err := (&hetznerDNS{Token: "falsch", BaseURL: srv.URL}).AppendRecords(ctx, "example.de.", []libdns.Record{rec}); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("falsches Token: %v", err)
	}
}

func TestDNSProviderBuild(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	for dns, extra := range map[string]string{
		"gandi":     "",
		"porkbun":   "ACME_DNS_SECRET=\"s\"\n",
		"namecheap": "ACME_DNS_USER=\"u\"\n",
		"acmedns":   "ACME_DNS_USER=\"u\"\nACME_DNS_SERVER=\"https://x.example\"\nACME_DNS_SUBDOMAIN=\"s\"\n",
	} {
		os.WriteFile(cfgPath, []byte("ACME_DNS=\""+dns+"\"\nACME_DNS_TOKEN=\"t\"\n"+extra), 0o600)
		c := &shellConfig{path: cfgPath}
		migrateConfigSecrets(c, dir) // Geheimnisse wie im Betrieb verschlüsseln
		if p, err := dnsProvider(&shellConfig{path: cfgPath}, dir); err != nil || p == nil {
			t.Errorf("%s: %v", dns, err)
		}
		// ohne die Zusatzangaben muss es scheitern
		if extra != "" {
			os.WriteFile(cfgPath, []byte("ACME_DNS=\""+dns+"\"\nACME_DNS_TOKEN=\"t\"\n"), 0o600)
			if _, err := dnsProvider(&shellConfig{path: cfgPath}, dir); err == nil {
				t.Errorf("%s ohne Zusatzangaben: Fehler erwartet", dns)
			}
		}
	}
}
