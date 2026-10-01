package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type serviceInfo struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

type sysStatus struct {
	TempC      float64       `json:"tempC"`
	UptimeSecs int           `json:"uptimeSecs"`
	Load1      float64       `json:"load1"`
	MemTotalMB int           `json:"memTotalMB"`
	MemFreeMB  int           `json:"memFreeMB"`
	DataFreeMB int           `json:"dataFreeMB"`
	DataSizeMB int           `json:"dataSizeMB"`
	Services   []serviceInfo `json:"services"`
}

var watched = []struct{ Name, Match string }{
	{"Spotify Connect (librespot)", "librespot"},
	{"UPnP/DLNA (gmrender)", "gmediarender"},
	{"Sendspin", "sendspin-player"},
	{"Cast", "castrecv"},
	{"AirPlay (shairport-sync)", "shairport-sync"},
	{"Tidal Connect", "tidal_connect_a"},
	{"Bluetooth (bluetoothd)", "bluetoothd"},
	{"Bluetooth-Agent (btagent)", "btagent"},
	{"Bluetooth-Audio (bluealsa)", "bluealsa"},
}

func readFloat(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	return f
}

func memInfo() (total, free int) {
	b, _ := os.ReadFile("/proc/meminfo")
	m := map[string]int{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			v, _ := strconv.Atoi(f[1])
			m[strings.TrimSuffix(f[0], ":")] = v
		}
	}
	return m["MemTotal"] / 1024, (m["MemFree"] + m["Buffers"] + m["Cached"]) / 1024
}

// running prüft, ob ein Prozess mit passendem Namen läuft (aus /proc).
func runningSet() map[string]bool {
	set := map[string]bool{}
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		set[strings.TrimSpace(string(b))] = true
	}
	return set
}

func collectSys() sysStatus {
	var s sysStatus
	// tsen_temp liefert °C (ganzzahlig)
	s.TempC = readFloat("/sys/class/hwmon/hwmon0/device/tsen_temp")
	s.UptimeSecs = int(readFloat("/proc/uptime"))
	s.Load1 = readFloat("/proc/loadavg")
	s.MemTotalMB, s.MemFreeMB = memInfo()
	var st syscall.Statfs_t
	if syscall.Statfs("/data", &st) == nil {
		s.DataFreeMB = int(uint64(st.Bavail) * uint64(st.Bsize) / 1024 / 1024)
		s.DataSizeMB = int(uint64(st.Blocks) * uint64(st.Bsize) / 1024 / 1024)
	}
	run := runningSet()
	for _, w := range watched {
		ok := false
		for n := range run {
			if strings.HasPrefix(n, w.Match) {
				ok = true
				break
			}
		}
		s.Services = append(s.Services, serviceInfo{w.Name, ok})
	}
	return s
}

func uptimeSince(t time.Time) int { return int(time.Since(t).Seconds()) }
