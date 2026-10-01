package main

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// player spielt genau einen Stream über gst-launch-1.0 (GStreamer 1.10 des Geräts). Pause und
// Fortsetzen per SIGSTOP/SIGCONT; Springen in der Zeit gibt es nicht, der Sender lädt den Stream
// dafür neu (so macht es Music Assistant).
type player struct {
	mu        sync.Mutex
	sink      string
	onChange  func()
	cmd       *exec.Cmd
	state     string // IDLE, BUFFERING, PLAYING, PAUSED
	idle      string // idleReason im Zustand IDLE: FINISHED, CANCELLED, ERROR
	media     map[string]any
	sessionID int
	started   time.Time
	elapsed   float64 // Sekunden bis zur letzten Pause
}

func newPlayer(sink string, onChange func()) *player {
	return &player{sink: sink, onChange: onChange, state: "IDLE"}
}

func (p *player) load(media map[string]any, url string) {
	p.mu.Lock()
	p.killLocked()
	p.sessionID++
	p.media = media
	p.state, p.idle = "BUFFERING", ""
	p.elapsed = 0
	cmd := exec.Command("gst-launch-1.0", "-e", "uridecodebin", "uri="+url,
		"!", "audioconvert", "!", "audioresample", "!", "alsasink", "device="+p.sink)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C") // Meldungen von gst-launch auf Englisch
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("gst-launch: %v", err)
		p.state, p.idle = "IDLE", "ERROR"
		p.mu.Unlock()
		p.onChange()
		return
	}
	p.cmd = cmd
	p.mu.Unlock()
	p.onChange()
	sc := bufio.NewScanner(out)
	sc.Split(splitLines)
	go p.watch(cmd, sc)
}

func (p *player) watch(cmd *exec.Cmd, sc *bufio.Scanner) {
	failed := false
	for sc.Scan() {
		line := sc.Text()
		if os.Getenv("CASTRECV_DEBUG") != "" {
			log.Printf("gst> %s", line)
		}
		switch {
		case strings.Contains(line, "Setting pipeline to PLAYING"):
			p.mu.Lock()
			if p.cmd == cmd && p.state == "BUFFERING" {
				p.state, p.started = "PLAYING", time.Now()
				p.mu.Unlock()
				p.onChange()
			} else {
				p.mu.Unlock()
			}
		case strings.HasPrefix(line, "ERROR"):
			failed = true
			log.Printf("gst: %s", line)
		}
	}
	err := cmd.Wait()
	log.Printf("gst-launch beendet: %v", err)
	p.mu.Lock()
	if p.cmd != cmd {
		p.mu.Unlock()
		return
	}
	p.cmd = nil
	p.state = "IDLE"
	if err != nil || failed {
		p.idle = "ERROR"
	} else {
		p.idle = "FINISHED"
	}
	p.mu.Unlock()
	p.onChange()
}

func (p *player) killLocked() {
	if p.cmd != nil {
		pid := p.cmd.Process.Pid
		syscall.Kill(-pid, syscall.SIGCONT)
		syscall.Kill(-pid, syscall.SIGTERM)
		p.cmd = nil
	}
}

func (p *player) stop() {
	p.mu.Lock()
	had := p.cmd != nil
	p.killLocked()
	p.state, p.idle = "IDLE", "CANCELLED"
	p.mu.Unlock()
	if had {
		p.onChange()
	}
}

func (p *player) pause() {
	p.mu.Lock()
	if p.cmd == nil || p.state != "PLAYING" {
		p.mu.Unlock()
		return
	}
	p.elapsed += time.Since(p.started).Seconds()
	p.state = "PAUSED"
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGSTOP)
	p.mu.Unlock()
	p.onChange()
}

func (p *player) resume() {
	p.mu.Lock()
	if p.cmd == nil || p.state != "PAUSED" {
		p.mu.Unlock()
		return
	}
	p.started = time.Now()
	p.state = "PLAYING"
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGCONT)
	p.mu.Unlock()
	p.onChange()
}

// status liefert das Element für MEDIA_STATUS (nil, wenn noch nie etwas geladen wurde).
func (p *player) status(vol float64, muted bool) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessionID == 0 {
		return nil
	}
	pos := p.elapsed
	if p.state == "PLAYING" {
		pos += time.Since(p.started).Seconds()
	}
	st := map[string]any{
		"mediaSessionId": p.sessionID,
		"playbackRate":   1,
		"playerState":    p.state,
		"currentTime":    pos,
		"volume":         map[string]any{"level": vol, "muted": muted},
		"media":          p.media,
		"currentItemId":  1,
	}
	st["supportedMediaCommands"] = 1 | 4 | 8 // Pause, Lautstärke, Stumm
	if p.state == "IDLE" {
		st["idleReason"] = p.idle
	}
	return st
}

func (p *player) sessionIDNow() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

// splitLines trennt bei \n und \r: gst-launch schreibt den Puffer-Fortschritt mit \r.
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// report liefert Zustand und Titel für invoked: state playing | paused | buffering | idle.
func (p *player) report() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := map[string]any{"state": map[string]string{"PLAYING": "playing", "PAUSED": "paused", "BUFFERING": "buffering"}[p.state], "title": "", "artist": "", "album": ""}
	if st["state"] == "" {
		st["state"] = "idle"
	}
	if md, ok := p.media["metadata"].(map[string]any); ok {
		for from, to := range map[string]string{"title": "title", "artist": "artist", "albumName": "album"} {
			if v, ok := md[from].(string); ok {
				st[to] = v
			}
		}
	}
	return st
}
