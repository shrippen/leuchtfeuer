package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// /metrics im Textformat von Prometheus (Version 0.0.4). Zugriff mit Sitzung oder API-Schlüssel (Umfang "read"
// genügt), z. B. in prometheus.yml: authorization: { credentials: lf_... }.

type metricWriter struct {
	w    io.Writer
	seen map[string]bool
}

func (m *metricWriter) head(name, typ, help string) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true
	fmt.Fprintf(m.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func (m *metricWriter) gauge(name, help string, v float64, labels ...string) {
	m.head(name, "gauge", help)
	m.sample(name, v, labels...)
}

func (m *metricWriter) counter(name, help string, v float64, labels ...string) {
	m.head(name, "counter", help)
	m.sample(name, v, labels...)
}

func (m *metricWriter) sample(name string, v float64, labels ...string) {
	var l []string
	for i := 0; i+1 < len(labels); i += 2 {
		l = append(l, fmt.Sprintf("%s=%q", labels[i], labels[i+1]))
	}
	ls := ""
	if len(l) > 0 {
		ls = "{" + strings.Join(l, ",") + "}"
	}
	fmt.Fprintf(m.w, "%s%s %s\n", name, ls, strconv.FormatFloat(v, 'g', -1, 64))
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// cpuSeconds liest die CPU-Zeiten aus /proc/stat (Ticks zu 1/100 s).
func cpuSeconds() map[string]float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 8 || f[0] != "cpu" {
			continue
		}
		out := map[string]float64{}
		for i, mode := range []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq"} {
			v, _ := strconv.ParseFloat(f[i+1], 64)
			out[mode] = v / 100
		}
		return out
	}
	return nil
}

func (w *webServer) writeMetrics(out io.Writer) {
	a := w.app
	m := &metricWriter{w: out, seen: map[string]bool{}}
	s := w.status()
	m.gauge("leuchtfeuer_info", "Version und Name", 1, "version", s.Version, "name", s.Name)
	m.gauge("leuchtfeuer_uptime_seconds", "Laufzeit des Geräts", float64(s.Sys.UptimeSecs))
	m.gauge("leuchtfeuer_temperature_celsius", "SoC-Temperatur", s.Sys.TempC)
	m.gauge("leuchtfeuer_load1", "Last (1 Minute)", s.Sys.Load1)
	m.gauge("leuchtfeuer_memory_total_bytes", "Arbeitsspeicher gesamt", float64(s.Sys.MemTotalMB)*1024*1024)
	m.gauge("leuchtfeuer_memory_available_bytes", "Arbeitsspeicher frei (mit Puffern)", float64(s.Sys.MemFreeMB)*1024*1024)
	m.gauge("leuchtfeuer_data_free_bytes", "Freier Speicher auf /data", float64(s.Sys.DataFreeMB)*1024*1024)
	cpu := cpuSeconds()
	modes := make([]string, 0, len(cpu))
	for k := range cpu {
		modes = append(modes, k)
	}
	sort.Strings(modes)
	for _, k := range modes {
		m.counter("leuchtfeuer_cpu_seconds_total", "CPU-Zeit je Art (alle Kerne)", cpu[k], "mode", k)
	}
	m.gauge("leuchtfeuer_wifi_rssi_dbm", "WLAN-Signal", float64(s.Wifi.RSSI))
	m.gauge("leuchtfeuer_wifi_loss_percent", "Paketverlust zum Router", float64(s.Wifi.LossPct))
	m.gauge("leuchtfeuer_wifi_rtt_seconds", "Laufzeit zum Router", s.Wifi.RttMs/1000)
	m.gauge("leuchtfeuer_wifi_good", "WLAN-Verbindung gut", b2f(s.Wifi.Good))
	m.gauge("leuchtfeuer_volume_percent", "Lautstärke", float64(s.Volume))
	m.gauge("leuchtfeuer_muted", "Stumm", b2f(s.Muted))
	m.gauge("leuchtfeuer_wamp_connected", "Verbindung zu audio-ui", b2f(s.WampOK))
	m.gauge("leuchtfeuer_mqtt_connected", "Verbindung zum MQTT-Broker", b2f(s.MQTT))
	if s.Clock.Checked.Unix() > 0 {
		m.gauge("leuchtfeuer_clock_offset_seconds", "Abweichung der Uhr (Server minus Gerät)", float64(s.Clock.OffsetMs)/1000)
	}
	for _, v := range s.Sys.Services {
		m.gauge("leuchtfeuer_service_enabled", "Dienst eingeschaltet", b2f(v.Enabled), "service", v.Name)
		m.gauge("leuchtfeuer_service_up", "Dienst läuft", b2f(v.Running), "service", v.Name)
		m.counter("leuchtfeuer_service_restarts_total", "Neustarts seit dem Gerätestart", float64(v.Restarts), "service", v.Name)
		m.gauge("leuchtfeuer_service_failures", "Ausfälle kurz nach dem Start in Folge", float64(v.Fails), "service", v.Name)
	}
	playing := map[string]bool{}
	for _, x := range s.Sources {
		playing[x.Name] = x.State == "playing"
	}
	for _, n := range append(append([]string{}, sourceNames...), "alarm", "announce") {
		m.gauge("leuchtfeuer_source_playing", "Quelle spielt", b2f(playing[n]), "source", n)
	}
	if a.logs != nil {
		xr, ln := a.logs.Counters()
		names := map[string]bool{}
		for k := range ln {
			names[k] = true
		}
		var keys []string
		for k := range names {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m.counter("leuchtfeuer_audio_underruns_total", "Tonaussetzer laut Protokoll (seit Start von leuchtfeuerd)", float64(xr[k]), "service", k)
			m.counter("leuchtfeuer_log_lines_total", "Protokollzeilen (seit Start von leuchtfeuerd)", float64(ln[k]), "service", k)
		}
	}
	if s.NextAlarm != "" {
		if t, err := parseRFC3339(s.NextAlarm); err == nil {
			m.gauge("leuchtfeuer_next_alarm_timestamp_seconds", "Nächster Wecker (Unixzeit)", float64(t.Unix()))
		}
	}
	m.gauge("leuchtfeuer_sleep_timer_seconds", "Restzeit des Schlummertimers", float64(s.SleepSecs))
}

func parseRFC3339(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

func (w *webServer) metrics(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	w.writeMetrics(rw)
}
