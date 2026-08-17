package cmd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
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

// Serials are decimal strings on the wire, as the API returns them.
const certsResponse = `{"data":[
	{"serial":"255","not_before":"2026-01-02 00:00:00Z","not_after":"2027-01-02 00:00:00Z"},
	{"serial":"65535","not_before":"2026-03-04T09:00:00","not_after":"2026-09-04T09:00:00"}
]}`

func TestDeviceCertificates(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(certsResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "certificates", "list", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates" {
		t.Errorf("path: got %q", gotPath)
	}
	// Serials are displayed as hex.
	for _, want := range []string{"SERIAL", "NOT BEFORE", "NOT AFTER", "FF", "FF:FF", "2026-01-02", "2027-01-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceCertificatesAlias(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// The "certs" alias resolves to the same command group.
	out, err := execCmd(t, "",
		"device", "certs", "list", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates" {
		t.Errorf("alias path: got %q", gotPath)
	}
	if !strings.Contains(out, "No certificates found for device dev-001") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestDeviceCertificatesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(certsResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "certificates", "list", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// JSON output keeps the raw decimal serial from the API.
	if !strings.Contains(out, `"serial": "255"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

func TestDeviceCertificatesMissingIdentifier(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "certificates", "list",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Device identifier missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestFormatSerial(t *testing.T) {
	cases := map[string]string{
		"255":      "FF",
		"65535":    "FF:FF",
		"99887766": "05:F4:2A:96",
		"0":        "00",
		"":         "",         // not decimal: unchanged
		"AA:BB:01": "AA:BB:01", // already hex: unchanged
	}
	for in, want := range cases {
		if got := formatSerial(in); got != want {
			t.Errorf("formatSerial(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSerial(t *testing.T) {
	cases := map[string]string{
		"99887766":    "99887766", // decimal passes through
		"05:F4:2A:96": "99887766", // displayed hex converts back
		"0xFF":        "255",
		"FF":          "255", // bare hex with letters
		"AA:BB:01":    "11188993",
		"not-hex":     "not-hex", // unrecognizable: unchanged
		"":            "",
	}
	for in, want := range cases {
		if got := normalizeSerial(in); got != want {
			t.Errorf("normalizeSerial(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeviceCertShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"99887766","not_before":"2026-01-02 00:00:00Z","not_after":"2027-01-02 00:00:00Z"}}`))
	}))
	defer srv.Close()

	// A decimal serial passes through to the API unchanged.
	out, err := execCmd(t, "",
		"device", "certificates", "show", "dev-001", "99887766",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates/99887766" {
		t.Errorf("path: got %q", gotPath)
	}
	// ... and is displayed as hex.
	for _, want := range []string{"Serial:", "05:F4:2A:96", "Not before:", "2026-01-02", "Not after:", "2027-01-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceCertGenerate(t *testing.T) {
	dataDir := t.TempDir()

	// Purely local: no server, no token.
	out, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--org", "acme", "--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	certDir := filepath.Join(dataDir, "certificates", "acme")
	keyPath := certFileGlob(t, certDir, "dev-001", "key")
	csrPath := certFileGlob(t, certDir, "dev-001", "csr")

	// Private key: 0600, EC on P-256 (secp256r1).
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key perm: want 0600, got %o", perm)
	}
	keyPEM, _ := os.ReadFile(keyPath)
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		t.Fatalf("key PEM block: %+v", keyBlock)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing key: %v", err)
	}
	if key.Curve != elliptic.P256() {
		t.Errorf("curve: got %v, want P-256", key.Curve.Params().Name)
	}

	// CSR: CN = identifier, O = org, valid signature, matching key.
	csrPEM, _ := os.ReadFile(csrPath)
	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil || csrBlock.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("csr PEM block: %+v", csrBlock)
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing csr: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("csr signature: %v", err)
	}
	if csr.Subject.CommonName != "dev-001" {
		t.Errorf("CN: got %q", csr.Subject.CommonName)
	}
	if len(csr.Subject.Organization) != 1 || csr.Subject.Organization[0] != "acme" {
		t.Errorf("O: got %v", csr.Subject.Organization)
	}
	if pub, ok := csr.PublicKey.(*ecdsa.PublicKey); !ok || !pub.Equal(&key.PublicKey) {
		t.Error("csr public key does not match the generated private key")
	}

	if !strings.Contains(out, "Nothing was uploaded") {
		t.Errorf("output should make clear nothing was uploaded, got %q", out)
	}
}

func TestDeviceCertGenerateRefusesSameMinuteCollision(t *testing.T) {
	dataDir := t.TempDir()
	args := []string{"device", "certificates", "generate", "dev-001", "--org", "acme", "--data-dir", dataDir}

	// Filenames are timestamped to the minute, so two runs in the same minute
	// collide and the second is refused rather than overwriting the first.
	// (Across minutes they get distinct names; the back-to-back calls here are
	// microseconds apart, so they share a minute.)
	if _, err := execCmd(t, "", args...); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	_, err := execCmd(t, "", args...)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second generate in the same minute should refuse, got %v", err)
	}
}

