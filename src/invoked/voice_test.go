package main

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWyomingRoundtrip(t *testing.T) {
	var b bytes.Buffer
	writeWyoming(&b, "audio-chunk", map[string]any{"rate": 16000, "width": 2, "channels": 1}, []byte{1, 2, 3, 4})
	writeWyoming(&b, "ping", nil, nil)
	// älteres Format: Daten in der Kopfzeile, dazu ein Abschnitt, der ergänzt
	b.WriteString(`{"type":"transcript","data":{"text":"alt","language":"de"},"data_length":14}` + "\n" + `{"text":"neu"}`)
	r := bufio.NewReader(&b)
	ev, err := readWyoming(r)
	if err != nil || ev.Type != "audio-chunk" || wyInt(ev.Data, "rate", 0) != 16000 || !bytes.Equal(ev.Payload, []byte{1, 2, 3, 4}) {
		t.Fatalf("%+v %v", ev, err)
	}
	if ev, _ = readWyoming(r); ev.Type != "ping" || len(ev.Payload) != 0 {
		t.Fatalf("%+v", ev)
	}
	if ev, _ = readWyoming(r); wyStr(ev.Data, "text") != "neu" || wyStr(ev.Data, "language") != "de" {
		t.Fatalf("Daten zusammenführen: %+v", ev)
	}
	if _, err := readWyoming(bufio.NewReader(strings.NewReader(`{"type":"x","payload_length":999999999}` + "\n"))); err == nil {
		t.Fatal("riesige Nutzlast angenommen")
	}
}

// Home Assistant als Gegenstelle: describe, run-satellite, Ton empfangen, ping, Antwort abspielen, played.
func TestVoiceSatellite(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	ta.st.Update(func(s *Settings) {
		s.Voice = VoiceSettings{Enabled: true, Port: 0, Mic: "plughw:9,0", Mode: "wake", DuckDB: 20}
	})
	portsFile = t.TempDir() + "/ports.invoked"
	v := newVoice(ta.app)
	ta.voice = v
	v.arecord = func(dev string) *exec.Cmd {
		if dev != "plughw:9,0" {
			t.Errorf("Mikrofon %s", dev)
		}
		return exec.Command("sh", "-c", "while :; do head -c 2048 /dev/zero; sleep 0.02; done")
	}
	played := make(chan bool, 1)
	v.aplay = func(rate, width, ch int) *exec.Cmd {
		if rate != 22050 || width != 2 || ch != 1 {
			t.Errorf("Ausgabe %d/%d/%d", rate, width, ch)
		}
		played <- true
		return exec.Command("sh", "-c", "cat >/dev/null")
	}
	// Port 0 = frei wählen: direkt lauschen wie Apply
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	v.ln, v.state = ln, "idle"
	go v.accept(ln)
	go v.Run()
	defer v.shutdown()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	next := func(want string) wyEvent {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			ev, err := readWyoming(r)
			if err != nil {
				t.Fatalf("warte auf %s: %v", want, err)
			}
			if ev.Type == want {
				return ev
			}
		}
	}
	writeWyoming(c, "describe", nil, nil)
	info := next("info")
	sat, _ := info.Data["satellite"].(map[string]any)
	if sat["installed"] != true || sat["name"] != "Test" {
		t.Fatalf("info: %v", info.Data)
	}
	writeWyoming(c, "run-satellite", nil, nil)
	rp := next("run-pipeline")
	if rp.Data["start_stage"] != "wake" || rp.Data["end_stage"] != "tts" || rp.Data["restart_on_end"] != true {
		t.Fatalf("run-pipeline: %v", rp.Data)
	}
	ch := next("audio-chunk")
	if wyInt(ch.Data, "rate", 0) != 16000 || len(ch.Payload) != micChunk {
		t.Fatalf("Ton: %v %d", ch.Data, len(ch.Payload))
	}
	writeWyoming(c, "ping", map[string]any{"text": "x"}, nil)
	if p := next("pong"); wyStr(p.Data, "text") != "x" {
		t.Fatalf("pong: %v", p.Data)
	}
	// Unterhaltung: erkannt, Text, Antwort
	writeWyoming(c, "detection", map[string]any{"name": "ok_nabu"}, nil)
	time.Sleep(50 * time.Millisecond)
	if st := v.Status(); st.State != "listening" || !st.Connected || !st.Streaming {
		t.Fatalf("Zustand: %+v", st)
	}
	if m := ta.mix; m.duck != 20 {
		t.Fatalf("Musik nicht abgesenkt: %d", m.duck)
	}
	writeWyoming(c, "transcript", map[string]any{"text": "Wie spät ist es"}, nil)
	writeWyoming(c, "synthesize", map[string]any{"text": "Es ist sieben Uhr."}, nil)
	writeWyoming(c, "audio-start", map[string]any{"rate": 22050, "width": 2, "channels": 1}, nil)
	writeWyoming(c, "audio-chunk", map[string]any{"rate": 22050, "width": 2, "channels": 1}, make([]byte, 2048))
	writeWyoming(c, "audio-stop", nil, nil)
	next("played")
	select {
	case <-played:
	default:
		t.Fatal("Antwort nicht abgespielt")
	}
	time.Sleep(50 * time.Millisecond)
	st := v.Status()
	if st.LastHeard != "Wie spät ist es" || st.LastAnswer != "Es ist sieben Uhr." || st.State != "idle" {
		t.Fatalf("nach der Antwort: %+v", st)
	}
	if m := ta.mix; m.duck != 0 {
		t.Fatal("Musik bleibt abgesenkt")
	}
	// Taste: Pipeline ab der Spracherkennung, danach wieder Aktivierungswort
	if err := v.PushToTalk(); err != nil {
		t.Fatal(err)
	}
	if rp := next("run-pipeline"); rp.Data["start_stage"] != "asr" || rp.Data["restart_on_end"] != false {
		t.Fatalf("Taste: %v", rp.Data)
	}
	writeWyoming(c, "error", map[string]any{"text": "kein Ergebnis", "code": "stt-no-text-recognized"}, nil)
	if rp := next("run-pipeline"); rp.Data["start_stage"] != "wake" {
		t.Fatalf("nach Taste nicht wieder wake: %v", rp.Data)
	}
	// Mikrofon aus: kein Ton mehr
	ta.st.Update(func(s *Settings) { s.Voice.Muted = true })
	v.wake()
	time.Sleep(150 * time.Millisecond)
	if v.Status().Streaming {
		t.Fatal("Mikrofon läuft trotz Stummschaltung")
	}
}

func TestVoiceSceneStates(t *testing.T) {
	ta := newTestApp(t, "2026-10-05 07:00:00")
	if ta.voiceScene(time.Now(), &ringFrame{}) {
		t.Fatal("ohne Sprachassistent")
	}
	ta.voice = newVoice(ta.app)
	ta.st.Update(func(s *Settings) { s.Voice.Enabled, s.Voice.Mic = true, "x" })
	for _, st := range []string{"listening", "thinking", "speaking"} {
		ta.voice.state = st
		var fr ringFrame
		if !ta.voiceScene(time.Now(), &fr) || fr == (ringFrame{}) {
			t.Fatalf("%s: kein Bild", st)
		}
	}
	ta.voice.state = "idle"
	if ta.voiceScene(time.Now(), &ringFrame{}) {
		t.Fatal("idle zeigt etwas")
	}
}
