package main

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WLAN-Wächter: Der Invoke hängt manchmal an einem Access Point, an dem die Verbindung schlecht ist (hoher
// Paketverlust, lange Laufzeiten), obwohl die Signalstärke ähnlich aussieht. Der Wächter misst deshalb die
// tatsächliche Qualität zum Router und wechselt bei anhaltend schlechten Werten zu einem anderen Access Point
// desselben Netzes (wpa_cli roam). Als schlecht erkannte Access Points meidet er für eine Zeit.

type wifiStatus struct {
	SSID       string    `json:"ssid"`
	BSSID      string    `json:"bssid"`
	FreqMHz    int       `json:"freq"`
	RSSI       int       `json:"rssi"`
	LinkMbps   int       `json:"linkMbps"`
	State      string    `json:"state"`
	Gateway    string    `json:"gateway"`
	LossPct    int       `json:"lossPct"`
	RttMs      float64   `json:"rttMs"`
	Good       bool      `json:"good"`
	LastCheck  time.Time `json:"lastCheck"`
	LastAction string    `json:"lastAction"`
	Log        []string  `json:"log"`
}

type wifiWatch struct {
	app    *app
	mu     sync.Mutex
	S      wifiStatus
	bad    map[string]time.Time // BSSID -> bis wann gemieden
	badCnt int
	idle   int
}

func newWifiWatch(a *app) *wifiWatch { return &wifiWatch{app: a, bad: map[string]time.Time{}} }

func (w *wifiWatch) Status() wifiStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.S
	s.Log = append([]string{}, w.S.Log...) // nie null
	return s
}

func (w *wifiWatch) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("WLAN: %s", msg)
	w.mu.Lock()
	w.S.LastAction = msg
	w.S.Log = append(w.S.Log, time.Now().Format("15:04:05 ")+msg)
	if len(w.S.Log) > 25 {
		w.S.Log = w.S.Log[len(w.S.Log)-25:]
	}
	w.mu.Unlock()
}

func wpa(args ...string) string {
	out, _ := exec.Command("wpa_cli", append([]string{"-i", hw.WifiIface}, args...)...).Output()
	return string(out)
}

func kv(s string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		if i := strings.IndexByte(l, '='); i > 0 {
			m[l[:i]] = strings.TrimSpace(l[i+1:])
		}
	}
	return m
}

func gateway() string {
	out, _ := exec.Command("ip", "route").Output()
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 3 && f[0] == "default" && f[1] == "via" {
			return f[2]
		}
	}
	return ""
}

var (
	lossRe = regexp.MustCompile(`(\d+)% packet loss`)
	rttRe  = regexp.MustCompile(`= [0-9.]+/([0-9.]+)/`)
)

// measure pingt den Router und liefert Paketverlust in % und mittlere Laufzeit in ms.
func measure(gw string) (loss int, rtt float64) {
	if gw == "" {
		return 100, 0
	}
	out, _ := exec.Command("ping", "-c", "5", "-W", "1", "-q", gw).CombinedOutput()
	s := string(out)
	loss = 100
	if m := lossRe.FindStringSubmatch(s); m != nil {
		loss, _ = strconv.Atoi(m[1])
	}
	if m := rttRe.FindStringSubmatch(s); m != nil {
		rtt, _ = strconv.ParseFloat(m[1], 64)
	}
	return
}

func (w *wifiWatch) refresh() (cur map[string]string, gw string) {
	cur = kv(wpa("status"))
	sp := kv(wpa("signal_poll"))
	gw = gateway()
	rssi, _ := strconv.Atoi(sp["RSSI"])
	link, _ := strconv.Atoi(sp["LINKSPEED"])
	freq, _ := strconv.Atoi(cur["freq"])
	w.mu.Lock()
	w.S.SSID, w.S.BSSID, w.S.FreqMHz, w.S.RSSI, w.S.LinkMbps, w.S.State, w.S.Gateway =
		cur["ssid"], cur["bssid"], freq, rssi, link, cur["wpa_state"], gw
	w.mu.Unlock()
	return
}

func (w *wifiWatch) Run() {
	for {
		set := w.app.st.Snapshot().Wifi
		iv := set.IntervalSec
		if iv < 10 {
			iv = 10
		}
		if set.Enabled {
			w.check(set)
		}
		time.Sleep(time.Duration(iv) * time.Second)
	}
}

func (w *wifiWatch) check(set WifiSettings) {
	cur, gw := w.refresh()
	if cur["wpa_state"] != "COMPLETED" {
		w.idle++
		if w.idle >= 3 {
			w.note("nicht verbunden (%s), versuche erneut zu verbinden", cur["wpa_state"])
			if !set.DryRun {
				wpa("reassociate")
			}
			w.idle = 0
		}
		return
	}
	w.idle = 0
	loss, rtt := measure(gw)
	good := loss < set.LossPct && (rtt == 0 || rtt < float64(set.RttMs))
	w.mu.Lock()
	w.S.LossPct, w.S.RttMs, w.S.Good, w.S.LastCheck = loss, rtt, good, time.Now()
	w.mu.Unlock()
	if good {
		w.badCnt = 0
		return
	}
	w.badCnt++
	log.Printf("WLAN: schlecht (Verlust %d %%, %.0f ms, %s, %d dBm), Messung %d/2", loss, rtt, cur["bssid"], w.S.RSSI, w.badCnt)
	if w.badCnt < 2 {
		return
	}
	w.badCnt = 0
	w.roam(set, cur, gw)
}