func TestParseValidFor(t *testing.T) {
	ok := map[string]time.Duration{
		"31y":  31 * 365 * 24 * time.Hour,
		"90d":  90 * 24 * time.Hour,
		"12h":  12 * time.Hour,
		"1.5h": 90 * time.Minute,
	}
	for in, want := range ok {
		got, err := parseValidFor(in)
		if err != nil {
			t.Errorf("parseValidFor(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseValidFor(%q) = %v, want %v", in, got, want)
		}
	}

	for _, bad := range []string{"", "abc", "31x", "-5d", "0d", "-12h"} {
		if _, err := parseValidFor(bad); err == nil {
			t.Errorf("parseValidFor(%q): expected error", bad)
		}
	}
}

// certFileGlob returns the single file in dir matching <identifier>-*-<suffix>,
// e.g. the timestamped "dev-001-202606101745-cert.pem". It fails unless exactly
// one matches.
func certFileGlob(t *testing.T, dir, identifier, suffix string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, identifier+"-*-"+suffix+".pem"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one %s file for %s, got %v", suffix, identifier, matches)
	}
	return matches[0]
}

// certFileAbsent asserts no <identifier>-*-<suffix> file exists in dir.
func certFileAbsent(t *testing.T, dir, identifier, suffix string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, identifier+"-*-"+suffix+".pem"))
	if len(matches) != 0 {
		t.Errorf("expected no %s file for %s, found %v", suffix, identifier, matches)
	}
}

// generateTestCA runs `ca generate` into dataDir and returns the CA cert.
// generateTestCA writes a CA's key and cert to the data dir under a fixed
// <name> prefix, so by-name lookups (`ca upload <name>`, `device certificates
// generate --ca <name>`) resolve. Real `ca generate` names files by timestamp;
// the name is just the on-disk prefix the loader looks up.
func generateTestCA(t *testing.T, dataDir, org, name string) *x509.Certificate {
	t.Helper()
	keyPEM, certPEM, err := pki.GenerateCA(org, name, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	dir := filepath.Join(dataDir, "ca", org)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+"-key.pem"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+"-cert.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}
	return caCert
}

func TestDeviceCertGenerateWithCA(t *testing.T) {
	dataDir := t.TempDir()
	caCert := generateTestCA(t, dataDir, "acme", "myca")

	out, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--ca", "myca", "--valid-for", "90d",
		"--org", "acme", "--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dir := filepath.Join(dataDir, "certificates", "acme")
	certPEM, err := os.ReadFile(certFileGlob(t, dir, "dev-001", "cert"))
	if err != nil {
		t.Fatalf("reading certificate: %v", err)
	}
	// No CSR is written when signing with a CA.
	certFileAbsent(t, dir, "dev-001", "csr")

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("cert PEM block: %+v", block)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing cert: %v", err)
	}
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("device certificate is not signed by the CA: %v", err)
	}
	if cert.Subject.CommonName != "dev-001" || cert.Subject.Organization[0] != "acme" {
		t.Errorf("subject: %+v", cert.Subject)
	}
	if span := cert.NotAfter.Sub(cert.NotBefore); span != 90*24*time.Hour+time.Hour {
		t.Errorf("validity span: got %v", span)
	}

	if !strings.Contains(out, "signed by CA myca") {
		t.Errorf("output should name the CA, got %q", out)
	}
	if !strings.Contains(out, "Nothing was uploaded") {
		t.Errorf("output should note nothing was uploaded, got %q", out)
	}
}

func TestDeviceCertGenerateMissingCA(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--ca", "ghost",
		"--org", "acme", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), `CA "ghost" not found`) {
		t.Errorf("expected missing-CA error, got %v", err)
	}
}

func TestDeviceCertGenerateSelfSigned(t *testing.T) {
	dataDir := t.TempDir()

	out, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--self-signed", "--valid-for", "90d",
		"--org", "acme", "--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dir := filepath.Join(dataDir, "certificates", "acme")
	cert := parseCertFile(t, certFileGlob(t, dir, "dev-001", "cert"))

	// A key and cert are written; no CSR, and no CA is involved.
	certFileGlob(t, dir, "dev-001", "key")
	certFileAbsent(t, dir, "dev-001", "csr")
	if entries, _ := filepath.Glob(filepath.Join(dataDir, "ca", "*", "*")); len(entries) != 0 {
		t.Errorf("self-signed generation must not create a CA, found %v", entries)
	}

	if cert.Subject.CommonName != "dev-001" || cert.Subject.Organization[0] != "acme" {
		t.Errorf("subject: %+v", cert.Subject)
	}
	if cert.IsCA {
		t.Error("device certificate must not be a CA")
	}

	// Self-signed: self-issued, and verifiable with its own public key.
	if !bytes.Equal(cert.RawIssuer, cert.RawSubject) {
		t.Error("a self-signed certificate should be self-issued")
	}
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		t.Errorf("certificate is not self-signed: %v", err)
	}

	// NervesHub requires SKI and AKI; Go omits them on a self-signed cert
	// unless set explicitly, and on a self-signed cert the AKI equals the SKI.
	if len(cert.SubjectKeyId) == 0 {
		t.Error("missing SubjectKeyId extension")
	}
	if len(cert.AuthorityKeyId) == 0 {
		t.Error("missing AuthorityKeyId extension")
	}
	if !bytes.Equal(cert.SubjectKeyId, cert.AuthorityKeyId) {
		t.Error("on a self-signed certificate the AKI should equal the SKI")
	}

	if span := cert.NotAfter.Sub(cert.NotBefore); span != 90*24*time.Hour+time.Hour {
		t.Errorf("validity span: got %v", span)
	}
	if !strings.Contains(out, "(self-signed)") {
		t.Errorf("output should mention self-signed, got %q", out)
	}
}

