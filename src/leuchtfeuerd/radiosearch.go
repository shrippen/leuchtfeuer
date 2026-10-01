package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Sendersuche über radio-browser.info (frei, ohne Schlüssel). leuchtfeuerd fragt selbst (der Browser bräuchte sonst
// Zugriff auf fremde Server, die Sicherheitsrichtlinie erlaubt nur die eigene Seite). Die Server-Liste kommt von
// all.api.radio-browser.info/json/servers; fällt einer aus, nimmt die Suche den nächsten. Beim Abspielen eines
// gefundenen Senders meldet leuchtfeuerd den "Klick" (so wünscht es radio-browser, die Zahl hilft beim Sortieren).

const radioBrowserUA = "Leuchtfeuer/1.0 (+https://git.arianw.de/shrippen/leuchtfeuer)"

type radioStation struct {
	UUID    string `json:"stationuuid"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Country string `json:"countrycode"`
	Codec   string `json:"codec"`
	Bitrate int    `json:"bitrate"`
	Tags    string `json:"tags"`
	Votes   int    `json:"votes"`
}

type radioBrowser struct {
	mu      sync.Mutex
	servers []string
	fetched time.Time
	client  *http.Client
	base    string // fest (Tests); leer = Server-Liste
}

func newRadioBrowser() *radioBrowser {
	return &radioBrowser{client: &http.Client{Timeout: 8 * time.Second}}
}

func (rb *radioBrowser) get(u string, v any) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", radioBrowserUA)
	resp, err := rb.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// hosts liefert die Server in zufälliger Reihenfolge (Liste 6 h gültig).
func (rb *radioBrowser) hosts() []string {
	if rb.base != "" {
		return []string{rb.base}
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.servers) == 0 || time.Since(rb.fetched) > 6*time.Hour {
		var list []struct {
			Name string `json:"name"`
		}
		if err := rb.get("https://all.api.radio-browser.info/json/servers", &list); err == nil {
			seen := map[string]bool{}
			rb.servers = nil
			for _, s := range list {
				if s.Name != "" && !seen[s.Name] {
					seen[s.Name] = true
					rb.servers = append(rb.servers, "https://"+s.Name)
				}
			}
			rb.fetched = time.Now()
		}
		if len(rb.servers) == 0 {
			rb.servers = []string{"https://de1.api.radio-browser.info", "https://de2.api.radio-browser.info"}
		}
	}
	out := append([]string{}, rb.servers...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// Search sucht Sender nach Name (und optional Land), sortiert nach Stimmen; nur funktionierende, nur http(s).
func (rb *radioBrowser) Search(q, country string) ([]radioStation, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 && len(country) != 2 {
		return nil, fmt.Errorf("mindestens 2 Zeichen oder ein Land / at least 2 characters or a country")
	}
	// ohne Suchbegriff: die beliebtesten Sender des Landes
	p := url.Values{"hidebroken": {"true"}, "order": {"votes"}, "reverse": {"true"}, "limit": {"40"}}
	if len([]rune(q)) >= 2 {
		p.Set("name", q)
	}
	if len(country) == 2 {
		p.Set("countrycode", strings.ToUpper(country))
	}
	var lastErr error
	for i, h := range rb.hosts() {
		if i >= 3 {
			break
		}
		var raw []struct {
			radioStation
			Resolved string `json:"url_resolved"`
		}
		if err := rb.get(h+"/json/stations/search?"+p.Encode(), &raw); err != nil {
			lastErr = err
			continue
		}
		out := []radioStation{}
		for _, r := range raw {
			s := r.radioStation
			if r.Resolved != "" {
				s.URL = r.Resolved
			}
			s.Name = strings.TrimSpace(s.Name)
			if s.Name == "" || !(strings.HasPrefix(s.URL, "http://") || strings.HasPrefix(s.URL, "https://")) {
				continue
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("Senderverzeichnis nicht erreichbar: %v", lastErr)
}

// Click meldet das Abspielen eines Senders (im Hintergrund, Fehler egal).
func (rb *radioBrowser) Click(uuid string) {
	if uuid == "" || strings.ContainsAny(uuid, "/?#") {
		return
	}
	go func() {
		var v map[string]any
		for i, h := range rb.hosts() {
			if i >= 2 || rb.get(h+"/json/url/"+url.PathEscape(uuid), &v) == nil {
				return
			}
		}
	}()
}
