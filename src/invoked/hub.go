package main

import (
	"log"
	"sync"
	"time"
)

// hub hält die WAMP-Verbindung zum Router des Invoke (127.0.0.1:9999), verbindet bei Verlust neu und verteilt
// Ereignisse an angemeldete Funktionen.
type hub struct {
	mu     sync.Mutex
	c      *wampClient
	on     map[string][]func([]any)
	onConn []func()
}

const wampAddr = "127.0.0.1:9999"

func newHub() *hub { return &hub{on: map[string][]func([]any){}} }

// Subscribe meldet fn für ein Thema an (auch nach Wiederverbindung).
func (h *hub) Subscribe(topic string, fn func([]any)) {
	h.mu.Lock()
	h.on[topic] = append(h.on[topic], fn)
	c := h.c
	h.mu.Unlock()
	if c != nil {
		c.subscribe(topic)
	}
}

// OnConnect ruft fn bei jeder (Wieder-)Verbindung auf.
func (h *hub) OnConnect(fn func()) { h.mu.Lock(); h.onConn = append(h.onConn, fn); h.mu.Unlock() }

func (h *hub) client() *wampClient { h.mu.Lock(); defer h.mu.Unlock(); return h.c }

func (h *hub) Connected() bool { return h.client() != nil }

func (h *hub) Run() {
	for {
		if h.client() == nil {
			c, err := wampConnect(wampAddr, h.dispatch)
			if err == nil {
				h.mu.Lock()
				h.c = c
				topics := make([]string, 0, len(h.on))
				for t := range h.on {
					topics = append(topics, t)
				}
				cbs := append([]func(){}, h.onConn...)
				h.mu.Unlock()
				for _, t := range topics {
					c.subscribe(t)
				}
				log.Printf("WAMP verbunden")
				for _, f := range cbs {
					go f()
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func (h *hub) dispatch(topic string, args []any) {
	if topic == "" { // Verbindung verloren
		h.mu.Lock()
		h.c = nil
		h.mu.Unlock()
		log.Printf("WAMP getrennt")
		return
	}
	h.mu.Lock()
	fns := append([]func([]any){}, h.on[topic]...)
	h.mu.Unlock()
	for _, f := range fns {
		f(args)
	}
}

func (h *hub) Call(proc string, args ...any) ([]any, error) {
	r, _, err := h.CallKw(proc, nil, args...)
	return r, err
}

func (h *hub) CallKw(proc string, kw map[string]any, args ...any) ([]any, map[string]any, error) {
	c := h.client()
	if c == nil {
		return nil, nil, errNoWamp
	}
	return c.callKw(proc, kw, args...)
}

func (h *hub) Publish(topic string, args ...any) {
	if c := h.client(); c != nil {
		c.publish(topic, args...)
	}
}

var errNoWamp = errString("keine WAMP-Verbindung")

type errString string

func (e errString) Error() string { return string(e) }
