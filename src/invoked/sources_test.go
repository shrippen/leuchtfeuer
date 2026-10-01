package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyLastMutesOthersAndReleases(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.src.Update("sendspin", "playing", nil)
	ta.src.Update("spotify", "playing", map[string]string{"title": "Lied"})
	if !ta.mix.Muted("sendspin") || ta.mix.Muted("spotify") {
		t.Fatalf("stumm: %v", ta.mix.MutedList())
	}
	if ta.src.Active() != "spotify" {
		t.Fatalf("aktiv %q", ta.src.Active())
	}
	// Spotify endet: nach 5 s wird Sendspin wieder frei
	ta.src.Update("spotify", "paused", map[string]string{})
	ta.src.tick()
	if !ta.mix.Muted("sendspin") {
		t.Fatal("zu früh freigegeben")
	}
	ta.at("2026-10-01 20:00:06")
	ta.src.tick()
	if ta.mix.Muted("sendspin") {
		t.Fatal("nicht freigegeben")
	}
}

func TestPolicyStopsRadio(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.pl.PlayURL("radio", "A", "http://a")
	ta.src.Update("radio", "playing", nil)
	ta.src.Update("airplay", "playing", nil)
	if k, _, _, _ := ta.pl.Info(); k == "radio" {
		t.Fatal("Radio läuft weiter")
	}
}

func TestPolicyMixKeepsAll(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.st.Update(func(s *Settings) { s.Sources.Policy = "mix" })
	ta.src.Update("sendspin", "playing", nil)
	ta.src.Update("spotify", "playing", nil)
	if len(ta.mix.MutedList()) != 0 {
		t.Fatalf("mix schaltet stumm: %v", ta.mix.MutedList())
	}
}

func TestVolumeLimits(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.st.Update(func(s *Settings) {
		s.Sources.Max = 80
		s.Sources.Limits = map[string]SourceLimit{"bluetooth": {Max: 50, Start: 25}}
	})
	ta.vol.SetVolume(90)
	ta.src.EnforceMax()
	if ta.vol.level() != 80 {
		t.Fatalf("Gesamtgrenze: %d", ta.vol.level())
	}
	ta.src.Update("bluetooth", "playing", nil)
	ta.src.EnforceMax()
	if ta.vol.level() != 50 && ta.vol.level() != 25 {
		t.Fatalf("Grenze Bluetooth: %d", ta.vol.level())
	}
}

func TestSpotifyEvents(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.src.spotifyEvent(map[string]string{"PLAYER_EVENT": "session_connected"})
	ta.src.spotifyEvent(map[string]string{"PLAYER_EVENT": "volume_changed", "VOLUME": "65535"})
	if ta.vol.level() != 30 {
		t.Fatal("Lautstärke beim Verbinden übernommen")
	}
	ta.at("2026-10-01 20:00:05")
	ta.src.spotifyEvent(map[string]string{"PLAYER_EVENT": "volume_changed", "VOLUME": "32768"})
	if ta.vol.level() != 50 {
		t.Fatalf("Lautstärke %d", ta.vol.level())
	}
	ta.src.spotifyEvent(map[string]string{"PLAYER_EVENT": "playing"})
	ta.src.spotifyEvent(map[string]string{"PLAYER_EVENT": "track_changed", "NAME": "Song", "ARTISTS": "Daft Punk\nPharrell Williams", "ALBUM": "RAM"})
	l := ta.src.List()
	if len(l) != 1 || l[0].Title != "Song" || l[0].Artist != "Daft Punk, Pharrell Williams" || l[0].State != "playing" {
		t.Fatalf("%+v", l)
	}
}

func TestAirplayEvents(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	ta.src.airplayEvent([]string{"volume", "-15.00,0.00,-96.30,0.00"})
	if ta.vol.level() != 50 {
		t.Fatalf("AirPlay -15 dB -> %d %%", ta.vol.level())
	}
	if airplayPercent(-144) != 0 || airplayPercent(0) != 100 {
		t.Fatal("Grenzen")
	}
}

func TestAirplayMetaParse(t *testing.T) {
	in := "<item><type>636f7265</type><code>6d696e6d</code><length>4</length>\n<data encoding=\"base64\">\nU29uZw==</data></item>\n" +
		"<item><type>73736e63</type><code>70656e64</code><length>0</length></item>\n"
	var got []airplayItem
	parseAirplayMeta(bufio.NewReader(strings.NewReader(in)), func(it airplayItem) { got = append(got, it) })
	if len(got) != 2 || got[0].Type != "core" || got[0].Code != "minm" || string(got[0].Data) != "Song" || got[1].Code != "pend" {
		t.Fatalf("%+v", got)
	}
}

