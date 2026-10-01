package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Morgen-Briefing: eine kurze Folge aus Begrüßung (Datum, Feiertag), Wetter (Open-Meteo), Unwetterwarnungen (DWD über
// Bright Sky), Pollenflug (DWD), Terminen aus Kalendern (ICS, auch Müllabfuhr für morgen), Nachrichten als Podcast
// (z. B. tagesschau in 100 Sekunden), Texten aus Home Assistant (Vorlage, z. B. Fahrzeit zur Arbeit) und freiem Text.
//
// Texte werden gesprochen, wenn eine Sprachausgabe eingerichtet ist: Home Assistant (/api/tts_get_url mit der
// eingestellten TTS-Entität) oder eine eigene Adresse ({text} wird ersetzt, z. B. ein Piper-Server). Ohne Sprachausgabe
// spielt das Briefing nur Podcasts; die Texte stehen in der Vorschau der Oberfläche.
//
// Start: Taste/Aktion "briefing", Oberfläche, Home Assistant (MQTT-Knopf, API) oder als Weckton (Wecker-Ton
// "Briefing": erst ein Gong, dann das Briefing, danach optional ein Sender). Vor einem Weck-Briefing lädt invoked die
// Daten schon einige Minuten vorher, damit es pünktlich beginnt.

type BriefItem struct {
	Type   string `json:"type"`   // greeting | weather | warnings | pollen | calendar | podcast | ha | text
	On     bool   `json:"on"`     // aktiv
	Name   string `json:"name"`   // Bezeichnung (Kalender, Podcast)
	URL    string `json:"url"`    // Kalender (ICS) oder Podcast (RSS)
	Days   int    `json:"days"`   // Kalender: 0 = heute, 1 = morgen (Müllabfuhr) ...
	Text   string `json:"text"`   // text: der Text; ha: die Vorlage (Jinja)
	Region int    `json:"region"` // Pollen: Teilregion des DWD (oder Region, wenn sie keine Teile hat)
}

type BriefingSettings struct {
	Lang   string      `json:"lang"`  // de | en
	Place  string      `json:"place"` // Ort (Anzeige und Wetter-Ansage)
	Lat    float64     `json:"lat"`
	Lon    float64     `json:"lon"`
	Items  []BriefItem `json:"items"`
	Then   string      `json:"then"`   // danach: "" oder "radio:<Index>"
	TTS    string      `json:"tts"`    // "" (keine) | ha | url
	TTSURL string      `json:"ttsUrl"` // Adresse mit {text} (und {lang})
}

type HASettings struct {
	URL       string `json:"url"`       // http://homeassistant.local:8123
	Token     string `json:"token"`     // langlebiges Zugriffstoken (für Vorlagen: Administrator)
	TTSEngine string `json:"ttsEngine"` // z. B. tts.piper oder tts.home_assistant_cloud
}

func defaultBriefing() BriefingSettings {
	return BriefingSettings{Lang: "de", Items: []BriefItem{
		{Type: "greeting", On: true},
		{Type: "weather", On: true},
		{Type: "warnings", On: true},
		{Type: "calendar", On: false, Name: "Kalender"},
		{Type: "calendar", On: false, Name: "Müllabfuhr", Days: 1},
		{Type: "podcast", On: true, Name: podcastPresets[0].Name, URL: podcastPresets[0].URL},
	}}
}

type podcastPreset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

var podcastPresets = []podcastPreset{
	{"tagesschau in 100 Sekunden", "https://www.tagesschau.de/multimedia/sendung/tagesschau_in_100_sekunden/podcast-ts100-audio-100~podcast.xml", "de"},
	{"Deutschlandfunk – Die Nachrichten", "https://www.deutschlandfunk.de/nachrichten-108.xml", "de"},
	{"NPR News Now", "https://feeds.npr.org/500005/podcast.xml", "en"},
	{"BBC Global News Podcast", "https://podcasts.files.bbci.co.uk/p02nq0gn.rss", "en"},
}

var briefTypes = map[string]bool{"greeting": true, "weather": true, "warnings": true, "pollen": true, "calendar": true, "podcast": true, "ha": true, "text": true}

type briefSegment struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`  // zu sprechen
	Audio string `json:"audio,omitempty"` // abzuspielen (Podcast)
	Error string `json:"error,omitempty"`
	url   string // fertige Adresse (Sprachausgabe aufgelöst)
}

type briefing struct {
	a      *app
	client *http.Client
	mu     sync.Mutex
	stop   chan struct{}
	run    bool
	cache  []briefSegment
	cacheT time.Time // Zeitpunkt, für den der Vorrat gilt (Weckzeit)
	cacheB time.Time // wann er gebaut wurde
	// austauschbar für Tests
	base map[string]string // Dienst -> Basisadresse
}

func newBriefing(a *app) *briefing {
	return &briefing{a: a, client: &http.Client{Timeout: 10 * time.Second}, base: map[string]string{
		"meteo": "https://api.open-meteo.com", "geo": "https://geocoding-api.open-meteo.com",
		"brightsky": "https://api.brightsky.dev", "dwd": "https://opendata.dwd.de",
	}}
}

func (b *briefing) settings() (BriefingSettings, HASettings) {
	s := b.a.st.Snapshot()
	return s.Briefing, s.HA
}

// ---- Abruf ----

func (b *briefing) get(ctx context.Context, u string, hdr map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", radioBrowserUA)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (b *briefing) getJSON(ctx context.Context, u string, v any) error {
	body, err := b.get(ctx, u, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// ---- Texte ----

type tr struct{ de, en string }

func (t tr) in(lang string) string {
	if lang == "en" {
		return t.en
	}
	return t.de
}

var weekdayNames = map[string][]string{
	"de": {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	"en": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
}
var monthNames = map[string][]string{
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

// sayTime: Uhrzeit so, wie eine Sprachausgabe sie gut vorliest ("7 Uhr 30" / "7:30 AM").
func sayTime(t time.Time, lang string) string {
	if lang == "en" {
		return t.Format("3:04 PM")
	}
	if t.Minute() == 0 {
		return fmt.Sprintf("%d Uhr", t.Hour())
	}
	return fmt.Sprintf("%d Uhr %d", t.Hour(), t.Minute())
}

func sayDate(t time.Time, lang string) string {
	if lang == "en" {
		return fmt.Sprintf("%s, %s %d", weekdayNames["en"][t.Weekday()], monthNames["en"][t.Month()-1], t.Day())
	}
	return fmt.Sprintf("%s, der %d. %s", weekdayNames["de"][t.Weekday()], t.Day(), monthNames["de"][t.Month()-1])
}

func (b *briefing) greeting(at time.Time, lang string) string {
	g := tr{"Guten Morgen.", "Good morning."}
	switch h := at.Hour(); {
	case h >= 18 || h < 4:
		g = tr{"Guten Abend.", "Good evening."}
	case h >= 11:
		g = tr{"Hallo.", "Hello."}
	}
	s := g.in(lang) + " " + tr{"Heute ist ", "Today is "}.in(lang) + sayDate(at, lang) + ". " +
		tr{"Es ist ", "It is "}.in(lang) + sayTime(at, lang) + "."
	if h := holidayName(b.a.st.Snapshot().Holidays, at); h != "" {
		s += " " + tr{"Heute ist Feiertag: ", "Today is a public holiday: "}.in(lang) + h + "."
	}
	return s
}

// WMO-Wettercodes (Open-Meteo)
func weatherText(code int, lang string) string {
	m := map[int]tr{
		0: {"klar", "clear"}, 1: {"überwiegend klar", "mainly clear"}, 2: {"teils bewölkt", "partly cloudy"}, 3: {"bedeckt", "overcast"},
		45: {"neblig", "foggy"}, 48: {"neblig mit Reif", "foggy with rime"},
		51: {"leichter Nieselregen", "light drizzle"}, 53: {"Nieselregen", "drizzle"}, 55: {"starker Nieselregen", "dense drizzle"},
		56: {"gefrierender Nieselregen", "freezing drizzle"}, 57: {"gefrierender Nieselregen", "freezing drizzle"},
		61: {"leichter Regen", "light rain"}, 63: {"Regen", "rain"}, 65: {"starker Regen", "heavy rain"},
		66: {"gefrierender Regen", "freezing rain"}, 67: {"gefrierender Regen", "freezing rain"},
		71: {"leichter Schneefall", "light snow"}, 73: {"Schneefall", "snow"}, 75: {"starker Schneefall", "heavy snow"}, 77: {"Schneegriesel", "snow grains"},
		80: {"leichte Regenschauer", "light showers"}, 81: {"Regenschauer", "showers"}, 82: {"heftige Regenschauer", "violent showers"},
		85: {"Schneeschauer", "snow showers"}, 86: {"starke Schneeschauer", "heavy snow showers"},
		95: {"Gewitter", "thunderstorms"}, 96: {"Gewitter mit Hagel", "thunderstorms with hail"}, 99: {"Gewitter mit Hagel", "thunderstorms with hail"},
	}
	if t, ok := m[code]; ok {
		return t.in(lang)
	}
	return ""
}

func roundInt(v float64) int {
	if v < 0 {
		return -int(-v + 0.5)
	}
	return int(v + 0.5)
}

func (b *briefing) weather(ctx context.Context, set BriefingSettings) (string, error) {
	if set.Lat == 0 && set.Lon == 0 {
		return "", fmt.Errorf("kein Ort eingestellt")
	}
	q := url.Values{
		"latitude": {strconv.FormatFloat(set.Lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(set.Lon, 'f', 4, 64)},
		"current":  {"temperature_2m,weather_code,wind_speed_10m"},
		"daily":    {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"timezone": {"auto"}, "forecast_days": {"1"},
	}
	var r struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Code int     `json:"weather_code"`
			Wind float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			Code []int     `json:"weather_code"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
			Rain []*int    `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := b.getJSON(ctx, b.base["meteo"]+"/v1/forecast?"+q.Encode(), &r); err != nil {
		return "", err
	}
	if len(r.Daily.Max) == 0 || len(r.Daily.Min) == 0 {
		return "", fmt.Errorf("Open-Meteo: keine Vorhersage")
	}
	l := set.Lang
	deg := func(v float64) string { return strconv.Itoa(roundInt(v)) }
	var sb strings.Builder
	if set.Place != "" {
		sb.WriteString(tr{"Das Wetter in " + set.Place + ": ", "The weather in " + set.Place + ": "}.in(l))
	} else {
		sb.WriteString(tr{"Das Wetter: ", "The weather: "}.in(l))
	}
	sb.WriteString(tr{"zurzeit " + deg(r.Current.Temp) + " Grad", "currently " + deg(r.Current.Temp) + " degrees"}.in(l))
	if w := weatherText(r.Current.Code, l); w != "" {
		sb.WriteString(tr{" und ", " and "}.in(l) + w)
	}
	sb.WriteString(". ")
	sb.WriteString(tr{"Heute " + deg(r.Daily.Min[0]) + " bis " + deg(r.Daily.Max[0]) + " Grad", "Today " + deg(r.Daily.Min[0]) + " to " + deg(r.Daily.Max[0]) + " degrees"}.in(l))
	if len(r.Daily.Code) > 0 && r.Daily.Code[0] != r.Current.Code {
		if w := weatherText(r.Daily.Code[0], l); w != "" {
			sb.WriteString(", " + w)
		}
	}
	if len(r.Daily.Rain) > 0 && r.Daily.Rain[0] != nil {
		if p := *r.Daily.Rain[0]; p >= 20 {
			sb.WriteString(tr{", Regenwahrscheinlichkeit " + strconv.Itoa(p) + " Prozent", ", " + strconv.Itoa(p) + " percent chance of rain"}.in(l))
		}
	}
	sb.WriteString(".")
	if r.Current.Wind >= 40 {
		sb.WriteString(tr{" Es ist windig.", " It is windy."}.in(l))
	}
	return sb.String(), nil
}

func (b *briefing) warnings(ctx context.Context, set BriefingSettings, at time.Time) (string, error) {
	if set.Lat == 0 && set.Lon == 0 {
		return "", fmt.Errorf("kein Ort eingestellt")
	}
	var r struct {
		Alerts []struct {
			HeadlineDE string    `json:"headline_de"`
			HeadlineEN string    `json:"headline_en"`
			EventDE    string    `json:"event_de"`
			EventEN    string    `json:"event_en"`
			Severity   string    `json:"severity"`
			Expires    time.Time `json:"expires"`
		} `json:"alerts"`
	}
	u := fmt.Sprintf("%s/alerts?lat=%.4f&lon=%.4f", b.base["brightsky"], set.Lat, set.Lon)
	if err := b.getJSON(ctx, u, &r); err != nil {
		return "", err
	}
	var parts []string
	seen := map[string]bool{}
	for _, al := range r.Alerts {
		if al.Severity == "minor" { // Hinweise auf leichte Wetterlagen nicht vorlesen
			continue
		}
		h := tr{al.HeadlineDE, al.HeadlineEN}.in(set.Lang)
		if h == "" {
			h = tr{"Warnung vor " + strings.ToLower(al.EventDE), "Warning of " + strings.ToLower(al.EventEN)}.in(set.Lang)
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		if !al.Expires.IsZero() {
			h += tr{" bis ", " until "}.in(set.Lang) + sayTime(al.Expires.In(at.Location()), set.Lang)
		}
		parts = append(parts, h)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return tr{"Achtung: ", "Attention: "}.in(set.Lang) + strings.Join(parts, ". ") + ".", nil
}

// ---- Pollenflug (DWD) ----

type pollenRegion struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type dwdPollen struct {
	Content []struct {
		RegionID   int    `json:"region_id"`
		RegionName string `json:"region_name"`
		PartID     int    `json:"partregion_id"`
		PartName   string `json:"partregion_name"`
		Pollen     map[string]struct {
			Today    string `json:"today"`
			Tomorrow string `json:"tomorrow"`
		} `json:"Pollen"`
	} `json:"content"`
}

func (b *briefing) pollenData(ctx context.Context) (dwdPollen, error) {
	var d dwdPollen
	err := b.getJSON(ctx, b.base["dwd"]+"/climate_environment/health/alerts/s31fg.json", &d)
	return d, err
}

// PollenRegions: Auswahlliste für die Oberfläche.
func (b *briefing) PollenRegions() ([]pollenRegion, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	d, err := b.pollenData(ctx)
	if err != nil {
		return nil, err
	}
	out := []pollenRegion{}
	for _, c := range d.Content {
		id, name := c.PartID, c.RegionName+" – "+c.PartName
		if c.PartID <= 0 {
			id, name = c.RegionID, c.RegionName
		}
		out = append(out, pollenRegion{id, name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

var pollenLevels = map[string]tr{
	"1": {"gering", "low"}, "1-2": {"gering bis mittel", "low to moderate"},
	"2": {"mittel", "moderate"}, "2-3": {"mittel bis hoch", "moderate to high"}, "3": {"hoch", "high"},
}
var pollenNames = map[string]tr{
	"Hasel": {"Hasel", "hazel"}, "Erle": {"Erle", "alder"}, "Esche": {"Esche", "ash"}, "Birke": {"Birke", "birch"},
	"Graeser": {"Gräser", "grass"}, "Roggen": {"Roggen", "rye"}, "Beifuss": {"Beifuß", "mugwort"}, "Ambrosia": {"Ambrosia", "ragweed"},
}

func (b *briefing) pollen(ctx context.Context, it BriefItem, lang string) (string, error) {
	if it.Region == 0 {
		return "", fmt.Errorf("keine Pollen-Region gewählt")
	}
	d, err := b.pollenData(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range d.Content {
		if c.PartID != it.Region && !(c.PartID <= 0 && c.RegionID == it.Region) {
			continue
		}
		var names []string
		for k := range c.Pollen {
			names = append(names, k)
		}
		sort.Strings(names)
		var parts []string
		for _, k := range names {
			if lv, ok := pollenLevels[c.Pollen[k].Today]; ok { // "0" und "0-1" nicht erwähnen
				n := pollenNames[k].in(lang)
				if n == "" {
					n = k
				}
				parts = append(parts, n+" "+lv.in(lang))
			}
		}
		if len(parts) == 0 {
			return "", nil
		}
		return tr{"Pollenflug heute: ", "Pollen today: "}.in(lang) + strings.Join(parts, ", ") + ".", nil
	}
	return "", fmt.Errorf("Pollen-Region %d nicht gefunden", it.Region)
}

// ---- Kalender ----

func (b *briefing) calendar(ctx context.Context, it BriefItem, at time.Time, lang string) (string, error) {
	u := strings.TrimSpace(it.URL)
	if strings.HasPrefix(u, "webcal://") {
		u = "https://" + strings.TrimPrefix(u, "webcal://")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("Kalender %q: keine Adresse", it.Name)
	}
	body, err := b.get(ctx, u, nil)
	if err != nil {
		return "", err
	}
	evs, err := parseICS(string(body), at.Location())
	if err != nil {
		return "", err
	}
	day := at.AddDate(0, 0, clamp(it.Days, 0, 7))
	list := eventsOn(evs, day, at.Location())
	if it.Days == 0 { // heute: was schon vorbei ist, nicht mehr
		var keep []calEvent
		for _, e := range list {
			if e.AllDay || e.End.After(at) {
				keep = append(keep, e)
			}
		}
		list = keep
	}
	if len(list) == 0 {
		return "", nil // nichts vorzulesen
	}
	when := tr{"Heute", "Today"}
	switch it.Days {
	case 0:
	case 1:
		when = tr{"Morgen", "Tomorrow"}
	default:
		when = tr{"Am " + sayDate(day, "de"), "On " + sayDate(day, "en")}
	}
	label := ""
	if it.Name != "" {
		label = it.Name + ". "
	}
	var parts []string
	for _, e := range list {
		if len(parts) == 6 {
			parts = append(parts, tr{fmt.Sprintf("und %d weitere", len(list)-6), fmt.Sprintf("and %d more", len(list)-6)}.in(lang))
			break
		}
		if e.AllDay {
			parts = append(parts, e.Summary)
		} else {
			parts = append(parts, tr{"um ", "at "}.in(lang)+sayTime(e.Start, lang)+" "+e.Summary)
		}
	}
	return label + when.in(lang) + ": " + strings.Join(parts, ", ") + ".", nil
}

// ---- Podcast (RSS) ----

func (b *briefing) podcast(ctx context.Context, it BriefItem) (string, error) {
	body, err := b.get(ctx, it.URL, nil)
	if err != nil {
		return "", err
	}
	var rss struct {
		Items []struct {
			PubDate   string `xml:"pubDate"`
			Enclosure struct {
				URL string `xml:"url,attr"`
			} `xml:"enclosure"`
		} `xml:"channel>item"`
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	if err := dec.Decode(&rss); err != nil {
		return "", fmt.Errorf("Podcast: %v", err)
	}
	best, bestT := "", time.Time{}
	for _, item := range rss.Items {
		u := item.Enclosure.URL
		if u == "" {
			continue
		}
		t, _ := parseRSSDate(item.PubDate)
		if best == "" || t.After(bestT) {
			best, bestT = u, t
		}
	}
	if best == "" {
		return "", fmt.Errorf("Podcast ohne Folge")
	}
	// BBC liefert http-Adressen, die auch über https gehen
	if strings.HasPrefix(best, "http://open.live.bbc.co.uk/") {
		best = "https://" + strings.TrimPrefix(strings.Replace(best, "/proto/http/", "/proto/https/", 1), "http://")
	}
	return best, nil
}

func parseRSSDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, f := range []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", time.RFC3339} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("Datum %q", s)
}

// ---- Home Assistant ----

func (b *briefing) haPost(ctx context.Context, ha HASettings, path string, body any) ([]byte, error) {
	if ha.URL == "" || ha.Token == "" {
		return nil, fmt.Errorf("Home Assistant: Adresse und Token nötig")
	}
	j, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ha.URL, "/")+path, bytes.NewReader(j))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ha.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Home Assistant %s: %s %s", path, resp.Status, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (b *briefing) haTemplate(ctx context.Context, ha HASettings, tpl string) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		return "", nil
	}
	out, err := b.haPost(ctx, ha, "/api/template", map[string]string{"template": tpl})
	return strings.TrimSpace(string(out)), err
}

// tts liefert eine abspielbare Adresse für einen Text (Home Assistant oder eigene Adresse).
func (b *briefing) tts(ctx context.Context, set BriefingSettings, ha HASettings, text string) (string, error) {
	switch set.TTS {
	case "ha":
		if ha.TTSEngine == "" {
			return "", fmt.Errorf("Home Assistant: TTS-Entität fehlt (z. B. tts.piper)")
		}
		out, err := b.haPost(ctx, ha, "/api/tts_get_url", map[string]any{"engine_id": ha.TTSEngine, "message": text, "language": set.Lang})
		if err != nil {
			return "", err
		}
		var r struct{ URL, Path string }
		if err := json.Unmarshal(out, &r); err != nil {
			return "", err
		}
		if r.Path != "" { // die Adresse von HA ist oft die interne; der Pfad mit unserer HA-Adresse passt sicher
			return strings.TrimRight(ha.URL, "/") + r.Path, nil
		}
		return r.URL, nil
	case "url":
		if !strings.Contains(set.TTSURL, "{text}") {
			return "", fmt.Errorf("Sprachausgabe: Adresse braucht {text}")
		}
		return strings.NewReplacer("{text}", url.QueryEscape(text), "{lang}", url.QueryEscape(set.Lang)).Replace(set.TTSURL), nil
	}
	return "", nil
}

// ---- Aufbau ----

// Build stellt das Briefing für den Zeitpunkt at zusammen (alle Abrufe gleichzeitig, je höchstens 8 s).
// Mit speak werden die Texte gleich in Sprachausgabe-Adressen verwandelt.
func (b *briefing) Build(at time.Time, speak bool) []briefSegment {
	set, ha := b.settings()
	if set.Lang != "en" {
		set.Lang = "de"
	}
	items := []BriefItem{}
	for _, it := range set.Items {
		if it.On && briefTypes[it.Type] {
			items = append(items, it)
		}
	}
	segs := make([]briefSegment, len(items))
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func(i int, it BriefItem) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			seg := briefSegment{Type: it.Type}
			var err error
			switch it.Type {
			case "greeting":
				seg.Text = b.greeting(at, set.Lang)
			case "weather":
				seg.Text, err = b.weather(ctx, set)
			case "warnings":
				seg.Text, err = b.warnings(ctx, set, at)
			case "pollen":
				seg.Text, err = b.pollen(ctx, it, set.Lang)
			case "calendar":
				seg.Text, err = b.calendar(ctx, it, at, set.Lang)
			case "podcast":
				seg.Audio, err = b.podcast(ctx, it)
			case "ha":
				seg.Text, err = b.haTemplate(ctx, ha, it.Text)
			case "text":
				seg.Text = strings.TrimSpace(it.Text)
			}
			if err != nil {
				seg.Error = err.Error()
				log.Printf("Briefing %s: %v", it.Type, err)
			}
			segs[i] = seg
		}(i, it)
	}
	wg.Wait()
	// aufeinanderfolgende Texte zusammenfassen (eine Sprachausgabe statt vieler)
	out := []briefSegment{}
	for _, s := range segs {
		if s.Text == "" && s.Audio == "" {
			if s.Error != "" {
				out = append(out, s)
			}
			continue
		}
		if n := len(out); s.Text != "" && n > 0 && out[n-1].Text != "" && out[n-1].Error == "" && len(out[n-1].Text) < 900 {
			out[n-1].Text += " " + s.Text
			out[n-1].Type += "+" + s.Type
			continue
		}
		out = append(out, s)
	}
	if speak {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for i := range out {
			switch {
			case out[i].Audio != "":
				out[i].url = out[i].Audio
			case out[i].Text != "" && set.TTS != "":
				u, err := b.tts(ctx, set, ha, out[i].Text)
				if err != nil {
					out[i].Error = err.Error()
					log.Printf("Briefing Sprachausgabe: %v", err)
				}
				out[i].url = u
			}
		}
	}
	return out
}

// ---- Abspielen ----

func (b *briefing) Running() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.run }

// Stop beendet ein laufendes Briefing (Wecker aus, Stopp, andere Quelle).
func (b *briefing) Stop() {
	b.mu.Lock()
	if b.stop != nil {
		close(b.stop)
		b.stop = nil
	}
	b.mu.Unlock()
}

// Start spielt das Briefing jetzt (Taste, Oberfläche, Home Assistant).
func (b *briefing) Start(origin string) error {
	set, _ := b.settings()
	n := 0
	for _, it := range set.Items {
		if it.On {
			n++
		}
	}
	if n == 0 {
		return fmt.Errorf("im Briefing ist nichts eingeschaltet")
	}
	log.Printf("Briefing startet (%s)", origin)
	go b.play(nil, false)
	return nil
}

// StartForAlarm: Weck-Briefing (Gong, Briefing, danach ggf. Sender; meldet das Ende dem Wecker).
func (b *briefing) StartForAlarm(name string) {
	b.mu.Lock()
	var segs []briefSegment
	now := b.a.clock()
	if !b.cacheT.IsZero() && now.Sub(b.cacheT) < 3*time.Minute && now.Sub(b.cacheB) < 15*time.Minute {
		segs = b.cache
	}
	b.cache, b.cacheT = nil, time.Time{}
	b.mu.Unlock()
	log.Printf("Wecker %q: Briefing (%s)", name, map[bool]string{true: "vorbereitet", false: "wird geladen"}[segs != nil])
	go b.play(segs, true)
}

func (b *briefing) play(segs []briefSegment, forAlarm bool) {
	b.Stop() // ein laufendes ablösen
	stop := make(chan struct{})
	b.mu.Lock()
	b.stop, b.run = stop, true
	b.mu.Unlock()
	a := b.a
	defer func() {
		b.mu.Lock()
		if b.stop == stop {
			b.stop = nil
		}
		b.run = false
		b.mu.Unlock()
		a.src.Update("briefing", "idle", nil)
	}()
	a.src.Update("briefing", "playing", map[string]string{"title": "Briefing"})
	a.pl.PlayTone("briefing", "Briefing", toneChime, false)
	if segs == nil {
		segs = b.Build(a.sch.Now(), true)
	}
	set, _ := b.settings()
	played := false
	for _, s := range segs {
		if s.url == "" {
			continue
		}
		if !b.waitIdle(stop, 30*time.Second) {
			return
		}
		a.pl.PlayURL("briefing", "Briefing", s.url)
		ok, started := b.waitSegment(stop)
		if !ok {
			return
		}
		played = played || started
		select {
		case <-stop:
			return
		case <-time.After(400 * time.Millisecond):
		}
	}
	if !b.waitIdle(stop, 10*time.Second) {
		return
	}
	thenURL, thenIdx := "", -1
	if i, err := strconv.Atoi(strings.TrimPrefix(set.Then, "radio:")); err == nil && strings.HasPrefix(set.Then, "radio:") {
		if r := a.st.Snapshot().Radio; i >= 0 && i < len(r) {
			thenURL, thenIdx = r[i].URL, i
		}
	}
	if forAlarm {
		go a.sch.BriefingDone(played, thenURL)
		return
	}
	if thenIdx >= 0 {
		a.RadioPlay(thenIdx)
	}
}

// waitIdle wartet, bis der Player frei ist (Gong zu Ende); false = abgebrochen oder andere Wiedergabe.
func (b *briefing) waitIdle(stop chan struct{}, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		k, _, _, _ := b.a.pl.Info()
		if k == "" {
			return true
		}
		if k != "briefing" {
			return false
		}
		select {
		case <-stop:
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false
}

// waitSegment wartet auf das Ende eines Abschnitts: ok=false bei Abbruch, started = er hat gespielt.
func (b *briefing) waitSegment(stop chan struct{}) (ok, started bool) {
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		k, _, st, _ := b.a.pl.Info()
		if k != "briefing" {
			return k == "", started
		}
		started = started || st == "playing"
		select {
		case <-stop:
			return false, started
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false, started
}

// Run: alle 30 s prüfen, ob in den nächsten 4 Minuten ein Weck-Briefing ansteht, und es vorbereiten.
func (b *briefing) Run() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		b.prefetch()
	}
}

func (b *briefing) prefetch() {
	now := b.a.sch.Now()
	var next time.Time
	for _, al := range b.a.st.Snapshot().Alarms {
		if !al.Enabled || al.Source != "briefing" {
			continue
		}
		if t := b.a.sch.nextOccurrence(al, now); !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	if next.IsZero() || next.Sub(now) > 4*time.Minute {
		return
	}
	b.mu.Lock()
	have := b.cacheT.Equal(next)
	b.mu.Unlock()
	if have {
		return
	}
	segs := b.Build(next, true)
	b.mu.Lock()
	b.cache, b.cacheT, b.cacheB = segs, next, b.a.clock()
	b.mu.Unlock()
	log.Printf("Briefing für %s vorbereitet (%d Abschnitte)", next.Format("15:04"), len(segs))
}

// Geocode: Ortssuche für die Oberfläche (Open-Meteo).
type geoPlace struct {
	Name    string  `json:"name"`
	Admin   string  `json:"admin"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

func (b *briefing) Geocode(q, lang string) ([]geoPlace, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, fmt.Errorf("mindestens 2 Zeichen")
	}
	if lang != "en" {
		lang = "de"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var r struct {
		Results []struct {
			Name    string  `json:"name"`
			Admin1  string  `json:"admin1"`
			Country string  `json:"country"`
			Lat     float64 `json:"latitude"`
			Lon     float64 `json:"longitude"`
		} `json:"results"`
	}
	u := b.base["geo"] + "/v1/search?" + url.Values{"name": {q}, "count": {"8"}, "language": {lang}, "format": {"json"}}.Encode()
	if err := b.getJSON(ctx, u, &r); err != nil {
		return nil, err
	}
	out := []geoPlace{}
	for _, x := range r.Results {
		out = append(out, geoPlace{x.Name, x.Admin1, x.Country, x.Lat, x.Lon})
	}
	return out, nil
}
