// Package wamp ist ein minimaler WAMP-v2-Client über Rawsocket/MessagePack für den Router "bonefish" des Invoke
// (127.0.0.1:9999, Realm "default"): Aufrufe (CALL), Abonnements (SUBSCRIBE) und Veröffentlichungen (PUBLISH).
// Gemeinsam genutzt von leuchtfeuerd, btagent und castrecv.
package wamp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// Addr ist der Router des Invoke.
const Addr = "127.0.0.1:9999"

// Client ist eine Verbindung zum Router. onEvent bekommt Ereignisse; topic "" meldet den Verlust der Verbindung.
type Client struct {
	conn    net.Conn
	wmu     sync.Mutex
	mu      sync.Mutex
	nextReq uint64
	pending map[uint64]chan []any
	onEvent func(topic string, args []any)
	topics  map[uint64]string // Abonnement-ID -> Topic
	subReq  map[uint64]string // Anfrage-ID -> Topic
}

// ToInt wandelt eine dekodierte MessagePack-Zahl in int.
func ToInt(v any) int {
	switch n := v.(type) {
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case int:
		return n
	case float32:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func toUint64(v any) uint64 { return uint64(ToInt(v)) }

// Connect verbindet sich mit dem Router und meldet sich im Realm "default" an.
func Connect(addr string, onEvent func(string, []any)) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, pending: map[uint64]chan []any{}, topics: map[uint64]string{}, subReq: map[uint64]string{}, onEvent: onEvent}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte{0x7f, 0xf2, 0, 0}); err != nil {
		conn.Close()
		return nil, err
	}
	var hs [4]byte
	if _, err := io.ReadFull(conn, hs[:]); err != nil || hs[0] != 0x7f || hs[1]&0x0f != 2 {
		conn.Close()
		return nil, fmt.Errorf("Rawsocket-Handshake fehlgeschlagen: %x %v", hs, err)
	}
	roles := map[string]any{"caller": map[string]any{}, "subscriber": map[string]any{}, "publisher": map[string]any{}}
	if err := c.send([]any{1, "default", map[string]any{"roles": roles}}); err != nil {
		conn.Close()
		return nil, err
	}
	msg, err := c.read()
	if err != nil || len(msg) == 0 || ToInt(msg[0]) != 2 {
		conn.Close()
		return nil, fmt.Errorf("kein WELCOME: %v %v", msg, err)
	}
	conn.SetDeadline(time.Time{})
	go c.loop()
	return c, nil
}

// Close beendet die Verbindung.
func (c *Client) Close() { c.conn.Close() }

func (c *Client) send(msg []any) error {
	b, err := msgpack.Marshal(msg)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	hdr[0] = 0 // reguläre WAMP-Nachricht
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.conn.Write(append(hdr[:], b...))
	return err
}

func (c *Client) read() ([]any, error) {
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint32(hdr[:]) & 0x00ffffff)
		body := make([]byte, n)
		if _, err := io.ReadFull(c.conn, body); err != nil {
			return nil, err
		}
		switch hdr[0] {
		case 1: // PING -> PONG
			pong := make([]byte, 4, 4+n)
			binary.BigEndian.PutUint32(pong, uint32(n))
			pong[0] = 2
			c.wmu.Lock()
			c.conn.Write(append(pong, body...))
			c.wmu.Unlock()
			continue
		case 2:
			continue
		}
		var msg []any
		if err := msgpack.NewDecoder(bytes.NewReader(body)).Decode(&msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
}

func (c *Client) loop() {
	for {
		msg, err := c.read()
		if err != nil {
			c.conn.Close()
			c.mu.Lock()
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			if c.onEvent != nil {
				c.onEvent("", nil) // Verbindung weg
			}
			return
		}
		if len(msg) < 2 {
			continue
		}
		switch ToInt(msg[0]) {
		case 33: // SUBSCRIBED [33, req, subId]
			if len(msg) >= 3 {
				c.mu.Lock()
				c.topics[toUint64(msg[2])] = c.subReq[toUint64(msg[1])]
				c.mu.Unlock()
			}
		case 36: // EVENT [36, subId, pubId, details, args, kwargs]
			c.mu.Lock()
			topic := c.topics[toUint64(msg[1])]
			c.mu.Unlock()
			var args []any
			if len(msg) >= 5 {
				args, _ = msg[4].([]any)
			}
			if c.onEvent != nil && topic != "" {
				c.onEvent(topic, args)
			}
		case 50: // RESULT [50, req, details, args, kwargs]
			c.resolve(toUint64(msg[1]), msg)
		case 8: // ERROR [8, reqType, req, details, error, ...]
			if len(msg) >= 3 {
				c.resolve(toUint64(msg[2]), msg)
			}
		}
	}
}

func (c *Client) resolve(id uint64, msg []any) {
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *Client) newReq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextReq++
	return c.nextReq
}

// Subscribe abonniert ein Thema; Ereignisse kommen bei onEvent an.
func (c *Client) Subscribe(topic string) error {
	id := c.newReq()
	c.mu.Lock()
	c.subReq[id] = topic
	c.mu.Unlock()
	return c.send([]any{32, id, map[string]any{}, topic})
}

// Call ruft eine Prozedur auf und gibt die Ergebnis-Argumente zurück.
func (c *Client) Call(proc string, args ...any) ([]any, error) {
	r, _, err := c.CallKw(proc, nil, args...)
	return r, err
}

// CallKw: wie Call, mit Schlüsselwort-Argumenten (kw) und liefert auch das Schlüsselwort-Ergebnis.
func (c *Client) CallKw(proc string, kw map[string]any, args ...any) ([]any, map[string]any, error) {
	id := c.newReq()
	ch := make(chan []any, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if args == nil {
		args = []any{}
	}
	msg := []any{48, id, map[string]any{}, proc, args}
	if kw != nil {
		msg = append(msg, kw)
	}
	if err := c.send(msg); err != nil {
		return nil, nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, nil, errors.New("Verbindung geschlossen")
		}
		if ToInt(m[0]) == 8 {
			return nil, nil, fmt.Errorf("WAMP-Fehler: %v", m)
		}
		var r []any
		var k map[string]any
		if len(m) >= 4 {
			r, _ = m[3].([]any)
		}
		if len(m) >= 5 {
			k = ToStrMap(m[4])
		}
		return r, k, nil
	case <-time.After(4 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, nil, errors.New("Zeitüberschreitung")
	}
}

// Publish veröffentlicht ein Ereignis (ohne Bestätigung).
func (c *Client) Publish(topic string, args ...any) error {
	if args == nil {
		args = []any{}
	}
	return c.send([]any{16, c.newReq(), map[string]any{}, topic, args})
}

// ToStrMap wandelt ein dekodiertes MessagePack-Objekt in map[string]any (verschachtelt).
func ToStrMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		o := map[string]any{}
		for k, x := range m {
			o[fmt.Sprint(k)] = normalize(x)
		}
		return o
	}
	return nil
}

func normalize(v any) any {
	if m, ok := v.(map[any]any); ok {
		return ToStrMap(m)
	}
	return v
}
