package cmd

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/pki"
)

func TestKeyCreate(t *testing.T) {
	var gotMethod, gotPath, gotName, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct{ Name, Key string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotName, gotKey = body.Name, body.Key
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"` + body.Name + `","key":"` + body.Key + `"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	out, err := execCmd(t, "",
		"key", "create", "ci",
		"--org", "acme",
		"--data-dir", dir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/keys" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotName != "ci" {
		t.Errorf("uploaded name: %q", gotName)
	}

	keyDir := filepath.Join(dir, "keys", "acme")
	privPath := filepath.Join(keyDir, "ci.priv")
	pubPath := filepath.Join(keyDir, "ci.pub")

	// Public key file matches what was uploaded.
	pubFile, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("reading public key: %v", err)
	}
	if strings.TrimSpace(string(pubFile)) != gotKey {
		t.Errorf("public key file %q != uploaded %q", strings.TrimSpace(string(pubFile)), gotKey)
	}

	// Private key file is 0600 and decrypts (no password) to a key whose public
	// half matches the uploaded public key.
	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key perm: want 0600, got %o", perm)
	}
	privFile, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := pki.DecryptPrivateKey(string(privFile), "")
	if err != nil {
		t.Fatalf("decrypting saved private key: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	if pki.PublicKeyString(pub) != gotKey {
		t.Errorf("saved private key does not match uploaded public key")
	}

	if !strings.Contains(out, "Created signing key ci in acme") || !strings.Contains(out, privPath) {
		t.Errorf("output: %q", out)
	}
}

func TestKeyCreateWithPassword(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, Key string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotKey = body.Key
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"ci","key":"` + body.Key + `"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, err := execCmd(t, "",
		"key", "create", "ci",
		"--org", "acme", "--password", "s3cret",
		"--data-dir", dir,
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	blob, _ := os.ReadFile(filepath.Join(dir, "keys", "acme", "ci.priv"))

	// Wrong password fails, correct password recovers the uploaded key.
	if _, err := pki.DecryptPrivateKey(string(blob), "wrong"); err == nil {
		t.Error("decrypt should fail with the wrong password")
	}
	priv, err := pki.DecryptPrivateKey(string(blob), "s3cret")
	if err != nil {
		t.Fatalf("decrypt with correct password: %v", err)
	}
	if pki.PublicKeyString(priv.Public().(ed25519.PublicKey)) != gotKey {
		t.Error("recovered key does not match uploaded public key")
	}
}

func TestKeyCreateRefusesExisting(t *testing.T) {
	var uploads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"ci","key":"k"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	args := []string{"key", "create", "ci", "--org", "acme", "--data-dir", dir, "--uri", srv.URL, "--token", "tok"}

	if _, err := execCmd(t, "", args...); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := execCmd(t, "", args...)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second create should refuse, got %v", err)
	}
	if uploads != 1 {
		t.Errorf("expected exactly one upload, got %d", uploads)
	}
}

func TestKeyCreateInvalidName(t *testing.T) {
	_, err := execCmd(t, "",
		"key", "create", "bad/name",
		"--org", "acme", "--data-dir", t.TempDir(),
		"--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid signing key name") {
		t.Errorf("expected invalid-name error, got %v", err)
	}
}
