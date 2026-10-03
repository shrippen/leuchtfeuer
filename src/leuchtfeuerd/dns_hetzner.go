package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/libdns/libdns"
)

// hetznerDNS: libdns-Anbieter für die DNS-API der Hetzner Console (api.hetzner.cloud, seit Mai 2026 die einzige; die
// alte dns.hetzner.com ist abgeschaltet). Eigene, schlanke Umsetzung statt libdns/hetzner/v2: die zieht hcloud-go samt
// Prometheus-Client mit und machte leuchtfeuerd rund 4,5 MB größer. Hier nur, was die DNS-Challenge braucht: TXT-Einträge
// anlegen und wieder entfernen.
type hetznerDNS struct {
	Token   string
	BaseURL string // Standard https://api.hetzner.cloud/v1 (Tests: eigener Server)
	Client  *http.Client
}

type hzRecord struct {
	Value string `json:"value"`
}

type hzAction struct {
	ID     int64  `json:"id"`
	Status string `json:"status"` // running | success | error
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *hetznerDNS) base() string {
	if p.BaseURL != "" {
		return strings.TrimSuffix(p.BaseURL, "/")
	}
	return "https://api.hetzner.cloud/v1"
}

func (p *hetznerDNS) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base()+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", "application/json")
	c := p.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			return res.StatusCode, fmt.Errorf("Hetzner: %s (%s)", e.Error.Message, e.Error.Code)
		}
		return res.StatusCode, fmt.Errorf("Hetzner: HTTP %d", res.StatusCode)
	}
	if out != nil {
		return res.StatusCode, json.Unmarshal(b, out)
	}
	return res.StatusCode, nil
}

// rrsetPath: /zones/<zone>/rrsets/<name>/TXT/actions/<op>; name relativ zur Zone, "@" für die Zone selbst.
func hzPath(zone, name, op string) string {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" {
		name = "@"
	}
	return "/zones/" + url.PathEscape(strings.TrimSuffix(zone, ".")) + "/rrsets/" + url.PathEscape(name) + "/TXT/actions/" + op
}

func hzQuote(s string) string {
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) >= 2 {
		return s
	}
	return strconv.Quote(s)
}

// wait fragt die Aktionen ab, bis sie fertig sind (Hetzner arbeitet Änderungen asynchron ab).
func (p *hetznerDNS) wait(ctx context.Context, a hzAction) error {
	for a.Status == "running" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		var r struct {
			Actions []hzAction `json:"actions"`
		}
		if _, err := p.do(ctx, "GET", "/actions?id="+strconv.FormatInt(a.ID, 10), nil, &r); err != nil {
			return err
		}
		if len(r.Actions) == 0 {
			return fmt.Errorf("Hetzner: Aktion %d nicht gefunden", a.ID)
		}
		a = r.Actions[0]
	}
	if a.Status == "error" && a.Error != nil {
		return fmt.Errorf("Hetzner: %s (%s)", a.Error.Message, a.Error.Code)
	}
	return nil
}

func (p *hetznerDNS) change(ctx context.Context, zone string, recs []libdns.Record, op string) ([]libdns.Record, error) {
	var done []libdns.Record
	for _, r := range recs {
		rr := r.RR()
		if !strings.EqualFold(rr.Type, "TXT") {
			return done, fmt.Errorf("Hetzner: nur TXT-Einträge (DNS-Challenge), nicht %s", rr.Type)
		}
		body := map[string]any{"records": []hzRecord{{Value: hzQuote(rr.Data)}}}
		if op == "add_records" {
			ttl := max(int(rr.TTL.Seconds()), 60) // kleinste TTL, die die API annimmt
			body["ttl"] = ttl
		}
		var res struct {
			Action hzAction `json:"action"`
		}
		code, err := p.do(ctx, "POST", hzPath(zone, rr.Name, op), body, &res)
		if err != nil {
			if op == "remove_records" && code == http.StatusNotFound {
				continue // schon weg
			}
			return done, err
		}
		if err := p.wait(ctx, res.Action); err != nil {
			return done, err
		}
		done = append(done, r)
	}
	return done, nil
}

func (p *hetznerDNS) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return p.change(ctx, zone, recs, "add_records")
}

func (p *hetznerDNS) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return p.change(ctx, zone, recs, "remove_records")
}
