package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Sprachassistent: Der Lautsprecher wird ein Wyoming-Satellit für Home Assistant (Assist). Home Assistant verbindet
// sich zum Lautsprecher (TCP, Standard 10700; per mDNS als _wyoming._tcp gefunden oder von Hand eingetragen).
//
// Modus "wake": Nach run-satellite schickt der Lautsprecher dauernd Mikrofon-Ton (16 kHz, 16 bit, mono); das
// Aktivierungswort erkennt Home Assistant (openWakeWord), danach Spracherkennung, Antwort, Sprachausgabe.
// Modus "button": Zugehört wird nur nach der Aktion "voice" (Taste, Oberfläche): Pipeline ab der Spracherkennung.
// Die Antwort spielt über "leuchtfeuer_announce"; die Musik wird solange abgesenkt, das Mikrofon schickt währenddessen
// Stille (keine Echounterdrückung: sonst hörte sich der Lautsprecher selbst). Der Leuchtring zeigt Zuhören (blau,
// pulsierend), Nachdenken (kreisend) und Sprechen.
//
// Welches ALSA-Gerät die Mikrofone liefert, ist am Gerät zu prüfen (scripts/smoke.sh listet und misst die
// Aufnahmegeräte); ohne Eintrag (Voice.Mic) bleibt der Satellit aus.

