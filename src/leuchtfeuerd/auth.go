package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Passwort-Ablage: "pbkdf2-sha256:<Runden>:<Salz hex>:<Hash hex>" in WEB_PASSWORD_HASH (nur Hex und ":",
// damit die Shell-Konfiguration es unverändert einliest). Das Klartext-Passwort wird nirgends gespeichert.
const (
	pbkdfRounds  = 60000
	sessionName  = "leuchtfeuer_session"
	sessionLife  = 30 * 24 * time.Hour
	minPassLen   = 6
	maxFailures  = 5
	lockDuration = time.Minute
)

func pbkdf2SHA256(pw, salt []byte, rounds, n int) []byte {
	prf := hmac.New(sha256.New, pw)
	var out []byte
	for block := 1; len(out) < n; block++ {
		prf.Reset()
		prf.Write(salt)
		prf.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < rounds; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:n]
}

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", pbkdfRounds, hex.EncodeToString(salt),
		hex.EncodeToString(pbkdf2SHA256([]byte(pw), salt, pbkdfRounds, 32)))
}

func verifyPassword(pw, stored string) bool {
	p := strings.Split(stored, ":")
	if len(p) != 4 || p[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(p[1])
	if err != nil || rounds < 1 || rounds > 5000000 {
		return false
	}
	salt, err1 := hex.DecodeString(p[2])
	want, err2 := hex.DecodeString(p[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(pbkdf2SHA256([]byte(pw), salt, rounds, len(want)), want) == 1
}

func checkNewPassword(pw string) error {
	if len([]rune(pw)) < minPassLen {
		return fmt.Errorf("Passwort: mindestens %d Zeichen / password: at least %d characters", minPassLen, minPassLen)
	}
	return nil
}

// storePassword hasht und schreibt WEB_PASSWORD_HASH; ein altes Klartext-Passwort wird entfernt.
func storePassword(cfg *shellConfig, pw string) (string, error) {
	h := hashPassword(pw)
	if err := cfg.Set(map[string]string{"WEB_PASSWORD_HASH": h}); err != nil {
		return "", err
	}
	return h, cfg.Delete("WEB_PASSWORD")
}

// setPasswordFromStdin: `leuchtfeuerd -set-password` liest das Passwort aus der ersten Zeile von stdin (nie aus der Kommandozeile).
func setPasswordFromStdin(cfgPath string) error {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("kein Passwort auf stdin")
	}
	pw := strings.TrimRight(line, "\r\n")
	if err := checkNewPassword(pw); err != nil {
		return err
	}
	_, err = storePassword(&shellConfig{path: cfgPath}, pw)
	return err
}

// ---- Anmeldung mit Sitzung ----

// Sitzungen: im Speicher und (nur als SHA-256 des Schlüssels) in /data/leuchtfeuer/sessions.json, damit ein Neustart von
// leuchtfeuerd niemanden abmeldet. Abgelaufene Sitzungen und alte Fehlversuche räumt ein Hintergrundlauf alle 10 Minuten weg.
type loginState struct {
	mu       sync.Mutex
	hash     string
	sessions map[string]time.Time // SHA-256 (hex) des Sitzungsschlüssels -> Ablauf
	fails    map[string]*failure
	path     string // Ablage der Sitzungen ("" = nicht speichern)
	secure   bool   // nur über HTTPS: Cookie mit Secure
}

type failure struct {
	n     int
	until time.Time
	last  time.Time
}

func newLoginState(hash string) *loginState {
	return &loginState{hash: hash, sessions: map[string]time.Time{}, fails: map[string]*failure{}}
}

func tokenKey(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// load liest gespeicherte Sitzungen (nur noch gültige).
func (l *loginState) load(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.path = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]time.Time
	if json.Unmarshal(b, &m) != nil {
		return
	}
	for k, exp := range m {
		if time.Now().Before(exp) && len(k) == 64 {
			l.sessions[k] = exp
		}
	}
}

// saveLocked schreibt die Sitzungen atomar (l.mu gehalten).
func (l *loginState) saveLocked() {
	if l.path == "" {
		return
	}
	b, _ := json.Marshal(l.sessions)
	tmp := l.path + ".new"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, l.path)
	}
}

// cleanup entfernt abgelaufene Sitzungen und Fehlversuche, die älter als 10 Minuten sind.
func (l *loginState) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now, changed := time.Now(), false
	for k, exp := range l.sessions {
		if now.After(exp) {
			delete(l.sessions, k)
			changed = true
		}
	}
	for ip, f := range l.fails {
		if now.After(f.until) && now.Sub(f.last) > 10*time.Minute {
			delete(l.fails, ip)
		}
	}
	if changed {
		l.saveLocked()
	}
}

func (l *loginState) janitor() {
	for range time.Tick(10 * time.Minute) {
		l.cleanup()
	}
}

func (l *loginState) setHash(h string) {
	l.mu.Lock()
	l.hash = h
	l.sessions = map[string]time.Time{} // alle Sitzungen beenden
	l.saveLocked()
	l.mu.Unlock()
}

