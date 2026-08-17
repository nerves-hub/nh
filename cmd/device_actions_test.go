package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceReboot(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "reboot", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/reboot" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Reboot requested for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceReconnect(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "reconnect", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/reconnect" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Reconnect requested for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceRebootMissingIdentifier(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "reboot",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Device identifier missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestDeviceRebootAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "reboot", "ghost",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should surface the status, got %v", err)
	}
}
