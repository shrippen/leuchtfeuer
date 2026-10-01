package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func fullTestApp(t *testing.T) *testApp {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	ta.sysFn = func() sysStatus { return sysStatus{TempC: 55} }
	ta.svcFn = func() []serviceInfo { return nil }
	ta.clkFn = func() clockStatus { return clockStatus{} }
	ta.wifiFn = func() wifiStatus { return wifiStatus{} }
	ta.btFn = func() btState { return btState{} }
	ta.mqttOK = func() bool { return false }
	ta.wampOK = func() bool { return true }
	ta.peers = newPeerHub(ta.app)
	return ta
}

// Zwei Lautsprecher: A trägt B mit dessen API-Schlüssel ein, sieht den Status, kopiert Einstellungen, löst Aktionen aus.
func TestPeers(t *testing.T) {
	a, b := fullTestApp(t), fullTestApp(t)
	b.cfg.Set(map[string]string{"DEVICE_NAME": "Küche"})
	bw := &webServer{app: b.app, login: newLoginState(hashPassword("geheim1")), tokens: loadTokens(t.TempDir() + "/t.json")}
	for _, tlsOn := range []bool{false, true} {
		var srvURL string
		if tlsOn {
			s := httptest.NewTLSServer(bw.routes())
			defer s.Close()
			srvURL = s.URL
		} else {
			s := httptest.NewServer(bw.routes())
			defer s.Close()
			srvURL = s.URL
		}
		full, _, _ := bw.tokens.Create("Wohnzimmer", "full")
		ro, _, _ := bw.tokens.Create("nur lesen", "read")
		if _, err := a.peers.Add("", srvURL, "kein-schluessel"); err == nil {
			t.Fatal("ohne Schlüssel eingetragen")
		}
		p, err := a.peers.Add("", srvURL+"/", full)
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "Küche" || p.URL != srvURL || (tlsOn && len(p.Fingerprint) != 64) || (!tlsOn && p.Fingerprint != "") {
			t.Fatalf("eingetragen: %+v", p)
		}
		st := a.peers.Status()
		if len(st) != 1 || !st[0].Online || st[0].Volume != 30 || st[0].TempC != 55 || st[0].Token != "" {
			t.Fatalf("Status: %+v", st)
		}
		// Einstellungen kopieren
		a.st.Update(func(s *Settings) {
			s.Radio = []Preset{{Name: "Eins", URL: "http://eins"}}
			s.Briefing.Place = "Hamburg"
			s.Timezone = "Europe/Vienna"
		})
		if err := a.peers.Copy(srvURL, []string{"radio", "briefing", "timezone"}); err != nil {
			t.Fatal(err)
		}
		bs := b.st.Snapshot()
		if len(bs.Radio) != 1 || bs.Radio[0].Name != "Eins" || bs.Briefing.Place != "Hamburg" || bs.Timezone != "Europe/Vienna" {
			t.Fatalf("kopiert: %+v %+v %s", bs.Radio, bs.Briefing.Place, bs.Timezone)
		}
		if err := a.peers.Copy(srvURL, []string{"device"}); err == nil {
			t.Fatal("Gerätebereich kopiert")
		}
		if err := a.peers.Action(srvURL, "volume_up"); err != nil || b.vol.level() != 35 {
			t.Fatalf("Aktion: %v %d", err, b.vol.level())
		}
		// nur-lesen-Schlüssel kann nichts ändern
		a.peers.Add("Küche lesen", srvURL, ro)
		if err := a.peers.Action(srvURL, "volume_up"); err == nil || !strings.Contains(err.Error(), "nur lesen") {
			t.Fatalf("lesender Schlüssel: %v", err)
		}
		if tlsOn { // anderes Zertifikat: abgelehnt
			a.st.Update(func(s *Settings) { s.Peers[0].Fingerprint = strings.Repeat("0", 64) })
			if st := a.peers.Status(); st[0].Online || !strings.Contains(st[0].Error, "Zertifikat") {
				t.Fatalf("Zertifikat nicht geprüft: %+v", st[0])
			}
		}
		if err := a.peers.Delete(srvURL); err != nil || len(a.peers.peers()) != 0 {
			t.Fatal("Löschen")
		}
		b.vol.SetVolume(30)
	}
	if _, err := normPeerURL("ftp://x"); err == nil {
		t.Fatal("ftp angenommen")
	}
	if u, _ := normPeerURL("invoke-kueche.lan"); u != "http://invoke-kueche.lan" {
		t.Fatal(u)
	}
	if a.st.Redacted().Peers != nil {
		for _, p := range a.st.Redacted().Peers {
			if p.Token != "" {
				t.Fatal("Schlüssel in der Oberfläche")
			}
		}
	}
}