func (l *loginState) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionName)
	if err != nil {
		return false
	}
	k := tokenKey(c.Value)
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.sessions[k]
	if ok && time.Now().After(exp) {
		delete(l.sessions, k)
		return false
	}
	return ok
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (l *loginState) login(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "POST nötig", http.StatusMethodNotAllowed)
		return
	}
	ip := clientIP(r)
	l.mu.Lock()
	f := l.fails[ip]
	if f == nil {
		f = &failure{}
		l.fails[ip] = f
	}
	if time.Now().Before(f.until) {
		wait := int(time.Until(f.until).Seconds()) + 1
		l.mu.Unlock()
		fail(rw, http.StatusTooManyRequests, fmt.Errorf("zu viele Versuche, bitte %d s warten / too many attempts, wait %d s", wait, wait))
		return
	}
	hash := l.hash
	l.mu.Unlock()

	var v struct {
		Password string `json:"password"`
	}
	if err := decode(r, &v); err != nil {
		fail(rw, 400, err)
		return
	}
	if !verifyPassword(v.Password, hash) {
		l.mu.Lock()
		f.last = time.Now()
		if f.n++; f.n >= maxFailures {
			f.n, f.until = 0, time.Now().Add(lockDuration)
		}
		l.mu.Unlock()
		time.Sleep(time.Second)
		fail(rw, http.StatusUnauthorized, fmt.Errorf("falsches Passwort / wrong password"))
		return
	}
	tok := make([]byte, 32)
	rand.Read(tok)
	t := hex.EncodeToString(tok)
	l.mu.Lock()
	f.n = 0
	l.sessions[tokenKey(t)] = time.Now().Add(sessionLife)
	for k, exp := range l.sessions { // abgelaufene aufräumen
		if time.Now().After(exp) {
			delete(l.sessions, k)
		}
	}
	l.saveLocked()
	secure := l.secure
	l.mu.Unlock()
	http.SetCookie(rw, &http.Cookie{Name: sessionName, Value: t, Path: "/", MaxAge: int(sessionLife.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	writeJSON(rw, map[string]bool{"ok": true})
}

func (l *loginState) logout(rw http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionName); err == nil {
		l.mu.Lock()
		delete(l.sessions, tokenKey(c.Value))
		l.saveLocked()
		l.mu.Unlock()
	}
	http.SetCookie(rw, &http.Cookie{Name: sessionName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(rw, map[string]bool{"ok": true})
}

// sameOrigin: Schreibende Anfragen mit Sitzungs-Cookie müssen von der eigenen Seite kommen (Origin bzw. Referer
// passt zum Host). Fehlen beide (curl, alte Programme), gilt die Anfrage als gleich: Browser schicken bei fetch/POST
// immer Origin mit, und nur Browser hängen das Cookie ungefragt an.
func sameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		ref := r.Header.Get("Referer")
		if ref == "" {
			return o == ""
		}
		o = ref
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// securityHeaders: strenge Richtlinie für die Oberfläche. Skripte nur aus eigenen Dateien; Stile auch inline
// (die Oberfläche setzt style-Attribute); Mikrofon nur für die eigene Seite (Raum einmessen).
func securityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
		"connect-src 'self'; media-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Permissions-Policy", "microphone=(self), geolocation=(self), camera=()")
}

// auth lässt nur die Anmeldeseite samt Gestaltung ohne Sitzung durch; die API antwortet mit 401, Seiten leiten um.
// API-Schlüssel (Authorization: Bearer) gelten nur für /api/ und /metrics, nicht für die Zugangsverwaltung.
func (w *webServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		securityHeaders(rw.Header())
		p := r.URL.Path
		deny := func(code int, msg string) {
			rw.Header().Set("Content-Type", "application/json; charset=utf-8")
			rw.WriteHeader(code)
			json.NewEncoder(rw).Encode(map[string]string{"error": msg})
		}
		if tok := bearer(r); tok != "" && w.tokens != nil {
			scope := w.tokens.Check(tok)
			switch {
			case scope == "":
				deny(http.StatusUnauthorized, "API-Schlüssel ungültig / invalid API key")
			case !(strings.HasPrefix(p, "/api/") || p == "/metrics") || sessionOnly(p) || p == "/api/login" || p == "/api/logout":
				deny(http.StatusForbidden, "mit API-Schlüssel nicht erlaubt / not allowed with an API key")
			case scope == "read" && !readOnlyOK(r):
				deny(http.StatusForbidden, "Schlüssel darf nur lesen / read-only key")
			default:
				next.ServeHTTP(rw, r)
			}
			return
		}
		if !sameOrigin(r) {
			deny(http.StatusForbidden, "fremde Herkunft / cross-origin request")
			return
		}
		switch {
		case p == "/api/login":
			w.login.login(rw, r)
			return
		case p == "/api/logout":
			w.login.logout(rw, r)
			return
		case p == "/login.html" || p == "/login.js" || p == "/favicon.svg" || p == "/app.css" || strings.HasPrefix(p, "/kante/"):
			next.ServeHTTP(rw, r)
			return
		case w.login.valid(r):
			next.ServeHTTP(rw, r)
			return
		}
		if strings.HasPrefix(p, "/api/") || p == "/metrics" {
			deny(http.StatusUnauthorized, "login")
			return
		}
		http.Redirect(rw, r, "/login.html", http.StatusFound)
	})
}
