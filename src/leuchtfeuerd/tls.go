package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HTTPS (optional, WEB_TLS="on"): leuchtfeuerd erzeugt einmal ein eigenes Zertifikat (ECDSA P-256, 10 Jahre) für den
// DHCP-Namen (<name>.lan, <name>.local) und die Adressen des Lautsprechers und legt es unter /data/leuchtfeuer/web-tls.*
// ab. Der Browser warnt beim ersten Besuch (selbst signiert); danach ist die Verbindung verschlüsselt, auch das
// Passwort. Der HTTP-Port leitet dann auf HTTPS um.

func ensureCert(dir, host string) (tls.Certificate, error) {
	crt, key := filepath.Join(dir, "web-tls.crt"), filepath.Join(dir, "web-tls.key")
	if c, err := tls.LoadX509KeyPair(crt, key); err == nil {
		return c, nil
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Leuchtfeuer"}},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{host, host + ".lan", host + ".local", "localhost"},
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ip, _, err := net.ParseCIDR(a.String()); err == nil {
				tpl.IPAddresses = append(tpl.IPAddresses, ip)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, _ := x509.MarshalECPrivateKey(priv)
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	logf("Weboberfläche: eigenes Zertifikat für %s erzeugt", host)
	return tls.LoadX509KeyPair(crt, key)
}

// redirectHTTPS leitet HTTP-Anfragen auf denselben Pfad über HTTPS um.
func redirectHTTPS(tlsPort string) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if tlsPort != "443" {
			host = net.JoinHostPort(host, tlsPort)
		}
		target := "https://" + host + r.URL.RequestURI()
		if strings.ContainsAny(target, "\r\n") {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}
		http.Redirect(rw, r, target, http.StatusMovedPermanently)
	})
}
