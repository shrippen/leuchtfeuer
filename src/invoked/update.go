package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Updates aus der Oberfläche: Die Release-Seite (Gitea) bietet je Version ein Paket leuchtfeuer-<Version>-invoke.tar.gz
// (Aufbau wie /data/invoke) und eine Signatur .sig an. invoked lädt beides, prüft die Ed25519-Signatur gegen den
// Schlüssel UPDATE_PUBKEY aus der Shell-Konfiguration (ohne Schlüssel kein Update aus der Oberfläche), packt nach
// /data/invoke/.stage-web aus (nur Programme, Dienste und Systemdateien, nie Einstellungen oder Schlüssel) und übergibt an
// apply-update.sh: das sichert den alten Stand, setzt den neuen in Kraft, startet die Dienste neu und nimmt das Update
// zurück, wenn danach ein Dienst wiederholt ausfällt. Signiert wird mit tools/make-release.sh (src/relsign).
//
// Signatur: Ed25519 über die Zeichenkette "leuchtfeuer-release:" + SHA-256 (hex) des Pakets, Base64.

type UpdateSettings struct {
	URL string `json:"url"` // Release-API (Gitea .../releases/latest); leer = Standard
}

const defaultUpdateURL = "https://git.arianw.de/api/v1/repos/shrippen/leuchtfeuer/releases/latest"

var invokeDir = "/data/invoke"

type updateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
	Available  bool   `json:"available"`
	KeySet     bool   `json:"keySet"`
	Busy       bool   `json:"busy"`
	Message    string `json:"message"`
	Pending    bool   `json:"pending"`    // läuft gerade die Beobachtung nach einem Update
	RolledBack string `json:"rolledBack"` // Grund des letzten Rückfalls ("" = keiner)
	asset      string
	sig        string
}

var upd struct {
	mu   sync.Mutex
	busy bool
	msg  string
}

func currentVersion() string {
	if b, err := os.ReadFile(filepath.Join(invokeDir, "VERSION")); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	return version
}

func releaseDigest(sum []byte) []byte {
	return []byte("leuchtfeuer-release:" + hex.EncodeToString(sum))
}

// verifyRelease prüft die Signatur eines Pakets (sig: Base64) mit dem Schlüssel (Base64, 32 Byte).
func verifyRelease(pkg io.Reader, sig, pubkey string) error {
	pk, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubkey))
	if err != nil || len(pk) != ed25519.PublicKeySize {
		return fmt.Errorf("UPDATE_PUBKEY ist kein Ed25519-Schlüssel")
	}
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return fmt.Errorf("Signatur unlesbar")
	}
	h := sha256.New()
	if _, err := io.Copy(h, pkg); err != nil {
		return err
	}
	if !ed25519.Verify(pk, releaseDigest(h.Sum(nil)), s) {
		return fmt.Errorf("Signatur passt nicht: Paket verändert oder falscher Schlüssel")
	}
	return nil
}

// allowedUpdatePath: welche Dateien ein Update ersetzen darf.
func allowedUpdatePath(p string) bool {
	p = path.Clean(strings.TrimPrefix(p, "./"))
	if p == "." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || strings.Contains(p, "/../") {
		return false
	}
	switch p {
	case "boot.sh", "hook.sh", "apply-update.sh", "asound-music.conf", "podium.conf", "ca-certificates.crt",
		"dropbearmulti", "VERSION", ".remove":
		return true
	}
	for _, pre := range []string{"bin/", "lib/", "services/", "bluez/bin/", "bluez/lib/", "bluez/libexec/"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// extractUpdate packt das Paket nach dir aus (nur erlaubte Pfade, nur Dateien und Verzeichnisse).
func extractUpdate(r io.Reader, dir string) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return n, fmt.Errorf("unerwarteter Eintrag %s", h.Name)
		}
		if !allowedUpdatePath(h.Name) {
			return n, fmt.Errorf("Paket enthält unzulässige Datei %s", h.Name)
		}
		dst := filepath.Join(dir, path.Clean(strings.TrimPrefix(h.Name, "./")))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o600)
		if err != nil {
			return n, err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 200<<20))
		f.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	if _, err := os.Stat(filepath.Join(dir, "VERSION")); err != nil {
		return n, fmt.Errorf("Paket ohne VERSION")
	}
	return n, nil
}