type VoiceSettings struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`   // Standard 10700
	Mic     string `json:"mic"`    // ALSA-Aufnahmegerät, z. B. "leuchtfeuer_mic" (Tonkette des Zielgeräts) oder "plughw:2,0"
	Mode    string `json:"mode"`   // wake | button
	Area    string `json:"area"`   // Bereich in Home Assistant (optional)
	DuckDB  int    `json:"duckDB"` // Musik während Zuhören und Antwort absenken
	Muted   bool   `json:"muted"`  // Mikrofon aus (Privatsphäre)
}

func defaultVoice() VoiceSettings { return VoiceSettings{Port: 10700, Mode: "wake", DuckDB: 20} }

const (
	micRate  = 16000
	micChunk = 2048 // Bytes = 1024 Abtastwerte = 64 ms
)

type voiceStatus struct {
	Enabled    bool   `json:"enabled"`
	Connected  bool   `json:"connected"` // Home Assistant verbunden (run-satellite)
	State      string `json:"state"`     // off | idle | listening | thinking | speaking
	Streaming  bool   `json:"streaming"` // Mikrofon läuft
	LastHeard  string `json:"lastHeard"`
	LastAnswer string `json:"lastAnswer"`
	Error      string `json:"error"`
	Muted      bool   `json:"muted"`
}

type wyConn struct {
	c  net.Conn
	mu sync.Mutex
}

func (w *wyConn) send(typ string, data map[string]any, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return writeWyoming(w.c, typ, data, payload)
}

type voiceSat struct {
	a  *app
	mu sync.Mutex

	ln       net.Listener
	port     int
	active   *wyConn
	stream   bool      // Modus wake: dauernd schicken
	ptt      time.Time // Taste: bis dahin schicken
	pttRun   bool      // die laufende Pipeline kam von der Taste
	speaking bool
	state    string
	until    time.Time // Ende der Unterhaltung spätestens
	heard    string
	answer   string
	errText  string
	ducked   bool
	micOn    bool
	lvlRMS   float64 // Pegel des letzten Blocks (dBFS), für die Anzeige in der Oberfläche
	lvlPeak  float64
	lvlAt    time.Time
	kick     chan struct{}
	out      *exec.Cmd
	outIn    io.WriteCloser

	// austauschbar für Tests
	arecord func(dev string) *exec.Cmd
	aplay   func(rate, width, ch int) *exec.Cmd
	zc      func(port int) func() // mDNS-Anmeldung, liefert die Abmeldung
	unzc    func()
}

func newVoice(a *app) *voiceSat {
	v := &voiceSat{a: a, state: "off", kick: make(chan struct{}, 1)}
	v.arecord = func(dev string) *exec.Cmd {
		return exec.Command("arecord", "-q", "-D", dev, "-f", "S16_LE", "-r", fmt.Sprint(micRate), "-c", "1", "-t", "raw")
	}
	v.aplay = func(rate, width, ch int) *exec.Cmd {
		f := map[int]string{1: "S8", 2: "S16_LE", 4: "S32_LE"}[width]
		if f == "" {
			f = "S16_LE"
		}
		return exec.Command("aplay", "-q", "-D", "leuchtfeuer_announce", "-f", f, "-r", fmt.Sprint(rate), "-c", fmt.Sprint(ch), "-t", "raw")
	}
	return v
}

func (v *voiceSat) settings() VoiceSettings { return v.a.st.Snapshot().Voice }

// Apply startet oder stoppt den Satelliten nach den Einstellungen (beim Start und nach jeder Änderung).
func (v *voiceSat) Apply() {
	set := v.settings()
	on := set.Enabled && set.Mic != ""
	v.mu.Lock()
	running, port := v.ln != nil, v.port
	v.mu.Unlock()
	if running && (!on || port != set.Port) {
		v.shutdown()
		running = false
	}
	if on && !running {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", set.Port))
		if err != nil {
			v.setErr(fmt.Sprintf("Port %d: %v", set.Port, err))
			return
		}
		v.mu.Lock()
		v.ln, v.port, v.state, v.errText = ln, set.Port, "idle", ""
		if v.zc != nil {
			v.unzc = v.zc(set.Port)
		}
		v.mu.Unlock()
		log.Printf("Sprachassistent: wartet auf Home Assistant (Port %d, Mikrofon %s, Modus %s)", set.Port, set.Mic, set.Mode)
		go v.accept(ln)
	}
	v.writePorts()
	v.wake()
}

func (v *voiceSat) shutdown() {
	v.mu.Lock()
	ln, act, unzc := v.ln, v.active, v.unzc
	v.ln, v.active, v.unzc, v.state, v.stream = nil, nil, nil, "off", false
	v.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	if act != nil {
		act.c.Close()
	}
	if unzc != nil {
		unzc()
	}
	v.endInteraction(false)
	v.wake()
}

func (v *voiceSat) setErr(e string) {
	v.mu.Lock()
	v.errText = e
	v.mu.Unlock()
	log.Printf("Sprachassistent: %s", e)
	v.a.emit("voice", nil)
}

func (v *voiceSat) wake() {
	select {
	case v.kick <- struct{}{}:
	default:
	}
}

func (v *voiceSat) Status() voiceStatus {
	set := v.settings()
	v.mu.Lock()
	defer v.mu.Unlock()
	st := v.state
	if !set.Enabled || set.Mic == "" {
		st = "off"
	}
	return voiceStatus{Enabled: set.Enabled, Connected: v.active != nil, State: st, Streaming: v.micOn,
		LastHeard: v.heard, LastAnswer: v.answer, Error: v.errText, Muted: set.Muted}
}

func (v *voiceSat) setState(s string) {
	v.mu.Lock()
	ch := v.state != s
	v.state = s
	v.mu.Unlock()
	if ch {
		v.a.emit("voice", map[string]any{"state": s})
	}
}

func (v *voiceSat) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go v.serve(&wyConn{c: c})
	}
}

func (v *voiceSat) info() map[string]any {
	set := v.settings()
	var area any
	if set.Area != "" {
		area = set.Area
	}
	name := v.a.cfg.Get("DEVICE_NAME", hw.DefaultName)
	return map[string]any{
		"asr": []any{}, "tts": []any{}, "handle": []any{}, "intent": []any{}, "wake": []any{}, "mic": []any{}, "snd": []any{},
		"satellite": map[string]any{"name": name, "description": "Leuchtfeuer " + name, "attribution": map[string]any{"name": "Leuchtfeuer", "url": "https://git.arianw.de/shrippen/leuchtfeuer"},
			"installed": true, "version": v.a.version, "area": area},
	}
}

func (v *voiceSat) runPipeline(w *wyConn, start string) error {
	restart := start == "wake"
	return w.send("run-pipeline", map[string]any{"start_stage": start, "end_stage": "tts", "restart_on_end": restart}, nil)
}

func (v *voiceSat) serve(w *wyConn) {
	defer func() {
		w.c.Close()
		v.mu.Lock()
		was := v.active == w
		if was {
			v.active, v.stream = nil, false
		}
		v.mu.Unlock()
		if was {
			log.Printf("Sprachassistent: Home Assistant getrennt")
			v.endInteraction(false)
			v.wake()
			v.a.emit("voice", nil)
		}
	}()
	r := bufio.NewReaderSize(w.c, 64<<10)
	for {
		w.c.SetReadDeadline(time.Now().Add(2 * time.Minute))
		ev, err := readWyoming(r)
		if err != nil {
			return
		}
		switch ev.Type {
		case "describe":
			w.send("info", v.info(), nil)
		case "ping":
			w.send("pong", map[string]any{"text": wyStr(ev.Data, "text")}, nil)
		case "run-satellite":
			set := v.settings()
			v.mu.Lock()
			old := v.active
			v.active, v.stream, v.errText = w, set.Mode != "button", ""
			v.mu.Unlock()
			if old != nil && old != w {
				old.c.Close()
			}
			log.Printf("Sprachassistent: Home Assistant verbunden (%s)", w.c.RemoteAddr())
			if set.Mode != "button" {
				v.runPipeline(w, "wake")
			}
			v.setState("idle")
			v.wake()
		case "pause-satellite":
			v.mu.Lock()
			if v.active == w {
				v.stream = false
			}
			v.mu.Unlock()
			v.wake()
		case "detection", "transcribe", "voice-started":
			v.beginInteraction()
			v.setState("listening")
		case "voice-stopped":
			v.setState("thinking")
		case "transcript":
			v.mu.Lock()
			v.heard = wyStr(ev.Data, "text")
			v.mu.Unlock()
			v.setState("thinking")
		case "synthesize":
			v.mu.Lock()
			v.answer = wyStr(ev.Data, "text")
			v.mu.Unlock()
		case "audio-start":
			v.startOutput(wyInt(ev.Data, "rate", 22050), wyInt(ev.Data, "width", 2), wyInt(ev.Data, "channels", 1))
		case "audio-chunk":
			v.mu.Lock()
			in := v.outIn
			v.mu.Unlock()
			if in != nil && len(ev.Payload) > 0 {
				in.Write(ev.Payload)
			}
		case "audio-stop":
			go v.finishOutput(w)
		case "error":
			v.setErr("Home Assistant: " + wyStr(ev.Data, "text"))
			v.endInteraction(true)
		case "timer-started", "timer-updated", "timer-cancelled", "timer-finished":
			logf("Sprachassistent: %s %v", ev.Type, ev.Data)
		}
	}
}

// beginInteraction: Musik absenken, Frist setzen (endet spätestens nach 40 s von selbst).
func (v *voiceSat) beginInteraction() {
	set := v.settings()
	v.mu.Lock()
	v.until = time.Now().Add(40 * time.Second)
	duck := !v.ducked && set.DuckDB > 0
	v.ducked = v.ducked || duck
	v.mu.Unlock()
	if duck && v.a.mix != nil {
		v.a.mix.Duck(set.DuckDB)
	}
}

// endInteraction: Musik zurück, Ring aus; nach einer Taste im Modus wake wieder auf das Aktivierungswort hören.
func (v *voiceSat) endInteraction(restartWake bool) {
	v.mu.Lock()
	ducked, w, ptt := v.ducked, v.active, v.pttRun
	v.ducked, v.pttRun, v.ptt, v.until, v.speaking = false, false, time.Time{}, time.Time{}, false
	stream := v.stream
	v.mu.Unlock()
	if ducked && v.a.mix != nil {
		v.a.mix.Duck(0)
	}
	if restartWake && ptt && stream && w != nil {
		v.runPipeline(w, "wake")
	}
	if v.Status().State != "off" {
		v.setState("idle")
	}
	v.wake()
}

func (v *voiceSat) startOutput(rate, width, ch int) {
	v.stopOutput()
	cmd := v.aplay(rate, width, ch)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil || cmd.Start() != nil {
		v.setErr("aplay für die Antwort startet nicht")
		return
	}
	v.mu.Lock()
	v.out, v.outIn, v.speaking = cmd, in, true
	v.until = time.Now().Add(2 * time.Minute)
	v.mu.Unlock()
	v.beginInteraction()
	v.setState("speaking")
}

func (v *voiceSat) stopOutput() {
	v.mu.Lock()
	cmd, in := v.out, v.outIn
	v.out, v.outIn = nil, nil
	v.mu.Unlock()
	if in != nil {
		in.Close()
	}
	if cmd != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		cmd.Wait()
	}
}

// finishOutput: Antwort zu Ende spielen lassen, dann "played" melden.
func (v *voiceSat) finishOutput(w *wyConn) {
	v.mu.Lock()
	cmd, in := v.out, v.outIn
	v.out, v.outIn = nil, nil
	v.mu.Unlock()
	if in != nil {
		in.Close()
	}
	if cmd != nil {
		cmd.Wait()
	}
	w.send("played", nil, nil)
	v.endInteraction(true)
}

// PushToTalk: zuhören ab der Spracherkennung (Taste, Oberfläche).
func (v *voiceSat) PushToTalk() error {
	set := v.settings()
	if !set.Enabled || set.Mic == "" {
		return fmt.Errorf("Sprachassistent ist aus")
	}
	if set.Muted {
		return fmt.Errorf("Mikrofon ist aus")
	}
	v.mu.Lock()
	w := v.active
	if w != nil {
		v.ptt, v.pttRun = time.Now().Add(20*time.Second), true
	}
	v.mu.Unlock()
	if w == nil {
		return fmt.Errorf("Home Assistant ist nicht verbunden")
	}
	if err := v.runPipeline(w, "asr"); err != nil {
		return err
	}
	v.beginInteraction()
	v.setState("listening")
	v.wake()
	return nil
}

// ToggleMute schaltet das Mikrofon aus/an (Aktion voice_mute).
func (v *voiceSat) ToggleMute() error {
	err := v.a.st.Update(func(s *Settings) { s.Voice.Muted = !s.Voice.Muted })
	v.a.emit("settings", nil)
	v.wake()
	return err
}

// micWanted: soll das Mikrofon gerade laufen?
func (v *voiceSat) micWanted() (*wyConn, bool) {
	set := v.settings()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.active == nil || !set.Enabled || set.Mic == "" || set.Muted {
		return v.active, false
	}
	return v.active, v.stream || time.Now().Before(v.ptt)
}

// Run: Mikrofon nach Bedarf starten/stoppen, Ton an Home Assistant; Unterhaltungen mit Frist beenden.
func (v *voiceSat) Run() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if w, want := v.micWanted(); want {
			v.capture(w)
			continue
		}
		v.mu.Lock()
		over := !v.until.IsZero() && time.Now().After(v.until)
		v.mu.Unlock()
		if over {
			v.endInteraction(true)
		}
		select {
		case <-v.kick:
		case <-t.C:
		}
	}
}

// capture läuft, solange das Mikrofon gebraucht wird.
func (v *voiceSat) capture(w *wyConn) {
	set := v.settings()
	cmd := v.arecord(set.Mic)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		v.setErr("Mikrofon " + set.Mic + ": " + err.Error())
		time.Sleep(5 * time.Second)
		return
	}
	v.mu.Lock()
	v.micOn = true
	v.mu.Unlock()
	v.a.emit("voice", nil)
	defer func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		cmd.Wait()
		v.mu.Lock()
		v.micOn = false
		v.mu.Unlock()
		v.a.emit("voice", nil)
	}()
	buf := make([]byte, micChunk)
	zero := make([]byte, micChunk)
	start := time.Now()
	for {
		if _, err := io.ReadFull(out, buf); err != nil {
			v.setErr("Mikrofon " + set.Mic + " liefert nichts mehr")
			time.Sleep(3 * time.Second)
			return
		}
		rms, peak := levelS16(buf)
		v.mu.Lock()
		v.lvlRMS, v.lvlPeak, v.lvlAt = rms, peak, time.Now()
		v.mu.Unlock()
		cur, want := v.micWanted()
		if !want || cur != w {
			return
		}
		v.mu.Lock()
		speaking, over := v.speaking, !v.until.IsZero() && time.Now().After(v.until)
		v.mu.Unlock()
		if over {
			v.endInteraction(true)
		}
		data := buf
		if speaking {
			data = zero // eigene Antwort nicht aufnehmen
		}
		ts := time.Since(start).Milliseconds()
		if err := w.send("audio-chunk", map[string]any{"rate": micRate, "width": 2, "channels": 1, "timestamp": ts}, data); err != nil {
			w.c.Close()
			return
		}
	}
}

// level: Pegel der laufenden Aufnahme (ok = frisch, höchstens 1 s alt).
func (v *voiceSat) level() (rms, peak float64, ok bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.micOn || time.Since(v.lvlAt) > time.Second {
		return 0, 0, false
	}
	return v.lvlRMS, v.lvlPeak, true
}

// voiceScene: Leuchtring während einer Unterhaltung (Cortana-Blau 27,80,180).
func (a *app) voiceScene(now time.Time, fr *ringFrame) bool {
	if a.voice == nil {
		return false
	}
	st := a.voice.Status().State
	base := [3]float64{27, 80, 180}
	t := float64(now.UnixNano()%int64(4*time.Second)) / float64(4*time.Second)
	switch st {
	case "listening":
		k := 0.55 + 0.45*math.Sin(2*math.Pi*t*2)
		for i := range fr {
			fr[i] = [3]byte{byte(base[0] * k), byte(base[1] * k), byte(base[2] * k)}
		}
	case "thinking":
		head := int(t*float64(vizLEDs)*2) % vizLEDs
		for i := range fr {
			d := (i - head + vizLEDs) % vizLEDs
			k := math.Max(0.08, 1-float64(d)*0.3)
			fr[i] = [3]byte{byte(base[0] * k), byte(base[1] * k), byte(base[2] * k)}
		}
	case "speaking":
		for i := range fr {
			fr[i] = [3]byte{byte(base[0] * 0.8), byte(base[1] * 0.8), byte(base[2] * 0.8)}
		}
	default:
		return false
	}
	return true
}

// ---- Ports für den Hook ----

// writePorts: zusätzliche Ports, die leuchtfeuerd braucht (Sprachassistent), für die Firewall des Hooks
// (/data/leuchtfeuer/ports.leuchtfeuerd, gleiches Format wie ports.local).

func (v *voiceSat) writePorts() {
	var lines []string
	if set := v.settings(); set.Enabled && set.Mic != "" {
		lines = append(lines, fmt.Sprintf("tcp %d sprachassistent", set.Port))
	}
	body := "# von leuchtfeuerd geschrieben (Sprachassistent), nicht von Hand ändern\n" + strings.Join(lines, "\n")
	if len(lines) > 0 {
		body += "\n"
	}
	if b, err := os.ReadFile(portsFile); err == nil && string(b) == body {
		return
	}
	tmp := portsFile + ".new"
	if os.WriteFile(tmp, []byte(body), 0o644) == nil {
		os.Rename(tmp, portsFile)
	}
}