func TestDeviceCertGenerateSelfSignedUpload(t *testing.T) {
	var gotPath, gotCert string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Cert string `json:"cert"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCert = body.Cert
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"255"}}`))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	out, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--self-signed", "--upload",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates" {
		t.Errorf("path: got %q", gotPath)
	}
	certPEM, err := os.ReadFile(certFileGlob(t, filepath.Join(dataDir, "certificates", "acme"), "dev-001", "cert"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotCert)
	if err != nil || string(decoded) != string(certPEM) {
		t.Errorf("uploaded cert should be base64 of the saved PEM (decode err %v)", err)
	}
	if !strings.Contains(out, "Uploaded certificate FF for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceCertGenerateCAAndSelfSignedConflict(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--ca", "myca", "--self-signed",
		"--org", "acme", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "only one of --ca or --self-signed") {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestDeviceCertGenerateWithCAUpload(t *testing.T) {
	var gotPath, gotCert string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Cert string `json:"cert"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCert = body.Cert
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"AA:BB:09"}}`))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	generateTestCA(t, dataDir, "acme", "myca")

	out, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--ca", "myca", "--upload",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates" {
		t.Errorf("path: got %q", gotPath)
	}

	// The registered certificate is exactly the one written to disk.
	certPEM, err := os.ReadFile(certFileGlob(t, filepath.Join(dataDir, "certificates", "acme"), "dev-001", "cert"))
	if err != nil {
		t.Fatalf("reading certificate: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotCert)
	if err != nil || string(decoded) != string(certPEM) {
		t.Errorf("uploaded cert should be base64 of the saved PEM (decode err %v)", err)
	}

	if !strings.Contains(out, "Uploaded certificate AA:BB:09 for device dev-001") {
		t.Errorf("output: %q", out)
	}
	if strings.Contains(out, "Nothing was uploaded") {
		t.Errorf("output should not show the not-uploaded note, got %q", out)
	}
}

func TestDeviceCertGenerateFlagGuards(t *testing.T) {
	if _, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001", "--upload",
		"--org", "acme", "--data-dir", t.TempDir(),
	); err == nil || !strings.Contains(err.Error(), "--upload requires --ca") {
		t.Errorf("upload without ca: got %v", err)
	}

	if _, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001", "--valid-for", "90d",
		"--org", "acme", "--data-dir", t.TempDir(),
	); err == nil || !strings.Contains(err.Error(), "--valid-for requires --ca") {
		t.Errorf("valid-for without ca: got %v", err)
	}
}

func TestDeviceCertGenerateUploadFailsBeforeWriting(t *testing.T) {
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")

	dataDir := t.TempDir()
	generateTestCA(t, dataDir, "acme", "myca")

	_, err := execCmd(t, "",
		"device", "certificates", "generate", "dev-001",
		"--ca", "myca", "--upload",
		"--org", "acme", "--data-dir", dataDir,
		"--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Fatalf("expected missing-product error, got %v", err)
	}
	// The early failure must not leave key material behind.
	certFileAbsent(t, filepath.Join(dataDir, "certificates", "acme"), "dev-001", "key")
}

// selfSignedCertPEM builds a valid PEM certificate for upload tests.
func selfSignedCertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "dev-001"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestDeviceCertUpload(t *testing.T) {
	var gotMethod, gotPath, gotCert string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		var body struct {
			Cert string `json:"cert"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCert = body.Cert
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"AA:BB:03"}}`))
	}))
	defer srv.Close()

	certPEM := selfSignedCertPEM(t)
	path := filepath.Join(t.TempDir(), "device-cert.pem")
	if err := os.WriteFile(path, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "",
		"device", "certificates", "upload", "dev-001", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotCert)
	if err != nil || string(decoded) != string(certPEM) {
		t.Errorf("cert body should be base64 of the PEM (decode err %v)", err)
	}
	if !strings.Contains(out, "Uploaded certificate AA:BB:03 for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceCertUploadInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-cert.pem")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"device", "certificates", "upload", "dev-001", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no PEM data") {
		t.Errorf("expected PEM validation error, got %v", err)
	}
}

func TestDeviceCertDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// The hex form shown by `list` is accepted and converted back to the
	// decimal serial the API expects.
	out, err := execCmd(t, "",
		"device", "certificates", "delete", "dev-001", "05:F4:2A:96", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/certificates/99887766" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Deleted certificate 05:F4:2A:96 from device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceCertDeleteAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "\n",
		"device", "certificates", "delete", "dev-001", "AA:BB:01",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no delete should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}
