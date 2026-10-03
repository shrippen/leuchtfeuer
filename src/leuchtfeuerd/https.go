package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"github.com/libdns/desec"
	"github.com/libdns/libdns"
	"github.com/libdns/netcup"
	"go.uber.org/zap"
)

// HTTPS der Weboberfläche (WEB_TLS in der config):
//   ""    aus (nur HTTP)
//   "on"  eigenes, selbst signiertes Zertifikat (tls.go); der Browser warnt beim ersten Besuch
//   "acme" Zertifikat von Let's Encrypt über die DNS-Challenge (certmagic, Caddys Zertifikats-Kern). Der Lautsprecher
//         muss dafür nicht aus dem Internet erreichbar sein: certmagic legt einen TXT-Eintrag beim DNS-Anbieter an.
//         Der Name (ACME_DOMAIN) muss im eigenen Netz auf den Lautsprecher zeigen (Router, Pi-hole, eigener DNS).
//         Bis das Zertifikat da ist, und für Aufrufe über die IP-Adresse, gilt das eigene Zertifikat.
// Zugangsdaten des DNS-Anbieters stehen nur in der config (Rechte 600), nie in der Oberfläche oder im Diagnosepaket.

// dnsProviders: unterstützte Anbieter (Schlüssel in ACME_DNS) mit ihren Feldern in der config.
var dnsProviders = []string{"cloudflare", "hetzner", "desec", "netcup"}

var domainRe = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

type httpsStatus struct {
	Mode     string `json:"mode"`               // "" | on | acme
	Domain   string `json:"domain,omitempty"`   // ACME_DOMAIN
	State    string `json:"state,omitempty"`    // acme: obtaining | ok | error
	Error    string `json:"error,omitempty"`    // letzter Fehler beim Abholen oder Erneuern
	Issuer   string `json:"issuer,omitempty"`   // Aussteller des Let's-Encrypt-Zertifikats
	NotAfter string `json:"notAfter,omitempty"` // gültig bis (RFC 3339)
	Staging  bool   `json:"staging,omitempty"`  // Test-CA von Let's Encrypt
}

type httpsMgr struct {
	mode, domain string
	staging      bool
	self         tls.Certificate
	magic        *certmagic.Config

	mu       sync.Mutex
	state    string
	err      string
	notAfter time.Time // des Let's-Encrypt-Zertifikats, gemerkt bei cert_obtained / cached_managed_cert
	issuer   string
}

// dnsProvider baut den libdns-Anbieter aus der config.
func dnsProvider(c *shellConfig) (certmagic.DNSProvider, error) {
	tok := c.Get("ACME_DNS_TOKEN", "")
	var p interface {
		libdns.RecordAppender
		libdns.RecordDeleter
	}
	switch c.Get("ACME_DNS", "") {
	case "cloudflare":
		p = &cloudflare.Provider{APIToken: tok}
	case "hetzner":
		p = &hetznerDNS{Token: tok}
	case "desec":
		p = &desec.Provider{Token: tok}
	case "netcup":
		p = &netcup.Provider{CustomerNumber: c.Get("ACME_NETCUP_CUSTOMER", ""), APIKey: tok, APIPassword: c.Get("ACME_NETCUP_PASSWORD", "")}
		if c.Get("ACME_NETCUP_CUSTOMER", "") == "" || c.Get("ACME_NETCUP_PASSWORD", "") == "" {
			return nil, fmt.Errorf("netcup braucht Kundennummer, API-Schlüssel und API-Passwort")
		}
	default:
		return nil, fmt.Errorf("DNS-Anbieter %q unbekannt", c.Get("ACME_DNS", ""))
	}
	if tok == "" {
		return nil, fmt.Errorf("Zugangsdaten des DNS-Anbieters fehlen")
	}
	return p, nil
}

// newHTTPS liest WEB_TLS; nil, wenn HTTPS aus ist. Fehler beim eigenen Zertifikat schalten HTTPS ab (Aufrufer fällt auf HTTP zurück).
func newHTTPS(c *shellConfig, dir string) (*httpsMgr, error) {
	mode := c.Get("WEB_TLS", "")
	if mode != "on" && mode != "acme" {
		return nil, nil
	}
	self, err := ensureCert(dir, hostName(c))
	if err != nil {
		return nil, err
	}
	m := &httpsMgr{mode: mode, self: self}
	if mode == "on" {
		return m, nil
	}
	m.domain = strings.ToLower(strings.TrimSpace(c.Get("ACME_DOMAIN", "")))
	m.staging = c.Get("ACME_STAGING", "") == "on"
	if err := m.setupACME(c, dir); err != nil {
		// ohne gültige Angaben bleibt es beim eigenen Zertifikat, die Oberfläche zeigt den Fehler
		m.state, m.err = "error", err.Error()
		logf("HTTPS: Let's Encrypt nicht möglich: %v - eigenes Zertifikat", err)
	}
	return m, nil
}

