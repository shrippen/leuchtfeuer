package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SSH-Schlüssel verwalten: /data/leuchtfeuer/authorized_keys (der Hook kopiert die Datei binnen 30 s auf das tmpfs unter
// /home/root/.ssh, siehe hook.sh). Erlaubt sind ed25519, ECDSA und RSA (das eigene dropbear kann alle drei).
// Der letzte Schlüssel lässt sich nicht löschen: sonst gäbe es keinen SSH-Zugang mehr.

var sshKeyTypes = map[string]bool{
	"ssh-ed25519": true, "ecdsa-sha2-nistp256": true, "ecdsa-sha2-nistp384": true, "ecdsa-sha2-nistp521": true, "ssh-rsa": true,
}

var sshKeysMu sync.Mutex

type sshKey struct {
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Fingerprint string `json:"fingerprint"` // SHA256:... wie ssh-keygen -l
	Options     bool   `json:"options"`     // Zeile hat Optionen (from=..., command=...)
}

func authorizedKeysPath() string { return filepath.Join(dataDir, "authorized_keys") }

// parseSSHKey zerlegt eine Zeile "[Optionen] Typ Base64 [Kommentar]" und prüft, dass der Typ im Schlüssel stimmt.
func parseSSHKey(line string) (sshKey, error) {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return sshKey{}, fmt.Errorf("leer")
	}
	opts := false
	if !sshKeyTypes[f[0]] && !strings.HasPrefix(f[0], "sk-") {
		// Optionen vorne (ohne Leerzeichen in Anführungszeichen ist das das erste Feld)
		opts = true
		f = f[1:]
	}
	if len(f) < 2 || !sshKeyTypes[f[0]] {
		return sshKey{}, fmt.Errorf("unbekannter Schlüsseltyp (erlaubt: ssh-ed25519, ecdsa-sha2-*, ssh-rsa)")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) < 8 {
		return sshKey{}, fmt.Errorf("Schlüssel ist kein gültiges Base64")
	}
	n := binary.BigEndian.Uint32(blob)
	if int(n)+4 > len(blob) || string(blob[4:4+n]) != f[0] {
		return sshKey{}, fmt.Errorf("Schlüsseltyp passt nicht zum Inhalt")
	}
	if f[0] == "ssh-rsa" && len(blob) < 200 {
		return sshKey{}, fmt.Errorf("RSA-Schlüssel zu kurz (mindestens 2048 Bit)")
	}
	sum := sha256.Sum256(blob)
	return sshKey{Type: f[0], Comment: strings.Join(f[2:], " "), Options: opts,
		Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])}, nil
}

func readKeyLines() ([]string, error) {
	b, err := os.ReadFile(authorizedKeysPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, nil
}

func listSSHKeys() ([]sshKey, error) {
	lines, err := readKeyLines()
	if err != nil {
		return nil, err
	}
	out := []sshKey{}
	for _, l := range lines {
		if k, err := parseSSHKey(l); err == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

func writeKeyLines(lines []string) error {
	p := authorizedKeysPath()
	tmp := p + ".new"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func addSSHKey(line string) (sshKey, error) {
	line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
	if strings.Contains(line, "\n") {
		return sshKey{}, fmt.Errorf("bitte nur einen Schlüssel (eine Zeile)")
	}
	k, err := parseSSHKey(line)
	if err != nil {
		return k, err
	}
	if k.Options {
		return k, fmt.Errorf("Schlüssel mit Optionen bitte über SSH eintragen")
	}
	sshKeysMu.Lock()
	defer sshKeysMu.Unlock()
	lines, err := readKeyLines()
	if err != nil {
		return k, err
	}
	for _, l := range lines {
		if o, err := parseSSHKey(l); err == nil && o.Fingerprint == k.Fingerprint {
			return k, fmt.Errorf("Schlüssel ist schon eingetragen")
		}
	}
	return k, writeKeyLines(append(lines, line))
}

func deleteSSHKey(fp string) error {
	sshKeysMu.Lock()
	defer sshKeysMu.Unlock()
	lines, err := readKeyLines()
	if err != nil {
		return err
	}
	var keep []string
	found, keys := false, 0
	for _, l := range lines {
		k, err := parseSSHKey(l)
		if err == nil && k.Fingerprint == fp {
			found = true
			continue
		}
		if err == nil {
			keys++
		}
		keep = append(keep, l)
	}
	if !found {
		return fmt.Errorf("Schlüssel nicht gefunden")
	}
	if keys == 0 {
		return fmt.Errorf("der letzte Schlüssel bleibt, sonst gibt es keinen SSH-Zugang mehr / the last key stays")
	}
	return writeKeyLines(keep)
}
