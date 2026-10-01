package wamp

import (
	"log"
	"sync"
	"time"
)

// Hub hält die Verbindung zum Router, verbindet bei Verlust neu, meldet Abonnements nach jeder Verbindung erneut an
// und verteilt Ereignisse an angemeldete Funktionen.
type Hub struct {
	addr   string
	mu     sync.Mutex
	c      *Client
	on     map[string][]func([]any)
	onConn []func()
	Logf   func(format string, args ...any)
}

// NewHub legt einen Hub für addr an (Run startet die Verbindung).
func NewHub(addr string) *Hub {
	return &Hub{addr: addr, on: map[string][]func([]any){}, Logf: log.Printf}
}

// Subscribe meldet fn für ein Thema an (auch nach Wiederverbindung).
func (h *Hub) Subscribe(topic string, fn func([]any)) {
	h.mu.Lock()
	first := len(h.on[topic]) == 0
	h.on[topic] = append(h.on[topic], fn)
	c := h.c
	h.mu.Unlock()
	if c != nil && first {
		c.Subscribe(topic)
	}
}

// OnConnect ruft fn bei jeder (Wieder-)Verbindung auf.
func (h *Hub) OnConnect(fn func()) { h.mu.Lock(); h.onConn = append(h.onConn, fn); h.mu.Unlock() }

func (h *Hub) client() *Client { h.mu.Lock(); defer h.mu.Unlock(); return h.c }

// Connected meldet, ob die Verbindung steht.
func (h *Hub) Connected() bool { return h.client() != nil }

// Run verbindet und hält die Verbindung (blockiert).
func (h *Hub) Run() {
	for {
		if h.client() == nil {
			c, err := Connect(h.addr, h.dispatch)
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
					c.Subscribe(t)
				}
				h.Logf("WAMP verbunden")
				for _, f := range cbs {
					go f()
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func (h *Hub) dispatch(topic string, args []any) {
	if topic == "" { // Verbindung verloren
		h.mu.Lock()
		h.c = nil
		h.mu.Unlock()
		h.Logf("WAMP getrennt")
		return
	}
	h.mu.Lock()
	fns := append([]func([]any){}, h.on[topic]...)
	h.mu.Unlock()
	for _, f := range fns {
		f(args)
	}
}

// ErrNotConnected: keine Verbindung zum Router.
var ErrNotConnected = errString("keine WAMP-Verbindung")

type errString string

func (e errString) Error() string { return string(e) }

// Call ruft eine Prozedur auf (Fehler, wenn nicht verbunden).
func (h *Hub) Call(proc string, args ...any) ([]any, error) {
	r, _, err := h.CallKw(proc, nil, args...)
	return r, err
}

// CallKw ruft eine Prozedur mit Schlüsselwort-Argumenten auf.
func (h *Hub) CallKw(proc string, kw map[string]any, args ...any) ([]any, map[string]any, error) {
	c := h.client()
	if c == nil {
		return nil, nil, ErrNotConnected
	}
	return c.CallKw(proc, kw, args...)
}

// Publish veröffentlicht ein Ereignis (ohne Wirkung, wenn nicht verbunden).
func (h *Hub) Publish(topic string, args ...any) {
	if c := h.client(); c != nil {
		c.Publish(topic, args...)
	}
}
