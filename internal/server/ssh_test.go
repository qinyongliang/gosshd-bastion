package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateHostSignerPersistsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh_host_key")

	first, err := loadOrCreateHostSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateHostSigner(path)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(second.PublicKey().Marshal()), string(first.PublicKey().Marshal()); got != want {
		t.Fatalf("host key changed after reload")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
			t.Fatalf("host key permissions mismatch: got %v want %v", got, want)
		}
	}
	if info.Size() == 0 {
		t.Fatalf("host key file is empty")
	}
}
