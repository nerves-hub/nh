package iroh

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/go-iroh/key"
)

func TestLoadOrCreateIdentityIsStable(t *testing.T) {
	dir := t.TempDir()

	path := OrgIdentityPath(dir, "acme")
	sk1, err := LoadOrCreateIdentity(dir, path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	// The file is created with owner-only permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("identity not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("identity perm: want 0600, got %o", perm)
	}

	// A second load returns the same key, not a fresh one.
	sk2, err := LoadOrCreateIdentity(dir, path)
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
	dir := t.TempDir()
	sk, err := LoadOrCreateIdentity(dir, OrgIdentityPath(dir, "acme"))
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

func TestIdentityPaths(t *testing.T) {
	dir := "/data"
	if got, want := LegacyIdentityPath(dir), filepath.Join(dir, "iroh", "identity"); got != want {
		t.Errorf("legacy path = %q, want %q", got, want)
	}
	if got, want := OrgIdentityPath(dir, "acme-co_1"), filepath.Join(dir, "iroh", "orgs", "acme-co_1", "identity"); got != want {
		t.Errorf("org path = %q, want %q", got, want)
	}
	// An org called "identity" must not land on the legacy key file.
	if OrgIdentityPath(dir, "identity") == LegacyIdentityPath(dir) {
		t.Error("org named identity collides with the legacy key")
	}
	// Orgs that differ only in case stay apart on a case-insensitive disk.
	upper, lower := OrgIdentityPath(dir, "Acme"), OrgIdentityPath(dir, "acme")
	if strings.EqualFold(upper, lower) {
		t.Errorf("Acme and acme share a key path: %q, %q", upper, lower)
	}
	// Nothing in an org name escapes the orgs directory.
	if got := OrgIdentityPath(dir, "../evil"); !strings.HasPrefix(got, filepath.Join(dir, "iroh", "orgs")+string(filepath.Separator)) || strings.Count(got, "..") > 0 {
		t.Errorf("org name escaped the key dir: %q", got)
	}
}

func TestLoadOrCreateIdentityKeepsAnExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := OrgIdentityPath(dir, "acme")
	first, err := LoadOrCreateIdentity(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := key.GenerateSecretKey()
	if err != nil {
		t.Fatal(err)
	}
	// Saving over a key that is already there keeps the one on disk: it may
	// already be registered.
	got, err := writeIdentity(dir, path, other)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes() != first.Bytes() {
		t.Error("writeIdentity replaced a key that was already on disk")
	}
}

func TestReadIdentityMissing(t *testing.T) {
	_, ok, err := ReadIdentity(filepath.Join(t.TempDir(), "nope"))
	if err != nil || ok {
		t.Fatalf("missing identity: ok=%v err=%v, want false, nil", ok, err)
	}
}
