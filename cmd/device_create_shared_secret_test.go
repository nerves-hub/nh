package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Shaped like DeviceJSON.show/1 with a shared secret: the device, plus
// data.shared_secret from DeviceSharedSecretJSON.with_secret/1.
const deviceCreatedWithSecret = `{"data":{"identifier":"dev-001","connection_status":"not_seen","tags":null,
	"shared_secret":{"key":"nhd_new","secret":"s3cr3t","deactivated_at":null,"last_used":null,"inserted_at":"2026-11-22T12:00:00Z"}}}`

// deviceCreateServer answers a device create with status and body, recording
// the decoded request body.
func deviceCreateServer(t *testing.T, status int, body string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orgs/acme/products/thermostat/devices" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestDeviceCreateSharedSecret(t *testing.T) {
	srv, got := deviceCreateServer(t, http.StatusCreated, deviceCreatedWithSecret)

	stdout, stderr, err := execCmdSplit(t, "",
		"device", "create", "dev-001", "--shared-secret",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// DeviceController.create reads shared_secret from the top level of the
	// params (cast_shared_secret(params)), not from under "device".
	if (*got)["shared_secret"] != true {
		t.Errorf("top-level shared_secret should be true, got %+v", *got)
	}
	dev, _ := (*got)["device"].(map[string]any)
	if dev["identifier"] != "dev-001" {
		t.Errorf("device body: %+v", dev)
	}
	if _, ok := dev["shared_secret"]; ok {
		t.Errorf("shared_secret must not be sent under device: %+v", dev)
	}
	for _, want := range []string{"Created device dev-001 in acme/thermostat", "Shared secret:", "nhd_new", "s3cr3t"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q, got:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "cannot be fetched again") {
		t.Errorf("stderr should warn the secret is shown once, got %q", stderr)
	}
}

func TestDeviceCreateSharedSecretJSON(t *testing.T) {
	srv, _ := deviceCreateServer(t, http.StatusCreated, deviceCreatedWithSecret)

	stdout, stderr, err := execCmdSplit(t, "",
		"device", "create", "dev-001", "--shared-secret", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	ss, _ := out["shared_secret"].(map[string]any)
	if ss["key"] != "nhd_new" || ss["secret"] != "s3cr3t" {
		t.Errorf("JSON should carry the shared secret, got %+v", out)
	}
	if !strings.Contains(stderr, "cannot be fetched again") {
		t.Errorf("stderr should warn the secret is shown once, got %q", stderr)
	}
}

func TestDeviceCreateWithoutSharedSecretFlag(t *testing.T) {
	srv, got := deviceCreateServer(t, http.StatusCreated, `{"data":{"identifier":"dev-001","connection_status":"not_seen"}}`)

	stdout, _, err := execCmdSplit(t, "",
		"device", "create", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := (*got)["shared_secret"]; ok {
		t.Errorf("shared_secret should only be sent with --shared-secret, got %+v", *got)
	}
	if strings.Contains(stdout, "Shared secret") {
		t.Errorf("no shared secret output without the flag, got:\n%s", stdout)
	}
}

func TestDeviceCreateSharedSecretIgnoredByServer(t *testing.T) {
	// A server that predates shared_secret creates the device and returns
	// it without one.
	srv, _ := deviceCreateServer(t, http.StatusCreated, `{"data":{"identifier":"dev-001","connection_status":"not_seen"}}`)

	stdout, _, err := execCmdSplit(t, "",
		"device", "create", "dev-001", "--shared-secret",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil {
		t.Fatal("want an error when the server returns no shared secret")
	}
	for _, want := range []string{"created device dev-001", "returned no shared secret", "nh device shared-secrets create dev-001"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	// The device was created, so it is still shown.
	if !strings.Contains(stdout, "Created device dev-001") {
		t.Errorf("stdout: %q", stdout)
	}
}

func TestDeviceCreateSharedSecretPrintedWhenAFieldIsNotApplied(t *testing.T) {
	// The server applied the secret but not the tags: the secret, the only
	// copy, must still be printed alongside the error.
	srv, _ := deviceCreateServer(t, http.StatusCreated, deviceCreatedWithSecret)

	stdout, _, err := execCmdSplit(t, "",
		"device", "create", "dev-001", "--shared-secret", "--tag", "prod",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "created device dev-001, but") {
		t.Fatalf("want a not-applied error, got %v", err)
	}
	if !strings.Contains(stdout, "s3cr3t") {
		t.Errorf("secret should be printed even when a field wasn't applied, got:\n%s", stdout)
	}
}

func TestDeviceCreateSharedSecretNotEnabled(t *testing.T) {
	// create_device_with_shared_secret_auth rolls the device back and
	// FallbackController renders :shared_secrets_not_enabled as a 422.
	srv, _ := deviceCreateServer(t, http.StatusUnprocessableEntity,
		`{"errors":{"detail":"Device shared secrets are not enabled on this platform, so a device could not connect with one. Contact your Ops team for more information."}}`)

	_, err := execCmd(t, "",
		"device", "create", "dev-001", "--shared-secret",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not enabled on this platform") {
		t.Errorf("want the server's reason, got %v", err)
	}
}

func TestDeviceUpdateHasNoSharedSecretFlag(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--shared-secret",
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --shared-secret") {
		t.Errorf("update should not take --shared-secret, got %v", err)
	}
}
