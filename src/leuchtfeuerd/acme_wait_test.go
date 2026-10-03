package main

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mholt/acmez/v3/acme"
	"github.com/miekg/dns"
)

// fakeDNS: ein Server spielt Resolver und beide Nameserver der Zone example.de (ns1/ns2, beide 127.0.0.1); welcher
// Nameserver gefragt wird, unterscheidet er nicht – stattdessen zählt er die TXT-Abfragen und antwortet nach Plan.
type fakeDNS struct {
	mu   sync.Mutex
	txt  func(n int) []string // TXT-Werte bei der n-ten Abfrage
	n    int
	addr string
}

func (f *fakeDNS) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	q := r.Question[0]
	hdr := func(t uint16) dns.RR_Header {
		return dns.RR_Header{Name: q.Name, Rrtype: t, Class: dns.ClassINET, Ttl: 60}
	}
	switch {
	case q.Qtype == dns.TypeNS && strings.EqualFold(q.Name, "example.de."):
		m.Answer = []dns.RR{&dns.NS{Hdr: hdr(dns.TypeNS), Ns: "ns1.example.de."}, &dns.NS{Hdr: hdr(dns.TypeNS), Ns: "ns2.example.de."}}
	case q.Qtype == dns.TypeA && strings.HasPrefix(q.Name, "ns"):
		m.Answer = []dns.RR{&dns.A{Hdr: hdr(dns.TypeA), A: net.ParseIP("127.0.0.1")}}
	case q.Qtype == dns.TypeTXT:
		f.mu.Lock()
		f.n++
		vals := f.txt(f.n)
		f.mu.Unlock()
		for _, v := range vals {
			m.Answer = append(m.Answer, &dns.TXT{Hdr: hdr(dns.TypeTXT), Txt: []string{v}})
		}
	}
	w.WriteMsg(m)
}

// set und count greifen unter der Sperre zu: der Server antwortet nebenher in eigenen Goroutinen.
func (f *fakeDNS) set(txt func(n int) []string) { f.mu.Lock(); f.txt = txt; f.mu.Unlock() }
func (f *fakeDNS) count() int                   { f.mu.Lock(); defer f.mu.Unlock(); return f.n }

func startFakeDNS(t *testing.T, f *fakeDNS) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: f}
	go srv.ActivateAndServe()
	t.Cleanup(func() { srv.Shutdown() })
	f.addr = pc.LocalAddr().String()
}

func TestNSWaitNeedsAllNameservers(t *testing.T) {
	ch := acme.Challenge{Identifier: acme.Identifier{Type: "dns", Value: "invoke.example.de"}, KeyAuthorization: "token.key"}
	want := ch.DNS01KeyAuthorization()
	// Abfrage 1+2 (erste Runde): ns1 kennt den Wert, ns2 nur einen alten; ab Runde 2 beide
	f := &fakeDNS{txt: func(n int) []string {
		if n == 2 {
			return []string{"alt"}
		}
		return []string{"alt", want}
	}}
	startFakeDNS(t, f)
	_, port, _ := net.SplitHostPort(f.addr)
	s := newNSWaitSolver(nil, "", 5*time.Second)
	s.resolvers, s.nsPort, s.interval = []string{f.addr}, port, 50*time.Millisecond
	if err := s.Wait(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if n := f.count(); n < 4 {
		t.Fatalf("nach der ersten Runde für verbreitet gehalten (%d Abfragen)", n)
	}
	// nie überall: Zeitüberschreitung mit Fehler
	f.set(func(int) []string { return []string{"alt"} })
	s.timeout = 300 * time.Millisecond
	if err := s.Wait(context.Background(), ch); err == nil || !strings.Contains(err.Error(), "fehlt noch") {
		t.Fatalf("Fehler erwartet, bekam %v", err)
	}
}