func (m *httpsMgr) setupACME(c *shellConfig, dir string) error {
	if !domainRe.MatchString(m.domain) {
		return fmt.Errorf("Name %q ist kein gültiger Domainname", m.domain)
	}
	prov, err := dnsProvider(c)
	if err != nil {
		return err
	}
	// netcup übernimmt Änderungen erst nach einigen Minuten in seine Nameserver
	timeout := 3 * time.Minute
	if c.Get("ACME_DNS", "") == "netcup" {
		timeout = 30 * time.Minute
	}
	ca := certmagic.LetsEncryptProductionCA
	if m.staging {
		ca = certmagic.LetsEncryptStagingCA
	}
	cache := certmagic.NewCache(certmagic.CacheOptions{GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return m.magic, nil }})
	m.magic = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: filepath.Join(dir, "acme")},
		Logger:  zap.NewNop(),
		OnEvent: m.onEvent,
	})
	m.magic.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(m.magic, certmagic.ACMEIssuer{
		CA: ca, Email: strings.TrimSpace(c.Get("ACME_EMAIL", "")), Agreed: true,
		DisableHTTPChallenge: true, DisableTLSALPNChallenge: true,
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{DNSProvider: prov, PropagationTimeout: timeout}},
		Logger:      zap.NewNop(),
	})}
	return nil
}

// Start holt das Zertifikat im Hintergrund und erneuert es rechtzeitig (certmagic-Wartung).
func (m *httpsMgr) Start(ctx context.Context) {
	if m == nil || m.magic == nil {
		return
	}
	m.setState("obtaining", "")
	if err := m.magic.ManageAsync(ctx, []string{m.domain}); err != nil {
		m.setState("error", err.Error())
	}
}

// remember liest das verwaltete Zertifikat (Ablauf, Aussteller) für die Statusanzeige. Nicht aus dem Ereignis heraus
// aufrufen (certmagic hält dann eigene Sperren), sondern nebenläufig.
func (m *httpsMgr) remember() {
	c, err := m.magic.CacheManagedCertificate(context.Background(), m.domain)
	if err != nil || len(c.Certificate.Certificate) == 0 {
		return
	}
	leaf := c.Leaf
	if leaf == nil {
		if leaf, err = x509.ParseCertificate(c.Certificate.Certificate[0]); err != nil {
			return
		}
	}
	m.mu.Lock()
	m.notAfter, m.issuer = leaf.NotAfter, strings.Join(leaf.Issuer.Organization, " ")
	m.mu.Unlock()
}

func (m *httpsMgr) setState(state, err string) {
	m.mu.Lock()
	m.state, m.err = state, err
	m.mu.Unlock()
}

func (m *httpsMgr) onEvent(_ context.Context, event string, data map[string]any) error {
	switch event {
	case "cert_obtaining":
		m.mu.Lock()
		if m.state != "ok" {
			m.state = "obtaining"
		}
		m.mu.Unlock()
	case "cert_obtained", "cached_managed_cert":
		m.setState("ok", "")
		go m.remember()
		if event == "cert_obtained" {
			logf("HTTPS: Zertifikat von Let's Encrypt für %s erhalten", m.domain)
		}
	case "cert_failed":
		msg := "unbekannter Fehler"
		if e, ok := data["error"].(error); ok {
			msg = e.Error()
		}
		m.mu.Lock()
		// eine gescheiterte Erneuerung lässt das gültige Zertifikat in Betrieb
		if m.state != "ok" {
			m.state = "error"
		}
		m.err = msg
		m.mu.Unlock()
		logf("HTTPS: Let's Encrypt für %s gescheitert: %s", m.domain, msg)
	}
	return nil
}

// GetCertificate: für den Let's-Encrypt-Namen dessen Zertifikat, sonst (IP-Adresse, .lan, noch nicht abgeholt) das eigene.
func (m *httpsMgr) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// ohne Verbindung (nur in Tests denkbar) nicht an certmagic: das greift bei fehlendem Zertifikat auf hello.Conn zu
	if m.magic != nil && hello.Conn != nil && strings.EqualFold(strings.TrimSuffix(hello.ServerName, "."), m.domain) {
		if c, err := m.magic.GetCertificate(hello); err == nil {
			return c, nil
		}
	}
	return &m.self, nil
}

func (m *httpsMgr) TLSConfig() *tls.Config {
	return &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}
}

