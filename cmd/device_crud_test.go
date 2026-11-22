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
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"offline","tags":["prod","eu"],"update_mode":"automatic"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "create", "dev-001",
		"--description", "edge unit", "--tag", "prod", "--tag", "eu", "--update-mode", "automatic",
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
	if dev["update_mode"] != "automatic" {
		t.Errorf("update_mode should be automatic, got %v", dev["update_mode"])
	}
	// Every field is nested under "device"; nothing is sent at the top level.
	if len(gotBody) != 1 {
		t.Errorf("body should only hold device, got %+v", gotBody)
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
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"online","update_mode":"off"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "update", "dev-001", "--update-mode", "off",
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
	// Only the changed field is sent.
	if len(dev) != 1 || dev["update_mode"] != "off" {
		t.Errorf("update body should only hold update_mode off, got %+v", dev)
	}
	if len(gotBody) != 1 {
		t.Errorf("body should only hold device, got %+v", gotBody)
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

func TestDeviceUpdateMode(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"online","update_mode":"device_managed"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--update-mode", "device_managed",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	dev, ok := gotBody["device"].(map[string]any)
	if !ok {
		t.Fatalf("body should be wrapped in device, got %+v", gotBody)
	}
	if dev["update_mode"] != "device_managed" {
		t.Errorf("update_mode should be device_managed, got %v", dev["update_mode"])
	}
	if _, present := gotBody["update_mode"]; present {
		t.Errorf("update_mode should only be sent under device, got %+v", gotBody)
	}
}

func TestDeviceUpdateModeInvalid(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--update-mode", "sometimes",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid --update-mode") {
		t.Errorf("want an invalid --update-mode error, got %v", err)
	}
}

// An older NervesHub answers the nested body with success but leaves tags and
// update_mode unchanged; nh reports that instead of claiming success.
func TestDeviceCreateFailsWhenServerIgnoresTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"offline","tags":[]}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "create", "dev-001", "--tag", "prod",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "created device dev-001, but the server didn't apply --tag") ||
		!strings.Contains(err.Error(), "v2.5.3") {
		t.Errorf("want an unapplied --tag error, got %v", err)
	}
}

func TestDeviceUpdateFailsWhenServerIgnoresUpdateMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"online","update_mode":"automatic"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--update-mode", "off",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "didn't apply --update-mode (sent off, device has automatic)") {
		t.Errorf("want an unapplied --update-mode error, got %v", err)
	}
}

func TestDeviceUpdateTagsInAnyOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"online","tags":["eu","prod"]}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--tag", "prod", "--tag", "eu",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Errorf("tags returned in another order should count as applied, got %v", err)
	}
}

func TestDeviceUpdatesEnabledFlagRemoved(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "update", "dev-001", "--updates-enabled",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --updates-enabled") {
		t.Errorf("want an unknown flag error, got %v", err)
	}
}
