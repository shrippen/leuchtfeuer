package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSignVerify(t *testing.T) {
	d := t.TempDir()
	key, pkg := filepath.Join(d, "k"), filepath.Join(d, "p.tar.gz")
	os.WriteFile(pkg, []byte("paket"), 0o644)
	if err := run([]string{"keygen", key}); err != nil {
		t.Fatal(err)
	}
	k, _ := readKey(key)
	pub := base64Pub(k)
	if err := run([]string{"sign", key, pkg}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", pub, pkg}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(pkg, []byte("Paket"), 0o644)
	if run([]string{"verify", pub, pkg}) == nil {
		t.Fatal("verändertes Paket gilt als gültig")
	}
}
