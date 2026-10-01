package main

import (
	"bytes"
	"strings"
	"testing"
)

// Eine CASTV2-Nachricht muss durch Rahmen schreiben und lesen unverändert zurückkommen (auch mit langer Nutzlast).
func TestFrameRoundTrip(t *testing.T) {
	for _, m := range []*castMessage{
		{Source: "sender-0", Dest: "receiver-0", Namespace: nsReceiver, Payload: []byte(`{"type":"GET_STATUS","requestId":1}`)},
		{Source: "a", Dest: "b", Namespace: nsMedia, Payload: []byte(strings.Repeat("x", 70000))},
		{Source: "a", Dest: "b", Namespace: nsDeviceAuth, Binary: true, Payload: []byte{0, 1, 2, 0xff}},
	} {
		var buf bytes.Buffer
		if err := writeFrame(&buf, m); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != m.Source || got.Dest != m.Dest || got.Namespace != m.Namespace || got.Binary != m.Binary || !bytes.Equal(got.Payload, m.Payload) {
			t.Fatalf("%+v != %+v", got, m)
		}
	}
}

func TestReportState(t *testing.T) {
	p := newPlayer("null", func() {})
	if st := p.report(); st["state"] != "idle" {
		t.Fatal(st)
	}
	p.media = map[string]any{"metadata": map[string]any{"title": "T", "artist": "A", "albumName": "B"}}
	p.state = "PLAYING"
	st := p.report()
	if st["state"] != "playing" || st["title"] != "T" || st["artist"] != "A" || st["album"] != "B" {
		t.Fatal(st)
	}
}