// Erkennung über offene Wiedergabegeräte: nachgebildetes /proc mit einem UPnP-Prozess.
func TestPCMDetection(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	dir := t.TempDir()
	servicesDir, runDir = filepath.Join(dir, "svc"), filepath.Join(dir, "run")
	t.Cleanup(func() { servicesDir, runDir = "/data/invoke/services", "/run" })
	os.MkdirAll(servicesDir, 0o755)
	os.MkdirAll(runDir, 0o755)
	os.WriteFile(filepath.Join(servicesDir, "gmrender.sh"), []byte("#!/bin/sh\n# title: UPnP\n# group: upnp\n# process: gmediarender\n"), 0o755)
	// Dienstskript-PID 4242 (exec), Kindprozess 4243 hat das Gerät offen
	os.WriteFile(filepath.Join(runDir, "invoke-svc-gmrender.pid"), []byte("4242\n"), 0o644)
	proc := ta.src.procRoot
	os.MkdirAll(filepath.Join(proc, "4243", "fd"), 0o755)
	os.WriteFile(filepath.Join(proc, "4243", "stat"), []byte("4243 (gst) S "+strconv.Itoa(4242)+" 1 1"), 0o644)
	os.Symlink("/dev/snd/pcmC1D0p", filepath.Join(proc, "4243", "fd", "7"))
	if g := ta.src.pcmGroups(); !g["upnp"] {
		t.Fatalf("UPnP nicht erkannt: %v", g)
	}
}

func TestMixerSync(t *testing.T) {
	m := &fakeMixer{ctl: map[string]int{"system": 180, "Invoke Music": 10, "Invoke Announce": 0}}
	for _, s := range sourceNames {
		m.ctl["Quelle "+s] = 0
	}
	x := newMixSync(m)
	x.sync(true)
	if m.ctl["Invoke Music"] != 180 || m.ctl["Quelle spotify"] != 255 || m.ctl["Invoke Announce"] != 180 {
		t.Fatalf("%v", m.ctl)
	}
	x.SetMuted("spotify", true)
	x.Duck(15)
	x.sync(false)
	if m.ctl["Quelle spotify"] != 0 || m.ctl["Quelle radio"] != softvolForDB(15) {
		t.Fatalf("%v", m.ctl)
	}
	if softvolForDB(0) != 255 || softvolForDB(60) != 0 {
		t.Fatal("softvolForDB")
	}
}

func TestServiceHeadersAndGroups(t *testing.T) {
	dir := t.TempDir()
	servicesDir = filepath.Join(dir, "svc")
	t.Cleanup(func() { servicesDir = "/data/invoke/services" })
	os.MkdirAll(servicesDir, 0o755)
	os.WriteFile(filepath.Join(servicesDir, "shairport.sh"), []byte("#!/bin/sh\n# title: AirPlay\n# group: airplay\n# process: shairport-sync\n# ports: tcp 5000\n# default: on\nexec x\n"), 0o755)
	os.WriteFile(filepath.Join(servicesDir, "snapclient.sh"), []byte("#!/bin/sh\n# title: Snapcast\n# group: snapcast\n# default: off\n"), 0o755)
	os.WriteFile(filepath.Join(servicesDir, "kaputt.sh"), []byte("#!/bin/sh\n"), 0o644) // nicht ausführbar: zählt nicht
	cfg := &shellConfig{path: filepath.Join(dir, "config")}
	cfg.Set(map[string]string{"AIRPLAY": "off"})
	defs := serviceDefs()
	if len(defs) != 2 || defs[0].Title != "AirPlay" || defs[0].Ports != "tcp 5000" {
		t.Fatalf("%+v", defs)
	}
	g := serviceGroups(cfg)
	if len(g) != 2 || g[0].Enabled || g[1].Enabled {
		t.Fatalf("alter Schalter AIRPLAY oder Vorgabe off nicht beachtet: %+v", g)
	}
	setGroup(cfg, "snapcast", true)
	setGroup(cfg, "airplay", true)
	if g := serviceGroups(cfg); !g[0].Enabled || !g[1].Enabled {
		t.Fatalf("%+v", g)
	}
	if setGroup(cfg, "gibtsnicht", true) == nil {
		t.Fatal("unbekannte Gruppe angenommen")
	}
}
