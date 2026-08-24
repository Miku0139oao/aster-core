package trafficcontrol

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStoreRejectsAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(link, "sub", "traffic.db")
	if _, err := OpenStore(path, DefaultStoreLimit); err == nil {
		t.Fatal("ancestor symlink was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "sub", "traffic.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside store was created: %v", err)
	}
}

func TestOpenStoreRejectsExistingOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	if err := os.WriteFile(path, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path, 1024); !errors.Is(err, ErrStoreLimit) {
		t.Fatalf("error = %v, want ErrStoreLimit", err)
	}
}
