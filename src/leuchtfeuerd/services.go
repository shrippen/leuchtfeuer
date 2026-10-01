package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Dienste: Die Kopfzeilen der Dienstskripte (/data/leuchtfeuer/services/<name>.sh) sind die eine Quelle für Hook und
// leuchtfeuerd: "# title:", "# group:", "# process:", "# ports:", "# default:". Eine Gruppe (spotify, bluetooth, tidal ...)
// wird mit SERVICE_<GRUPPE>="on|off" in der Shell-Konfiguration geschaltet; der Hook startet nur eingeschaltete
// Dienste und öffnet nur deren Ports. Zustand je Dienst schreibt der Hook nach /run/leuchtfeuer-svc-<name>.state.

var (
	servicesDir = "/data/leuchtfeuer/services"
	runDir      = "/run"
	logDir      = "/data/leuchtfeuer/log"
	hookLog     = "/data/leuchtfeuer/hook.log"
)

type serviceDef struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Group   string `json:"group"`
	Process string `json:"process"`
	Ports   string `json:"ports"`
	Default string `json:"default"`
}

type serviceInfo struct {
	serviceDef
	Running  bool `json:"running"`
	Enabled  bool `json:"enabled"`
	Fails    int  `json:"fails"`    // Ausfälle kurz nach dem Start in Folge
	Restarts int  `json:"restarts"` // Neustarts seit dem Gerätestart
	Waiting  int  `json:"waitSecs"` // Pause bis zum nächsten Startversuch
	Failing  bool `json:"failing"`  // fällt wiederholt aus
}

func parseServiceHeader(name string, r *bufio.Scanner) serviceDef {
	d := serviceDef{Name: name, Title: name, Default: "on"}
	for i := 0; i < 12 && r.Scan(); i++ {
		l := r.Text()
		if !strings.HasPrefix(l, "# ") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(l, "# "), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "title":
			d.Title = v
		case "group":
			d.Group = v
		case "process":
			d.Process = v
		case "ports":
			d.Ports = v
		case "default":
			d.Default = v
		}
	}
	return d
}

// serviceDefs liest alle ausführbaren Dienstskripte.
func serviceDefs() []serviceDef {
	files, _ := filepath.Glob(filepath.Join(servicesDir, "*.sh"))
	sort.Strings(files)
	var out []serviceDef
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.Mode()&0o111 == 0 {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		out = append(out, parseServiceHeader(strings.TrimSuffix(filepath.Base(f), ".sh"), bufio.NewScanner(fh)))
		fh.Close()
	}
	return out
}

func serviceByName(name string) (serviceDef, bool) {
	for _, d := range serviceDefs() {
		if d.Name == name {
			return d, true
		}
	}
	return serviceDef{}, false
}

// groupKey: Name des Schalters in der Shell-Konfiguration.
func groupKey(g string) string {
	return "SERVICE_" + strings.ToUpper(strings.ReplaceAll(g, "-", "_"))
}

// groupEnabled wertet den Schalter einer Gruppe aus (wie svc_on im Hook).
func groupEnabled(cfg *shellConfig, d serviceDef) bool {
	if d.Group == "" || d.Group == "core" {
		return true
	}
	v := cfg.Get(groupKey(d.Group), "")
	if v == "" && d.Group == "airplay" {
		v = cfg.Get("AIRPLAY", "")
	}
	if v == "" {
		v = d.Default
	}
	return v != "off"
}

func pidAlive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func readPid(name string) int {
	b, err := os.ReadFile(filepath.Join(runDir, "leuchtfeuer-svc-"+name+".pid"))
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return p
}

func serviceStatus(cfg *shellConfig, now int64) []serviceInfo {
	var out []serviceInfo
	for _, d := range serviceDefs() {
		si := serviceInfo{serviceDef: d, Enabled: groupEnabled(cfg, d), Running: pidAlive(readPid(d.Name))}
		if b, err := os.ReadFile(filepath.Join(runDir, "leuchtfeuer-svc-"+d.Name+".state")); err == nil {
			f := strings.Fields(string(b))
			n := func(i int) int64 {
				if i < len(f) {
					v, _ := strconv.ParseInt(f[i], 10, 64)
					return v
				}
				return 0
			}
			si.Fails, si.Restarts = int(n(0)), int(n(3))
			if next := n(1); next > now {
				si.Waiting = int(next - now)
			}
		}
		si.Failing = si.Enabled && si.Fails >= 3
		out = append(out, si)
	}
	return out
}

// serviceGroups: Gruppen mit Titel und Schalterzustand, für die Einstellungen.
type serviceGroup struct {
	Group   string   `json:"group"`
	Enabled bool     `json:"enabled"`
	Members []string `json:"members"`
}

func serviceGroups(cfg *shellConfig) []serviceGroup {
	idx := map[string]int{}
	var out []serviceGroup
	for _, d := range serviceDefs() {
		if d.Group == "" || d.Group == "core" {
			continue
		}
		i, ok := idx[d.Group]
		if !ok {
			i = len(out)
			idx[d.Group] = i
			out = append(out, serviceGroup{Group: d.Group, Enabled: groupEnabled(cfg, d)})
		}
		out[i].Members = append(out[i].Members, d.Name)
	}
	return out
}

func setGroup(cfg *shellConfig, group string, on bool) error {
	found := false
	for _, g := range serviceGroups(cfg) {
		found = found || g.Group == group
	}
	if !found {
		return fmt.Errorf("unbekannte Dienstgruppe %q", group)
	}
	kv := map[string]string{groupKey(group): map[bool]string{true: "on", false: "off"}[on]}
	if group == "airplay" { // alter Schalter folgt mit
		kv["AIRPLAY"] = kv[groupKey(group)]
	}
	return cfg.Set(kv)
}

// restartService beendet den Dienst; der Hook startet ihn innerhalb von 30 s neu (ohne das als Ausfall zu zählen).
func restartService(name string) error {
	if _, ok := serviceByName(name); !ok {
		return fmt.Errorf("unbekannter Dienst")
	}
	pid := readPid(name)
	if !pidAlive(pid) {
		return fmt.Errorf("Dienst läuft nicht (kein PID)")
	}
	os.WriteFile(filepath.Join(runDir, "leuchtfeuer-svc-"+name+".expected"), nil, 0o644)
	return syscall.Kill(pid, syscall.SIGTERM)
}

func logPath(name string) (string, error) {
	if name == "hook" {
		return hookLog, nil
	}
	if _, ok := serviceByName(name); !ok {
		return "", fmt.Errorf("unbekanntes Protokoll")
	}
	return filepath.Join(logDir, name+".log"), nil
}

func tailLog(name string, lines int) (string, error) {
	path, err := logPath(name)
	if err != nil {
		return "", err
	}
	b, err := readTail(path, 256<<10)
	if err != nil {
		return "", err
	}
	l := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(l) > lines {
		l = l[len(l)-lines:]
	}
	// lange dmix-Debugzeilen der ALSA-Bibliothek ausblenden
	var out []string
	for _, x := range l {
		if !strings.HasPrefix(x, "dmix<") && len(x) < 400 {
			out = append(out, x)
		}
	}
	return strings.Join(out, "\n"), nil
}

// readTail liest höchstens die letzten max Bytes einer Datei.
func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := st.Size() - max
	if off < 0 {
		off = 0
	}
	b := make([]byte, st.Size()-off)
	n, err := f.ReadAt(b, off)
	return b[:n], nilIfEOF(err)
}

func nilIfEOF(err error) error {
	if err != nil && err.Error() == "EOF" {
		return nil
	}
	return err
}
