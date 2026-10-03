package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSecretsSealOpen(t *testing.T) {
	dir := t.TempDir()
	enc, err := sealSecret(dir, "ACME_DNS_TOKEN", "geheim-123")
	if err != nil || !strings.HasPrefix(enc, secretPrefix) || strings.Contains(enc, "geheim") {
		t.Fatalf("sealSecret: %q %v", enc, err)
	}
	if st, err := os.Stat(filepath.Join(dir, "secret.key")); err != nil || st.Mode().Perm() != 0o600 || st.Size() != 32 {
		t.Fatalf("Geräteschlüssel: %v %v", st, err)
	}
	if v, err := openSecret(dir, "ACME_DNS_TOKEN", enc); err != nil || v != "geheim-123" {
		t.Fatalf("openSecret: %q %v", v, err)
	}
	// gleicher Klartext, anderer Geheimtext (zufällige Nonce)
	if enc2, _ := sealSecret(dir, "ACME_DNS_TOKEN", "geheim-123"); enc2 == enc {
		t.Fatal("Nonce wird wiederverwendet")
	}
	// unter einem anderen Namen nicht lesbar
	if _, err := openSecret(dir, "ACME_NETCUP_PASSWORD", enc); !errors.Is(err, errSecretForeign) {
		t.Fatalf("anderer Name: %v", err)
	}
	// Klartext (von Hand eingetragen) gilt unverändert
	if v, err := openSecret(dir, "ACME_DNS_TOKEN", "klartext"); err != nil || v != "klartext" {
		t.Fatalf("Klartext: %q %v", v, err)
	}
	// anderes Gerät: anderer Schlüssel bzw. gar keiner
	other := t.TempDir()
	if _, err := openSecret(other, "ACME_DNS_TOKEN", enc); !errors.Is(err, errSecretForeign) {
		t.Fatalf("ohne Geräteschlüssel: %v", err)
	}
	sealSecret(other, "X", "y")
	if _, err := openSecret(other, "ACME_DNS_TOKEN", enc); !errors.Is(err, errSecretForeign) {
		t.Fatalf("fremder Geräteschlüssel: %v", err)
	}
	if _, err := openSecret(dir, "ACME_DNS_TOKEN", secretPrefix+"!!kaputt"); err == nil {
		t.Fatal("beschädigter Wert muss scheitern")
	}
	// der Geräteschlüssel gehört nicht in die Sicherung
	if slices.Contains(backupFiles, "secret.key") {
		t.Fatal("secret.key darf nicht in die Sicherung")
	}
}

func TestSecretsMigrateAndUse(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	os.WriteFile(cfgPath, []byte("WEB_TLS=\"acme\"\nACME_DOMAIN=\"a.example.de\"\nACME_DNS=\"netcup\"\nACME_NETCUP_CUSTOMER=\"1\"\nACME_DNS_TOKEN=\"key-klar\"\nACME_NETCUP_PASSWORD=\"pass-klar\"\n"), 0o600)
	c := &shellConfig{path: cfgPath}
	m, err := newHTTPS(c, dir)
	if err != nil || m.magic == nil {
		t.Fatalf("newHTTPS: %v %+v", err, m.Status())
	}
	b, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(b), "key-klar") || strings.Contains(string(b), "pass-klar") || strings.Count(string(b), secretPrefix) != 2 {
		t.Fatalf("Klartext nicht verschlüsselt:\n%s", b)
	}
	if v, _ := configSecret(c, dir, "ACME_NETCUP_PASSWORD"); v != "pass-klar" {
		t.Fatalf("nach der Umstellung: %q", v)
	}
	// Sicherung von einem anderen Gerät zurückgespielt: verständlicher Fehler, HTTPS läuft mit dem eigenen Zertifikat
	os.Remove(filepath.Join(dir, "secret.key"))
	m, _ = newHTTPS(c, dir)
	if st := m.Status(); st.State != "error" || !strings.Contains(st.Error, "neu eintragen") {
		t.Fatalf("fremde Sicherung: %+v", st)
	}
}

func TestHTTPSAlias(t *testing.T) {
	base := httpsSettings{Mode: "acme", Port: 443, Domain: "lautsprecher.example.de", DNS: "desec", Token: "t"}
	for alias, ok := range map[string]bool{
		"":                                       true,
		"lautsprecher.acme.dedyn.io":             true,
		"_acme-challenge.lautsprecher.dedyn.io.": true,
		"lautsprecher.example.de":                false, // zeigt auf sich selbst
		"_acme-challenge.lautsprecher.example.de": false,
		"kein name": false,
		"ohnepunkt": false,
	} {
		v := base
		v.Alias = alias
		kv, err := httpsConfig(v, httpsSettings{})
		if (err == nil) != ok {
			t.Errorf("%q: Fehler %v, erwartet ok=%v", alias, err, ok)
		}
		if err == nil && strings.HasSuffix(kv["ACME_DNS_ALIAS"], ".") {
			t.Errorf("%q: Punkt am Ende nicht entfernt", alias)
		}
	}
}
