package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Protokolle live: invoked liest die Dienst-Protokolle (/data/invoke/log/*.log und hook.log) einmal je Sekunde weiter
// und verteilt neue Zeilen an die Live-Ansicht (/api/logs/stream, Server-Sent Events), an einen Syslog-Server
// (Settings.Syslog) und an die Zählung der Tonaussetzer (aplay meldet "underrun!!!"; metrics.go).
// Das Kürzen durch den Hook (copytruncate) erkennt der Leser daran, dass die Datei kleiner wird.

type logLine struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Text    string    `json:"text"`
}

type SyslogSettings struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`  // Standard 514
	Proto   string `json:"proto"` // udp | tcp
}

type logHub struct {
	a       *app
	mu      sync.Mutex
	pos     map[string]int64
	recent  []logLine // die letzten 400 Zeilen aller Dienste
	subs    map[int]chan logLine
	nextSub int
	xruns   map[string]int // Tonaussetzer je Dienst seit dem Start von invoked
	lines   map[string]int // Zeilen je Dienst seit dem Start von invoked

	slMu   sync.Mutex
	slConn net.Conn
	slKey  string
	slErr  time.Time
}

func newLogHub(a *app) *logHub {
	return &logHub{a: a, pos: map[string]int64{}, subs: map[int]chan logLine{}, xruns: map[string]int{}, lines: map[string]int{}}
}

var xrunRe = regexp.MustCompile(`(?i)underrun|xrun`)

func logFiles() map[string]string {
	m := map[string]string{"hook": hookLog}
	fs, _ := filepath.Glob(filepath.Join(logDir, "*.log"))
	for _, f := range fs {
		m[strings.TrimSuffix(filepath.Base(f), ".log")] = f
	}
	return m
}

// Run liest beim Start nur das Ende (nichts Altes erneut verschicken) und danach jede Sekunde weiter.
func (h *logHub) Run() {
	for name, f := range logFiles() {
		if st, err := os.Stat(f); err == nil {
			h.pos[name] = st.Size()
		}
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		h.poll()
	}
}

func (h *logHub) poll() {
	for name, f := range logFiles() {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		h.mu.Lock()
		pos, known := h.pos[name]
		h.mu.Unlock()
		if !known {
			pos = 0 // neue Datei: von vorne
		}
		if st.Size() < pos {
			pos = 0 // gekürzt
		}
		if st.Size() == pos {
			h.mu.Lock()
			h.pos[name] = pos
			h.mu.Unlock()
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		n := st.Size() - pos
		if n > 256<<10 { // nach langer Pause nur das Ende
			pos, n = st.Size()-256<<10, 256<<10
		}
		buf := make([]byte, n)
		k, _ := fh.ReadAt(buf, pos)
		fh.Close()
		buf = buf[:k]
		// nur vollständige Zeilen; den Rest beim nächsten Mal
		last := bytes.LastIndexByte(buf, '\n')
		if last < 0 {
			continue
		}
		h.mu.Lock()
		h.pos[name] = pos + int64(last) + 1
		h.mu.Unlock()
		sc := bufio.NewScanner(bytes.NewReader(buf[:last+1]))
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			if txt := strings.TrimRight(sc.Text(), "\r"); txt != "" {
				h.publish(logLine{Time: time.Now(), Service: name, Text: txt})
			}
		}
	}
}

func (h *logHub) publish(l logLine) {
	h.mu.Lock()
	h.recent = append(h.recent, l)
	if len(h.recent) > 400 {
		h.recent = h.recent[len(h.recent)-400:]
	}
	h.lines[l.Service]++
	if xrunRe.MatchString(l.Text) {
		h.xruns[l.Service]++
	}
	subs := make([]chan logLine, 0, len(h.subs))
	for _, c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- l:
		default: // langsamer Leser: Zeile verwerfen statt zu warten
		}
	}
	if h.a != nil {
		h.syslog(l)
	}
}

func (h *logHub) Counters() (xruns, lines map[string]int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	xruns, lines = map[string]int{}, map[string]int{}
	for k, v := range h.xruns {
		xruns[k] = v
	}
	for k, v := range h.lines {
		lines[k] = v
	}
	return
}

func (h *logHub) subscribe() (chan logLine, func()) {
	c := make(chan logLine, 256)
	h.mu.Lock()
	id := h.nextSub
	h.nextSub++
	h.subs[id] = c
	h.mu.Unlock()
	return c, func() { h.mu.Lock(); delete(h.subs, id); h.mu.Unlock() }
}

func (h *logHub) Recent(service string, n int) []logLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []logLine{}
	for _, l := range h.recent {
		if service == "" || l.Service == service {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// stream: /api/logs/stream?service=<name> (leer = alle) - erst die letzten Zeilen, dann live.
func (h *logHub) stream(rw http.ResponseWriter, r *http.Request) {
	fl, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "kein Streaming", http.StatusInternalServerError)
		return
	}
	svc := r.URL.Query().Get("service")
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Accel-Buffering", "no")
	c, stop := h.subscribe()
	defer stop()
	send := func(l logLine) bool {
		b, _ := json.Marshal(l)
		if _, err := fmt.Fprintf(rw, "event: line\ndata: %s\n\n", b); err != nil {
			return false
		}
		return true
	}
	for _, l := range h.Recent(svc, 200) {
		if !send(l) {
			return
		}
	}
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case l := <-c:
			if svc != "" && l.Service != svc {
				continue
			}
			if !send(l) {
				return
			}
			fl.Flush()
		case <-ping.C:
			if _, err := io.WriteString(rw, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// ---- Syslog (RFC 5424, Facility local0) ----

func syslogSeverity(text string) int {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "fehler") || strings.Contains(t, "error") || strings.Contains(t, "fatal") || strings.Contains(t, "panic"):
		return 3
	case strings.Contains(t, "warn") || xrunRe.MatchString(t):
		return 4
	}
	return 6
}

func syslogMessage(host string, l logLine) string {
	pri := 16*8 + syslogSeverity(l.Text)
	app := strings.Map(func(r rune) rune {
		if r <= 32 || r > 126 {
			return '-'
		}
		return r
	}, l.Service)
	if host == "" {
		host = "-"
	}
	return fmt.Sprintf("<%d>1 %s %s leuchtfeuer-%s - - - %s", pri, l.Time.UTC().Format("2006-01-02T15:04:05.000Z"), host, app, l.Text)
}

func (h *logHub) syslog(l logLine) {
	set := h.a.st.Snapshot().Syslog
	if !set.Enabled || set.Host == "" {
		h.closeSyslog()
		return
	}
	port := set.Port
	if port == 0 {
		port = 514
	}
	proto := set.Proto
	if proto != "tcp" {
		proto = "udp"
	}
	key := fmt.Sprintf("%s:%s:%d", proto, set.Host, port)
	msg := syslogMessage(h.a.cfg.Get("DHCP_HOSTNAME", "invoke"), l)
	h.slMu.Lock()
	defer h.slMu.Unlock()
	if h.slConn != nil && h.slKey != key {
		h.slConn.Close()
		h.slConn = nil
	}
	if h.slConn == nil {
		if time.Since(h.slErr) < 30*time.Second {
			return
		}
		c, err := net.DialTimeout(proto, net.JoinHostPort(set.Host, fmt.Sprint(port)), 3*time.Second)
		if err != nil {
			h.slErr = time.Now()
			return
		}
		h.slConn, h.slKey = c, key
	}
	h.slConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	var err error
	if proto == "tcp" { // Oktett-Zählung (RFC 6587)
		_, err = fmt.Fprintf(h.slConn, "%d %s", len(msg), msg)
	} else {
		_, err = io.WriteString(h.slConn, msg)
	}
	if err != nil {
		h.slConn.Close()
		h.slConn, h.slErr = nil, time.Now()
	}
}

func (h *logHub) closeSyslog() {
	h.slMu.Lock()
	if h.slConn != nil {
		h.slConn.Close()
		h.slConn = nil
	}
	h.slMu.Unlock()
}

// logNames: bekannte Protokolle (für die Auswahl in der Oberfläche).
func logNames() []string {
	var out []string
	for n := range logFiles() {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
