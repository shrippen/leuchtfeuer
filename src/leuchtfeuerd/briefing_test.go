package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Nachbildung der Dienste (Open-Meteo, Bright Sky, DWD, Kalender, Podcast, Home Assistant).
func briefServer(t *testing.T) (*httptest.Server, *[]string) {
	var ttsReqs []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latitude") != "53.5511" {
			t.Errorf("Breite: %s", r.URL.Query().Get("latitude"))
		}
		io.WriteString(w, `{"current":{"temperature_2m":11.6,"weather_code":3,"wind_speed_10m":12},
			"daily":{"weather_code":[61],"temperature_2m_max":[16.4],"temperature_2m_min":[8.2],"precipitation_probability_max":[70]}}`)
	})
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results":[{"name":"Hamburg","admin1":"Hamburg","country":"Deutschland","latitude":53.55,"longitude":10.0}]}`)
	})
	mux.HandleFunc("/alerts", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"alerts":[{"headline_de":"Amtliche WARNUNG vor STURMBÖEN","severity":"moderate","expires":"2026-10-05T16:00:00+00:00"},
			{"headline_de":"Amtliche WARNUNG vor FROST","severity":"minor"}]}`)
	})
	mux.HandleFunc("/climate_environment/health/alerts/s31fg.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":[{"region_id":10,"region_name":"SH und HH","partregion_id":11,"partregion_name":"Inseln",
			"Pollen":{"Birke":{"today":"2","tomorrow":"1"},"Graeser":{"today":"0-1"},"Erle":{"today":"1"}}},
			{"region_id":50,"region_name":"Brandenburg und Berlin","partregion_id":-1,"partregion_name":"","Pollen":{}}]}`)
	})
	mux.HandleFunc("/cal.ics", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, testICS) })
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><rss><channel><title>t</title>
			<item><pubDate>Mon, 05 Oct 2026 06:00:00 +0200</pubDate><enclosure url="https://cdn/alt.mp3" type="audio/mpeg"/></item>
			<item><pubDate>Mon, 05 Oct 2026 06:45:00 +0200</pubDate><enclosure url="https://cdn/neu.mp3" type="audio/mpeg"/></item>
			</channel></rss>`)
	})
	mux.HandleFunc("/api/template", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ha-token" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "Bis zur Arbeit brauchst du 25 Minuten.\n")
	})
	mux.HandleFunc("/api/tts_get_url", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		ttsReqs = append(ttsReqs, v["message"].(string))
		if v["engine_id"] != "tts.piper" || v["language"] != "de" {
			t.Errorf("TTS-Anfrage: %v", v)
		}
		io.WriteString(w, `{"url":"http://intern:8123/api/tts_proxy/abc.mp3","path":"/api/tts_proxy/abc.mp3"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &ttsReqs
}

func TestBriefingBuild(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	srv, tts := briefServer(t)
	b := newBriefing(ta.app)
	for k := range b.base {
		b.base[k] = srv.URL
	}
	ta.brief = b
	ta.st.Update(func(s *Settings) {
		s.Holidays = "DE"
		s.HA = HASettings{URL: srv.URL, Token: "ha-token", TTSEngine: "tts.piper"}
		s.Briefing = BriefingSettings{Lang: "de", Place: "Hamburg", Lat: 53.5511, Lon: 9.9937, TTS: "ha", Items: []BriefItem{
			{Type: "greeting", On: true},
			{Type: "weather", On: true},
			{Type: "warnings", On: true},
			{Type: "pollen", On: true, Region: 11},
			{Type: "calendar", On: true, Name: "Familie", URL: srv.URL + "/cal.ics"},
			{Type: "calendar", On: true, Name: "Müll", URL: srv.URL + "/cal.ics", Days: 8}, // auf 7 begrenzt: 12.10.
			{Type: "podcast", On: true, Name: "News", URL: srv.URL + "/feed.xml"},
			{Type: "ha", On: true, Text: "{{ states('sensor.fahrzeit') }}"},
			{Type: "text", On: false, Text: "aus"},
			{Type: "calendar", On: true, Name: "kaputt", URL: srv.URL + "/fehlt.ics"},
		}}
	})
	at := ta.sch.Now()
	segs := b.Build(at, true)
	if len(segs) != 4 {
		t.Fatalf("Abschnitte: %d %+v", len(segs), segs)
	}
	txt := segs[0].Text
	for _, want := range []string{
		"Guten Morgen. Heute ist Montag, der 5. Oktober. Es ist 7 Uhr.",
		"Das Wetter in Hamburg: zurzeit 12 Grad und bedeckt. Heute 8 bis 16 Grad, leichter Regen, Regenwahrscheinlichkeit 70 Prozent.",
		"Achtung: Amtliche WARNUNG vor STURMBÖEN bis 18 Uhr.",
		"Pollenflug heute: Birke mittel, Erle gering.",
		"Familie. Heute: Urlaub, um 9 Uhr Zahnarzt, Kontrolle, um 10 Uhr Team-Runde, um 14 Uhr 30 Mittag (UTC).",
		"Müll. Am Montag, der 12. Oktober: um 14 Uhr Team-Runde (verschoben).",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("fehlt: %q\nin: %s", want, txt)
		}
	}
	if strings.Contains(txt, "FROST") || strings.Contains(txt, "Gräser") {
		t.Errorf("leichte Warnung/geringe Pollen vorgelesen: %s", txt)
	}
	if segs[0].url != srv.URL+"/api/tts_proxy/abc.mp3" {
		t.Errorf("TTS-Adresse: %s", segs[0].url)
	}
	if segs[1].Audio != "https://cdn/neu.mp3" || segs[1].url != segs[1].Audio {
		t.Errorf("Podcast: %+v", segs[1])
	}
	if segs[2].Text != "Bis zur Arbeit brauchst du 25 Minuten." {
		t.Errorf("Vorlage: %+v", segs[2])
	}
	if segs[3].Error == "" || segs[3].Type != "calendar" {
		t.Errorf("Fehler fehlt: %+v", segs[3])
	}
	if len(*tts) != 2 {
		t.Errorf("TTS-Anfragen: %d", len(*tts))
	}
	// englisch, eigene TTS-Adresse
	ta.st.Update(func(s *Settings) {
		s.Briefing.Lang, s.Briefing.TTS, s.Briefing.TTSURL = "en", "url", "http://piper:5000/?text={text}&l={lang}"
		s.Briefing.Items = []BriefItem{{Type: "greeting", On: true}, {Type: "weather", On: true}}
	})
	segs = b.Build(at, true)
	if len(segs) != 1 || !strings.HasPrefix(segs[0].Text, "Good morning. Today is Monday, October 5. It is 7:00 AM.") ||
		!strings.Contains(segs[0].Text, "currently 12 degrees and overcast") ||
		!strings.HasPrefix(segs[0].url, "http://piper:5000/?text=Good+morning.") || !strings.HasSuffix(segs[0].url, "&l=en") {
		t.Errorf("englisch: %+v %s", segs, segs[0].url)
	}
	if pl, err := b.Geocode("Hamburg", "de"); err != nil || len(pl) != 1 || pl[0].Lat != 53.55 {
		t.Errorf("Ortssuche: %v %v", pl, err)
	}
	if r, err := b.PollenRegions(); err != nil || len(r) != 2 || r[0].ID != 11 || r[1].ID != 50 {
		t.Errorf("Pollen-Regionen: %v %v", r, err)
	}
}

