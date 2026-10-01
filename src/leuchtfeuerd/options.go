package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

// Auswahllisten für die Oberfläche: was auf dem Gerät oder in Home Assistant wirklich vorhanden ist, damit niemand
// interne Namen (ALSA-Geräte, Entitäten) kennen muss.

type audioInput struct {
	ID          string `json:"id"`             // ALSA-Gerät für arecord
	Name        string `json:"name,omitempty"` // Name der Karte (die Oberfläche setzt die Beschriftung zweisprachig zusammen)
	Card        int    `json:"card"`
	Device      int    `json:"device"`
	Recommended bool   `json:"recommended"` // leuchtfeuer_mic: Vorgabe der Tonkette
}

var pcmLine = regexp.MustCompile(`^(\d+)-(\d+): ([^:]*):.*\bcapture \d+`)
var cardLine = regexp.MustCompile(`^\s*(\d+) \[(\S+)\s*\]: \S+ - (.+)$`)

// audioInputs liest die Aufnahmegeräte aus /proc/asound (procDir). Zuerst steht leuchtfeuer_mic aus der Tonkette des
// Zielgeräts (auf dem Invoke der einzige Weg, den Codec richtig zu lesen), danach die Karten roh. Loopback-Karten
// (snd-aloop) sind keine Mikrofone und fehlen.
func audioInputs(procDir string) []audioInput {
	out := []audioInput{{ID: "leuchtfeuer_mic", Card: -1, Device: -1, Recommended: true}}
	if procDir == "" { // Demo: nur die Tonkette
		return out
	}
	cards := map[string][2]string{} // Nummer -> {ID, Name}
	if b, err := os.ReadFile(filepath.Join(procDir, "cards")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if m := cardLine.FindStringSubmatch(l); m != nil {
				cards[m[1]] = [2]string{m[2], strings.TrimSpace(m[3])}
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(procDir, "pcm"))
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		m := pcmLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		card, _ := strconv.Atoi(m[1])
		dev, _ := strconv.Atoi(m[2])
		c := cards[strconv.Itoa(card)]
		if c[0] == "Loopback" {
			continue
		}
		name := c[1]
		if name == "" {
			name = strings.TrimSpace(m[3])
		}
		out = append(out, audioInput{ID: fmt.Sprintf("plughw:%d,%d", card, dev), Name: name, Card: card, Device: dev})
	}
	return out
}

type haOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"` // Sensoren: Einheit
}

// haOptions fragt Home Assistant nach Sprachausgabe-Entitäten und Bereichen (für Briefing und Sprachassistent).
// Braucht Adresse und Token unter Home Assistant; Vorlagen-API (Administrator-Token).
func (a *app) haOptions() (map[string][]haOption, error) {
	set := a.st.Snapshot()
	if demoMode { // keine echten Server: nur was die Demodaten schon eingestellt haben
		return map[string][]haOption{"tts": {{ID: set.HA.TTSEngine, Name: set.HA.TTSEngine}},
			"areas": {{ID: set.Voice.Area, Name: set.Voice.Area}}, "sensors": {}}, nil
	}
	ha := set.HA
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	get := func(tpl string) ([]haOption, error) {
		s, err := a.brief.haTemplate(ctx, ha, tpl)
		if err != nil {
			return nil, err
		}
		var v []haOption
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("Home Assistant: unerwartete Antwort")
		}
		sort.Slice(v, func(i, j int) bool { return strings.ToLower(v[i].Name) < strings.ToLower(v[j].Name) })
		return v, nil
	}
	tts, err := get(`[{% for s in states.tts %}{"id": {{ s.entity_id | tojson }}, "name": {{ s.name | tojson }}}{{ "," if not loop.last }}{% endfor %}]`)
	if err != nil {
		return nil, err
	}
	areas, err := get(`[{% for a in areas() %}{"id": {{ a | tojson }}, "name": {{ area_name(a) | tojson }}}{{ "," if not loop.last }}{% endfor %}]`)
	if err != nil {
		return nil, err
	}
	sensors, err := get(`[{% for s in states.sensor %}{"id": {{ s.entity_id | tojson }}, "name": {{ s.name | tojson }}, "unit": {{ (s.attributes.unit_of_measurement or "") | tojson }}}{{ "," if not loop.last }}{% endfor %}]`)
	if err != nil {
		return nil, err
	}
	return map[string][]haOption{"tts": tts, "areas": areas, "sensors": sensors}, nil
}

// ---- Suche im Netz (mDNS): Music Assistant (Sendspin), MQTT-Broker, Home Assistant ----

type foundService struct {
	Name string `json:"name"`
	Host string `json:"host"` // IPv4
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"` // Home Assistant: Adresse aus dem TXT-Eintrag
}

var discoverTypes = map[string]string{"sendspin": "_sendspin-server._tcp", "mqtt": "_mqtt._tcp", "ha": "_home-assistant._tcp"}

