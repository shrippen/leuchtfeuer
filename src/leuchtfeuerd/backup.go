package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Sichern und Wiederherstellen der Einstellungen, und ein Diagnosepaket für Fehlerberichte.
//
// Sicherung (tar.gz): Shell-Konfiguration, Einstellungen von leuchtfeuerd, SSH-Schlüssel (erlaubte Schlüssel und die
// Host-Schlüssel, damit der Fingerabdruck gleich bleibt), Bluetooth-Kopplungen, Spotify-Anmeldung. Sie enthält damit
// Geheimnisse (Passwort-Hash, MQTT-Passwort, Schlüssel): sicher aufbewahren.
// Wiederherstellen schreibt nur diese Dateien zurück und startet danach die Dienste neu.
// Diagnose (tar.gz): Status, Einstellungen ohne Geheimnisse, Protokolle (je die letzten 256 KiB), dmesg, Firewall,
// Prozesse, freier Speicher. Zum Anhängen an ein Issue.

var backupFiles = []string{"config", "leuchtfeuerd.json", "authorized_keys", "host_ed25519", "host_ecdsa", "sessions.json", "tokens.json"}
var backupDirs = []string{"bluez/var", "librespot-cache"}

func backupAllowed(p string) bool {
	p = path.Clean(strings.TrimPrefix(p, "./"))
	if strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, f := range backupFiles {
		if p == f {
			return true
		}
	}
	for _, d := range backupDirs {
		if strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

func tarFile(tw *tar.Writer, name string, data []byte, mode int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func fileName(prefix, name string) string {
	n := regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(name, "-")
	return fmt.Sprintf("%s-%s-%s.tar.gz", prefix, strings.Trim(n, "-"), time.Now().Format("2006-01-02"))
}

// writeBackup schreibt die Sicherung als tar.gz.
func writeBackup(w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, f := range backupFiles {
		if b, err := os.ReadFile(filepath.Join(dataDir, f)); err == nil {
			if err := tarFile(tw, f, b, 0o600); err != nil {
				return err
			}
		}
	}
	for _, d := range backupDirs {
		root := filepath.Join(dataDir, d)
		filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !e.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(dataDir, p)
			if b, err := os.ReadFile(p); err == nil && len(b) < 4<<20 {
				tarFile(tw, filepath.ToSlash(rel), b, 0o600)
			}
			return nil
		})
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// restoreBackup spielt eine Sicherung zurück (nur bekannte Dateien) und startet die Dienste neu.
func restoreBackup(r io.Reader) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("keine Sicherung (tar.gz): %v", err)
	}
	tr := tar.NewReader(gz)
	type entry struct {
		name string
		data []byte
	}
	var files []entry
	total := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		name := h.Name
		if path.Clean(strings.TrimPrefix(name, "./")) == "invoked.json" { // Sicherung von vor der Umbenennung (Oktober 2026)
			name = "leuchtfeuerd.json"
		}
		if h.Typeflag != tar.TypeReg || !backupAllowed(name) {
			return 0, fmt.Errorf("unerwartete Datei in der Sicherung: %s", h.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, 8<<20))
		if err != nil {
			return 0, err
		}
		if total += len(b); total > 40<<20 {
			return 0, fmt.Errorf("Sicherung zu groß")
		}
		files = append(files, entry{path.Clean(strings.TrimPrefix(name, "./")), b})
	}
	hasCfg := false
	for _, f := range files {
		hasCfg = hasCfg || f.name == "config"
	}
	if !hasCfg {
		return 0, fmt.Errorf("Sicherung ohne config")
	}
	// erst alles prüfen, dann schreiben (je Datei atomar)
	for _, f := range files {
		dst := filepath.Join(dataDir, filepath.FromSlash(f.name))
		os.MkdirAll(filepath.Dir(dst), 0o700)
		tmp := dst + ".restore"
		if err := os.WriteFile(tmp, f.data, 0o600); err != nil {
			return 0, err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// restartAllServices beendet alle Dienste (der Hook startet sie neu, ohne Ausfall zu zählen), zuletzt leuchtfeuerd selbst.
func restartAllServices() {
	self := 0
	for _, d := range serviceDefs() {
		pid := readPid(d.Name)
		if !pidAlive(pid) {
			continue
		}
		os.WriteFile(filepath.Join(runDir, "leuchtfeuer-svc-"+d.Name+".expected"), nil, 0o644)
		if d.Name == "leuchtfeuerd" {
			self = pid
			continue
		}
		syscall.Kill(pid, syscall.SIGTERM)
	}
	if self > 0 {
		go func() { time.Sleep(time.Second); syscall.Kill(self, syscall.SIGTERM) }()
	}
}

var secretKeys = regexp.MustCompile(`(?m)^((?:WEB_PASSWORD_HASH|WEB_PASSWORD|UPDATE_PUBKEY|[A-Z_]*(?:PASS|SECRET|TOKEN|KEY)[A-Z_]*)=).*$`)

// writeDiag schreibt das Diagnosepaket.
func (w *webServer) writeDiag(out io.Writer) error {
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	st, _ := json.MarshalIndent(w.status(), "", "  ")
	tarFile(tw, "status.json", st, 0o644)
	set := w.app.st.Redacted()
	sb, _ := json.MarshalIndent(set, "", "  ")
	tarFile(tw, "leuchtfeuerd.json", sb, 0o644)
	if b, err := os.ReadFile(w.app.cfg.path); err == nil {
		tarFile(tw, "config", secretKeys.ReplaceAll(b, []byte("${1}\"(entfernt)\"")), 0o644)
	}
	tarFile(tw, "VERSION", []byte(currentVersion()+"\n"), 0o644)
	logs, _ := filepath.Glob(filepath.Join(logDir, "*.log*"))
	logs = append(logs, hookLog)
	for _, l := range logs {
		if b, err := readTail(l, 256<<10); err == nil {
			tarFile(tw, "log/"+filepath.Base(l), b, 0o644)
		}
	}
	for name, cmd := range map[string][]string{
		"dmesg.txt": {"dmesg"}, "iptables.txt": {"iptables", "-S"}, "ps.txt": {"ps"}, "df.txt": {"df"},
		"amixer.txt": {"amixer", "-c", "0", "contents"}, "free.txt": {"free"},
	} {
		if b, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err == nil || len(b) > 0 {
			tarFile(tw, "system/"+name, b, 0o644)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func sendTarGz(rw http.ResponseWriter, name string, f func(io.Writer) error) {
	rw.Header().Set("Content-Type", "application/gzip")
	rw.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	rw.Header().Set("Cache-Control", "no-store")
	if err := f(rw); err != nil {
		logf("Paket %s: %v", name, err)
	}
}
