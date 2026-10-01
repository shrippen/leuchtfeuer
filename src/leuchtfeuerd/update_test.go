package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg})
		tw.Write([]byte(c))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestReleaseSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pkg := mkTarGz(t, map[string]string{"VERSION": "v2\n", "bin/leuchtfeuerd": "x"})
	sum := sha256.Sum256(pkg)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, releaseDigest(sum[:])))
	key := base64.StdEncoding.EncodeToString(pub)
	if err := verifyRelease(bytes.NewReader(pkg), sig, key); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, pkg...)
	bad[len(bad)/2] ^= 1
	if verifyRelease(bytes.NewReader(bad), sig, key) == nil {
		t.Fatal("verändertes Paket angenommen")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if verifyRelease(bytes.NewReader(pkg), sig, base64.StdEncoding.EncodeToString(other)) == nil {
		t.Fatal("falscher Schlüssel angenommen")
	}
}

func TestUpdatePathsAndExtract(t *testing.T) {
	for p, ok := range map[string]bool{"bin/leuchtfeuerd": true, "./services/a.sh": true, "hook.sh": true, "lib/ladspa/x.so": true,
		"config": false, "authorized_keys": false, "../etc/passwd": false, "bin/../config": false, "/bin/sh": false, "bluez/var/x": false} {
		if allowedUpdatePath(p) != ok {
			t.Errorf("%s: %v", p, !ok)
		}
	}
	dir := t.TempDir()
	n, err := extractUpdate(bytes.NewReader(mkTarGz(t, map[string]string{"VERSION": "v2", "bin/leuchtfeuerd": "neu", "services/x.sh": "#!/bin/sh"})), dir)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bin/leuchtfeuerd")); string(b) != "neu" {
		t.Fatal("Inhalt")
	}
	if _, err := extractUpdate(bytes.NewReader(mkTarGz(t, map[string]string{"VERSION": "v2", "config": "X"})), t.TempDir()); err == nil {
		t.Fatal("config im Update angenommen")
	}
}

func TestBackupRoundTrip(t *testing.T) {
	src := t.TempDir()
	dataDir = src
	t.Cleanup(func() { dataDir = "/data/leuchtfeuer" })
	os.WriteFile(filepath.Join(src, "config"), []byte("DEVICE_NAME=\"A\"\n"), 0o600)
	os.WriteFile(filepath.Join(src, "leuchtfeuerd.json"), []byte("{}"), 0o600)
	os.MkdirAll(filepath.Join(src, "bluez/var/lib/bluetooth/AA"), 0o700)
	os.WriteFile(filepath.Join(src, "bluez/var/lib/bluetooth/AA/info"), []byte("[General]"), 0o600)
	os.WriteFile(filepath.Join(src, "bin-nicht-sichern"), []byte("x"), 0o600)
	var buf bytes.Buffer
	if err := writeBackup(&buf); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	dataDir = dst
	n, err := restoreBackup(bytes.NewReader(buf.Bytes()))
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "bluez/var/lib/bluetooth/AA/info")); string(b) != "[General]" {
		t.Fatal("Kopplung fehlt")
	}
	if _, err := restoreBackup(bytes.NewReader(mkTarGz(t, map[string]string{"config": "x", "bin/leuchtfeuerd": "böse"}))); err == nil {
		t.Fatal("fremde Datei in der Sicherung angenommen")
	}
	if _, err := restoreBackup(strings.NewReader("kein gzip")); err == nil {
		t.Fatal("Unsinn angenommen")
	}
}

func TestDiagRedactsSecrets(t *testing.T) {
	in := []byte("DEVICE_NAME=\"A\"\nWEB_PASSWORD_HASH=\"pbkdf2-sha256:1:aa:bb\"\nMQTT_PASS=\"x\"\nUPDATE_PUBKEY=\"k\"\n")
	out := string(secretKeys.ReplaceAll(in, []byte("${1}\"(entfernt)\"")))
	if strings.Contains(out, "pbkdf2") || strings.Contains(out, `"x"`) || !strings.Contains(out, `DEVICE_NAME="A"`) {
		t.Fatal(out)
	}
}

func TestEqEffectiveAndEncode(t *testing.T) {
	p := eqEffective(EqSettings{Bass: 4, Loudness: true}, 0)
	if p.BassDB != 13 || p.PreampDB != -13 || p.TrebleDB != 3 {
		t.Fatalf("%+v", p)
	}
	if p := eqEffective(EqSettings{Loudness: true}, 100); p.BassDB != 0 || p.PreampDB != 0 {
		t.Fatalf("volle Lautstärke: %+v", p)
	}
	if p := eqEffective(EqSettings{Night: true}, 50); !p.Comp || p.Ratio != 4 {
		t.Fatalf("Nacht: %+v", p)
	}
	path := filepath.Join(t.TempDir(), "eq")
	w := newEqWriter(nil, path)
	w.write(p, true)
	w.write(eqEffective(EqSettings{Bass: 2}, 0), false)
	b, _ := os.ReadFile(path)
	if len(b) != eqSize || b[0] != 'I' || b[3] != '2' || b[4]&1 != 0 || b[4] != 4 {
		t.Fatalf("Datei %x", b[:8])
	}
	// Raumkorrektur: nur mit RoomOn, begrenzt; eine Anhebung senkt die Vorverstärkung
	room := []PEQBand{{Hz: 55, DB: -6.6, Q: 4.3}, {Hz: 2000, DB: 9, Q: 50}, {Hz: 5, DB: -30, Q: 0}}
	if p := eqEffective(EqSettings{Room: room}, 50); p.NPEQ != 0 {
		t.Fatalf("Raumkorrektur ohne RoomOn: %+v", p)
	}
	p = eqEffective(EqSettings{RoomOn: true, Room: room}, 50)
	if p.NPEQ != 3 || p.PEQ[1].DB != 6 || p.PEQ[1].Q != 10 || p.PEQ[2].Hz != 20 || p.PEQ[2].DB != -15 || p.PreampDB != -6 {
		t.Fatalf("Raumfilter: %+v", p)
	}
	enc := p.encode(2)
	if f := math.Float32frombits(binary.LittleEndian.Uint32(enc[8+4*9:])); f != 3 {
		t.Fatalf("Anzahl im Puffer: %v", f)
	}
	if f := math.Float32frombits(binary.LittleEndian.Uint32(enc[8+4*10:])); f != 55 {
		t.Fatalf("erster Filter: %v", f)
	}
	if len(cleanPEQ(make([]PEQBand, 9))) != maxPEQ {
		t.Fatal("zu viele Filter")
	}
}

func TestMQTTRingCommand(t *testing.T) {
	ta := newTestApp(t, "2026-10-01 20:00:00")
	m := &mqttBridge{app: ta.app}
	m.ringCommand(`{"state":"ON","color":{"r":255,"g":0,"b":10},"brightness":40}`)
	v := ta.st.Snapshot().Viz
	if v.Mode != "static" || v.Color != "custom" || v.RGB != [3]int{255, 0, 10} || v.Brightness != 40 {
		t.Fatalf("%+v", v)
	}
	m.ringCommand(`{"state":"ON","effect":"Spektrum"}`)
	if ta.st.Snapshot().Viz.Mode != "spectrum" {
		t.Fatal("Effekt")
	}
	if !strings.Contains(m.ringState(), `"effect":"Spektrum"`) {
		t.Fatal(m.ringState())
	}
	m.ringCommand(`{"state":"OFF"}`)
	if ta.st.Snapshot().Viz.Mode != "off" {
		t.Fatal("aus")
	}
}
