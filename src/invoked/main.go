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
	cfgPath := flag.String("config", "/data/invoke/config", "Shell-Konfiguration")
	dataPath := flag.String("data", "/data/invoke/invoked.json", "Einstellungen von invoked")
	listen := flag.String("listen", ":80", "Adresse der Weboberfläche")
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
	go a.h.Run()
	go a.sch.Run()
	go a.wifi.Run()
	go a.mq.Run()
	a.viz = newVisualizer(a)
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
	if len(os.Args) > 0 {
		w.Run(*listen)
	}
}
