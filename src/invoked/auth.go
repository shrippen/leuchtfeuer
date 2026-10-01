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
	sessionName  = "invoke_session"
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

// setPasswordFromStdin: `invoked -set-password` liest das Passwort aus der ersten Zeile von stdin (nie aus der Kommandozeile).
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

type loginState struct {
	mu       sync.Mutex
	hash     string
	sessions map[string]time.Time
	fails    map[string]*failure
}

type failure struct {
	n     int
	until time.Time
}

func newLoginState(hash string) *loginState {
	return &loginState{hash: hash, sessions: map[string]time.Time{}, fails: map[string]*failure{}}
}

func (l *loginState) setHash(h string) {
	l.mu.Lock()
	l.hash = h
	l.sessions = map[string]time.Time{} // alle Sitzungen beenden
	l.mu.Unlock()
}

func (l *loginState) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionName)
	if err != nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.sessions[c.Value]
	if ok && time.Now().After(exp) {
		delete(l.sessions, c.Value)
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
	l.sessions[t] = time.Now().Add(sessionLife)
	for k, exp := range l.sessions { // abgelaufene aufräumen
		if time.Now().After(exp) {
			delete(l.sessions, k)
		}
	}
	l.mu.Unlock()
	http.SetCookie(rw, &http.Cookie{Name: sessionName, Value: t, Path: "/", MaxAge: int(sessionLife.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(rw, map[string]bool{"ok": true})
}

func (l *loginState) logout(rw http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionName); err == nil {
		l.mu.Lock()
		delete(l.sessions, c.Value)
		l.mu.Unlock()
	}
	http.SetCookie(rw, &http.Cookie{Name: sessionName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(rw, map[string]bool{"ok": true})
}

// auth lässt nur die Anmeldeseite samt Gestaltung ohne Sitzung durch; die API antwortet mit 401, Seiten leiten um.
func (w *webServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/login":
			w.login.login(rw, r)
			return
		case p == "/api/logout":
			w.login.logout(rw, r)
			return
		case p == "/login.html" || p == "/app.css" || strings.HasPrefix(p, "/kante/"):
			next.ServeHTTP(rw, r)
			return
		case w.login.valid(r):
			next.ServeHTTP(rw, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			rw.Header().Set("Content-Type", "application/json; charset=utf-8")
			rw.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(rw).Encode(map[string]string{"error": "login"})
			return
		}
		http.Redirect(rw, r, "/login.html", http.StatusFound)
	})
}
