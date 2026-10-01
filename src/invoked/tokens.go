package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// API-Schlüssel für Skripte, Home Assistant (custom_components/leuchtfeuer), andere Leuchtfeuer (Geräte-Übersicht) und
// Prometheus: "Authorization: Bearer lf_<64 Hex>". Gespeichert wird nur der SHA-256 des Schlüssels
// (/data/invoke/tokens.json); der Schlüssel selbst wird nur einmal beim Anlegen angezeigt.
// Umfang "read": nur lesen (GET, /api/events, /metrics); "full": alles außer Zugangsverwaltung (Schlüssel,
// SSH-Schlüssel, Web-Passwort), die braucht eine angemeldete Sitzung.

const tokenPrefix = "lf_"

type apiToken struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Scope    string    `json:"scope"` // read | full
	Hash     string    `json:"hash,omitempty"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed"`
}

type tokenStore struct {
	mu    sync.Mutex
	path  string
	list  []apiToken
	dirty bool
}

func loadTokens(path string) *tokenStore {
	ts := &tokenStore{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &ts.list)
	}
	return ts
}

func (ts *tokenStore) saveLocked() error {
	if ts.path == "" {
		return nil
	}
	b, _ := json.MarshalIndent(ts.list, "", "  ")
	tmp := ts.path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	ts.dirty = false
	return os.Rename(tmp, ts.path)
}

// Create legt einen Schlüssel an und liefert ihn im Klartext (einmalig).
func (ts *tokenStore) Create(name, scope string) (string, apiToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return "", apiToken{}, fmt.Errorf("Name nötig (höchstens 60 Zeichen) / name required (max. 60 characters)")
	}
	if scope != "read" && scope != "full" {
		return "", apiToken{}, fmt.Errorf("Umfang read oder full")
	}
	b := make([]byte, 32)
	rand.Read(b)
	tok := tokenPrefix + hex.EncodeToString(b)
	t := apiToken{ID: newID(), Name: name, Scope: scope, Hash: tokenKey(tok), Created: time.Now()}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.list) >= 50 {
		return "", apiToken{}, fmt.Errorf("höchstens 50 Schlüssel")
	}
	ts.list = append(ts.list, t)
	if err := ts.saveLocked(); err != nil {
		return "", apiToken{}, err
	}
	t.Hash = ""
	return tok, t, nil
}

func (ts *tokenStore) Delete(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for i, t := range ts.list {
		if t.ID == id {
			ts.list = append(ts.list[:i], ts.list[i+1:]...)
			return ts.saveLocked()
		}
	}
	return fmt.Errorf("Schlüssel %q gibt es nicht", id)
}

// List liefert die Schlüssel ohne Hash.
func (ts *tokenStore) List() []apiToken {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]apiToken, 0, len(ts.list))
	for _, t := range ts.list {
		t.Hash = ""
		out = append(out, t)
	}
	return out
}

// Check prüft einen Schlüssel und liefert seinen Umfang ("" = ungültig).
func (ts *tokenStore) Check(tok string) string {
	if !strings.HasPrefix(tok, tokenPrefix) || len(tok) != len(tokenPrefix)+64 {
		return ""
	}
	k := tokenKey(tok)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for i := range ts.list {
		if subtle.ConstantTimeCompare([]byte(ts.list[i].Hash), []byte(k)) == 1 {
			if time.Since(ts.list[i].LastUsed) > time.Minute { // nicht bei jeder Anfrage schreiben
				ts.list[i].LastUsed = time.Now()
				ts.dirty = true
			}
			return ts.list[i].Scope
		}
	}
	return ""
}

// flush schreibt "zuletzt benutzt" gelegentlich (Hintergrundlauf).
func (ts *tokenStore) flush() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.dirty {
		ts.saveLocked()
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

// Zugangsverwaltung nur mit angemeldeter Sitzung, nie mit einem API-Schlüssel.
func sessionOnly(path string) bool {
	return strings.HasPrefix(path, "/api/tokens") || strings.HasPrefix(path, "/api/ssh-keys") || strings.HasPrefix(path, "/api/peers") ||
		path == "/api/settings/device" || path == "/api/restore" || path == "/api/backup"
}

// readOnlyOK: was ein Schlüssel mit Umfang "read" darf.
func readOnlyOK(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}
