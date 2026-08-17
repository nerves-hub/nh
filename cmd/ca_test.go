package cmd

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/pki"
)

// parseCertFile reads and parses a PEM certificate file.
func parseCertFile(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	block, _ := pem.Decode(pemData)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("certificate PEM block: %+v", block)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}
	return cert
}

// caFileGlob returns the single timestamp-named CA file with the given suffix.
func caFileGlob(t *testing.T, dir, suffix string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, "*-"+suffix+".pem"))
	if len(matches) != 1 {
		t.Fatalf("want exactly one *-%s.pem in %s, got %v", suffix, dir, matches)
	}
	return matches[0]
}

func TestCAGenerate(t *testing.T) {
	dataDir := t.TempDir()

	// --name sets the common name; files are named by timestamp.
	out, err := execCmd(t, "",
		"ca", "generate", "--name", "myca",
		"--org", "acme", "--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	dir := filepath.Join(dataDir, "ca", "acme")
	keyPath := caFileGlob(t, dir, "key")
	certPath := caFileGlob(t, dir, "cert")

	// Files are timestamp-named, e.g. 2026-06-11-16-41-10-cert.pem.
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}-cert\.pem$`).MatchString(filepath.Base(certPath)) {
		t.Errorf("cert filename should be a timestamp, got %q", filepath.Base(certPath))
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key perm: want 0600, got %o", perm)
	}

	caCert := parseCertFile(t, certPath)
	if !caCert.IsCA {
		t.Error("certificate should be a CA")
	}
	if caCert.Subject.CommonName != "myca" || caCert.Subject.Organization[0] != "acme" {
		t.Errorf("subject: %+v", caCert.Subject)
	}

	if !strings.Contains(out, "Nothing was uploaded") || !strings.Contains(out, "nh ca upload") {
		t.Errorf("output should point at ca upload, got %q", out)
	}
}

func TestCAGenerateDefaultsNameToOrg(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := execCmd(t, "",
		"ca", "generate",
		"--org", "acme", "--data-dir", dataDir,
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	caCert := parseCertFile(t, caFileGlob(t, filepath.Join(dataDir, "ca", "acme"), "cert"))
	if caCert.Subject.CommonName != "acme" {
		t.Errorf("CN should default to the org name, got %q", caCert.Subject.CommonName)
	}
}

// caUploadCapture records what an upload sent.
type caUploadCapture struct {
	tokenFetched bool
	cert         string
	description  string
	verification string
}

// caUploadServer issues token and accepts a CA registration, capturing the
// request. It distinguishes the two endpoints by path.
func caUploadServer(t *testing.T, token string) (*httptest.Server, *caUploadCapture) {
	t.Helper()
	cap := &caUploadCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/verification_token"):
			cap.tokenFetched = true
			_, _ = w.Write([]byte(`{"data":{"verification_token":"` + token + `"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ca_certificates"):
			var body struct {
				Cert             string `json:"cert"`
				Description      string `json:"description"`
				VerificationCert string `json:"verification_cert"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			cap.cert, cap.description, cap.verification = body.Cert, body.Description, body.VerificationCert
			_, _ = w.Write([]byte(`{"data":{"serial":"99887766","description":"` + body.Description + `"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// assertVerificationCert decodes the base64 verification cert and checks it is
// signed by caCert and carries the expected token SAN.
func assertVerificationCert(t *testing.T, b64 string, caCert *x509.Certificate, token string) {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("verification cert is not base64: %v", err)
	}
	block, _ := pem.Decode(der)
	if block == nil {
		t.Fatal("verification cert is not PEM")
	}
	vcert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing verification cert: %v", err)
	}
	if err := vcert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("verification cert is not signed by the CA: %v", err)
	}
	want := "urn:nerveshub:verify:" + token
	if len(vcert.URIs) != 1 || vcert.URIs[0].String() != want {
		t.Errorf("verification SAN: got %v, want %s", vcert.URIs, want)
	}
}

func TestCAUploadByName(t *testing.T) {
	const token = "verify-token-123"
	srv, cap := caUploadServer(t, token)

	dataDir := t.TempDir()
	caCert := generateTestCA(t, dataDir, "acme", "myca")
	certPEM, _ := os.ReadFile(filepath.Join(dataDir, "ca", "acme", "myca-cert.pem"))

	out, err := execCmd(t, "",
		"ca", "upload", "myca", "--description", "CI CA",
		"--org", "acme", "--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !cap.tokenFetched {
		t.Error("a verification token should have been fetched")
	}
	if decoded, _ := base64.StdEncoding.DecodeString(cap.cert); string(decoded) != string(certPEM) {
		t.Error("uploaded cert should be base64 of the CA PEM")
	}
	if cap.description != "CI CA" {
		t.Errorf("description: got %q", cap.description)
	}
	assertVerificationCert(t, cap.verification, caCert, token)
	if !strings.Contains(out, "Registered CA certificate 05:F4:2A:96 with acme") {
		t.Errorf("output: %q", out)
	}
}

func TestCAUploadExternalCertAndKey(t *testing.T) {
	const token = "ext-token"
	srv, cap := caUploadServer(t, token)

	// A CA whose files live outside the data dir.
	dir := t.TempDir()
	caKeyPEM, caCertPEM, err := pki.GenerateCA("acme", "external-ca", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "rootCA.crt")
	keyPath := filepath.Join(dir, "rootCA.key")
	_ = os.WriteFile(certPath, caCertPEM, 0o644)
	_ = os.WriteFile(keyPath, caKeyPEM, 0o600)
	caCert := parseCertFile(t, certPath)

	if _, err := execCmd(t, "",
		"ca", "upload", "--cert", certPath, "--key", keyPath, "--description", "external",
		"--org", "acme", "--data-dir", t.TempDir(),
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertVerificationCert(t, cap.verification, caCert, token)
}

func TestCAUploadWithoutDescription(t *testing.T) {
	const token = "no-desc-token"
	srv, cap := caUploadServer(t, token)

	dataDir := t.TempDir()
	caCert := generateTestCA(t, dataDir, "acme", "myca")

	// --description is optional.
	if _, err := execCmd(t, "",
		"ca", "upload", "myca",
		"--org", "acme", "--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cap.description != "" {
		t.Errorf("description should be empty, got %q", cap.description)
	}
	assertVerificationCert(t, cap.verification, caCert, token)
}

func TestCAUploadConflictingInputs(t *testing.T) {
	_, err := execCmd(t, "",
		"ca", "upload", "myca", "--cert", "c.pem", "--key", "k.pem", "--description", "x",
		"--org", "acme", "--data-dir", t.TempDir(), "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestCAUploadNeedsInput(t *testing.T) {
	_, err := execCmd(t, "",
		"ca", "upload", "--description", "x",
		"--org", "acme", "--data-dir", t.TempDir(), "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "provide a CA name") {
		t.Errorf("expected missing-input error, got %v", err)
	}
}

func TestCAUploadRejectsNonCA(t *testing.T) {
	leafPEM := selfSignedCertPEM(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "leaf.pem")
	keyPath := filepath.Join(dir, "leaf.key")
	_ = os.WriteFile(certPath, leafPEM, 0o644)
	_ = os.WriteFile(keyPath, []byte("unused"), 0o600)

	_, err := execCmd(t, "",
		"ca", "upload", "--cert", certPath, "--key", keyPath, "--description", "nope",
		"--org", "acme", "--data-dir", t.TempDir(), "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not a CA certificate") {
		t.Errorf("expected non-CA rejection, got %v", err)
	}
}

func TestCAGenerateUpload(t *testing.T) {
	const token = "gen-token"
	srv, cap := caUploadServer(t, token)

	dataDir := t.TempDir()
	out, err := execCmd(t, "",
		"ca", "generate", "--name", "myca", "--upload", "--description", "Prod CA",
		"--org", "acme", "--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The CA was saved locally and registered.
	caCert := parseCertFile(t, caFileGlob(t, filepath.Join(dataDir, "ca", "acme"), "cert"))
	if !cap.tokenFetched || cap.cert == "" {
		t.Error("generate --upload should fetch a token and register the CA")
	}
	assertVerificationCert(t, cap.verification, caCert, token)
	if !strings.Contains(out, "Generated CA myca") || !strings.Contains(out, "Registered CA certificate") {
		t.Errorf("output should report both generate and register, got %q", out)
	}
}

func TestCAGenerateDescriptionRequiresUpload(t *testing.T) {
	_, err := execCmd(t, "",
		"ca", "generate", "--name", "myca", "--description", "x",
		"--org", "acme", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "--description requires --upload") {
		t.Errorf("expected description-requires-upload error, got %v", err)
	}
}

func TestCAList(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"serial":"255","description":"prod CA","not_before":"2026-01-02 00:00:00Z","not_after":"2027-01-02 00:00:00Z"},
			{"serial":"65535","description":"","not_before":"2026-03-04T09:00:00","not_after":"2026-09-04T09:00:00"}
		]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"ca", "list", "--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/ca_certificates" {
		t.Errorf("path: got %q", gotPath)
	}
	// Serials are displayed as hex.
	for _, want := range []string{"SERIAL", "DESCRIPTION", "FF", "FF:FF", "prod CA", "2026-01-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestCAListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "", "ca", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No CA certificates found for acme") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestCAShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"99887766","description":"prod CA","not_before":"2026-01-02 00:00:00Z","not_after":"2027-01-02 00:00:00Z"}}`))
	}))
	defer srv.Close()

	// A displayed hex serial is normalized back to decimal for the path.
	out, err := execCmd(t, "",
		"ca", "show", "05:F4:2A:96", "--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/ca_certificates/99887766" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Serial:", "05:F4:2A:96", "Description:", "prod CA", "Not before:", "2026-01-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q, got:\n%s", want, out)
		}
	}
}

func TestCADelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"ca", "delete", "FF", "--yes", "--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/ca_certificates/255" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Deleted CA certificate FF from acme") {
		t.Errorf("output: %q", out)
	}
}

func TestCADeleteAbortsWithoutConfirmation(t *testing.T) {
	_, err := execCmd(t, "",
		"ca", "delete", "FF", "--org", "acme",
		"--non-interactive", "--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "without confirmation") {
		t.Errorf("expected confirmation guard, got %v", err)
	}
}
