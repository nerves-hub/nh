package cmd

import (
	"crypto/ed25519"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/pki"
)

// stubFwup replaces fwup with a script that records its arguments (one per
// line) and writes the input file plus a "SIGNED" marker to the output file.
func stubFwup(t *testing.T) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")

	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + argsFile + `"` + "\n" +
		`in=""` + "\n" + `out=""` + "\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -i) in=\"$2\"; shift 2 ;;\n" +
		"    -o) out=\"$2\"; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		`cat "$in" > "$out"` + "\n" +
		`printf 'SIGNED' >> "$out"` + "\n"

	path := filepath.Join(dir, "fwup")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	old := fwupBin
	fwupBin = path
	t.Cleanup(func() { fwupBin = old })
	return argsFile
}

// writeSigningKey generates an Ed25519 keypair and stores its encrypted
// private key where `firmware upload --key` looks for it.
func writeSigningKey(t *testing.T, dataDir, org, name, password string) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := pki.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := pki.EncryptPrivateKey(priv, password)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(dataDir, "keys", org)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".priv"), []byte(enc+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return priv
}

func signUploadServer(t *testing.T) (srv *httptest.Server, gotName, gotContent *string) {
	t.Helper()
	gotName, gotContent = new(string), new(string)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, hdr, err := r.FormFile("firmware")
		if err == nil {
			*gotName = hdr.Filename
			b, _ := io.ReadAll(f)
			*gotContent = string(b)
			f.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"uuid":"new-uuid","version":"1.4.0"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, gotName, gotContent
}

func TestFirmwareUploadSigned(t *testing.T) {
	argsFile := stubFwup(t)
	srv, gotName, gotContent := signUploadServer(t)

	dataDir := t.TempDir()
	priv := writeSigningKey(t, dataDir, "acme", "ci", "")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// fwup was invoked with the documented arguments and the decrypted key.
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fwup was not invoked: %v", err)
	}
	args := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(args) != 7 || args[0] != "--sign" || args[1] != "-i" || args[2] != fwPath ||
		args[3] != "-o" || args[5] != "--private-key" {
		t.Errorf("fwup args: %v", args)
	}
	if args[6] != pki.PrivateKeyString(priv) {
		t.Errorf("fwup received the wrong private key")
	}

	// The signed image was uploaded, under the original filename.
	if *gotContent != "FWDATASIGNED" {
		t.Errorf("uploaded content: got %q, want signed image", *gotContent)
	}
	if *gotName != "image.fw" {
		t.Errorf("uploaded filename: got %q", *gotName)
	}
	if !strings.Contains(out, "Signing image.fw with key ci") {
		t.Errorf("output should mention signing, got %q", out)
	}

	// The signed temp file is cleaned up.
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "nh-signed-*.fw")); len(matches) != 0 {
		t.Errorf("signed temp files left behind: %v", matches)
	}
}

func TestFirmwareUploadSignsByDefaultFromEnv(t *testing.T) {
	argsFile := stubFwup(t)
	srv, gotName, gotContent := signUploadServer(t)

	// No --key: the unencrypted key comes from the environment.
	_, priv, err := pki.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NERVES_HUB_PRIVATE_KEY", pki.PrivateKeyString(priv))

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "",
		"firmware", "upload", fwPath,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fwup was not invoked: %v", err)
	}
	if !strings.Contains(string(raw), pki.PrivateKeyString(priv)) {
		t.Error("fwup did not receive the env private key")
	}
	if *gotContent != "FWDATASIGNED" {
		t.Errorf("uploaded content: got %q, want signed image", *gotContent)
	}
	if *gotName != "image.fw" {
		t.Errorf("uploaded filename: got %q", *gotName)
	}
	if !strings.Contains(out, "Signing image.fw with key from NERVES_HUB_PRIVATE_KEY") {
		t.Errorf("output should mention the env key, got %q", out)
	}
}

func TestFirmwareUploadKeyFlagBeatsEnv(t *testing.T) {
	argsFile := stubFwup(t)
	srv, _, _ := signUploadServer(t)

	dataDir := t.TempDir()
	storedPriv := writeSigningKey(t, dataDir, "acme", "ci", "")

	_, envPriv, _ := pki.GenerateSigningKey()
	t.Setenv("NERVES_CLOUD_PRIVATE_KEY", pki.PrivateKeyString(envPriv))

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(raw), pki.PrivateKeyString(storedPriv)) {
		t.Error("fwup should receive the --key stored key")
	}
	if strings.Contains(string(raw), pki.PrivateKeyString(envPriv)) {
		t.Error("--key should take precedence over the env key")
	}
}

func TestFirmwareUploadNoKeyErrors(t *testing.T) {
	t.Setenv("NERVES_CLOUD_PRIVATE_KEY", "")
	t.Setenv("NERVES_HUB_PRIVATE_KEY", "")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath,
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no signing key") ||
		!strings.Contains(err.Error(), "--skip-signing") {
		t.Errorf("expected no-signing-key guidance, got %v", err)
	}
}

func TestFirmwareUploadInvalidEnvKey(t *testing.T) {
	t.Setenv("NERVES_HUB_PRIVATE_KEY", "not-base64!!")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath,
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid NERVES_HUB_PRIVATE_KEY") {
		t.Errorf("expected invalid-env-key error, got %v", err)
	}
}

func TestFirmwareUploadKeyAndSkipConflict(t *testing.T) {
	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci", "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "only one of --key or --skip-signing") {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestFirmwareUploadSignedWithPassword(t *testing.T) {
	argsFile := stubFwup(t)
	srv, _, gotContent := signUploadServer(t)

	dataDir := t.TempDir()
	priv := writeSigningKey(t, dataDir, "acme", "ci", "s3cret")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci", "--password", "s3cret",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(raw), pki.PrivateKeyString(priv)) {
		t.Error("fwup did not receive the decrypted key")
	}
	if *gotContent != "FWDATASIGNED" {
		t.Errorf("uploaded content: got %q", *gotContent)
	}
}

func TestFirmwareUploadSignedWrongPassword(t *testing.T) {
	stubFwup(t)
	dataDir := t.TempDir()
	writeSigningKey(t, dataDir, "acme", "ci", "s3cret")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci", "--password", "wrong",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "decrypting signing key") {
		t.Errorf("expected decrypt error, got %v", err)
	}
}

func TestFirmwareUploadSignedMissingKey(t *testing.T) {
	stubFwup(t)
	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ghost",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", t.TempDir(),
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), `signing key "ghost" not found`) {
		t.Errorf("expected missing-key error, got %v", err)
	}
}

func TestFirmwareUploadFwupMissing(t *testing.T) {
	old := fwupBin
	fwupBin = filepath.Join(t.TempDir(), "no-such-fwup")
	t.Cleanup(func() { fwupBin = old })

	dataDir := t.TempDir()
	writeSigningKey(t, dataDir, "acme", "ci", "")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "fwup not found") {
		t.Errorf("expected fwup-not-found error, got %v", err)
	}
}

func TestFirmwareUploadSignFails(t *testing.T) {
	// A stub that fails with a message on stderr.
	dir := t.TempDir()
	path := filepath.Join(dir, "fwup")
	script := "#!/bin/sh\necho 'bad keys' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := fwupBin
	fwupBin = path
	t.Cleanup(func() { fwupBin = old })

	dataDir := t.TempDir()
	writeSigningKey(t, dataDir, "acme", "ci", "")

	fwPath := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(fwPath, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "",
		"firmware", "upload", fwPath, "--key", "ci",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "fwup signing failed: bad keys") {
		t.Errorf("expected fwup failure with stderr message, got %v", err)
	}
}
