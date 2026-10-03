package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/mholt/acmez/v3/acme"
	"github.com/miekg/dns"
)

// nsWaitSolver: DNS-Challenge mit eigener Wartelogik. certmagic hält einen TXT-Eintrag für verbreitet, sobald *ein*
// zuständiger Nameserver ihn kennt, und fragt dafür den Resolver des Systems (auf dem Invoke dnsmasq, der dabei oft nicht
// antwortete). Let's Encrypt prüft aber von mehreren Orten aus gegen beliebige Nameserver der Zone; bei netcup, das
// Änderungen erst nach Minuten auf alle Server verteilt, scheiterte die Prüfung deshalb ("During secondary validation").
// Hier: warten, bis *alle* zuständigen Nameserver den Wert liefern. Gefragt werden öffentliche Resolver, der des Systems
// zuletzt.
type nsWaitSolver struct {
	*certmagic.DNS01Solver
	resolvers []string      // rekursive Resolver ("ip:port")
	timeout   time.Duration // höchstens so lange warten
	interval  time.Duration // Abstand der Prüfungen
	nsPort    string        // Port der zuständigen Nameserver (Tests), Standard 53
}

var publicResolvers = []string{"1.1.1.1:53", "9.9.9.9:53", "8.8.8.8:53"}

// acmeResolvers: öffentliche Resolver, danach die des Systems (falls ausgehendes DNS ins Internet gesperrt ist).
func acmeResolvers() []string {
	out := append([]string{}, publicResolvers...)
	for _, r := range certmagic.RecursiveNameservers(nil) {
		known := false
		for _, o := range out {
			known = known || o == r
		}
		if !known {
			out = append(out, r)
		}
	}
	return out
}

// newNSWaitSolver: alias = Challenge-Domain (CNAME-Ziel) oder leer.
func newNSWaitSolver(prov certmagic.DNSProvider, alias string, timeout time.Duration) *nsWaitSolver {
	res := acmeResolvers()
	s := &certmagic.DNS01Solver{}
	s.DNSProvider, s.OverrideDomain = prov, alias
	s.Resolvers = res         // auch die Zone der Challenge über diese Resolver finden
	s.PropagationTimeout = -1 // eigene Prüfung in Wait
	return &nsWaitSolver{DNS01Solver: s, resolvers: res, timeout: timeout, interval: 10 * time.Second}
}

// Wait blockiert, bis alle zuständigen Nameserver den Challenge-Wert liefern.
func (s *nsWaitSolver) Wait(ctx context.Context, ch acme.Challenge) error {
	name := ch.DNS01TXTRecordName()
	if s.OverrideDomain != "" {
		name = s.OverrideDomain
	}
	want := ch.DNS01KeyAuthorization()
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	start, lastLog := time.Now(), time.Now()
	for {
		ok, n, err := s.propagated(ctx, dns.Fqdn(name), want)
		if ok {
			logf("HTTPS: Challenge-Eintrag nach %s auf allen %d Nameservern", time.Since(start).Round(time.Second), n)
			return nil
		}
		if time.Since(lastLog) >= 5*time.Minute {
			logf("HTTPS: warte auf Challenge-Eintrag: %v", err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Challenge-Eintrag %s nach %s nicht auf allen Nameservern: %v", name, s.timeout, err)
		case <-time.After(s.interval):
		}
	}
}

// propagated: true, wenn jeder zuständige Nameserver den Wert liefert; n = Zahl der Nameserver.
func (s *nsWaitSolver) propagated(ctx context.Context, fqdn, want string) (bool, int, error) {
	// _acme-challenge per CNAME umgeleitet (ohne eingetragene Challenge-Domain): dem Ziel folgen
	if r, err := s.ask(ctx, fqdn, dns.TypeCNAME, s.resolvers, true); err == nil {
		for _, rr := range r.Answer {
			if c, ok := rr.(*dns.CNAME); ok && strings.EqualFold(c.Hdr.Name, fqdn) {
				fqdn = dns.Fqdn(c.Target)
			}
		}
	}
	ns, err := s.authNS(ctx, fqdn)
	if err != nil {
		return false, 0, err
	}
	var missing []string
	for _, addr := range ns {
		r, err := s.ask(ctx, fqdn, dns.TypeTXT, []string{addr}, false)
		if err != nil || !hasTXT(r, want) {
			missing = append(missing, addr)
		}
	}
	if len(missing) > 0 {
		return false, len(ns), fmt.Errorf("fehlt noch bei %s", strings.Join(missing, ", "))
	}
	return true, len(ns), nil
}

func hasTXT(r *dns.Msg, want string) bool {
	for _, rr := range r.Answer {
		if t, ok := rr.(*dns.TXT); ok && strings.Join(t.Txt, "") == want {
			return true
		}
	}
	return false
}

// authNS: Adressen der zuständigen Nameserver der Zone, in der fqdn liegt (von unten nach oben die erste Zone mit NS).
func (s *nsWaitSolver) authNS(ctx context.Context, fqdn string) ([]string, error) {
	port := s.nsPort
	if port == "" {
		port = "53"
	}
	labels := dns.SplitDomainName(fqdn)
	for i := range labels {
		zone := dns.Fqdn(strings.Join(labels[i:], "."))
		r, err := s.ask(ctx, zone, dns.TypeNS, s.resolvers, true)
		if err != nil {
			return nil, err
		}
		var hosts []string
		for _, rr := range r.Answer {
			if n, ok := rr.(*dns.NS); ok && strings.EqualFold(n.Hdr.Name, zone) {
				hosts = append(hosts, n.Ns)
			}
		}
		if len(hosts) == 0 {
			continue
		}
		var addrs []string
		for _, h := range hosts {
			a, err := s.ask(ctx, h, dns.TypeA, s.resolvers, true)
			if err != nil {
				continue
			}
			for _, rr := range a.Answer {
				if ip, ok := rr.(*dns.A); ok {
					addrs = append(addrs, net.JoinHostPort(ip.A.String(), port))
					break // eine Adresse je Nameserver genügt
				}
			}
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("Nameserver von %s ohne Adresse", zone)
		}
		return addrs, nil
	}
	return nil, fmt.Errorf("keine Nameserver für %s gefunden", fqdn)
}

// ask fragt die Server der Reihe nach, bis einer antwortet (UDP, bei Abschneiden TCP).
func (s *nsWaitSolver) ask(ctx context.Context, name string, qtype uint16, servers []string, recursive bool) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = recursive
	m.SetEdns0(4096, false)
	var last error
	for _, srv := range servers {
		c := &dns.Client{Net: "udp", Timeout: 4 * time.Second}
		r, _, err := c.ExchangeContext(ctx, m, srv)
		if err == nil && r.Truncated {
			c.Net = "tcp"
			r, _, err = c.ExchangeContext(ctx, m, srv)
		}
		if err != nil {
			last = err
			continue
		}
		if r.Rcode != dns.RcodeSuccess && r.Rcode != dns.RcodeNameError {
			last = fmt.Errorf("%s: %s", srv, dns.RcodeToString[r.Rcode])
			continue
		}
		return r, nil
	}
	if last == nil {
		last = fmt.Errorf("kein Resolver")
	}
	return nil, last
}