// Weck-Briefing: Gong, Abschnitte nacheinander, danach der Sender als Weckton; ohne Inhalt der Ersatzton.
func TestBriefingAlarmFlow(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 06:59:00")
	b := newBriefing(ta.app)
	ta.brief = b
	ta.st.Update(func(s *Settings) {
		s.Alarms = []Alarm{{ID: "b", Name: "Morgen", Time: "07:00", Enabled: true, Source: "briefing"}}
		s.Briefing = BriefingSettings{Lang: "de", Then: "radio:0", Items: []BriefItem{{Type: "text", On: true, Text: "Hallo"}}}
	})
	// vorbereiten (Sprachausgabe fehlt: Text ohne Adresse)
	b.prefetch()
	if b.cacheT.IsZero() {
		t.Fatal("nicht vorbereitet")
	}
	ta.at("2026-10-05 07:00:00")
	ta.sch.tick()
	// der Nachbau-Player "spielt" sofort; Gong läuft -> beenden, damit das Briefing weitergeht
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		k, _, _, _ := ta.pl.Info()
		if k == "alarm" {
			break
		}
		if k == "briefing" {
			ta.pl.Stop()
		}
		time.Sleep(20 * time.Millisecond)
	}
	// kein Abschnitt spielbar -> Ersatzton (Weckton ohne Adresse)
	if k, _, _, _ := ta.pl.Info(); k != "alarm" || ta.pl.url != "" {
		t.Fatalf("kein Ersatzton: %+v", ta.pl)
	}
	ta.sch.StopAlarm()
	if b.Running() {
		time.Sleep(200 * time.Millisecond)
	}

	// mit spielbarem Abschnitt: danach der Sender
	ta.st.Update(func(s *Settings) { s.Alarms[0].Time = "07:05" })
	ta.at("2026-10-05 07:05:00")
	b.mu.Lock()
	b.cache, b.cacheT, b.cacheB = []briefSegment{{Type: "podcast", Audio: "https://cdn/x.mp3", url: "https://cdn/x.mp3"}}, ta.clock(), ta.clock()
	b.mu.Unlock()
	ta.sch.tick()
	sawPodcast := false
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		ta.pl.mu.Lock()
		k, u := ta.pl.kind, ta.pl.url
		ta.pl.mu.Unlock()
		if k == "briefing" && u == "https://cdn/x.mp3" {
			sawPodcast = true
		}
		if k == "alarm" && u == "http://ice1.somafm.com/groovesalad-128-mp3" {
			break
		}
		if k == "briefing" {
			time.Sleep(150 * time.Millisecond)
			ta.pl.Stop() // Abschnitt zu Ende
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawPodcast || ta.pl.kind != "alarm" || ta.pl.url != "http://ice1.somafm.com/groovesalad-128-mp3" {
		t.Fatalf("Ablauf: Podcast %v, Player %+v", sawPodcast, ta.pl)
	}
	if st := ta.sch.State(); st.State != "ringing" {
		t.Fatalf("Wecker: %+v", st)
	}
}

func TestSayTimeAndRSSDate(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	if s := sayTime(time.Date(2026, 1, 1, 14, 5, 0, 0, loc), "de"); s != "14 Uhr 5" {
		t.Fatal(s)
	}
	if s := sayTime(time.Date(2026, 1, 1, 14, 5, 0, 0, loc), "en"); s != "2:05 PM" {
		t.Fatal(s)
	}
	if _, err := parseRSSDate("Thu, 01 Oct 2026 14:45:00 +0200"); err != nil {
		t.Fatal(err)
	}
	if weatherText(95, "de") != "Gewitter" || weatherText(1234, "de") != "" {
		t.Fatal("Wettertext")
	}
}
