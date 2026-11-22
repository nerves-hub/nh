package cmd

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/pki"
)

// keyServer is a fake POST /api/orgs/{org}/keys that records the raw request
// body and echoes it back as KeyJSON does.
type keyServer struct {
	*httptest.Server
	requests int
	method   string
	path     string
	body     map[string]any
}

func newKeyServer(t *testing.T) *keyServer {
	t.Helper()
	ks := &keyServer{}
	ks.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ks.requests++
		ks.method, ks.path = r.Method, r.URL.Path
		ks.body = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&ks.body)
		scheme, _ := ks.body["scheme"].(string)
		if scheme == "" {
			scheme = "ed25519"
		}
		data := map[string]any{"name": ks.body["name"], "key": ks.body["key"], "scheme": scheme}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(ks.Close)
	return ks
}

func TestKeyCreateDefaultSendsNoScheme(t *testing.T) {
	ks := newKeyServer(t)
	if _, err := execCmd(t, "", "key", "create", "ci", "--org", "acme", "--data-dir", t.TempDir(), "--uri", ks.URL, "--token", "tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ks.body["scheme"]; ok {
		t.Errorf("default create must not send a scheme, body: %v", ks.body)
	}
}

func TestKeyCreateSecureBootV2RSA(t *testing.T) {
	ks := newKeyServer(t)
	dir := t.TempDir()
	out, err := execCmd(t, "",
		"key", "create", "esp", "--scheme", "secure_boot_v2_rsa",
		"--org", "acme", "--data-dir", dir, "--uri", ks.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if ks.method != http.MethodPost || ks.path != "/api/orgs/acme/keys" {
		t.Errorf("request: %s %s", ks.method, ks.path)
	}
	if ks.body["name"] != "esp" || ks.body["scheme"] != "secure_boot_v2_rsa" {
		t.Errorf("body: %v", ks.body)
	}
	uploaded, _ := ks.body["key"].(string)
	if !strings.HasPrefix(uploaded, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("uploaded key is not a PEM public key: %q", uploaded)
	}
	if err := pki.ValidatePublicKey(pki.SchemeSecureBootV2RSA, uploaded); err != nil {
		t.Errorf("uploaded key fails the server's check: %v", err)
	}

	// The private key lives at exactly <data-dir>/keys/<org>/<name>.rsa.pem:
	// ESP-IDF firmware signing reads it from there.
	privPath := filepath.Join(dir, "keys", "acme", "esp.rsa.pem")
	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key perm: want 0600, got %o", perm)
	}
	privPEM, _ := os.ReadFile(privPath)
	if !strings.HasPrefix(string(privPEM), "-----BEGIN RSA PRIVATE KEY-----\n") {
		t.Errorf("private key should be an unencrypted PKCS#1 PEM, got %.40q", privPEM)
	}
	priv, err := pki.ParseRSAPrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	if priv.N.BitLen() != 3072 {
		t.Errorf("key size %d", priv.N.BitLen())
	}
	wantPub, _ := pki.EncodeRSAPublicKeyPEM(&priv.PublicKey)
	if wantPub != uploaded {
		t.Error("uploaded public key does not match the saved private key")
	}
	pubFile, _ := os.ReadFile(filepath.Join(dir, "keys", "acme", "esp.rsa.pub.pem"))
	if string(pubFile) != uploaded {
		t.Error("public key file does not match the upload")
	}
	if _, err := os.Stat(filepath.Join(dir, "keys", "acme", "esp.priv")); !os.IsNotExist(err) {
		t.Error("an RSA key must not write an ed25519 .priv")
	}
	for _, want := range []string{"secure_boot_v2_rsa", privPath, "eFuse digest: " + pki.SecureBootV2KeyDigest(&priv.PublicKey), "not encrypted"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestKeyCreateRSARefusesPassword(t *testing.T) {
	ks := newKeyServer(t)
	_, err := execCmd(t, "", "key", "create", "esp", "--scheme", "secure_boot_v2_rsa", "--password", "x",
		"--org", "acme", "--data-dir", t.TempDir(), "--uri", ks.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "--password is not supported") {
		t.Errorf("got %v", err)
	}
	if ks.requests != 0 {
		t.Error("no request expected")
	}
}

func TestKeyCreateRefusesX509AndUnknownSchemes(t *testing.T) {
	for scheme, want := range map[string]string{"x509_certificate": "nh key add", "rsa": "unknown signing key scheme"} {
		_, err := execCmd(t, "", "key", "create", "k", "--scheme", scheme, "--org", "acme", "--data-dir", t.TempDir(), "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", scheme, err)
		}
	}
}

func TestKeyCreateNameTakenByOtherScheme(t *testing.T) {
	ks := newKeyServer(t)
	dir := t.TempDir()
	if _, err := execCmd(t, "", "key", "create", "ci", "--org", "acme", "--data-dir", dir, "--uri", ks.URL, "--token", "tok"); err != nil {
		t.Fatal(err)
	}
	_, err := execCmd(t, "", "key", "create", "ci", "--scheme", "secure_boot_v2_rsa", "--org", "acme", "--data-dir", dir, "--uri", ks.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "ci.priv") {
		t.Errorf("an ed25519 key named ci should block an RSA one, got %v", err)
	}
	if ks.requests != 1 {
		t.Errorf("requests: %d", ks.requests)
	}
}

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeyAddEd25519(t *testing.T) {
	ks := newKeyServer(t)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	path := writeTempFile(t, "fwup-key.pub", []byte(pki.PublicKeyString(pub)+"\n"))

	out, err := execCmd(t, "", "key", "add", "fw", "--public-key", path, "--org", "acme", "--uri", ks.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if ks.path != "/api/orgs/acme/keys" || ks.body["key"] != pki.PublicKeyString(pub) || ks.body["scheme"] != "ed25519" || ks.body["name"] != "fw" {
		t.Errorf("request %s body %v", ks.path, ks.body)
	}
	if !strings.Contains(out, "Added ed25519 signing key fw to acme") {
		t.Errorf("output: %s", out)
	}
}

func TestKeyAddRSAPrivateKeySendsOnlyPublicHalf(t *testing.T) {
	ks := newKeyServer(t)
	priv, err := pki.GenerateSecureBootV2Key()
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempFile(t, "secure_boot_signing_key.pem", pki.EncodeRSAPrivateKeyPEM(priv))

	out, err := execCmd(t, "", "key", "add", "esp", "--public-key", path, "--org", "acme", "--uri", ks.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	wantPub, _ := pki.EncodeRSAPublicKeyPEM(&priv.PublicKey)
	if ks.body["scheme"] != "secure_boot_v2_rsa" || ks.body["key"] != wantPub {
		t.Errorf("body: %v", ks.body)
	}
	if strings.Contains(ks.body["key"].(string), "PRIVATE") {
		t.Fatal("private key sent to the server")
	}
	if !strings.Contains(out, "eFuse digest: "+pki.SecureBootV2KeyDigest(&priv.PublicKey)) {
		t.Errorf("output: %s", out)
	}
}

func TestKeyAddCertificateFromStdin(t *testing.T) {
	ks := newKeyServer(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "rauc"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	out, err := execCmd(t, string(cert), "key", "add", "rauc", "--public-key", "-", "--scheme", "x509_certificate", "--org", "acme", "--uri", ks.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if ks.body["scheme"] != "x509_certificate" || ks.body["key"] != string(cert) {
		t.Errorf("body: %v", ks.body)
	}
	if !strings.Contains(out, "SHA-256 fingerprint: ") {
		t.Errorf("output: %s", out)
	}
}

func TestKeyAddValidatesBeforeSending(t *testing.T) {
	ks := newKeyServer(t)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	edPath := writeTempFile(t, "k.pub", []byte(pki.PublicKeyString(pub)))
	cases := []struct {
		args []string
		want string
	}{
		// An ed25519 key passed as RSA.
		{[]string{"--public-key", edPath, "--scheme", "secure_boot_v2_rsa"}, "expected a PEM-encoded RSA public or private key"},
		{[]string{"--public-key", writeTempFile(t, "junk", []byte("junk"))}, "valid Ed25519 public key"},
		{[]string{"--public-key", edPath, "--scheme", "nope"}, "unknown signing key scheme"},
		{[]string{"--public-key", filepath.Join(t.TempDir(), "missing")}, "reading key file"},
		{[]string{}, "public-key"},
	}
	for _, c := range cases {
		args := append([]string{"key", "add", "k", "--org", "acme", "--uri", ks.URL, "--token", "tok"}, c.args...)
		_, err := execCmd(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: got %v, want %q", c.args, err, c.want)
		}
	}
	if ks.requests != 0 {
		t.Errorf("invalid keys must not be sent, got %d requests", ks.requests)
	}
}

func TestKeyAddExplainsOldServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":{"key":["invalid key, please check this is a valid Ed25519 public key"]}}`))
	}))
	defer srv.Close()
	priv, _ := pki.GenerateSecureBootV2Key()
	path := writeTempFile(t, "k.pem", pki.EncodeRSAPrivateKeyPEM(priv))
	_, err := execCmd(t, "", "key", "add", "esp", "--public-key", path, "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "predates signing key schemes") {
		t.Errorf("got %v", err)
	}
}

const rsaPublicPEM = "-----BEGIN PUBLIC KEY-----\nMIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEA9ycdCE+qOLh/Xp/kbN4E\n7CKTu7YyLJcKME8j7YLEHUdknQPIoyFhAHisnTqfh99/Py0iH1XQHwoY8Qga/y2h\nyraDsSusvXGOyYowcgmD2WCvQYZ0Ym6EJ6Gd9TEu36+1dFe9mBKy/FSwSOoNQcAQ\n8UYVZ2lKWYEMNsq1r1OPJbLt+eJ0w0Bah9EG+6gJ4KEtnDOyZA8C20Ya/lXDoAYo\nP1B2kAgq7f6lkEOcZ7QgmOjhQZkYSfXZt300C/ZfPZafg7+E6k4QrsOYt2Libg1h\nmjYSxrpZix9nGYMehDY+aKWfOdkk8if4oMyYxPXQHrmMatzhqDyiYrC76IgQu2o4\nqDAXEXv5A9Y0E0DXmEZIlwjSwUw5ID+CuPyiKzHPvmKCXKtcycuAYQKZ8fS6Me1B\nGRYmzxXV40fqvRpn2HnsjFAsLBBWsJ2MEr8ppQZtcThT0G0v0BlHXSuRoGjCRbWo\nAvAmYVnSGg2sJQ7iTqqgSZAFGDTncPlV0qrhg4UNrn9LAgMBAAE=\n-----END PUBLIC KEY-----\n"

const rsaPublicDigest = "32b6f7fd427fa71fa2139b33f0c68ef3eb57f5f17ca05ff7d9787a772e40b87d"

func schemeKeysServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esp := map[string]any{"name": "esp", "key": rsaPublicPEM, "scheme": "secure_boot_v2_rsa"}
		var resp any
		switch r.URL.Path {
		case "/api/orgs/acme/keys":
			resp = map[string]any{"data": []any{
				map[string]any{"name": "fw", "key": "ntpubkeyAAA", "scheme": "ed25519"},
				esp,
				map[string]any{"name": "legacy", "key": "ntpubkeyBBB"}, // server without schemes
			}}
		case "/api/orgs/acme/keys/esp":
			resp = map[string]any{"data": esp}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestKeyListShowsSchemes(t *testing.T) {
	srv := schemeKeysServer(t)
	out, err := execCmd(t, "", "key", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("a PEM key must stay on one row:\n%s", out)
	}
	for i, want := range [][]string{{"NAME", "SCHEME", "KEY"}, {"fw", "ed25519", "ntpubkeyAAA"}, {"esp", "secure_boot_v2_rsa", rsaPublicDigest}, {"legacy", "ed25519", "ntpubkeyBBB"}} {
		if got := strings.Fields(lines[i]); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("row %d: got %q, want %q", i, got, want)
		}
	}

	out, err = execCmd(t, "", "key", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var keys []map[string]any
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if keys[1]["scheme"] != "secure_boot_v2_rsa" || keys[1]["key"] != rsaPublicPEM {
		t.Errorf("json: %v", keys[1])
	}
}

func TestKeyShowRSA(t *testing.T) {
	srv := schemeKeysServer(t)
	out, err := execCmd(t, "", "key", "show", "esp", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Scheme:", "secure_boot_v2_rsa", "eFuse digest:", rsaPublicDigest, "Public key:\n" + rsaPublicPEM} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	out, err = execCmd(t, "", "key", "show", "esp", "--org", "acme", "--uri", srv.URL, "--token", "tok", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"scheme": "secure_boot_v2_rsa"`) {
		t.Errorf("json: %s", out)
	}
}

func TestKeyDeleteListsLocalFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/orgs/acme/keys/esp" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	dir := t.TempDir()
	priv := pki.RSAPrivateKeyPath(dir, "acme", "esp")
	if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(priv, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := execCmd(t, "", "key", "delete", "esp", "--yes", "--org", "acme", "--data-dir", dir, "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "left in place") || !strings.Contains(out, priv) {
		t.Errorf("output: %s", out)
	}
	if _, err := os.Stat(priv); err != nil {
		t.Error("delete must not remove local files")
	}
}

func TestMigrateSkipsNameTakenByRSAKey(t *testing.T) {
	from := writeLegacyDir(t)
	data := t.TempDir()
	rsaPath := pki.RSAPrivateKeyPath(data, "acme", "dev")
	if err := os.MkdirAll(filepath.Dir(rsaPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rsaPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data, "--force")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "0 imported, 1 skipped") || !strings.Contains(out, "secure_boot_v2_rsa key with this name") {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "keys", "acme", "dev.priv")); !os.IsNotExist(err) {
		t.Error("an ed25519 key must not be written beside an RSA key of the same name")
	}
}

func TestKeyCreateChecksLoginBeforeWriting(t *testing.T) {
	t.Setenv("NERVES_HUB_TOKEN", "")
	dir := t.TempDir()
	_, err := execCmd(t, "", "key", "create", "esp", "--scheme", "secure_boot_v2_rsa", "--org", "acme", "--data-dir", dir)
	if err == nil {
		t.Fatal("expected an auth error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "keys", "acme")); !os.IsNotExist(statErr) {
		t.Errorf("no key files should be written without a login (err: %v)", err)
	}
}

func TestKeyAddStdinErrorNamesStdin(t *testing.T) {
	_, err := execCmd(t, "junk", "key", "add", "k", "--public-key", "-", "--org", "acme", "--token", "tok", "--uri", "http://127.0.0.1:1")
	if err == nil || !strings.HasPrefix(err.Error(), "stdin is not a valid ed25519 key") {
		t.Errorf("got %v", err)
	}
}

func TestKeyShowUnparseableRSAHasNoDigestRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"esp","key":"-----BEGIN PUBLIC KEY-----\nzz\n-----END PUBLIC KEY-----\n","scheme":"secure_boot_v2_rsa"}}`))
	}))
	defer srv.Close()
	out, err := execCmd(t, "", "key", "show", "esp", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "eFuse digest") || !strings.Contains(out, "Public key:\n-----BEGIN PUBLIC KEY-----") {
		t.Errorf("output:\n%s", out)
	}
}