// discover sucht 3 s nach einem Dienst-Typ im WLAN. Im Demo-Modus nichts (kein echtes Netz).
func discover(kind string) ([]foundService, error) {
	typ, ok := discoverTypes[kind]
	if !ok {
		return nil, fmt.Errorf("unbekannte Art %q", kind)
	}
	out := []foundService{}
	if demoMode {
		return out, nil
	}
	r, err := zeroconf.NewResolver(zeroconf.SelectIfaces(mdnsIfaces()), zeroconf.SelectIPTraffic(zeroconf.IPv4))
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seen := map[string]bool{}
	done := make(chan struct{})
	go func() {
		for e := range entries {
			if len(e.AddrIPv4) == 0 {
				continue
			}
			f := foundService{Name: e.Instance, Host: e.AddrIPv4[0].String(), Port: e.Port}
			for _, t := range e.Text {
				if k, v, ok := strings.Cut(t, "="); ok && (k == "internal_url" || (k == "base_url" && f.URL == "")) && v != "" {
					f.URL = v
				}
			}
			if kind == "ha" && f.URL == "" {
				f.URL = fmt.Sprintf("http://%s:%d", f.Host, f.Port)
			}
			key := fmt.Sprintf("%s:%d", f.Host, f.Port)
			if !seen[key] {
				seen[key] = true
				out = append(out, f)
			}
		}
		close(done)
	}()
	if err := r.Browse(ctx, typ, "local.", entries); err != nil {
		return nil, err
	}
	<-ctx.Done()
	<-done
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ---- Home Assistant: Verbindung testen ----

type haTestResult struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Admin   bool   `json:"admin"`   // Vorlagen-API erlaubt (Briefing-Vorlagen, Auswahllisten)
	TTS     int    `json:"tts"`     // Anzahl Sprachausgabe-Entitäten
	Message string `json:"message"` // was nicht geht, mit Hinweis
}

// haTest prüft Adresse und Token (leeres Token: das gespeicherte).
func (a *app) haTest(url, token string) haTestResult {
	ha := a.st.Snapshot().HA
	if url = strings.TrimSpace(url); url != "" {
		ha.URL = url
	}
	if token = strings.TrimSpace(token); token != "" {
		ha.Token = token
	}
	if demoMode {
		return haTestResult{OK: true, Version: "demo", Admin: true, TTS: 1}
	}
	if ha.URL == "" || ha.Token == "" {
		return haTestResult{Message: "Adresse und Token nötig / address and token needed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(ha.URL, "/")+"/api/config", nil)
	if err != nil {
		return haTestResult{Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+ha.Token)
	resp, err := a.brief.client.Do(req)
	if err != nil {
		return haTestResult{Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return haTestResult{Message: "Home Assistant /api/config: " + resp.Status}
	}
	var cfg struct {
		Version string `json:"version"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cfg)
	res := haTestResult{OK: true, Version: cfg.Version}
	if s, err := a.brief.haTemplate(ctx, ha, "{{ states.tts | list | count }}"); err == nil {
		res.Admin = true
		res.TTS, _ = strconv.Atoi(strings.TrimSpace(s))
	} else {
		res.Message = err.Error()
	}
	return res
}

// ---- Briefing: Kalender prüfen, bevor er gespeichert wird ----

type calCheck struct {
	Total int      `json:"total"` // Termine in der Datei
	Day   int      `json:"day"`   // am gewählten Tag
	Next  []string `json:"next"`  // die ersten Titel an diesem Tag
}

func (a *app) checkCalendar(u string, days int) (calCheck, error) {
	if demoMode {
		return calCheck{}, fmt.Errorf("im Demo-Modus nicht verfügbar / not in demo mode")
	}
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "webcal://") {
		u = "https://" + strings.TrimPrefix(u, "webcal://")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return calCheck{}, fmt.Errorf("keine http(s)-Adresse / not an http(s) address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body, err := a.brief.get(ctx, u, nil)
	if err != nil {
		return calCheck{}, err
	}
	loc, _ := time.LoadLocation(a.st.Snapshot().Timezone)
	if loc == nil {
		loc = time.UTC
	}
	evs, err := parseICS(string(body), loc)
	if err != nil {
		return calCheck{}, err
	}
	day := time.Now().In(loc).AddDate(0, 0, clamp(days, 0, 7))
	list := eventsOn(evs, day, loc)
	out := calCheck{Total: len(evs), Day: len(list), Next: []string{}}
	for _, e := range list {
		if len(out.Next) == 3 {
			break
		}
		out.Next = append(out.Next, e.Summary)
	}
	return out, nil
}

// ---- Mikrofon: Pegel für die Anzeige in der Oberfläche ----

// micLevel liefert den Pegel (dBFS, RMS und Spitze). Nimmt der Sprachassistent gerade auf, sein letzter Wert (das
// Gerät ist dann belegt), sonst eine kurze eigene Aufnahme von dev.
func (a *app) micLevel(dev string) (map[string]float64, error) {
	if demoMode {
		return nil, fmt.Errorf("im Demo-Modus nicht verfügbar / not in demo mode")
	}
	if a.voice != nil {
		if rms, peak, ok := a.voice.level(); ok {
			return map[string]float64{"rms": rms, "peak": peak}, nil
		}
	}
	if dev == "" {
		dev = "leuchtfeuer_mic"
	}
	if strings.ContainsAny(dev, " \t'\"$`;|&") {
		return nil, fmt.Errorf("ungültiges Gerät")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "arecord", "-q", "-D", dev, "-f", "S16_LE", "-r", "16000", "-c", "1", "-d", "1", "-t", "raw").Output()
	if err != nil {
		return nil, fmt.Errorf("Mikrofon %s: %v", dev, err)
	}
	rms, peak := levelS16(out)
	return map[string]float64{"rms": rms, "peak": peak}, nil
}

// levelS16: RMS und Spitze von S16_LE-Daten in dBFS (Stille: -120).
func levelS16(b []byte) (rms, peak float64) {
	n := len(b) / 2
	if n == 0 {
		return -120, -120
	}
	var sum, pk float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(b[2*i:]))) / 32768
		sum += v * v
		if math.Abs(v) > pk {
			pk = math.Abs(v)
		}
	}
	db := func(x float64) float64 {
		if x <= 1e-6 {
			return -120
		}
		return math.Round(20*math.Log10(x)*10) / 10
	}
	return db(math.Sqrt(sum / float64(n))), db(pk)
}
