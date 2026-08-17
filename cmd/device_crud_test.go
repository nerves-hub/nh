package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceCreate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"offline","tags":["prod","eu"]}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "create", "dev-001",
		"--description", "edge unit", "--tag", "prod", "--tag", "eu", "--updates-enabled",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	dev, ok := gotBody["device"].(map[string]any)
	if !ok {
		t.Fatalf("body should be wrapped in device, got %+v", gotBody)
	}
	// tags are sent as a comma-separated string.
	if dev["identifier"] != "dev-001" || dev["description"] != "edge unit" || dev["tags"] != "prod,eu" {
		t.Errorf("device body: %+v", dev)
	}
	if dev["updates_enabled"] != true {
		t.Errorf("updates_enabled should be true, got %v", dev["updates_enabled"])
	}
	if !strings.Contains(out, "Created device dev-001 in acme/thermostat") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceUpdate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"online"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "update", "dev-001", "--updates-enabled=false",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	dev, ok := gotBody["device"].(map[string]any)
	if !ok {
		t.Fatalf("body should be wrapped in device, got %+v", gotBody)
	}
	// Only the changed field is sent; updates_enabled can be set to false.
	if dev["updates_enabled"] != false {
		t.Errorf("updates_enabled should be false, got %v", dev["updates_enabled"])
	}
	if _, present := dev["identifier"]; present {
		t.Errorf("identifier should not be in an update body, got %+v", dev)
	}
	if !strings.Contains(out, "Updated device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceUpdateNothingToChange(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "update", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("expected nothing-to-update error, got %v", err)
	}
}

func TestDeviceDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "delete", "dev-001", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Deleted device dev-001 from acme/thermostat") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceDeleteAbortsWithoutConfirmation(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "delete", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--non-interactive", "--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "without confirmation") {
		t.Errorf("expected confirmation guard, got %v", err)
	}
}
