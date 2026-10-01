package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

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

func collectSys() sysStatus {
	var s sysStatus
	// tsen_temp liefert °C (ganzzahlig)
	s.TempC = readFloat(hw.TempPath)
	s.UptimeSecs = int(readFloat("/proc/uptime"))
	s.Load1 = readFloat("/proc/loadavg")
	s.MemTotalMB, s.MemFreeMB = memInfo()
	var st syscall.Statfs_t
	if syscall.Statfs("/data", &st) == nil {
		s.DataFreeMB = int(uint64(st.Bavail) * uint64(st.Bsize) / 1024 / 1024)
		s.DataSizeMB = int(uint64(st.Blocks) * uint64(st.Bsize) / 1024 / 1024)
	}
	return s
}

// cachedSys hält die Messwerte eine Weile: sie werden für jede Statusmeldung gebraucht.
func cachedSys(f func() sysStatus, d time.Duration) func() sysStatus {
	var mu sync.Mutex
	var at time.Time
	var last sysStatus
	return func() sysStatus {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > d {
			last, at = f(), time.Now()
		}
		return last
	}
}

func uptimeSince(t time.Time) int { return int(time.Since(t).Seconds()) }
