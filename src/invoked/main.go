package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

var version = "dev"

func main() {
	// Aufruf aus librespot (--onevent) und shairport-sync: Ereignis weitergeben und sofort enden (events.go)
	if len(os.Args) >= 3 && os.Args[1] == "-source-event" {
		sendSourceEvent(os.Args[2], os.Args[3:])
		return
	}
	cfgPath := flag.String("config", "/data/invoke/config", "Shell-Konfiguration")
	dataPath := flag.String("data", "/data/invoke/invoked.json", "Einstellungen von invoked")
	listen := flag.String("listen", ":80", "Adresse der Weboberfläche")
	sessPath := flag.String("sessions", "/data/invoke/sessions.json", "Ablage der Anmelde-Sitzungen")
	setPass := flag.Bool("set-password", false, "Passwort der Weboberfläche aus der ersten Zeile von stdin setzen (gehasht) und beenden")
	flag.Parse()
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
	// Tasten und Bluetooth-Knopf
	a.h.Subscribe("com.harman.test.inputEvent", a.onButton)
	// Lautstärke-Abgleich "system" -> "Invoke Music" und Quellen-Regler (ersetzt volume-sync.sh)
	a.mix = newMixSync(amixer{card: "0"})
	a.h.Subscribe("com.harman.volumeChanged", func([]any) { a.mix.Kick() })
	a.h.Subscribe("com.harman.musicMuteChanged", func([]any) { a.mix.Kick() })
	go a.mix.Run()
	// Zustand und Titel von Bluetooth (btagent) und Cast (castrecv)
	a.h.Subscribe("invoke.source.state", func(args []any) {
		if len(args) < 2 {
			return
		}
		name, _ := args[0].(string)
		m := toStrMap(args[1])
		if name == "" || m == nil {
			return
		}
		meta := map[string]string{}
		for _, k := range []string{"title", "artist", "album"} {
			if v, ok := m[k].(string); ok {
				meta[k] = v
			}
		}
		st, _ := m["state"].(string)
		a.src.Update(name, st, meta)
	})
	go a.h.Run()
	go a.sch.Run()
	go a.wifi.Run()
	go a.mq.Run()
	go a.src.Run()
	go a.serveEvents()
	go a.readAirplayMeta()
	go a.watchClock()
	a.eq = newEqWriter(a, eqPath)
	go a.eq.Run()
	a.viz = newVisualizer(a)
	a.viz.scenes = []ringScene{a.sunriseScene, a.timerScene}
	a.led = holdLED{ledAPI: a.led, v: a.viz}
	go a.viz.Run()
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
	w := &webServer{app: a, login: newLoginState(hash)}
	w.login.load(*sessPath)
	go w.login.janitor()
	w.Run(*listen)
}
