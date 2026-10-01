package main

import (
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Uhr: Wecker hängen an der richtigen Zeit. Der Hook stellt die Uhr per NTP (busybox ntpd -q, nach dem Start und alle
// 6 h). invoked misst alle 30 Minuten selbst die Abweichung zu NTP_SERVER (SNTP-Abfrage) und zeigt sie an; weicht die
// Uhr mehr als 2 s ab, meldet die Oberfläche das.

type clockStatus struct {
	OffsetMs  int64     `json:"offsetMs"` // Serverzeit minus Gerätezeit
	Checked   time.Time `json:"checked"`  // letzte Messung (Nullwert: noch keine)
	Server    string    `json:"server"`
	LastSync  time.Time `json:"lastSync"` // letztes Stellen durch den Hook
	Error     string    `json:"error"`
	Synced    bool      `json:"synced"`    // Abweichung höchstens 2 s
	NoNtpTool bool      `json:"noNtpTool"` // auf dem Gerät gibt es kein busybox ntpd
}

var clk struct {
	mu sync.Mutex
	s  clockStatus
}

// sntpOffset fragt einen NTP-Server (Version 4, Client) und liefert Serverzeit minus eigene Zeit.
func sntpOffset(server string) (time.Duration, error) {
	if !strings.Contains(server, ":") {
		server += ":123"
	}
	c, err := net.DialTimeout("udp", server, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	req := make([]byte, 48)
	req[0] = 0x23 // LI 0, Version 4, Client
	t1 := time.Now()
	if _, err := c.Write(req); err != nil {
		return 0, err
	}
	resp := make([]byte, 48)
	if _, err := c.Read(resp); err != nil {
		return 0, err
	}
	t4 := time.Now()
	ntp := func(b []byte) time.Time {
		sec := binary.BigEndian.Uint32(b[0:4])
		frac := binary.BigEndian.Uint32(b[4:8])
		return time.Unix(int64(sec)-2208988800, int64(uint64(frac)*1e9>>32))
	}
	t2, t3 := ntp(resp[32:40]), ntp(resp[40:48])
	return (t2.Sub(t1) + t3.Sub(t4)) / 2, nil
}

func (a *app) watchClock() {
	for {
		server := a.cfg.Get("NTP_SERVER", "")
		if server == "" {
			server = "pool.ntp.org"
		}
		off, err := sntpOffset(server)
		clk.mu.Lock()
		clk.s.Server, clk.s.Checked = server, time.Now()
		if err != nil {
			clk.s.Error = err.Error()
		} else {
			clk.s.Error, clk.s.OffsetMs = "", off.Milliseconds()
			clk.s.Synced = off < 2*time.Second && off > -2*time.Second
		}
		clk.mu.Unlock()
		time.Sleep(30 * time.Minute)
	}
}

func clockState() clockStatus {
	clk.mu.Lock()
	s := clk.s
	clk.mu.Unlock()
	if b, err := os.ReadFile(runDir + "/invoke-ntp.ok"); err == nil {
		if t, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			s.LastSync = time.Unix(t, 0)
		}
	}
	_, err := os.Stat(runDir + "/invoke-ntp.none")
	s.NoNtpTool = err == nil
	return s
}
