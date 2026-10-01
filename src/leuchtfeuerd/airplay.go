package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Titel von AirPlay: shairport-sync schreibt Metadaten in die Pipe /run/shairport-sync-metadata (shairport.sh).
// Jedes Element: <item><type>HEX</type><code>HEX</code><length>N</length>[<data encoding="base64">B64</data>]</item>.
// Gebraucht werden core/minm (Titel), core/asar (Interpret), core/asal (Album) und ssnc/pend (Wiedergabe beendet).
// Die Pipe wird lesend und schreibend geöffnet: so blockiert das Öffnen nicht und sie bleibt bestehen,
// auch wenn shairport-sync neu startet.

var airplayPipe = filepath.Join(runDir, "shairport-sync-metadata")

var (
	apType = regexp.MustCompile(`<type>([0-9a-f]{8})</type><code>([0-9a-f]{8})</code><length>(\d+)</length>`)
)

type airplayItem struct {
	Type, Code string
	Data       []byte
}

// parseAirplayMeta liest Elemente aus dem Strom und ruft f für jedes auf.
func parseAirplayMeta(r *bufio.Reader, f func(airplayItem)) error {
	var cur *airplayItem
	var b64 strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if line == "" && err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if m := apType.FindStringSubmatch(line); m != nil {
			t, _ := hex.DecodeString(m[1])
			c, _ := hex.DecodeString(m[2])
			n, _ := strconv.Atoi(m[3])
			cur = &airplayItem{Type: string(t), Code: string(c)}
			if n == 0 {
				f(*cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, `<data encoding="base64">`) {
			inData = true
			b64.Reset()
			line = strings.TrimPrefix(line, `<data encoding="base64">`)
		}
		if inData {
			end := strings.Contains(line, "</data>")
			b64.WriteString(strings.TrimSuffix(strings.TrimSpace(strings.Split(line, "</data>")[0]), "\n"))
			if end {
				cur.Data, _ = base64.StdEncoding.DecodeString(b64.String())
				f(*cur)
				cur, inData = nil, false
			}
		}
	}
}

func (a *app) readAirplayMeta() {
	for {
		syscall.Mkfifo(airplayPipe, 0o644)
		fh, err := os.OpenFile(airplayPipe, os.O_RDWR, 0)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}
		parseAirplayMeta(bufio.NewReader(fh), func(it airplayItem) {
			key := map[string]string{"core/minm": "title", "core/asar": "artist", "core/asal": "album"}[it.Type+"/"+it.Code]
			switch {
			case key != "":
				a.src.mu.Lock()
				st := "playing"
				if s := a.src.s["airplay"]; s != nil {
					st = s.State
				}
				a.src.mu.Unlock()
				a.src.Update("airplay", st, map[string]string{key: string(it.Data)})
			case it.Type == "ssnc" && it.Code == "pend": // Wiedergabe beendet
				a.src.Update("airplay", "idle", nil)
			}
		})
		fh.Close()
		time.Sleep(time.Second)
	}
}
