package iroh

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateIdentityIsStable(t *testing.T) {
	dir := t.TempDir()

	sk1, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	// The file is created with owner-only permissions.
	path := filepath.Join(dir, "iroh", "identity")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("identity not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("identity perm: want 0600, got %o", perm)
	}

	// A second load returns the same key, not a fresh one.
	sk2, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if sk1.Bytes() != sk2.Bytes() {
		t.Error("identity changed between loads; it must persist")
	}

	// The data-dir README is dropped on first use.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("data-dir README not written: %v", err)
	}
}

func TestEndpointIDHex(t *testing.T) {
	sk, err := LoadOrCreateIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := EndpointIDHex(sk.Public())
	if len(id) != 64 {
		t.Errorf("endpoint id should be 64 hex chars, got %d: %q", len(id), id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("endpoint id is not valid hex: %v", err)
	}
}