func updateURL(a *app) string {
	if u := a.st.Snapshot().Update.URL; u != "" {
		return u
	}
	return defaultUpdateURL
}

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// checkUpdate fragt die Release-Seite nach der neuesten Version.
func (a *app) checkUpdate() (updateInfo, error) {
	info := a.updateStatus()
	req, _ := http.NewRequest("GET", updateURL(a), nil)
	req.Header.Set("Accept", "application/json")
	c := &http.Client{Timeout: 20 * time.Second}
	res, err := c.Do(req)
	if err != nil {
		return info, fmt.Errorf("Release-Seite nicht erreichbar: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return info, fmt.Errorf("Release-Seite: %s", res.Status)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Name   string `json:"name"`
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&rel); err != nil {
		return info, err
	}
	info.Latest, info.Name, info.Notes = rel.Tag, rel.Name, rel.Body
	for _, as := range rel.Assets {
		switch {
		case strings.HasSuffix(as.Name, "-invoke.tar.gz"):
			info.asset = as.URL
		case strings.HasSuffix(as.Name, "-invoke.tar.gz.sig"):
			info.sig = as.URL
		}
	}
	info.Available = rel.Tag != "" && rel.Tag != info.Current && info.asset != "" && info.sig != ""
	return info, nil
}

func (a *app) updateStatus() updateInfo {
	upd.mu.Lock()
	info := updateInfo{Current: currentVersion(), Busy: upd.busy, Message: upd.msg}
	upd.mu.Unlock()
	info.KeySet = a.cfg.Get("UPDATE_PUBKEY", "") != ""
	_, err := os.Stat(filepath.Join(invokeDir, "update-pending"))
	info.Pending = err == nil
	if b, err := os.ReadFile(filepath.Join(invokeDir, "update-rolledback")); err == nil {
		info.RolledBack = strings.TrimSpace(string(b))
		if info.RolledBack == "" {
			info.RolledBack = "?"
		}
	}
	return info
}

func setUpdateMsg(busy bool, msg string) {
	upd.mu.Lock()
	upd.busy, upd.msg = busy, msg
	upd.mu.Unlock()
}

// startUpdate lädt die neueste Version und setzt sie in Kraft (im Hintergrund).
func (a *app) startUpdate() error {
	key := a.cfg.Get("UPDATE_PUBKEY", "")
	if key == "" {
		return fmt.Errorf("kein UPDATE_PUBKEY gesetzt: Updates nur mit install.sh")
	}
	upd.mu.Lock()
	if upd.busy {
		upd.mu.Unlock()
		return fmt.Errorf("Update läuft bereits")
	}
	upd.busy = true
	upd.mu.Unlock()
	go func() {
		info, err := a.checkUpdate()
		if err == nil && !info.Available {
			err = fmt.Errorf("keine neuere Version")
		}
		var pkg string
		if err == nil {
			setUpdateMsg(true, "lade "+info.Latest)
			pkg, err = download(info.asset, filepath.Join(invokeDir, ".update.tar.gz"))
		}
		var sig string
		if err == nil {
			var b string
			b, err = download(info.sig, filepath.Join(invokeDir, ".update.sig"))
			if err == nil {
				s, _ := os.ReadFile(b)
				sig = string(s)
				os.Remove(b)
			}
		}
		if err == nil {
			err = a.installPackage(pkg, sig, key)
		}
		if pkg != "" {
			os.Remove(pkg)
		}
		if err != nil {
			logf("Update: %v", err)
			setUpdateMsg(false, "Fehler: "+err.Error())
		}
	}()
	return nil
}

func download(url, dst string) (string, error) {
	res, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", url, res.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(res.Body, 300<<20))
	f.Close()
	if err != nil {
		os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// installPackage prüft, packt aus und übergibt an apply-update.sh (das invoked selbst neu startet).
func (a *app) installPackage(pkg, sig, key string) error {
	setUpdateMsg(true, "prüfe Signatur")
	f, err := os.Open(pkg)
	if err != nil {
		return err
	}
	err = verifyRelease(f, sig, key)
	f.Close()
	if err != nil {
		return err
	}
	st, _ := os.Stat(pkg)
	var fs syscall.Statfs_t
	if st != nil && syscall.Statfs(invokeDir, &fs) == nil {
		if free := int64(fs.Bavail) * int64(fs.Bsize); free < st.Size()*4 {
			return fmt.Errorf("zu wenig Platz auf /data (%d MB frei)", free>>20)
		}
	}
	stage := filepath.Join(invokeDir, ".stage-web")
	os.RemoveAll(stage)
	setUpdateMsg(true, "packe aus")
	f, err = os.Open(pkg)
	if err != nil {
		return err
	}
	n, err := extractUpdate(f, stage)
	f.Close()
	if err != nil {
		os.RemoveAll(stage)
		return err
	}
	script := filepath.Join(stage, "apply-update.sh")
	if _, err := os.Stat(script); err != nil {
		script = filepath.Join(invokeDir, "apply-update.sh")
	}
	setUpdateMsg(true, fmt.Sprintf("setze %d Dateien in Kraft, Dienste starten neu", n))
	logf("Update: %d Dateien, übergebe an %s", n, script)
	cmd := exec.Command("sh", script, "apply", stage)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Start() // beendet am Ende auch dieses invoked; der Hook startet das neue
}

// uploadUpdate: Paket und Signatur von Hand hochgeladen (ohne Internetzugang des Lautsprechers).
func (a *app) uploadUpdate(r *http.Request) error {
	key := a.cfg.Get("UPDATE_PUBKEY", "")
	if key == "" {
		return fmt.Errorf("kein UPDATE_PUBKEY gesetzt: Updates nur mit install.sh")
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return err
	}
	sig := r.FormValue("sig")
	file, _, err := r.FormFile("file")
	if err != nil {
		return fmt.Errorf("Paket fehlt")
	}
	defer file.Close()
	dst := filepath.Join(invokeDir, ".update.tar.gz")
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(file, 300<<20))
	f.Close()
	if err != nil {
		return err
	}
	defer os.Remove(dst)
	if err := a.installPackage(dst, sig, key); err != nil {
		setUpdateMsg(false, "Fehler: "+err.Error())
		return err
	}
	return nil
}

// rollbackUpdate nimmt das letzte Update von Hand zurück.
func rollbackUpdate() error {
	if _, err := os.Stat(filepath.Join(invokeDir, ".prev")); err != nil {
		return fmt.Errorf("kein voriger Stand vorhanden")
	}
	cmd := exec.Command("sh", filepath.Join(invokeDir, "apply-update.sh"), "rollback", "von Hand")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
