package wamp

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// fakeRouter spricht genug Rawsocket/WAMP, um Anmeldung, Aufruf, Abonnement und Ereignis zu prüfen.
func fakeRouter(t *testing.T) (string, func(topic string, args []any)) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	events := make(chan []any, 4)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		var hs [4]byte
		io.ReadFull(c, hs[:])
		c.Write([]byte{0x7f, 0xf2, 0, 0})
		send := func(m []any) {
			b, _ := msgpack.Marshal(m)
			var h [4]byte
			binary.BigEndian.PutUint32(h[:], uint32(len(b)))
			c.Write(append(h[:], b...))
		}
		go func() {
			for e := range events {
				send(e)
			}
		}()
		for {
			var h [4]byte
			if _, err := io.ReadFull(c, h[:]); err != nil {
				return
			}
			body := make([]byte, binary.BigEndian.Uint32(h[:])&0xffffff)
			io.ReadFull(c, body)
			var m []any
			msgpack.Unmarshal(body, &m)
			switch ToInt(m[0]) {
			case 1:
				send([]any{2, 1, map[string]any{}})
			case 48:
				args, _ := m[4].([]any)
				send([]any{50, m[1], map[string]any{}, []any{ToInt(args[0]) + 1}, map[string]any{"group": "music"}})
			case 32:
				send([]any{33, m[1], 77})
			}
		}
	}()
	return ln.Addr().String(), func(topic string, args []any) { events <- []any{36, 77, 1, map[string]any{}, args} }
}

func TestCallAndEvent(t *testing.T) {
	addr, emit := fakeRouter(t)
	got := make(chan []any, 1)
	c, err := Connect(addr, func(topic string, args []any) {
		if topic == "com.harman.volumeChanged" {
			got <- args
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, kw, err := c.CallKw("com.harman.volumeAdjust", nil, 41)
	if err != nil || ToInt(r[0]) != 42 || kw["group"] != "music" {
		t.Fatalf("Ergebnis %v %v %v", r, kw, err)
	}
	if err := c.Subscribe("com.harman.volumeChanged"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	emit("com.harman.volumeChanged", []any{"music", 30})
	select {
	case a := <-got:
		if a[0] != "music" || ToInt(a[1]) != 30 {
			t.Fatalf("Ereignis %v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kein Ereignis")
	}
}

func TestToStrMapNested(t *testing.T) {
	m := ToStrMap(map[any]any{"music": map[any]any{"volume": int8(5)}})
	if ToInt(ToStrMap(m["music"])["volume"]) != 5 {
		t.Fatalf("%v", m)
	}
}
