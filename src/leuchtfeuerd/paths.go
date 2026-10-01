package main

import (
	"os"
	"path/filepath"
)

// Verzeichnisse: Daten (Programme, Einstellungen, Protokolle) unter LEUCHTFEUER_DIR, Laufzeitdateien (PIDs, Zustände,
// Sockets) unter LEUCHTFEUER_RUN. Der Hook setzt beide für alle Dienste; Standard ist die Anordnung auf dem Invoke.
// Auf einem anderen Linux z. B. /opt/leuchtfeuer und /run/leuchtfeuer (targets/generic).

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	dataDir     = envOr("LEUCHTFEUER_DIR", "/data/leuchtfeuer")
	runDir      = envOr("LEUCHTFEUER_RUN", "/run")
	servicesDir = filepath.Join(dataDir, "services")
	logDir      = filepath.Join(dataDir, "log")
	hookLog     = filepath.Join(dataDir, "hook.log")
	eventsSock  = filepath.Join(runDir, "leuchtfeuer-events.sock")
	busSock     = filepath.Join(runDir, "leuchtfeuer-bus.sock")
	portsFile   = filepath.Join(dataDir, "ports.leuchtfeuerd")
	btStateFile = filepath.Join(runDir, "leuchtfeuer-bt-state.json")
)
