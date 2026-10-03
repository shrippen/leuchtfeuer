package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Geheimnisse in der config (Zugangsdaten des DNS-Anbieters) liegen verschlüsselt: AES-256-GCM mit einem zufälligen
// Geräteschlüssel in /data/leuchtfeuer/secret.key (Rechte 600). Der Schlüssel gehört nicht zur Sicherung; eine
// Sicherungsdatei enthält die Zugangsdaten also nur als Geheimtext, und nach dem Zurückspielen auf ein anderes Gerät
// müssen sie neu eingetragen werden. Gegen Root auf dem Lautsprecher schützt das nicht (Schlüssel und config liegen
// nebeneinander, ein Sicherheitschip ist nicht nutzbar) - dagegen hilft nur ein eng begrenztes Token (ACME_DNS_ALIAS).
// Format: enc:v1:<Base64 von Nonce|Geheimtext>; der Name des Schlüssels ist zusätzliche Authentisierung, ein Wert lässt
// sich also nicht unter einen anderen Namen kopieren. Von Hand eingetragene Klartextwerte gelten weiter und werden beim
// nächsten Start verschlüsselt.

const secretPrefix = "enc:v1:"

// secretConfigKeys: Schlüssel der config, die verschlüsselt abgelegt werden.
var secretConfigKeys = []string{"ACME_DNS_TOKEN", "ACME_NETCUP_PASSWORD"}

var errSecretForeign = errors.New("mit einem anderen Geräteschlüssel verschlüsselt (Sicherung von einem anderen Gerät?) - bitte neu eintragen")

var secretKeyMu sync.Mutex

// deviceKey liest den Geräteschlüssel; create legt ihn bei Bedarf an.
func deviceKey(dir string, create bool) ([]byte, error) {
	secretKeyMu.Lock()
	defer secretKeyMu.Unlock()
	p := filepath.Join(dir, "secret.key")
	b, err := os.ReadFile(p)
	if err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("%s beschädigt (%d statt 32 Bytes)", p, len(b))
		}
		return b, nil
	}
	if !os.IsNotExist(err) || !create {
		return nil, err
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	tmp := p + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return nil, err
	}
	return b, os.Rename(tmp, p)
}

func secretAEAD(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// sealSecret verschlüsselt einen Wert für den config-Schlüssel name.
func sealSecret(dir, name, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key, err := deviceKey(dir, true)
	if err != nil {
		return "", err
	}
	g, err := secretAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return secretPrefix + base64.RawStdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), []byte(name))), nil
}

// openSecret entschlüsselt; Klartext (ohne Präfix) kommt unverändert zurück.
func openSecret(dir, name, v string) (string, error) {
	enc, ok := strings.CutPrefix(v, secretPrefix)
	if !ok {
		return v, nil
	}
	key, err := deviceKey(dir, false)
	if os.IsNotExist(err) {
		return "", errSecretForeign
	}
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("verschlüsselter Wert beschädigt")
	}
	g, err := secretAEAD(key)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", fmt.Errorf("verschlüsselter Wert beschädigt")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte(name))
	if err != nil {
		return "", errSecretForeign
	}
	return string(plain), nil
}

// configSecret liest ein Geheimnis aus der config.
func configSecret(c *shellConfig, dir, name string) (string, error) {
	v, err := openSecret(dir, name, c.Get(name, ""))
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

// sealConfigSecrets verschlüsselt die Geheimnisse in kv (vor cfg.Set).
func sealConfigSecrets(dir string, kv map[string]string) error {
	for _, k := range secretConfigKeys {
		if v, ok := kv[k]; ok && v != "" && !strings.HasPrefix(v, secretPrefix) {
			s, err := sealSecret(dir, k, v)
			if err != nil {
				return err
			}
			kv[k] = s
		}
	}
	return nil
}

// migrateConfigSecrets verschlüsselt von Hand eingetragene Klartextwerte.
func migrateConfigSecrets(c *shellConfig, dir string) {
	kv := map[string]string{}
	for _, k := range secretConfigKeys {
		if v := c.Get(k, ""); v != "" && !strings.HasPrefix(v, secretPrefix) {
			kv[k] = v
		}
	}
	if len(kv) == 0 {
		return
	}
	if err := sealConfigSecrets(dir, kv); err != nil {
		logf("Geheimnisse in der config nicht verschlüsselt: %v", err)
		return
	}
	if err := c.Set(kv); err != nil {
		logf("Geheimnisse in der config nicht verschlüsselt: %v", err)
		return
	}
	logf("Geheimnisse in der config verschlüsselt (%d)", len(kv))
}
