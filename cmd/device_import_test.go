package cmd

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/pki"
)

// writeManifest writes a Trust&Go manifest with one entry per identifier, each
// carrying a certificate signed by a fresh CA, and returns its path and bytes.
// An identifier of "" leaves the entry without a payload, which the server
// can't read.
func writeManifest(t *testing.T, identifiers ...string) (string, []byte) {
	t.Helper()
	caKey, caCert, err := pki.GenerateCA("acme", "Test Signer", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var entries []any
	for _, id := range identifiers {
		if id == "" {
			entries = append(entries, map[string]any{"header": map[string]any{"uniqueId": "broken"}})
			continue
		}
		_, certPEM, err := pki.SignDeviceCertificate(caCert, caKey, "acme", id, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(certPEM)
		payload, _ := json.Marshal(map[string]any{
			"uniqueId": id,
			"publicKeySet": map[string]any{"keys": []any{
				map[string]any{"kid": "0", "x5c": []any{base64.RawStdEncoding.EncodeToString(block.Bytes)}},
			}},
		})
		entries = append(entries, map[string]any{
			"header":  map[string]any{"uniqueId": id},
			"payload": base64.RawStdEncoding.EncodeToString(payload),
		})
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestDeviceImportDryRun(t *testing.T) {
	path, _ := writeManifest(t, "SN0001", "SN0002")

	// No server, token, org or product: a dry run never leaves the machine.
	out, err := execCmd(t, "", "device", "import", path, "--dry-run", "--data-dir", t.TempDir())
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	for _, want := range []string{"ROW", "IDENTIFIER", "CERTIFICATE SERIAL", "SN0001", "SN0002", "ok", "No problems found in 2 devices. Dry run: nothing was sent."} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDeviceImportDryRunInvalid(t *testing.T) {
	path, _ := writeManifest(t, "SN0001", "")

	out, err := execCmd(t, "", "device", "import", path, "--dry-run", "--data-dir", t.TempDir())
	if err == nil || err.Error() != "1 of 2 manifest entries have problems: nothing was sent" {
		t.Fatalf("want a problems error, got %v", err)
	}
	for _, want := range []string{"SN0001", "broken", "error: payload is missing or not a string", "1 of 2 entries would not import"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDeviceImport(t *testing.T) {
	path, data := writeManifest(t, "SN0001", "SN0002")

	var gotMethod, gotPath, gotCT, gotFormat, gotTags string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotCT, gotFormat, gotTags = r.Header.Get("Content-Type"), r.Header.Get("Format"), r.Header.Get("Tags")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "import", path, "--tag", "batch-7", "--tag", "eu, lab",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/import" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotCT != "application/json" || gotFormat != "microchip_trust_and_go" || gotTags != "batch-7,eu,lab" {
		t.Errorf("headers: content-type %q, format %q, tags %q", gotCT, gotFormat, gotTags)
	}
	if string(gotBody) != string(data) {
		t.Errorf("the manifest should be sent unchanged, got:\n%s", gotBody)
	}
	for _, want := range []string{"SN0001", "SN0002", "Queued 2 devices for import into acme/thermostat with tags batch-7, eu, lab.", "posts a notification"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDeviceImportRefusesInvalidManifest(t *testing.T) {
	path, _ := writeManifest(t, "SN0001", "SN0001")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "import", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if called {
		t.Error("a manifest with problems must not be sent")
	}
	if !strings.Contains(out, "error: identifier SN0001 is already used by row 1") {
		t.Errorf("output should name the duplicate:\n%s", out)
	}
}

func TestDeviceImportJSON(t *testing.T) {
	path, _ := writeManifest(t, "SN0001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "import", path, "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	var got struct {
		Org     string   `json:"org"`
		Product string   `json:"product"`
		Format  string   `json:"format"`
		Tags    []string `json:"tags"`
		DryRun  bool     `json:"dry_run"`
		Sent    bool     `json:"sent"`
		Devices int      `json:"devices"`
		Invalid int      `json:"invalid"`
		Rows    []struct {
			Row         int    `json:"row"`
			Identifier  string `json:"identifier"`
			Error       string `json:"error"`
			Certificate struct {
				Serial      string `json:"serial"`
				Fingerprint string `json:"fingerprint"`
			} `json:"certificate"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Org != "acme" || got.Product != "thermostat" || got.Format != "microchip_trust_and_go" ||
		got.Tags == nil || got.DryRun || !got.Sent || got.Devices != 1 || got.Invalid != 0 {
		t.Errorf("result: %+v", got)
	}
	if len(got.Rows) != 1 || got.Rows[0].Row != 1 || got.Rows[0].Identifier != "SN0001" ||
		got.Rows[0].Certificate.Serial == "" || len(got.Rows[0].Certificate.Fingerprint) != 40 {
		t.Errorf("rows: %+v", got.Rows)
	}
}

func TestDeviceImportStdin(t *testing.T) {
	_, data := writeManifest(t, "SN0001")
	out, err := execCmd(t, string(data), "device", "import", "-", "--dry-run", "--data-dir", t.TempDir())
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No problems found in 1 device.") || !strings.Contains(out, "SN0001") {
		t.Errorf("output:\n%s", out)
	}
}

func TestDeviceImportNeedsScopeBeforeOutput(t *testing.T) {
	path, _ := writeManifest(t, "SN0001")
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")
	t.Chdir(t.TempDir()) // no Mix project to take a product from

	out, err := execCmd(t, "", "device", "import", path, "--org", "acme", "--token", "tok", "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Fatalf("want a missing product error, got %v", err)
	}
	if strings.Contains(out, "SN0001") {
		t.Errorf("nothing should be printed before the scope check:\n%s", out)
	}
}

func TestDeviceImportFileErrors(t *testing.T) {
	dir := t.TempDir()
	notList := filepath.Join(dir, "obj.json")
	if err := os.WriteFile(notList, []byte(`{"devices":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"device", "import"}, "Manifest file missing"},
		{[]string{"device", "import", filepath.Join(dir, "missing.json"), "--dry-run"}, "reading manifest"},
		{[]string{"device", "import", notList, "--dry-run"}, "must be a JSON list of entries"},
		{[]string{"device", "import", notList, "--dry-run", "--tag", "has space"}, "invalid --tag"},
	}
	for _, tc := range cases {
		_, err := execCmd(t, "", append(tc.args, "--data-dir", t.TempDir())...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: want error containing %q, got %v", tc.args, tc.want, err)
		}
	}
}
