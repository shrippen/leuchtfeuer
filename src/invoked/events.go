package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"
)

// Ereignisse der Dienste an invoked: librespot (--onevent) und shairport-sync (sessioncontrol) rufen
// `invoked -source-event <quelle> [argumente]` auf. Dieser kurze Aufruf schickt das Ereignis (Argumente und die
// librespot-Umgebung) als eine JSON-Zeile über den Unix-Socket /run/invoke-events.sock an das laufende invoked und
// endet sofort; er wartet höchstens 1 s und meldet nie einen Fehler zurück (der Dienst soll nie hängen).

var eventsSock = "/run/invoke-events.sock"

type sourceEvent struct {
	Source string            `json:"source"`
	Args   []string          `json:"args"`
	Env    map[string]string `json:"env"`
}

var eventEnv = []string{"PLAYER_EVENT", "TRACK_ID", "NAME", "ARTISTS", "ALBUM", "VOLUME", "SINK_STATUS", "POSITION_MS", "DURATION_MS"}

// sendSourceEvent ist der Client (Aufruf aus den Diensten).
func sendSourceEvent(src string, args []string) {
	ev := sourceEvent{Source: src, Args: args, Env: map[string]string{}}
	for _, k := range eventEnv {
		if v, ok := os.LookupEnv(k); ok {
			ev.Env[k] = v
		}
	}
	c, err := net.DialTimeout("unix", eventsSock, time.Second)
	if err != nil {
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	b, _ := json.Marshal(ev)
	c.Write(append(b, '\n'))
}

// serveEvents nimmt Ereignisse an und gibt sie an die Quellen weiter.
func (a *app) serveEvents() {
	os.Remove(eventsSock)
	ln, err := net.Listen("unix", eventsSock)
	if err != nil {
		logf("Ereignis-Socket: %v", err)
		return
	}
	os.Chmod(eventsSock, 0o600)
	for {
		c, err := ln.Accept()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			c.SetDeadline(time.Now().Add(2 * time.Second))
			line, err := bufio.NewReaderSize(c, 64<<10).ReadString('\n')
			if err != nil && line == "" {
				return
			}
			var ev sourceEvent
			if json.Unmarshal([]byte(strings.TrimSpace(line)), &ev) != nil {
				return
			}
			a.handleSourceEvent(ev)
		}(c)
	}
}

func (a *app) handleSourceEvent(ev sourceEvent) {
	switch ev.Source {
	case "spotify":
		a.src.spotifyEvent(ev.Env)
	case "airplay":
		a.src.airplayEvent(ev.Args)
	}
}
