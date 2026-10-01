// relsign: Schlüssel erzeugen, Release-Pakete signieren und prüfen (Ed25519).
// Signiert wird die Zeichenkette "leuchtfeuer-release:" + SHA-256 (hex) des Pakets; das prüft leuchtfeuerd vor einem Update
// aus der Weboberfläche (src/leuchtfeuerd/update.go) und install.sh --prebuilt.
//
//	relsign keygen <geheimer-schlüssel>          Schlüsselpaar erzeugen; gibt den öffentlichen Schlüssel (Base64) aus
//	relsign pub <geheimer-schlüssel>             öffentlichen Schlüssel ausgeben
//	relsign sign <geheimer-schlüssel> <paket>    schreibt <paket>.sig (Base64)
//	relsign verify <öffentlich-base64> <paket> [<sig>]
//
// Der geheime Schlüssel (Base64 des 32-Byte-Seeds) darf nie ins Repository; in der CI kommt er aus einem Secret.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func digest(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return []byte("leuchtfeuer-release:" + hex.EncodeToString(h.Sum(nil))), nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s ist kein geheimer Schlüssel von relsign", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("Aufruf: relsign keygen|pub|sign|verify ... (siehe Quelltext)")
	}
	switch args[0] {
	case "keygen":
		if _, err := os.Stat(args[1]); err == nil {
			return fmt.Errorf("%s existiert bereits", args[1])
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[1], []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
	case "pub":
		k, err := readKey(args[1])
		if err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey)))
	case "sign":
		if len(args) < 3 {
			return fmt.Errorf("relsign sign <schlüssel> <paket>")
		}
		k, err := readKey(args[1])
		if err != nil {
			return err
		}
		d, err := digest(args[2])
		if err != nil {
			return err
		}
		return os.WriteFile(args[2]+".sig", []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(k, d))+"\n"), 0o644)
	case "verify":
		if len(args) < 3 {
			return fmt.Errorf("relsign verify <öffentlich> <paket> [<sig>]")
		}
		sigPath := args[2] + ".sig"
		if len(args) > 3 {
			sigPath = args[3]
		}
		pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args[1]))
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("öffentlicher Schlüssel ungültig")
		}
		sb, err := os.ReadFile(sigPath)
		if err != nil {
			return err
		}
		sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sb)))
		if err != nil {
			return fmt.Errorf("Signatur unlesbar")
		}
		d, err := digest(args[2])
		if err != nil {
			return err
		}
		if !ed25519.Verify(pub, d, sig) {
			return fmt.Errorf("Signatur passt NICHT")
		}
		fmt.Println("Signatur gültig")
	default:
		return fmt.Errorf("unbekannter Befehl %q", args[0])
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "relsign:", err)
		os.Exit(1)
	}
}

func base64Pub(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}
