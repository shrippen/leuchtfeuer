package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"leuchtfeuer/lfbus"
)

// Bluetooth-Lautsprecher als Ausgabe (Weboberfläche > Einstellungen > Ausgabe): btagent sucht, koppelt, verbindet und
// trennt sie und hält die Verbindung zu dem Lautsprecher, den leuchtfeuerd als Ausgabe gewählt hat. Den Ton spielt
// bluez-alsa als A2DP-Quelle (ALSA "bluealsa:DEV=..." in output.conf, geschrieben von leuchtfeuerd).
//
//	POST /bt {devices, scanning, busy, error} -> {ok, want}   Zustand melden, gewünschten Lautsprecher erfahren
//	Ereignis bt-sink {action: scan|stop|connect|disconnect|forget, addr}   Befehle der Oberfläche

const (
	uuidA2DPSink = "0000110b-0000-1000-8000-00805f9b34fb"
	scanTime     = 30 * time.Second
	retryEvery   = 20 * time.Second
)

type sinkDev struct {
	Addr      string `json:"addr"`
	Name      string `json:"name"`
	Paired    bool   `json:"paired"`
	Connected bool   `json:"connected"`
	RSSI      int    `json:"rssi,omitempty"`
}

type sinkReport struct {
	Devices  []sinkDev `json:"devices"`
	Scanning bool      `json:"scanning"`
	Busy     string    `json:"busy,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type sinkState struct {
	mu        sync.Mutex
	scanUntil time.Time
	busy      string
	err       string
	want      string
	lastTry   time.Time
	lastJSON  string
	lastPost  time.Time
	devices   []sinkDev
}

var sinks = &sinkState{}

func devPath(addr string) dbus.ObjectPath {
	return dbus.ObjectPath("/org/bluez/" + *adapter + "/dev_" + strings.ReplaceAll(strings.ToUpper(addr), ":", "_"))
}

// sinkDevices wählt aus den BlueZ-Objekten die Lautsprecher: Geräte mit A2DP-Senke oder der Geräteklasse Audio/Video
// (Telefone und Computer fallen heraus, auch das gerade verbundene Handy, das auf uns spielt).
func sinkDevices(objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant) []sinkDev {
	var out []sinkDev
	for _, ifs := range objs {
		d, ok := ifs["org.bluez.Device1"]
		if !ok {
			continue
		}
		isSink := false
		if u, ok := d["UUIDs"].Value().([]string); ok {
			for _, x := range u {
				isSink = isSink || strings.EqualFold(x, uuidA2DPSink)
			}
		}
		if c, ok := d["Class"].Value().(uint32); ok && (c>>8)&0x1f == 4 {
			isSink = true
		}
		if !isSink {
			continue
		}
		addr, _ := d["Address"].Value().(string)
		if addr == "" {
			continue
		}
		dev := sinkDev{Addr: strings.ToUpper(addr)}
		dev.Name, _ = d["Alias"].Value().(string)
		if dev.Name == "" {
			dev.Name, _ = d["Name"].Value().(string)
		}
		if dev.Name == "" {
			dev.Name = addr
		}
		dev.Paired, _ = d["Paired"].Value().(bool)
		dev.Connected, _ = d["Connected"].Value().(bool)
		if r, ok := d["RSSI"].Value().(int16); ok {
			dev.RSSI = int(r)
		}
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Paired != out[j].Paired {
			return out[i].Paired
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func managedObjects() map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := bus.Object(bluez, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return nil
	}
	return objs
}

func (s *sinkState) setErr(msg string) {
	s.mu.Lock()
	s.err = msg
	s.mu.Unlock()
	if msg != "" {
		log.Printf("Bluetooth-Ausgabe: %s", msg)
	}
}

func adapterCall(method string, args ...any) error {
	return bus.Object(bluez, dbus.ObjectPath("/org/bluez/"+*adapter)).Call("org.bluez.Adapter1."+method, 0, args...).Err
}

func (s *sinkState) scan(on bool) {
	if on {
		adapterCall("SetDiscoveryFilter", map[string]dbus.Variant{"Transport": dbus.MakeVariant("bredr")})
		if err := adapterCall("StartDiscovery"); err != nil && !strings.Contains(err.Error(), "InProgress") {
			s.setErr("Suche: " + err.Error())
			return
		}
		s.mu.Lock()
		s.scanUntil, s.err = time.Now().Add(scanTime), ""
		s.mu.Unlock()
		log.Printf("Bluetooth-Suche für %s", scanTime)
		return
	}
	s.mu.Lock()
	s.scanUntil = time.Time{}
	s.mu.Unlock()
	if err := adapterCall("StopDiscovery"); err != nil && !strings.Contains(err.Error(), "NotReady") && !strings.Contains(err.Error(), "Failed") {
		log.Printf("Suche beenden: %v", err)
	}
}

func devCall(ctx context.Context, addr, method string, args ...any) error {
	return bus.Object(bluez, devPath(addr)).CallWithContext(ctx, "org.bluez.Device1."+method, 0, args...).Err
}

// connect koppelt (falls nötig), vertraut und verbindet; läuft im Hintergrund, Ergebnis im nächsten Bericht.
func (s *sinkState) connect(addr string) {
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return
	}
	s.busy, s.err, s.lastTry = addr, "", time.Now()
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); s.busy = ""; s.mu.Unlock(); s.report() }()
		s.scan(false) // Verbinden während der Suche scheitert oft
		var d sinkDev
		for _, x := range sinkDevices(managedObjects()) {
			if x.Addr == strings.ToUpper(addr) {
				d = x
			}
		}
		if d.Addr == "" {
			s.setErr("Lautsprecher nicht gefunden (erst suchen)")
			return
		}
		if !d.Paired {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			err := devCall(ctx, addr, "Pair")
			cancel()
			if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
				s.setErr(fmt.Sprintf("Koppeln mit %s: %v", d.Name, err))
				return
			}
			log.Printf("Gekoppelt mit %s (%s)", d.Name, addr)
		}
		setProp(devPath(addr), "org.bluez.Device1", "Trusted", true)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := devCall(ctx, addr, "Connect"); err != nil && !strings.Contains(err.Error(), "AlreadyConnected") {
			s.setErr(fmt.Sprintf("Verbinden mit %s: %v", d.Name, err))
			return
		}
		log.Printf("Verbunden mit %s (%s)", d.Name, addr)
	}()
}

func (s *sinkState) disconnect(addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := devCall(ctx, addr, "Disconnect"); err != nil {
		s.setErr("Trennen: " + err.Error())
	}
	s.report()
}

func (s *sinkState) forget(addr string) {
	if err := adapterCall("RemoveDevice", devPath(addr)); err != nil {
		s.setErr("Vergessen: " + err.Error())
	}
	s.report()
}

func (s *sinkState) command(c lfbus.Action) {
	switch c.Action {
	case "scan":
		s.scan(true)
	case "stop":
		s.scan(false)
	case "connect":
		s.connect(c.Addr)
	case "disconnect":
		s.disconnect(c.Addr)
	case "forget":
		s.forget(c.Addr)
	}
	s.report()
}

// tick (alle 2 s): Suche beenden, gewünschten Lautsprecher wieder verbinden, Zustand melden.
func (s *sinkState) tick() {
	s.mu.Lock()
	stop := !s.scanUntil.IsZero() && time.Now().After(s.scanUntil)
	s.mu.Unlock()
	if stop {
		s.scan(false)
	}
	s.report()
	s.mu.Lock()
	want, busy, last, scanning := s.want, s.busy, s.lastTry, !s.scanUntil.IsZero()
	devs := s.devices
	s.mu.Unlock()
	if want == "" || busy != "" || scanning || time.Since(last) < retryEvery {
		return
	}
	for _, d := range devs {
		if d.Addr == want && !d.Connected {
			log.Printf("Ausgabe-Lautsprecher %s getrennt: verbinde neu", d.Name)
			s.connect(want)
		}
	}
}

// report schickt den Zustand an leuchtfeuerd (bei Änderung, sonst alle 10 s als Lebenszeichen) und merkt sich die Antwort.
func (s *sinkState) report() {
	if !lf.Connected() {
		return
	}
	devs := sinkDevices(managedObjects())
	s.mu.Lock()
	r := sinkReport{Devices: devs, Scanning: !s.scanUntil.IsZero(), Busy: s.busy, Error: s.err}
	s.devices = devs
	j, _ := json.Marshal(r)
	if string(j) == s.lastJSON && time.Since(s.lastPost) < 10*time.Second {
		s.mu.Unlock()
		return
	}
	s.lastJSON, s.lastPost = string(j), time.Now()
	s.mu.Unlock()
	var resp struct {
		Want string `json:"want"`
	}
	if err := lf.Post("/bt", r, &resp); err != nil {
		return
	}
	s.mu.Lock()
	s.want = strings.ToUpper(resp.Want)
	s.mu.Unlock()
}
