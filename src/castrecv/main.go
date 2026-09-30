// castrecv: minimaler Google-Cast-Empfänger (CASTV2) für den Harman Kardon Invoke.
// Meldet sich per mDNS als Chromecast Audio und spielt Medien-URLs über GStreamer/ALSA ab.
// Läuft mit Sendern, die das Gerät nicht per Google-Zertifikat prüfen (Music Assistant,
// Home Assistant, VLC, pychromecast). Der Empfänger ist nachgebildet, keine Google-Software.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsDeviceAuth = "urn:x-cast:com.google.cast.tp.deviceauth"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"
	transportID  = "web-1"
)

var (
	name    = flag.String("name", "HK Invoke", "Anzeigename")
	sink    = flag.String("device", "invoke_music", "ALSA-Gerät")
	iface   = flag.String("iface", "wlan0", "Netzwerkschnittstelle für mDNS (leer = alle)")
	port    = flag.Int("port", 8009, "Cast-Port (TLS)")
	mixer   = flag.String("mixer", "system", "ALSA-Regler für die Lautstärke (Drehrad)")
	devID   string
	pl      *player
	srv     = &server{conns: map[*conn]bool{}}
	app     map[string]any // gestartete Anwendung oder nil
	appLock sync.Mutex
	muted   bool
	premute int
)

type conn struct {
	c      net.Conn
	mu     sync.Mutex
	sender string          // sourceId des Senders
	joined map[string]bool // Ziele, mit denen der Sender per CONNECT verbunden ist
}

type server struct {
	mu    sync.Mutex
	conns map[*conn]bool
}

func (c *conn) send(src, dst, ns string, payload any) {
	b, _ := json.Marshal(payload)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := writeFrame(c.c, &castMessage{Source: src, Dest: dst, Namespace: ns, Payload: b}); err != nil {
		c.c.Close()
	}
}

// broadcast schickt an alle Sender, die mit dst verbunden sind.
func (s *server) broadcast(src, ns string, payload any) {
	s.mu.Lock()
	var list []*conn
	for c := range s.conns {
		c.mu.Lock()
		ok := c.joined[src]
		to := c.sender
		c.mu.Unlock()
		if ok {
			list = append(list, c)
			_ = to
		}
	}
	s.mu.Unlock()
	for _, c := range list {
		c.mu.Lock()
		to := c.sender
		c.mu.Unlock()
		c.send(src, to, ns, payload)
	}
}

var levelRe = regexp.MustCompile(`Front Left: (\d+) `)

func getVolume() (float64, bool) {
	out, err := exec.Command("amixer", "-c", "0", "sget", *mixer).Output()
	if err != nil {
		return 1, muted
	}
	if m := levelRe.FindSubmatch(out); m != nil {
		v, _ := strconv.Atoi(string(m[1]))
		return float64(v) / 255, muted
	}
	return 1, muted
}

func setMixer(v int) {
	exec.Command("amixer", "-c", "0", "-q", "sset", *mixer, strconv.Itoa(v)).Run()
}

func setVolume(vol map[string]any) {
	if l, ok := vol["level"].(float64); ok {
		muted = false
		setMixer(int(l*255 + 0.5))
	}
	if m, ok := vol["muted"].(bool); ok && m != muted {
		if m {
			v, _ := getVolume()
			premute = int(v*255 + 0.5)
			setMixer(0)
		} else {
			setMixer(premute)
		}
		muted = m
	}
}

func receiverStatus(reqID float64) map[string]any {
	level, m := getVolume()
	st := map[string]any{
		"isActiveInput": true,
		"volume":        map[string]any{"controlType": "attenuation", "level": level, "muted": m, "stepInterval": 0.05},
	}
	appLock.Lock()
	if app != nil {
		st["applications"] = []any{app}
	}
	appLock.Unlock()
	return map[string]any{"type": "RECEIVER_STATUS", "requestId": reqID, "status": st}
}

func mediaStatus(reqID float64) map[string]any {
	level, m := getVolume()
	var list []any
	if s := pl.status(level, m); s != nil {
		list = []any{s}
	}
	if list == nil {
		list = []any{}
	}
	return map[string]any{"type": "MEDIA_STATUS", "requestId": reqID, "status": list}
}

func broadcastMedia()    { srv.broadcast(transportID, nsMedia, mediaStatus(0)) }
func broadcastReceiver() { srv.broadcast("receiver-0", nsReceiver, receiverStatus(0)) }

func launch(appID string) {
	appLock.Lock()
	app = map[string]any{
		"appId":             appID,
		"universalAppId":    appID,
		"displayName":       "Invoke",
		"isIdleScreen":      false,
		"launchedFromCloud": false,
		"namespaces":        []any{map[string]any{"name": nsMedia}, map[string]any{"name": nsConnection}},
		"sessionId":         "b7a5c1f0-0000-4000-8000-" + devID[len(devID)-12:],
		"statusText":        "Ready To Cast",
		"transportId":       transportID,
	}
	appLock.Unlock()
}

