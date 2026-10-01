// Package lfbus: Client für den lokalen Bus von leuchtfeuerd (HTTP über den Unix-Socket
// $LEUCHTFEUER_RUN/leuchtfeuer-bus.sock, siehe src/leuchtfeuerd/bus.go). Damit sprechen btagent und castrecv mit
// leuchtfeuerd, auf jedem Zielgerät gleich: Ereignisse abonnieren (Server-Sent Events, verbindet selbst neu) und
// Befehle schicken (JSON).
package lfbus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultPath: Socket nach LEUCHTFEUER_RUN (Standard /run).
func DefaultPath() string {
	r := os.Getenv("LEUCHTFEUER_RUN")
	if r == "" {
		r = "/run"
	}
	return filepath.Join(r, "leuchtfeuer-bus.sock")
}

type Client struct {
	path   string
	hc     *http.Client
	stream *http.Client
	mu     sync.Mutex
	on     map[string][]func(json.RawMessage)
	onConn []func()
	conn   atomic.Bool
	Logf   func(format string, args ...any)
}

func New(path string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", path)
	}
	return &Client{
		path:   path,
		hc:     &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 5 * time.Second},
		stream: &http.Client{Transport: &http.Transport{DialContext: dial}},
		on:     map[string][]func(json.RawMessage){},
		Logf:   log.Printf,
	}
}

// On meldet einen Empfänger für ein Ereignis an (vor Run).
func (c *Client) On(event string, f func(json.RawMessage)) {
	c.mu.Lock()
	c.on[event] = append(c.on[event], f)
	c.mu.Unlock()
}

// OnConnect: nach jeder (Wieder-)Verbindung.
func (c *Client) OnConnect(f func()) { c.mu.Lock(); c.onConn = append(c.onConn, f); c.mu.Unlock() }

func (c *Client) Connected() bool { return c.conn.Load() }

// Run hält das Abonnement offen (Pause nach Abbruch 1, 2, 4 ... höchstens 15 s).
func (c *Client) Run() {
	wait := time.Second
	for {
		t0 := time.Now()
		err := c.listen()
		c.conn.Store(false)
		if time.Since(t0) > 30*time.Second {
			wait = time.Second
		}
		if err != nil && wait >= 8*time.Second {
			c.Logf("Bus %s: %v", c.path, err)
		}
		time.Sleep(wait)
		if wait < 15*time.Second {
			wait *= 2
		}
	}
}

func (c *Client) listen() error {
	resp, err := c.stream.Get("http://bus/events")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	c.conn.Store(true)
	c.mu.Lock()
	cbs := append([]func(){}, c.onConn...)
	c.mu.Unlock()
	for _, f := range cbs {
		go f()
	}
	rd := bufio.NewReader(resp.Body)
	event, data := "", []string{}
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("Verbindung beendet")
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if event != "" && len(data) > 0 {
				c.dispatch(event, json.RawMessage(strings.Join(data, "\n")))
			}
			event, data = "", data[:0]
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(line[5:], " "))
		}
	}
}

func (c *Client) dispatch(event string, data json.RawMessage) {
	c.mu.Lock()
	fs := append([]func(json.RawMessage){}, c.on[event]...)
	c.mu.Unlock()
	for _, f := range fs {
		f(data)
	}
}

// Post schickt einen Befehl (body als JSON), out (optional) nimmt die Antwort auf.
func (c *Client) Post(path string, body, out any) error {
	b, _ := json.Marshal(body)
	resp, err := c.hc.Post("http://bus"+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	return decode(resp, out)
}

// Get liest einen Zustand.
func (c *Client) Get(path string, out any) error {
	resp, err := c.hc.Get("http://bus" + path)
	if err != nil {
		return err
	}
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Volume: Zustand der Lautstärke (Ereignis "volume", GET /state).
type Volume struct {
	Volume int  `json:"volume"`
	Muted  bool `json:"muted"`
	Known  bool `json:"known"`
}

// Action: Ereignisse mit einer Aktion (bt-pairing, bt-control).
type Action struct {
	Action string `json:"action"`
}

// Claim: eine Quelle beginnt zu spielen (Ereignis "claim").
type Claim struct {
	Source string `json:"source"`
}

// Button: Taste am Gerät (Ereignis "button").
type Button struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