type cand struct {
	bssid string
	freq  int
	rssi  int
}

func (w *wifiWatch) candidates(ssid, current string, set WifiSettings) []cand {
	wpa("scan")
	time.Sleep(4 * time.Second)
	var out []cand
	for i, l := range strings.Split(wpa("scan_results"), "\n") {
		f := strings.Split(l, "\t")
		if i == 0 || len(f) < 5 || f[4] != ssid || f[0] == current {
			continue
		}
		fr, _ := strconv.Atoi(f[1])
		rs, _ := strconv.Atoi(f[2])
		w.mu.Lock()
		until := w.bad[f[0]]
		w.mu.Unlock()
		if time.Now().Before(until) {
			continue
		}
		out = append(out, cand{f[0], fr, rs})
	}
	sort.Slice(out, func(i, j int) bool {
		if set.Prefer5GHz && (out[i].freq > 4000) != (out[j].freq > 4000) {
			return out[i].freq > 4000
		}
		return out[i].rssi > out[j].rssi
	})
	return out
}

func (w *wifiWatch) roam(set WifiSettings, cur map[string]string, gw string) {
	pen := time.Duration(set.PenaltyMins) * time.Minute
	w.mu.Lock()
	w.bad[cur["bssid"]] = time.Now().Add(pen)
	w.mu.Unlock()
	cs := w.candidates(cur["ssid"], cur["bssid"], set)
	if len(cs) == 0 {
		w.note("Verbindung schlecht an %s, aber kein anderer Access Point verfügbar", cur["bssid"])
		if !set.DryRun {
			wpa("reassociate")
		}
		return
	}
	for i, c := range cs {
		if i >= 2 {
			break
		}
		w.note("Verbindung schlecht an %s: wechsle zu %s (%d MHz, %d dBm)%s", cur["bssid"], c.bssid, c.freq, c.rssi,
			map[bool]string{true: " [Probelauf]", false: ""}[set.DryRun])
		if set.DryRun {
			return
		}
		wpa("roam", c.bssid)
		time.Sleep(10 * time.Second)
		w.refresh()
		loss, rtt := measure(gateway())
		if loss < set.LossPct && (rtt == 0 || rtt < float64(set.RttMs)) {
			w.note("Wechsel erfolgreich: %s (Verlust %d %%, %.0f ms)", c.bssid, loss, rtt)
			return
		}
		w.note("%s ist auch schlecht (Verlust %d %%, %.0f ms)", c.bssid, loss, rtt)
		w.mu.Lock()
		w.bad[c.bssid] = time.Now().Add(pen)
		w.mu.Unlock()
	}
}

type apInfo struct {
	BSSID   string `json:"bssid"`
	FreqMHz int    `json:"freq"`
	RSSI    int    `json:"rssi"`
	Current bool   `json:"current"`
	Avoided int    `json:"avoidedMins"` // so lange meidet der Wächter diesen Access Point noch
}

// Scan listet alle Access Points des eigenen Netzes (gleiche SSID).
func (w *wifiWatch) Scan() []apInfo {
	cur := kv(wpa("status"))
	wpa("scan")
	time.Sleep(4 * time.Second)
	out := []apInfo{}
	for i, l := range strings.Split(wpa("scan_results"), "\n") {
		f := strings.Split(l, "\t")
		if i == 0 || len(f) < 5 || f[4] != cur["ssid"] {
			continue
		}
		fr, _ := strconv.Atoi(f[1])
		rs, _ := strconv.Atoi(f[2])
		a := apInfo{BSSID: f[0], FreqMHz: fr, RSSI: rs, Current: f[0] == cur["bssid"]}
		w.mu.Lock()
		if until := w.bad[f[0]]; time.Now().Before(until) {
			a.Avoided = int(time.Until(until).Minutes()) + 1
		}
		w.mu.Unlock()
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RSSI > out[j].RSSI })
	return out
}

// RoamTo wechselt von Hand zu einem Access Point.
func (w *wifiWatch) RoamTo(bssid string) error {
	if !regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`).MatchString(bssid) {
		return fmt.Errorf("ungültige BSSID")
	}
	w.note("Wechsel von Hand zu %s", bssid)
	out := strings.TrimSpace(wpa("roam", bssid))
	if !strings.HasPrefix(out, "OK") {
		return fmt.Errorf("wpa_cli: %s", out)
	}
	return nil
}