func (m *httpsMgr) Status() httpsStatus {
	if m == nil {
		return httpsStatus{}
	}
	s := httpsStatus{Mode: m.mode, Domain: m.domain, Staging: m.staging}
	if m.mode != "acme" {
		return s
	}
	m.mu.Lock()
	s.State, s.Error, s.Issuer = m.state, m.err, m.issuer
	if !m.notAfter.IsZero() {
		s.NotAfter = m.notAfter.Format(time.RFC3339)
	}
	m.mu.Unlock()
	return s
}

// httpsSettings: Formular der Oberfläche (System > HTTPS); Geheimnisse nur als „gesetzt“.
type httpsSettings struct {
	Mode           string `json:"mode"`
	Port           int    `json:"port"`
	Domain         string `json:"domain"`
	Email          string `json:"email"`
	DNS            string `json:"dns"`
	Staging        bool   `json:"staging"`
	TokenSet       bool   `json:"tokenSet"`
	NetcupCustomer string `json:"netcupCustomer"`
	NetcupPassSet  bool   `json:"netcupPasswordSet"`
	// nur beim Speichern; leer = behalten
	Token          string `json:"token,omitempty"`
	NetcupPassword string `json:"netcupPassword,omitempty"`
}

func readHTTPSSettings(c *shellConfig) httpsSettings {
	port, _ := strconv.Atoi(c.Get("WEB_TLS_PORT", "443"))
	return httpsSettings{
		Mode: c.Get("WEB_TLS", ""), Port: port, Domain: c.Get("ACME_DOMAIN", ""), Email: c.Get("ACME_EMAIL", ""),
		DNS: c.Get("ACME_DNS", ""), Staging: c.Get("ACME_STAGING", "") == "on", TokenSet: c.Get("ACME_DNS_TOKEN", "") != "",
		NetcupCustomer: c.Get("ACME_NETCUP_CUSTOMER", ""), NetcupPassSet: c.Get("ACME_NETCUP_PASSWORD", "") != "",
	}
}

// httpsConfig prüft das Formular und liefert die Schlüssel für die config.
func httpsConfig(v httpsSettings, old httpsSettings) (map[string]string, error) {
	kv := map[string]string{}
	switch v.Mode {
	case "", "off":
		kv["WEB_TLS"] = ""
		return kv, nil
	case "on", "acme":
		kv["WEB_TLS"] = v.Mode
	default:
		return nil, fmt.Errorf("HTTPS-Art %q unbekannt", v.Mode)
	}
	if v.Port < 1 || v.Port > 65535 {
		return nil, fmt.Errorf("Port %d ungültig", v.Port)
	}
	kv["WEB_TLS_PORT"] = strconv.Itoa(v.Port)
	if v.Mode == "on" {
		return kv, nil
	}
	v.Domain = strings.ToLower(strings.TrimSpace(v.Domain))
	if !domainRe.MatchString(v.Domain) {
		return nil, fmt.Errorf("Name %q ist kein gültiger Domainname (z. B. lautsprecher.example.de)", v.Domain)
	}
	known := false
	for _, p := range dnsProviders {
		known = known || p == v.DNS
	}
	if !known {
		return nil, fmt.Errorf("DNS-Anbieter wählen")
	}
	if email := strings.TrimSpace(v.Email); email != "" && (!strings.Contains(email, "@") || strings.ContainsAny(email, " \"'")) {
		return nil, fmt.Errorf("E-Mail-Adresse %q ungültig", email)
	}
	if v.Token == "" && !(old.TokenSet && old.DNS == v.DNS) {
		return nil, fmt.Errorf("Zugangsdaten (Token bzw. API-Schlüssel) des DNS-Anbieters fehlen")
	}
	if v.DNS == "netcup" {
		if strings.TrimSpace(v.NetcupCustomer) == "" {
			return nil, fmt.Errorf("netcup: Kundennummer fehlt")
		}
		if v.NetcupPassword == "" && !(old.NetcupPassSet && old.DNS == "netcup") {
			return nil, fmt.Errorf("netcup: API-Passwort fehlt")
		}
		kv["ACME_NETCUP_CUSTOMER"] = strings.TrimSpace(v.NetcupCustomer)
		if v.NetcupPassword != "" {
			kv["ACME_NETCUP_PASSWORD"] = v.NetcupPassword
		}
	}
	kv["ACME_DOMAIN"], kv["ACME_EMAIL"], kv["ACME_DNS"] = v.Domain, strings.TrimSpace(v.Email), v.DNS
	kv["ACME_STAGING"] = map[bool]string{true: "on", false: ""}[v.Staging]
	if v.Token != "" {
		kv["ACME_DNS_TOKEN"] = strings.TrimSpace(v.Token)
	}
	return kv, nil
}
