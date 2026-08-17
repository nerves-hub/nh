package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const deploymentsResponse = `{"data":[
	{"name":"production","state":"on","is_active":true,"device_count":42,"releases_count":3,
	 "firmware_uuid":"d9f8c63a-1111","delta_updatable":false,
	 "conditions":{"tags":["prod"],"version":">= 1.0.0"}},
	{"name":"beta","state":"off","is_active":false,"device_count":5,"releases_count":1,
	 "firmware_uuid":"d9f8c63a-2222","delta_updatable":true,
	 "conditions":{"tags":["beta"],"version":""}}
]}`

func TestDeploymentList(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deploymentsResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/deployments" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"NAME", "STATE", "DEVICES", "production", "on", "yes", "42", ">= 1.0.0", "beta", "off", "no"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeploymentListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No deployment groups found in acme/thermostat") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestDeploymentShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{
			"name":"production","state":"on","is_active":true,"device_count":42,"releases_count":3,
			"firmware_uuid":"d9f8c63a-1111","delta_updatable":false,
			"conditions":{"tags":["prod","eu"],"version":">= 1.0.0"},
			"current_release":{"number":3,"firmware":{"uuid":"d9f8c63a-1111","version":"1.0.0"}}
		}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "show", "production",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/deployments/production" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Name:", "production", "State:", "Active:", "yes", "Devices:", "42", "prod, eu", "Current release:", "#3", "1.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeploymentDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "delete", "production", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/products/thermostat/deployments/production" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, `Deleted deployment group "production" from acme/thermostat`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentDeleteAbortsWithoutConfirmation(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "delete", "production",
		"--org", "acme", "--product", "thermostat",
		"--non-interactive", "--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "without confirmation") {
		t.Errorf("expected confirmation guard, got %v", err)
	}
}

func TestDeploymentListMissingProduct(t *testing.T) {
	clearScopeEnv(t)
	_, err := execCmd(t, "",
		"deployment", "list", "--org", "acme",
		"--token", "tok", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Errorf("expected missing-product error, got %v", err)
	}
}

func TestDeploymentCreate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"name":"production","state":"on","is_active":true}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "create", "production",
		"--firmware", "fw-uuid-1", "--state", "on",
		"--version", ">= 1.0.0", "--tag", "prod", "--tag", "eu",
		"--delta-updatable",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/deployments" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// Create body is flat (no "deployment" wrapper).
	if gotBody["name"] != "production" || gotBody["firmware"] != "fw-uuid-1" || gotBody["state"] != "on" {
		t.Errorf("body: %+v", gotBody)
	}
	if gotBody["delta_updatable"] != true {
		t.Errorf("delta_updatable should be true, got %v", gotBody["delta_updatable"])
	}
	cond, _ := gotBody["conditions"].(map[string]any)
	if cond["version"] != ">= 1.0.0" {
		t.Errorf("conditions.version: %+v", cond)
	}
	tags, _ := cond["tags"].([]any)
	if len(tags) != 2 || tags[0] != "prod" || tags[1] != "eu" {
		t.Errorf("conditions.tags: %+v", cond["tags"])
	}
	if !strings.Contains(out, `Created deployment group "production" in acme/thermostat`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentCreateRequiresFirmware(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "create", "production",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "--firmware is required") {
		t.Errorf("expected firmware-required error, got %v", err)
	}
}

func TestDeploymentCreateRejectsBadState(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "create", "production",
		"--firmware", "fw", "--state", "maybe",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid --state") {
		t.Errorf("expected state-validation error, got %v", err)
	}
}

func TestDeploymentUpdate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"production","state":"off"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"deployment", "update", "production", "--state", "off",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/acme/products/thermostat/deployments/production" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// Update body is wrapped in "deployment", with only the changed field.
	dep, ok := gotBody["deployment"].(map[string]any)
	if !ok {
		t.Fatalf("body should be wrapped in deployment, got %+v", gotBody)
	}
	if dep["state"] != "off" {
		t.Errorf("deployment.state: %+v", dep)
	}
	if _, present := dep["firmware"]; present {
		t.Errorf("unchanged fields should be omitted, got %+v", dep)
	}
	if !strings.Contains(out, `Updated deployment group "production" in acme/thermostat`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentUpdateNothingToChange(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "update", "production",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("expected nothing-to-update error, got %v", err)
	}
}
