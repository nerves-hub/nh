package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sharedSecretsPath = "/api/orgs/acme/products/thermostat/devices/dev-001/shared_secrets"

// Shaped like DeviceSharedSecretJSON.index/1: keys only, never secrets, with
// null deactivated_at/last_used for an active, unused key.
const sharedSecretsListResponse = `{"data":[
	{"key":"nhd_active","deactivated_at":null,"last_used":"2026-10-08T12:30:00Z","inserted_at":"2026-10-08T12:00:00Z"},
	{"key":"nhd_old","deactivated_at":"2026-10-09T08:00:00Z","last_used":null,"inserted_at":"2026-10-01T12:00:00Z"}
]}`

// execCmdSplit runs the CLI like execCmd but keeps stdout and stderr apart.
func execCmdSplit(t *testing.T, in string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetState(t)

	var out, errOut bytes.Buffer
	rootCmd.SetArgs(args)
	if in != "" {
		rootCmd.SetIn(strings.NewReader(in))
	}
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)

	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

// sharedSecretServer answers one request with status and body, recording the
// method, path and request body it received.
func sharedSecretServer(t *testing.T, status int, body string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		rec.method, rec.path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		rec.body = string(b)
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

type recordedRequest struct {
	calls              int
	method, path, body string
}

func TestDeviceSharedSecretsList(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusOK, sharedSecretsListResponse)

	out, err := execCmd(t, "",
		"device", "shared-secrets", "list", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != sharedSecretsPath {
		t.Errorf("request: %s %s", rec.method, rec.path)
	}
	for _, want := range []string{
		"KEY", "STATUS", "LAST USED", "CREATED", "DEACTIVATED",
		"nhd_active", "active", "2026-10-08 12:30:00 UTC",
		"nhd_old", "deactivated", "2026-10-09 08:00:00 UTC", "never",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
	// The active key has no deactivation time and the old one was never used.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "nhd_active") && !strings.HasSuffix(strings.TrimSpace(line), "-") {
			t.Errorf("active key should show no deactivation time: %q", line)
		}
	}
}

func TestDeviceSharedSecretsListJSON(t *testing.T) {
	srv, _ := sharedSecretServer(t, http.StatusOK, sharedSecretsListResponse)

	out, err := execCmd(t, "",
		"device", "shared-secrets", "list", "dev-001", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 secrets, got %d", len(got))
	}
	if _, ok := got[0]["secret"]; ok {
		t.Errorf("listed secrets must not carry a secret field: %v", got[0])
	}
	if got[0]["deactivated_at"] != nil {
		t.Errorf("active key: deactivated_at should be null, got %v", got[0]["deactivated_at"])
	}
	if got[1]["last_used"] != nil {
		t.Errorf("unused key: last_used should be null, got %v", got[1]["last_used"])
	}
}

func TestDeviceSharedSecretsListEmpty(t *testing.T) {
	srv, _ := sharedSecretServer(t, http.StatusOK, `{"data":[]}`)

	out, err := execCmd(t, "",
		"device", "shared-secrets", "list", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No shared secrets found for device dev-001.") {
		t.Errorf("output: %q", out)
	}

	out, err = execCmd(t, "",
		"device", "shared-secrets", "list", "dev-001", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty JSON list: got %q", out)
	}
}

// Shaped like DeviceSharedSecretJSON.created/1, the one view with the secret.
const sharedSecretCreateResponse = `{"data":{"key":"nhd_new","secret":"s3cr3t","deactivated_at":null,"last_used":null,"inserted_at":"2026-10-08T12:00:00Z"}}`

func TestDeviceSharedSecretsCreate(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusCreated, sharedSecretCreateResponse)

	stdout, stderr, err := execCmdSplit(t, "",
		"device", "shared-secrets", "create", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// DeviceSharedSecretController.create reads no parameters.
	if rec.method != http.MethodPost || rec.path != sharedSecretsPath || rec.body != "" {
		t.Errorf("request: %s %s body %q", rec.method, rec.path, rec.body)
	}
	for _, want := range []string{"Created shared secret for device dev-001", "nhd_new", "s3cr3t"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q, got:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "cannot be fetched again") {
		t.Errorf("stderr should warn the secret is shown once, got %q", stderr)
	}
}

