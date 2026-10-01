package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// Mehrere Lautsprecher: Jedes Leuchtfeuer meldet sich per mDNS als _leuchtfeuer._tcp (Name, Version, Gerätekennung)
// und sucht die anderen. Eingetragen wird ein anderes Gerät mit seiner Adresse und einem API-Schlüssel (Umfang "full",
// dort unter System > Zugang angelegt). Dann zeigt die Übersicht alle Geräte (erreichbar, Version, Lautstärke, was
// läuft), kopiert Einstellungsbereiche auf andere Geräte und stößt dort Updates an. Die Anfragen stellt invoked selbst
// (der Browser spricht nur mit diesem Gerät). Bei HTTPS merkt sich invoked beim Eintragen den Fingerabdruck des
// Zertifikats (selbst signiert) und verlangt danach genau dieses.
// Der Sprachassistent meldet sich zusätzlich als _wyoming._tcp (Home Assistant findet ihn so von selbst).

type Peer struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Token       string `json:"token,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"` // SHA-256 des TLS-Zertifikats (nur https)
}

type foundPeer struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Version string `json:"version"`
	ID      string `json:"id"`
	Known   bool   `json:"known"`
}

type peerStatus struct {
	Peer
	Online  bool   `json:"online"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
	Volume  int    `json:"volume"`
	Muted   bool   `json:"muted"`
	Playing string `json:"playing,omitempty"` // Quelle und Titel
	TempC   int    `json:"tempC"`
}

// Bereiche, die sich auf andere Geräte kopieren lassen (nicht: Gerätename, Passwort, Sprachassistent, Geräte-Liste).
var copySections = []string{"radio", "alarms", "buttons", "viz", "sources", "eq", "holidays", "timezone", "briefing", "homeAssistant", "mqtt", "syslog", "wifi"}

type peerHub struct {
	a     *app
	mu    sync.Mutex
	found map[string]foundPeer
	id    string
	stopZ []func()

	statusFn func() []peerStatus // Demo-Modus
}

func newPeerHub(a *app) *peerHub {
	b, _ := os.ReadFile("/sys/class/net/wlan0/address")
	id := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(string(b)), ":", ""))
	if id == "" {
		id = "invoke"
	}
	return &peerHub{a: a, found: map[string]foundPeer{}, id: id}
}

func normPeerURL(u string) (string, error) {
	u = strings.TrimSpace(strings.TrimRight(u, "/"))
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || (p.Path != "" && p.Path != "/") {
		return "", fmt.Errorf("Adresse wie http://invoke-kueche.lan oder https://192.168.1.23")
	}
	return p.Scheme + "://" + p.Host, nil
}

func certFP(c *x509.Certificate) string {
	s := sha256.Sum256(c.Raw)
	return hex.EncodeToString(s[:])
}

// client: für https nur das gemerkte Zertifikat (oder beim Eintragen: jedes, Fingerabdruck wird gemerkt).
func peerClient(fp string, seen *string) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 {
					return fmt.Errorf("kein Zertifikat")
				}
				c, err := x509.ParseCertificate(raw[0])
				if err != nil {
					return err
				}
				got := certFP(c)
				if seen != nil {
					*seen = got
				}
				if fp != "" && got != fp {
					return fmt.Errorf("Zertifikat hat sich geändert (erwartet %.16s…, bekommen %.16s…)", fp, got)
				}
				return nil
			}},
		DialContext:         (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSHandshakeTimeout: 4 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: 8 * time.Second}
}

func (h *peerHub) call(p Peer, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.URL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := peerClient(p.Fingerprint, nil).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s: %s", p.Name, e.Error)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (h *peerHub) peers() []Peer { return h.a.st.Snapshot().Peers }

func (h *peerHub) find(u string) (Peer, error) {
	for _, p := range h.peers() {
		if p.URL == u {
			return p, nil
		}
	}
	return Peer{}, fmt.Errorf("Gerät %s ist nicht eingetragen", u)
}

// Add prüft Adresse und Schlüssel (Status abrufen) und trägt das Gerät ein.
func (h *peerHub) Add(name, rawURL, token string) (Peer, error) {
	u, err := normPeerURL(rawURL)
	if err != nil {
		return Peer{}, err
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		return Peer{}, fmt.Errorf("API-Schlüssel des anderen Geräts nötig (lf_…)")
	}
	var fp string
	req, _ := http.NewRequest("GET", u+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := peerClient("", &fp).Do(req)
	if err != nil {
		return Peer{}, fmt.Errorf("nicht erreichbar: %v", err)
	}
	defer resp.Body.Close()
	var st struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if resp.StatusCode != 200 {
		return Peer{}, fmt.Errorf("Antwort %s (Schlüssel falsch oder nur lesend?)", resp.Status)
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st)
	if strings.TrimSpace(name) == "" {
		name = st.Name
	}
	p := Peer{Name: strings.TrimSpace(name), URL: u, Token: token}
	if strings.HasPrefix(u, "https://") {
		p.Fingerprint = fp
	}
	err = h.a.st.Update(func(s *Settings) {
		for i := range s.Peers {
			if s.Peers[i].URL == u {
				s.Peers[i] = p
				return
			}
		}
		s.Peers = append(s.Peers, p)
	})
	log.Printf("Gerät %s (%s) eingetragen", p.Name, u)
	return p, err
}

func (h *peerHub) Delete(u string) error {
	return h.a.st.Update(func(s *Settings) {
		var keep []Peer
		for _, p := range s.Peers {
			if p.URL != u {
				keep = append(keep, p)
			}
		}
		s.Peers = keep
	})
}

// Status fragt alle eingetragenen Geräte gleichzeitig ab.
func (h *peerHub) Status() []peerStatus {
	if h.statusFn != nil {
		return h.statusFn()
	}
	ps := h.peers()
	out := make([]peerStatus, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p Peer) {
			defer wg.Done()
			var st statusResp
			s := peerStatus{Peer: p}
			s.Token, s.Fingerprint = "", ""
			if err := h.call(p, "GET", "/api/status", nil, &st); err != nil {
				s.Error = err.Error()
			} else {
				s.Online, s.Version, s.Volume, s.Muted, s.TempC = true, st.Version, st.Volume, st.Muted, int(st.Sys.TempC)
				for _, src := range st.Sources {
					if src.State == "playing" {
						s.Playing = src.Name
						if src.Title != "" {
							s.Playing += ": " + src.Title
						}
						break
					}
				}
			}
			out[i] = s
		}(i, p)
	}
	wg.Wait()
	return out
}

// Copy überträgt Einstellungsbereiche dieses Geräts auf ein anderes.
func (h *peerHub) Copy(u string, sections []string) error {
	p, err := h.find(u)
	if err != nil {
		return err
	}
	set := h.a.st.Snapshot()
	var m map[string]json.RawMessage
	b, _ := json.Marshal(set)
	json.Unmarshal(b, &m)
	ok := map[string]bool{}
	for _, s := range copySections {
		ok[s] = true
	}
	var done []string
	for _, sec := range sections {
		if !ok[sec] {
			return fmt.Errorf("Bereich %q lässt sich nicht kopieren", sec)
		}
		var body any = m[sec]
		switch sec { // Bereiche mit eigener Form beim Speichern
		case "holidays":
			body = map[string]string{"region": set.Holidays}
		case "timezone":
			body = map[string]string{"timezone": set.Timezone}
		}
		if err := h.call(p, "PUT", "/api/settings/"+sec, body, nil); err != nil {
			return fmt.Errorf("%s nach %s: %v", sec, p.Name, err)
		}
		done = append(done, sec)
	}
	log.Printf("Einstellungen %v nach %s kopiert", done, p.Name)
	return nil
}

// Update stößt auf dem anderen Gerät die Installation des neuesten Releases an.
func (h *peerHub) Update(u string) error {
	p, err := h.find(u)
	if err != nil {
		return err
	}
	return h.call(p, "POST", "/api/update/install", map[string]any{}, nil)
}

// Action führt auf dem anderen Gerät eine Aktion aus (z. B. stop_all).
func (h *peerHub) Action(u, action string) error {
	p, err := h.find(u)
	if err != nil {
		return err
	}
	return h.call(p, "POST", "/api/action", map[string]string{"action": action}, nil)
}

// ---- mDNS ----

// Announce meldet die Weboberfläche als _leuchtfeuer._tcp an.
func (h *peerHub) Announce(port int, tlsOn bool) {
	name := h.a.cfg.Get("DEVICE_NAME", "HK Invoke")
	scheme := "http"
	if tlsOn {
		scheme = "https"
	}
	txt := []string{"name=" + name, "version=" + h.a.version, "id=" + h.id, "scheme=" + scheme}
	ifs := mdnsIfaces()
	srv, err := zeroconf.Register("Leuchtfeuer-"+h.id, "_leuchtfeuer._tcp", "local.", port, txt, ifs)
	if err != nil {
		log.Printf("mDNS (_leuchtfeuer._tcp): %v", err)
		return
	}
	h.mu.Lock()
	h.stopZ = append(h.stopZ, srv.Shutdown)
	h.mu.Unlock()
}

// WyomingZeroconf: Anmeldung des Sprachassistenten (für voiceSat.zc).
func (h *peerHub) WyomingZeroconf(port int) func() {
	srv, err := zeroconf.Register(h.id, "_wyoming._tcp", "local.", port, nil, mdnsIfaces())
	if err != nil {
		log.Printf("mDNS (_wyoming._tcp): %v", err)
		return func() {}
	}
	return srv.Shutdown
}

func mdnsIfaces() []net.Interface {
	if ni, err := net.InterfaceByName("wlan0"); err == nil {
		return []net.Interface{*ni}
	}
	return nil
}

// Browse sucht alle 2 Minuten 4 s lang nach anderen Leuchtfeuern.
func (h *peerHub) Browse() {
	for {
		h.browseOnce(4 * time.Second)
		time.Sleep(2 * time.Minute)
	}
}

func (h *peerHub) browseOnce(d time.Duration) {
	r, err := zeroconf.NewResolver(zeroconf.SelectIfaces(mdnsIfaces()), zeroconf.SelectIPTraffic(zeroconf.IPv4))
	if err != nil {
		return
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	got := map[string]foundPeer{}
	done := make(chan struct{})
	go func() {
		for e := range entries {
			kv := map[string]string{}
			for _, t := range e.Text {
				if k, v, ok := strings.Cut(t, "="); ok {
					kv[k] = v
				}
			}
			if kv["id"] == h.id || len(e.AddrIPv4) == 0 {
				continue
			}
			scheme := kv["scheme"]
			if scheme != "https" {
				scheme = "http"
			}
			host := e.AddrIPv4[0].String()
			u := scheme + "://" + host
			if !(scheme == "http" && e.Port == 80) && !(scheme == "https" && e.Port == 443) {
				u += ":" + strconv.Itoa(e.Port)
			}
			got[kv["id"]] = foundPeer{Name: kv["name"], URL: u, Version: kv["version"], ID: kv["id"]}
		}
		close(done)
	}()
	if err := r.Browse(ctx, "_leuchtfeuer._tcp", "local.", entries); err != nil {
		return
	}
	<-ctx.Done()
	<-done
	h.mu.Lock()
	h.found = got
	h.mu.Unlock()
}

func (h *peerHub) Found() []foundPeer {
	known := map[string]bool{}
	for _, p := range h.peers() {
		known[p.URL] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []foundPeer{}
	for _, f := range h.found {
		f.Known = known[f.URL]
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
