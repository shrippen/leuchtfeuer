package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

func main() {
	// Aufruf aus librespot (--onevent) und shairport-sync: Ereignis weitergeben und sofort enden (events.go)
	if len(os.Args) >= 3 && os.Args[1] == "-source-event" {
		sendSourceEvent(os.Args[2], os.Args[3:])
		return
	}
	cfgPath := flag.String("config", filepath.Join(dataDir, "config"), "Shell-Konfiguration")
	dataPath := flag.String("data", filepath.Join(dataDir, "leuchtfeuerd.json"), "Einstellungen von leuchtfeuerd")
	listen := flag.String("listen", ":80", "Adresse der Weboberfläche")
	sessPath := flag.String("sessions", filepath.Join(dataDir, "sessions.json"), "Ablage der Anmelde-Sitzungen")
	setPass := flag.Bool("set-password", false, "Passwort der Weboberfläche aus der ersten Zeile von stdin setzen (gehasht) und beenden")
	wdAlive := flag.String("watchdog", "", "Hardware-Watchdog füttern, solange diese Lebenszeichen-Datei frisch ist (vom Hook)")
	wdDev := flag.String("watchdog-dev", "/dev/watchdog", "Watchdog-Gerät")
	flag.Parse()
	if *wdAlive != "" {
		runWatchdog(*wdAlive, *wdDev)
		return
	}
	if *setPass {
		if err := setPasswordFromStdin(*cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if demoRequested() {
		runDemo(*listen)
		return
	}
	log.SetFlags(log.LstdFlags)

	cfg := &shellConfig{path: *cfgPath}
	selectTarget(envOr("LEUCHTFEUER_TARGET", cfg.Get("TARGET", defaultTarget)))
	if c := cfg.Get("ALSA_CARD", ""); c != "" {
		hw.MixerCard = c
	}
	if w := cfg.Get("WIFI_IFACE", ""); w != "" {
		hw.WifiIface = w
	}
	st := loadStore(*dataPath)
	if st.S.Timezone == "" || st.S.Timezone == "Europe/Berlin" {
		if tz := cfg.Get("TIMEZONE", ""); tz != "" {
			st.Update(func(s *Settings) { s.Timezone = tz })
		}
	}
	a := newApp(cfg, st)

	hash := cfg.Get("WEB_PASSWORD_HASH", "")
	if plain := cfg.Get("WEB_PASSWORD", ""); plain != "" { // Altbestand: Klartext einmalig in einen Hash umwandeln
		if hash == "" && checkNewPassword(plain) == nil {
			hash, _ = storePassword(cfg, plain)
		} else {
			cfg.Delete("WEB_PASSWORD")
		}
	}
	if hash == "" {
		pw := randomPassword()
		var err error
		if hash, err = storePassword(cfg, pw); err != nil {
			log.Printf("Passwort speichern: %v", err)
		}
		log.Printf("Weboberfläche: kein Passwort gesetzt, einmalig erzeugt: %s (ändern: scripts/set-web-password.sh)", pw)
	}
	// Lautstärke ("Leuchtfeuer Music": gespiegelt oder eigen) und Quellen-Regler
	a.out.ensureConf()
	a.mix = newMixSync(amixer{card: hw.MixerCard})
	a.mix.mirror = hw.MirrorCtl
	a.mix.SetTrims(st.Snapshot().Sources.Trims())
	a.Listen(func(kind string, _ map[string]any) {
		if kind == "settings" {
			a.mix.SetTrims(a.st.Snapshot().Sources.Trims())
		}
	})
	go a.mix.Run()
	// Treiber des Zielgeräts (Invoke: WAMP-Router, Tasten), lokaler Bus für btagent und castrecv
	if hw.start != nil {
		hw.start(a)
	}
	go a.bus.Serve(busSock)
	go a.sch.Run()
	go a.wifi.Run()
	go a.mq.Run()
	go a.src.Run()
	go a.serveEvents()
	go a.readAirplayMeta()
	go a.watchClock()
	a.eq = newEqWriter(a, eqPath)
	go a.eq.Run()
	if hw.ring != nil { // Erweiterung Leuchtring: Visualizer und Szenen
		a.viz = newVisualizer(a, hw.ring.writer())
		a.viz.scenes = []ringScene{a.voiceScene, a.sunriseScene, a.timerScene}
		a.led = holdLED{ledAPI: a.led, v: a.viz}
		go a.viz.Run()
	}
	// abgelaufene Timer nach Neustart nicht erneut klingeln lassen
	st.Update(func(s *Settings) {
		var keep []Timer
		for _, t := range s.Timers {
			if t.End.After(time.Now()) {
				keep = append(keep, t)
			}
		}
		s.Timers = keep
	})
	a.logs = newLogHub(a)
	go a.logs.Run()
	a.brief = newBriefing(a)
	go a.brief.Run()
	a.peers = newPeerHub(a)
	webPort, _ := strconv.Atoi(strings.TrimPrefix(*listen, ":"))
	tlsOn := cfg.Get("WEB_TLS", "") == "on"
	if tlsOn {
		webPort, _ = strconv.Atoi(cfg.Get("WEB_TLS_PORT", "443"))
	}
	if webPort > 0 {
		go a.peers.Announce(webPort, tlsOn)
	}
	go a.peers.Browse()
	a.voice = newVoice(a)
	a.voice.zc = a.peers.WyomingZeroconf
	a.voice.Apply()
	go a.voice.Run()
	w := &webServer{app: a, login: newLoginState(hash), tokens: loadTokens(filepath.Join(dataDir, "tokens.json"))}
	w.login.load(*sessPath)
	go w.login.janitor()
	go func() {
		for range time.Tick(10 * time.Minute) {
			w.tokens.flush()
		}
	}()
	w.Run(*listen)
}