func TestDeviceSharedSecretsCreateJSON(t *testing.T) {
	srv, _ := sharedSecretServer(t, http.StatusCreated, sharedSecretCreateResponse)

	stdout, stderr, err := execCmdSplit(t, "",
		"device", "shared-secrets", "create", "dev-001", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got["key"] != "nhd_new" || got["secret"] != "s3cr3t" {
		t.Errorf("JSON: %v", got)
	}
	if !strings.Contains(stderr, "cannot be fetched again") {
		t.Errorf("stderr should warn the secret is shown once, got %q", stderr)
	}
}

func TestDeviceSharedSecretsCreateWithoutSecret(t *testing.T) {
	srv, _ := sharedSecretServer(t, http.StatusCreated, `{"data":{"key":"nhd_new","deactivated_at":null,"last_used":null,"inserted_at":"2026-10-08T12:00:00Z"}}`)

	stdout, _, err := execCmdSplit(t, "",
		"device", "shared-secrets", "create", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if strings.Contains(stdout, "Secret:") {
		t.Errorf("no secret line when the server returns none, got:\n%s", stdout)
	}
	// The key is useless without its secret, so this fails and says how to
	// clean up.
	if err == nil || !strings.Contains(err.Error(), "without its secret") ||
		!strings.Contains(err.Error(), "nh device shared-secrets delete dev-001 nhd_new") {
		t.Errorf("want an error with the cleanup command, got %v", err)
	}
}

func TestDeviceSharedSecretsCreateNotEnabled(t *testing.T) {
	// FallbackController's {:error, :shared_secrets_not_enabled} clause.
	srv, _ := sharedSecretServer(t, http.StatusUnprocessableEntity,
		`{"errors":{"detail":"Device shared secrets are not enabled on this platform, so a device could not connect with one. Contact your Ops team for more information."}}`)

	_, err := execCmd(t, "",
		"device", "shared-secrets", "create", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not enabled on this platform") {
		t.Errorf("want the server's reason, got %v", err)
	}
}

func TestDeviceSharedSecretsDelete(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusNoContent, "")

	out, err := execCmd(t, "",
		"device", "shared-secrets", "delete", "dev-001", "nhd_abc-_XYZ", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != sharedSecretsPath+"/nhd_abc-_XYZ" {
		t.Errorf("request: %s %s", rec.method, rec.path)
	}
	if !strings.Contains(out, "Deactivated shared secret nhd_abc-_XYZ on device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceSharedSecretsDeactivateAlias(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusNoContent, "")

	_, err := execCmd(t, "y\n",
		"device", "shared-secrets", "deactivate", "dev-001", "nhd_abc",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != sharedSecretsPath+"/nhd_abc" {
		t.Errorf("request: %s %s", rec.method, rec.path)
	}
}

func TestDeviceSharedSecretsDeleteAbort(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusNoContent, "")

	out, err := execCmd(t, "\n",
		"device", "shared-secrets", "delete", "dev-001", "nhd_abc",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.calls != 0 {
		t.Error("no request should be sent when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}

func TestDeviceSharedSecretsDeleteNonInteractive(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusNoContent, "")

	_, err := execCmd(t, "",
		"device", "shared-secrets", "delete", "dev-001", "nhd_abc", "--non-interactive",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("want a refusal mentioning --yes, got %v", err)
	}
	if rec.calls != 0 {
		t.Error("no request should be sent without confirmation")
	}
}

func TestDeviceSharedSecretsDeleteNotFound(t *testing.T) {
	srv, _ := sharedSecretServer(t, http.StatusNotFound,
		`{"errors":{"detail":"Resource Not Found or Authorization Insufficient"}}`)

	_, err := execCmd(t, "",
		"device", "shared-secrets", "delete", "dev-001", "nhd_gone", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"api: 404", "no active shared secret nhd_gone", "already deactivated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestDeviceSharedSecretsArgs(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "shared-secrets", "delete", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "usage: device shared-secrets delete <identifier> <key>") {
		t.Errorf("want usage error, got %v", err)
	}
}

func TestDeviceSharedSecretsDeleteRefusesPathKeys(t *testing.T) {
	srv, rec := sharedSecretServer(t, http.StatusNoContent, "")

	for _, key := range []string{"..", ".", "../..", "nhd_a/b"} {
		_, err := execCmd(t, "",
			"device", "shared-secrets", "delete", "dev-001", key, "--yes",
			"--org", "acme", "--product", "thermostat",
			"--uri", srv.URL, "--token", "tok",
		)
		if err == nil || !strings.Contains(err.Error(), "invalid shared secret key") {
			t.Errorf("key %q: want an invalid key error, got %v", key, err)
		}
	}
	if rec.calls != 0 {
		t.Errorf("no request should be sent for a key that isn't one path segment, got %d", rec.calls)
	}
}
