package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libdns/libdns"
)

// netcupDNS: libdns-Anbieter für die DNS-API von netcup (CCP-Webservice, JSON). Eigene Umsetzung statt libdns/netcup:
// Die ließ TXT-Einträge gescheiterter Versuche stehen; weil netcup Änderungen erst nach Minuten auf alle Nameserver
// verteilt, fand Let's Encrypt dann einen alten Wert ("Incorrect TXT record") und scheiterte jedes Mal wieder.
// Hier ersetzt AppendRecords alle TXT-Einträge unter dem Challenge-Namen in einer einzigen Änderung durch den neuen Wert.
type netcupDNS struct {
	Customer string
	Key      string
	Password string
	BaseURL  string // Standard netcupAPI (Tests: eigener Server)
	Client   *http.Client
	mu       sync.Mutex // netcup mag keine gleichzeitigen Sitzungen eines Kontos
}

const netcupAPI = "https://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON"

type ncRecord struct {
	ID          string `json:"id,omitempty"`
	Hostname    string `json:"hostname"`
	Type        string `json:"type"`
	Priority    string `json:"priority,omitempty"`
	Destination string `json:"destination"`
	Delete      bool   `json:"deleterecord"`
}

type ncRecordSet struct {
	Records []ncRecord `json:"dnsrecords"`
}

func (p *netcupDNS) call(ctx context.Context, action string, param map[string]any, out any) error {
	param["customernumber"], param["apikey"] = p.Customer, p.Key
	b, err := json.Marshal(map[string]any{"action": action, "param": param})
	if err != nil {
		return err
	}
	u := p.BaseURL
	if u == "" {
		u = netcupAPI
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := p.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != 200 {
		return fmt.Errorf("netcup %s: HTTP %d", action, res.StatusCode)
	}
	var r struct {
		Status string          `json:"status"`
		Short  string          `json:"shortmessage"`
		Long   string          `json:"longmessage"`
		Data   json.RawMessage `json:"responsedata"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("netcup %s: Antwort unlesbar", action)
	}
	if r.Status != "success" {
		msg := strings.TrimSpace(r.Short + " " + r.Long)
		return fmt.Errorf("netcup %s: %s", action, msg)
	}
	if out != nil && len(r.Data) > 0 && string(r.Data) != `""` {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

// session meldet sich an, ruft f auf und meldet sich wieder ab.
func (p *netcupDNS) session(ctx context.Context, f func(sid string) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var login struct {
		SID string `json:"apisessionid"`
	}
	if err := p.call(ctx, "login", map[string]any{"apipassword": p.Password}, &login); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p.call(ctx, "logout", map[string]any{"apisessionid": login.SID}, nil)
	}()
	return f(login.SID)
}

func (p *netcupDNS) records(ctx context.Context, sid, zone string) ([]ncRecord, error) {
	var set ncRecordSet
	err := p.call(ctx, "infoDnsRecords", map[string]any{"apisessionid": sid, "domainname": zone}, &set)
	return set.Records, err
}

func (p *netcupDNS) update(ctx context.Context, sid, zone string, recs []ncRecord) error {
	return p.call(ctx, "updateDnsRecords", map[string]any{"apisessionid": sid, "domainname": zone, "dnsrecordset": ncRecordSet{recs}}, nil)
}

// ncHost: Name relativ zur Zone, wie netcup ihn führt ("@" für die Zone selbst).
func ncHost(name string) string {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" {
		return "@"
	}
	return name
}

func ncTXT(rec libdns.Record) (libdns.RR, error) {
	rr := rec.RR()
	if !strings.EqualFold(rr.Type, "TXT") {
		return rr, fmt.Errorf("netcup: nur TXT-Einträge (DNS-Challenge), nicht %s", rr.Type)
	}
	return rr, nil
}

func ncSameValue(a, b string) bool { return strings.Trim(a, `"`) == strings.Trim(b, `"`) }

// AppendRecords legt die TXT-Einträge an und löscht dabei alle anderen TXT-Einträge unter denselben Namen (Reste
// früherer Versuche). Die Namen sind nur für die Challenge da; certmagic löst je Name eine Challenge zugleich.
func (p *netcupDNS) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	zone = strings.TrimSuffix(zone, ".")
	var add []ncRecord
	names := map[string]bool{}
	for _, r := range recs {
		rr, err := ncTXT(r)
		if err != nil {
			return nil, err
		}
		h := ncHost(rr.Name)
		names[h] = true
		add = append(add, ncRecord{Hostname: h, Type: "TXT", Destination: rr.Data})
	}
	err := p.session(ctx, func(sid string) error {
		old, err := p.records(ctx, sid, zone)
		if err != nil {
			// eine Zone ganz ohne Einträge meldet netcup als Fehler; anlegen geht trotzdem
			old = nil
		}
		var change []ncRecord
		for _, o := range old {
			if strings.EqualFold(o.Type, "TXT") && names[strings.ToLower(o.Hostname)] {
				o.Delete = true
				change = append(change, o)
			}
		}
		if n := len(change); n > 0 {
			logf("HTTPS: netcup: %d alte Challenge-Einträge entfernt", n)
		}
		return p.update(ctx, sid, zone, append(change, add...))
	})
	if err != nil {
		return nil, err
	}
	return recs, nil
}

// DeleteRecords entfernt die TXT-Einträge mit genau diesem Namen und Wert.
func (p *netcupDNS) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	zone = strings.TrimSuffix(zone, ".")
	var done []libdns.Record
	err := p.session(ctx, func(sid string) error {
		old, err := p.records(ctx, sid, zone)
		if err != nil {
			return err
		}
		var change []ncRecord
		for _, r := range recs {
			rr, err := ncTXT(r)
			if err != nil {
				return err
			}
			for _, o := range old {
				if strings.EqualFold(o.Type, "TXT") && strings.EqualFold(o.Hostname, ncHost(rr.Name)) && ncSameValue(o.Destination, rr.Data) {
					o.Delete = true
					change = append(change, o)
					done = append(done, r)
					break
				}
			}
		}
		if len(change) == 0 {
			return nil // schon weg
		}
		return p.update(ctx, sid, zone, change)
	})
	return done, err
}
