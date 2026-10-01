package main

import (
	"flag"
	"log"
	"os"
	"time"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "/data/invoke/config", "Shell-Konfiguration")
	dataPath := flag.String("data", "/data/invoke/invoked.json", "Einstellungen von invoked")
	listen := flag.String("listen", ":8080", "Adresse der Weboberfläche")
	flag.Parse()
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

	pass := cfg.Get("WEB_PASSWORD", "")
	if pass == "" {
		pass = randomPassword()
		if err := cfg.Set(map[string]string{"WEB_PASSWORD": pass}); err != nil {
			log.Printf("Passwort speichern: %v", err)
		}
		log.Printf("Weboberfläche: neues Passwort erzeugt (Benutzer admin), siehe WEB_PASSWORD in %s", *cfgPath)
	}
	// Tasten und Bluetooth-Knopf
	a.h.Subscribe("com.harman.test.inputEvent", a.onButton)
	go a.h.Run()
	go a.sch.Run()
	go a.wifi.Run()
	go a.mq.Run()
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
	w := &webServer{app: a, pass: pass}
	if len(os.Args) > 0 {
		w.Run(*listen)
	}
}