func (c *conn) handle(m *castMessage) {
	if m.Binary {
		return // deviceauth u. Ä.: bewusst nicht beantwortet
	}
	var req map[string]any
	json.Unmarshal(m.Payload, &req)
	typ, _ := req["type"].(string)
	reqID, _ := req["requestId"].(float64)
	reply := func(ns string, p any) { c.send(m.Dest, m.Source, ns, p) }

	switch m.Namespace {
	case nsConnection:
		c.mu.Lock()
		c.sender = m.Source
		if typ == "CONNECT" {
			c.joined[m.Dest] = true
		} else if typ == "CLOSE" {
			delete(c.joined, m.Dest)
		}
		c.mu.Unlock()
	case nsHeartbeat:
		if typ == "PING" {
			reply(nsHeartbeat, map[string]any{"type": "PONG"})
		}
	case nsReceiver:
		switch typ {
		case "GET_STATUS":
			reply(nsReceiver, receiverStatus(reqID))
		case "GET_APP_AVAILABILITY":
			av := map[string]any{}
			if ids, ok := req["appId"].([]any); ok {
				for _, id := range ids {
					av[fmt.Sprint(id)] = "APP_AVAILABLE"
				}
			}
			reply(nsReceiver, map[string]any{"responseType": "GET_APP_AVAILABILITY", "requestId": reqID, "availability": av})
		case "LAUNCH":
			id, _ := req["appId"].(string)
			log.Printf("LAUNCH %s", id)
			launch(id)
			reply(nsReceiver, receiverStatus(reqID))
			broadcastReceiver()
		case "STOP":
			pl.stop()
			appLock.Lock()
			app = nil
			appLock.Unlock()
			reply(nsReceiver, receiverStatus(reqID))
			broadcastReceiver()
		case "SET_VOLUME":
			if v, ok := req["volume"].(map[string]any); ok {
				setVolume(v)
			}
			reply(nsReceiver, receiverStatus(reqID))
			broadcastReceiver()
		default:
			log.Printf("receiver: unbekannt %s", m.Payload)
		}
	case nsMedia:
		switch typ {
		case "GET_STATUS":
			reply(nsMedia, mediaStatus(reqID))
		case "LOAD":
			media, _ := req["media"].(map[string]any)
			url, _ := media["contentId"].(string)
			log.Printf("LOAD %s (%v)", url, media["contentType"])
			if url == "" {
				reply(nsMedia, map[string]any{"type": "LOAD_FAILED", "requestId": reqID})
				return
			}
			pl.load(media, url)
			reply(nsMedia, mediaStatus(reqID))
		case "PLAY":
			pl.resume()
			reply(nsMedia, mediaStatus(reqID))
		case "PAUSE":
			pl.pause()
			reply(nsMedia, mediaStatus(reqID))
		case "STOP":
			pl.stop()
			reply(nsMedia, mediaStatus(reqID))
		case "SET_VOLUME":
			if v, ok := req["volume"].(map[string]any); ok {
				setVolume(v)
			}
			reply(nsMedia, mediaStatus(reqID))
			broadcastReceiver()
		case "SEEK":
			log.Printf("SEEK ignoriert: %s", m.Payload)
			reply(nsMedia, mediaStatus(reqID))
		default:
			log.Printf("media: unbekannt %s", m.Payload)
		}
	default:
		log.Printf("namespace %s: %s", m.Namespace, m.Payload)
	}
}

func (s *server) serve(ln net.Listener) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		c := &conn{c: nc, joined: map[string]bool{}}
		s.mu.Lock()
		s.conns[c] = true
		s.mu.Unlock()
		go func() {
			defer func() {
				nc.Close()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
			}()
			for {
				nc.SetReadDeadline(time.Now().Add(30 * time.Second)) // Sender pingt alle 5 s
				m, err := readFrame(nc)
				if err != nil {
					return
				}
				c.handle(m)
			}
		}()
	}
}

func selfSigned() tls.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Invoke"},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func deviceID() string {
	if b, err := os.ReadFile("/sys/class/net/" + *iface + "/address"); err == nil {
		h := strings.ReplaceAll(strings.TrimSpace(string(b)), ":", "")
		return h + "00000000000000000000"[:32-len(h)]
	}
	return "00000000000000000000000000000001"
}

func main() {
	flag.Parse()
	devID = deviceID()
	pl = newPlayer(*sink, broadcastMedia)

	// HTTP 8008 und HTTPS 8443: manche Sender fragen /setup/eureka_info ab
	info := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/setup/eureka_info" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"name": *name, "version": 12, "build_version": "12", "cast_build_revision": "1.0",
				"ssdp_udn": devID, "hotspot_bssid": "", "uptime": 1, "has_update": false, "wpa_configured": true})
			return
		}
		http.NotFound(w, r)
	})
	cert := selfSigned()
	go http.ListenAndServe(":8008", info)
	go (&http.Server{Addr: ":8443", Handler: info, TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}}).ListenAndServeTLS("", "")

	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", *port), &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		log.Fatal(err)
	}

	var ifs []net.Interface
	var ips []string
	if *iface != "" {
		if ni, err := net.InterfaceByName(*iface); err == nil {
			ifs = []net.Interface{*ni}
			addrs, _ := ni.Addrs()
			for _, a := range addrs {
				if ip, _, err := net.ParseCIDR(a.String()); err == nil && ip.To4() != nil {
					ips = append(ips, ip.String())
				}
			}
		}
	}
	txt := []string{"id=" + devID, "cd=" + devID, "rm=", "ve=05", "md=Chromecast Audio", "ic=/setup/icon.png",
		"fn=" + *name, "ca=4", "st=0", "bs=" + devID[:12], "nf=1", "rs="}
	host := devID
	var reg *zeroconf.Server
	if len(ips) > 0 {
		reg, err = zeroconf.RegisterProxy("HK-Invoke-"+devID, "_googlecast._tcp", "local.", *port, host, ips, txt, ifs)
	} else {
		reg, err = zeroconf.Register("HK-Invoke-"+devID, "_googlecast._tcp", "local.", *port, txt, ifs)
	}
	if err != nil {
		log.Fatalf("mDNS: %v", err)
	}
	defer reg.Shutdown()
	log.Printf("castrecv %q id=%s ips=%v port=%d", *name, devID, ips, *port)
	srv.serve(ln)
}
