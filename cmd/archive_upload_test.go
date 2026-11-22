package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/pki"
)

// goodArchiveMeta is `fwup -m` output for an archive NervesHub accepts.
const goodArchiveMeta = `meta-product="extras"
meta-description="Supporting files"
meta-version="1.2.0"
meta-platform="rpi0"
meta-architecture="arm"
meta-uuid="arch-uuid"
`

// stubArchiveFwup installs a fake fwup. `fwup -m` prints meta (or, when meta
// is empty, fails with a message on stderr); `fwup --sign` copies -i to -o
// and appends "SIGNED". Every invocation's arguments are appended to the
// returned file, one per line, each call ending with "--".
func stubArchiveFwup(t *testing.T, meta string) string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	metaFile := filepath.Join(dir, "meta")
	if err := os.WriteFile(metaFile, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" >> "` + argsFile + `"` + "\n" +
		`printf -- '--\n' >> "` + argsFile + `"` + "\n" +
		"if [ \"$1\" = \"-m\" ]; then\n" +
		`  if [ -s "` + metaFile + `" ]; then cat "` + metaFile + `"; exit 0; fi` + "\n" +
		"  echo 'fwup: not a valid archive' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
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

// fwupCalls splits the stub's argument log into one slice per invocation.
func fwupCalls(t *testing.T, argsFile string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	var cur []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "--" {
			calls = append(calls, cur)
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return calls
}

type archiveUpload struct {
	method, path, field, filename, content string
}

// archiveUploadServer stands in for ArchiveController.create: it records the
// file sent in the "archive" field and answers 201 with the created archive.
func archiveUploadServer(t *testing.T) (*httptest.Server, *archiveUpload) {
	t.Helper()
	got := &archiveUpload{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for field, files := range r.MultipartForm.File {
				got.field = field
				f, err := files[0].Open()
				if err == nil {
					b, _ := io.ReadAll(f)
					got.content = string(b)
					f.Close()
				}
				got.filename = files[0].Filename
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"data":{"uuid":"arch-uuid","version":"1.2.0","platform":"rpi0","architecture":"arm","size":6,"inserted_at":"2026-01-15T09:34:27"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// refuseServer fails the test if any request reaches it.
func refuseServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be made, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeArchiveFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "extras.fw")
	if err := os.WriteFile(path, []byte("ARCHIV"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchiveUploadSignedWithKey(t *testing.T) {
	argsFile := stubArchiveFwup(t, goodArchiveMeta)
	srv, got := archiveUploadServer(t)

	dataDir := t.TempDir()
	priv := writeSigningKey(t, dataDir, "acme", "ci", "s3cret")
	path := writeArchiveFile(t)

	out, err := execCmd(t, "",
		"archive", "upload", path, "--key", "ci", "--password", "s3cret",
		"--org", "acme", "--product", "thermostat",
		"--data-dir", dataDir,
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}

	calls := fwupCalls(t, argsFile)
	if len(calls) != 2 {
		t.Fatalf("fwup calls: %v", calls)
	}
	if strings.Join(calls[0], " ") != "-m -i "+path {
		t.Errorf("metadata call: %v", calls[0])
	}
	sign := calls[1]
	if len(sign) != 7 || sign[0] != "--sign" || sign[2] != path || sign[5] != "--private-key" ||
		sign[6] != pki.PrivateKeyString(priv) {
		t.Errorf("sign call: %v", sign)
	}

	if got.method != http.MethodPost || got.path != "/api/orgs/acme/products/thermostat/archives" {
		t.Errorf("request: %s %s", got.method, got.path)
	}
	if got.field != "archive" || got.filename != "extras.fw" || got.content != "ARCHIVSIGNED" {
		t.Errorf("upload: %+v", got)
	}
	for _, want := range []string{"version:      1.2.0", "Signing extras.fw with key ci", "Uploaded archive 1.2.0 (arch-uuid)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "nh-signed-*.fw")); len(matches) != 0 {
		t.Errorf("signed temp files left behind: %v", matches)
	}
}

func TestArchiveUploadSignsFromEnvJSON(t *testing.T) {
	argsFile := stubArchiveFwup(t, goodArchiveMeta)
	srv, got := archiveUploadServer(t)

	_, priv, err := pki.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NERVES_HUB_PRIVATE_KEY", pki.PrivateKeyString(priv))
	path := writeArchiveFile(t)

	// Status lines go to stderr, so read stdout on its own.
	resetState(t)
	var stdout, stderr strings.Builder
	rootCmd.SetArgs([]string{
		"archive", "upload", path, "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	})
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, stderr.String())
	}

	calls := fwupCalls(t, argsFile)
	if len(calls) != 2 || !strings.Contains(strings.Join(calls[1], " "), pki.PrivateKeyString(priv)) {
		t.Errorf("fwup should sign with the env key: %v", calls)
	}
	if got.content != "ARCHIVSIGNED" {
		t.Errorf("uploaded content: %q", got.content)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if decoded["uuid"] != "arch-uuid" {
		t.Errorf("JSON output: %v", decoded)
	}
	if !strings.Contains(stderr.String(), "key from NERVES_HUB_PRIVATE_KEY") {
		t.Errorf("stderr: %q", stderr.String())
	}
}

func TestArchiveUploadSkipSigning(t *testing.T) {
	argsFile := stubArchiveFwup(t, goodArchiveMeta)
	srv, got := archiveUploadServer(t)
	path := writeArchiveFile(t)

	if out, err := execCmd(t, "",
		"archive", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}

	calls := fwupCalls(t, argsFile)
	if len(calls) != 1 || calls[0][0] != "-m" {
		t.Errorf("only the metadata check should run: %v", calls)
	}
	if got.content != "ARCHIV" {
		t.Errorf("the file should be uploaded as is, got %q", got.content)
	}
}

func TestArchiveUploadRejectsNonSemVer(t *testing.T) {
	for _, version := range []string{"1.0", "v1.0.0", "01.0.0", "2026-01-15"} {
		t.Run(version, func(t *testing.T) {
			meta := strings.Replace(goodArchiveMeta, `meta-version="1.2.0"`, `meta-version="`+version+`"`, 1)
			argsFile := stubArchiveFwup(t, meta)
			srv := refuseServer(t)
			path := writeArchiveFile(t)

			_, err := execCmd(t, "",
				"archive", "upload", path, "--skip-signing",
				"--org", "acme", "--product", "thermostat",
				"--uri", srv.URL, "--token", "tok",
			)
			if err == nil || !strings.Contains(err.Error(), `meta-version "`+version+`" is not a SemVer version`) {
				t.Errorf("expected a SemVer error, got %v", err)
			}
			if calls := fwupCalls(t, argsFile); len(calls) != 1 {
				t.Errorf("nothing should be signed: %v", calls)
			}
		})
	}
}

func TestArchiveUploadRejectsMissingMetadata(t *testing.T) {
	stubArchiveFwup(t, "meta-version=\"1.0.0\"\nmeta-uuid=\"u\"\n")
	srv := refuseServer(t)
	path := writeArchiveFile(t)

	_, err := execCmd(t, "",
		"archive", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "missing meta-product, meta-platform, meta-architecture") {
		t.Errorf("expected a missing-metadata error, got %v", err)
	}
}

func TestArchiveUploadUnreadableFile(t *testing.T) {
	stubArchiveFwup(t, "")
	srv := refuseServer(t)
	path := writeArchiveFile(t)

	_, err := execCmd(t, "",
		"archive", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not a readable fwup archive: fwup: not a valid archive") {
		t.Errorf("expected fwup's message, got %v", err)
	}
}

func TestArchiveUploadFwupMissing(t *testing.T) {
	old := fwupBin
	fwupBin = filepath.Join(t.TempDir(), "no-such-fwup")
	t.Cleanup(func() { fwupBin = old })
	path := writeArchiveFile(t)

	t.Run("signing", func(t *testing.T) {
		t.Setenv("NERVES_HUB_PRIVATE_KEY", "")
		t.Setenv("NERVES_CLOUD_PRIVATE_KEY", "")
		srv := refuseServer(t)
		_, err := execCmd(t, "",
			"archive", "upload", path,
			"--org", "acme", "--product", "thermostat",
			"--uri", srv.URL, "--token", "tok",
		)
		if err == nil || !strings.Contains(err.Error(), "fwup not found: install fwup to sign archives") {
			t.Errorf("expected fwup-not-found error, got %v", err)
		}
	})

	t.Run("skip-signing", func(t *testing.T) {
		srv, got := archiveUploadServer(t)
		out, err := execCmd(t, "",
			"archive", "upload", path, "--skip-signing",
			"--org", "acme", "--product", "thermostat",
			"--uri", srv.URL, "--token", "tok",
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(out, "Warning: fwup not found") {
			t.Errorf("expected a warning, got %q", out)
		}
		if got.content != "ARCHIV" {
			t.Errorf("uploaded content: %q", got.content)
		}
	})
}

func TestArchiveUploadNoKey(t *testing.T) {
	stubArchiveFwup(t, goodArchiveMeta)
	t.Setenv("NERVES_HUB_PRIVATE_KEY", "")
	t.Setenv("NERVES_CLOUD_PRIVATE_KEY", "")
	srv := refuseServer(t)
	path := writeArchiveFile(t)

	_, err := execCmd(t, "",
		"archive", "upload", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no signing key") ||
		!strings.Contains(err.Error(), "upload the archive as is") {
		t.Errorf("expected no-signing-key guidance, got %v", err)
	}
}

func TestArchiveUploadKeyAndSkipConflict(t *testing.T) {
	path := writeArchiveFile(t)
	_, err := execCmd(t, "",
		"archive", "upload", path, "--key", "ci", "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "only one of --key or --skip-signing") {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestArchiveUploadMissingFile(t *testing.T) {
	_, err := execCmd(t, "",
		"archive", "upload", filepath.Join(t.TempDir(), "nope.fw"),
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("expected a not-exist error, got %v", err)
	}
	if _, err := execCmd(t, "", "archive", "upload"); err == nil || !strings.Contains(err.Error(), "Archive file path missing") {
		t.Errorf("no path: %v", err)
	}
}

func TestArchiveUploadServerRejects(t *testing.T) {
	stubArchiveFwup(t, goodArchiveMeta)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"errors":{"detail":"The signature does not match any of this organization's signing keys, or the file is corrupt."}}`)
	}))
	t.Cleanup(srv.Close)
	path := writeArchiveFile(t)

	_, err := execCmd(t, "",
		"archive", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "signature does not match") {
		t.Errorf("expected the server's message, got %v", err)
	}
}
