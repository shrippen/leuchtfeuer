package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Die Konfiguration /data/leuchtfeuer/config ist eine sh-Datei mit Zeilen KEY="value" (wird von den Dienst-Skripten
// per "." geladen). leuchtfeuerd liest und ändert sie, ohne sie auszuführen: Zeilen, die nicht dem Muster
// entsprechen (Kommentare, Leerzeilen), bleiben unverändert erhalten.

type shellConfig struct {
	mu   sync.Mutex
	path string
}

func parseLine(l string) (k, v string, ok bool) {
	l = strings.TrimSpace(l)
	if l == "" || strings.HasPrefix(l, "#") {
		return
	}
	i := strings.IndexByte(l, '=')
	if i <= 0 {
		return
	}
	k = l[:i]
	for _, c := range k {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", "", false
		}
	}
	v = strings.TrimSpace(l[i+1:])
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	return k, v, true
}

func (c *shellConfig) lines() []string {
	f, err := os.Open(c.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func (c *shellConfig) Get(key, def string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	val := def
	for _, l := range c.lines() {
		if k, v, ok := parseLine(l); ok && k == key {
			val = v
		}
	}
	return val
}

func shellQuote(v string) string {
	// sh-sicher in doppelten Anführungszeichen: \ " $ ` maskieren, Zeilenumbrüche entfernen
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`", "\n", " ", "\r", " ")
	return `"` + r.Replace(v) + `"`
}

// Set setzt Schlüssel (Reihenfolge der Datei bleibt, neue Schlüssel kommen ans Ende).
func (c *shellConfig) Set(kv map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := c.lines()
	done := map[string]bool{}
	for i, l := range lines {
		if k, _, ok := parseLine(l); ok {
			if v, has := kv[k]; has {
				lines[i] = k + "=" + shellQuote(v)
				done[k] = true
			}
		}
	}
	for k, v := range kv {
		if !done[k] {
			lines = append(lines, k+"="+shellQuote(v))
		}
	}
	tmp := c.path + ".new"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Delete entfernt Schlüssel samt Zeile.
func (c *shellConfig) Delete(keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	changed := false
	for _, l := range c.lines() {
		if k, _, ok := parseLine(l); ok {
			drop := false
			for _, d := range keys {
				drop = drop || k == d
			}
			if drop {
				changed = true
				continue
			}
		}
		out = append(out, l)
	}
	if !changed {
		return nil
	}
	tmp := c.path + ".new"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *shellConfig) String() string { return fmt.Sprintf("config(%s)", c.path) }
