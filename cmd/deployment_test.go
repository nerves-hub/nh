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

// deploymentRecordedRequest is one request a fake deployment server saw.
type deploymentRecordedRequest struct {
	method, path string
	body         map[string]any
}

// deploymentFake answers deployment requests in order with responses, and
// records what it saw.
func deploymentFake(t *testing.T, responses ...string) (*httptest.Server, *[]deploymentRecordedRequest) {
	t.Helper()
	var reqs []deploymentRecordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := deploymentRecordedRequest{method: r.Method, path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&rec.body)
		reqs = append(reqs, rec)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		body := `{"data":{"name":"production"}}`
		if i := len(reqs) - 1; i < len(responses) {
			body = responses[i]
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestDeploymentCreate(t *testing.T) {
	srv, reqs := deploymentFake(t,
		`{"data":{"name":"production","state":"off","is_active":false}}`,
		`{"data":{"name":"production","state":"on","is_active":true}}`,
	)

	out, err := execCmd(t, "",
		"deployment", "create", "production",
		"--firmware", "fw-uuid-1", "--state", "on",
		"--version", ">= 1.0.0", "--tag", "prod", "--tag", "eu",
		"--tag-operator", "or", "--notes", "summer batch",
		"--delta-updatable",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("want POST then PUT, got %+v", *reqs)
	}
	post := (*reqs)[0]
	if post.method != http.MethodPost || post.path != "/api/orgs/acme/products/thermostat/deployments" {
		t.Errorf("request: %s %s", post.method, post.path)
	}
	// Create body is flat (no "deployment" wrapper).
	gotBody := post.body
	if gotBody["name"] != "production" || gotBody["firmware"] != "fw-uuid-1" || gotBody["notes"] != "summer batch" {
		t.Errorf("body: %+v", gotBody)
	}
	if gotBody["delta_updatable"] != true {
		t.Errorf("delta_updatable should be true, got %v", gotBody["delta_updatable"])
	}
	cond, _ := gotBody["conditions"].(map[string]any)
	if cond["version"] != ">= 1.0.0" || cond["tag_operator"] != "or" {
		t.Errorf("conditions: %+v", cond)
	}
	tags, _ := cond["tags"].([]any)
	if len(tags) != 2 || tags[0] != "prod" || tags[1] != "eu" {
		t.Errorf("conditions.tags: %+v", cond["tags"])
	}

	// create_changeset ignores state, so --state on is a follow-up PUT.
	put := (*reqs)[1]
	if put.method != http.MethodPut || put.path != "/api/orgs/acme/products/thermostat/deployments/production" {
		t.Errorf("turn-on request: %s %s", put.method, put.path)
	}
	if dep, _ := put.body["deployment"].(map[string]any); dep["state"] != "on" || len(dep) != 1 {
		t.Errorf("turn-on body: %+v", put.body)
	}
	if !strings.Contains(out, `Created deployment group "production" in acme/thermostat (state: on)`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentCreateWithoutConditions(t *testing.T) {
	srv, reqs := deploymentFake(t, `{"data":{"name":"production","state":"off"}}`)

	if _, err := execCmd(t, "",
		"deployment", "create", "production", "--firmware", "fw-uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("want one request, got %+v", *reqs)
	}
	// The server requires conditions, even when there are none.
	cond, ok := (*reqs)[0].body["conditions"].(map[string]any)
	if !ok {
		t.Fatalf("conditions must be sent, got %+v", (*reqs)[0].body)
	}
	if tags, ok := cond["tags"].([]any); !ok || len(tags) != 0 || cond["version"] != "" {
		t.Errorf("empty conditions: %+v", cond)
	}
}

func TestDeploymentCreateHasNoReleaseFlags(t *testing.T) {
	for _, flag := range []string{"--archive", "--required"} {
		_, err := execCmd(t, "",
			"deployment", "create", "production", "--firmware", "fw", flag, "x",
			"--org", "acme", "--product", "thermostat",
			"--token", "tok", "--uri", "https://example.com",
		)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%s: want unknown flag, got %v", flag, err)
		}
	}
}

func TestDeploymentCreateRejectsBadTagOperator(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "create", "production",
		"--firmware", "fw", "--tag-operator", "xor",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid --tag-operator") {
		t.Errorf("expected tag-operator error, got %v", err)
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

// --firmware used to make the server drop every other flag; now the release
// and the settings go in separate requests.
func TestDeploymentUpdateFirmwareAndSettings(t *testing.T) {
	srv, reqs := deploymentFake(t,
		`{"data":{"name":"production","state":"off"}}`,
		`{"data":{"name":"production","state":"off","firmware_uuid":"fw-2"}}`,
		`{"data":{"name":"production","state":"on","firmware_uuid":"fw-2"}}`,
	)

	out, err := execCmd(t, "",
		"deployment", "update", "production",
		"--firmware", "fw-2", "--required",
		"--state", "on", "--version", "", "--tag", "", "--notes", "n",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok", "-o", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want settings, release, turn-on; got %+v", *reqs)
	}
	for _, r := range *reqs {
		if r.method != http.MethodPut || r.path != "/api/orgs/acme/products/thermostat/deployments/production" {
			t.Errorf("request: %s %s", r.method, r.path)
		}
	}
	settings, _ := (*reqs)[0].body["deployment"].(map[string]any)
	if settings["notes"] != "n" {
		t.Errorf("settings body: %+v", settings)
	}
	for _, k := range []string{"firmware", "state"} {
		if _, ok := settings[k]; ok {
			t.Errorf("settings must not carry %q: %+v", k, settings)
		}
	}
	cond, _ := settings["conditions"].(map[string]any)
	if tags, ok := cond["tags"].([]any); !ok || len(tags) != 0 || cond["version"] != "" {
		t.Errorf("--tag \"\" and --version \"\" should clear the conditions, got %+v", cond)
	}
	release, _ := (*reqs)[1].body["deployment"].(map[string]any)
	if len(release) != 2 || release["firmware"] != "fw-2" || release["required"] != true {
		t.Errorf("release body: %+v", release)
	}
	if on, _ := (*reqs)[2].body["deployment"].(map[string]any); len(on) != 1 || on["state"] != "on" {
		t.Errorf("turn-on body: %+v", on)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["state"] != "on" {
		t.Errorf("should print the final group, got %q (%v)", out, err)
	}
}

func TestDeploymentUpdateArchive(t *testing.T) {
	srv, reqs := deploymentFake(t,
		`{"data":{"name":"production","firmware_uuid":"fw-current"}}`,
		`{"data":{"name":"production","firmware_uuid":"fw-current","archive_uuid":"ar-1"}}`,
	)

	if _, err := execCmd(t, "",
		"deployment", "update", "production", "--archive", "ar-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 2 || (*reqs)[0].method != http.MethodGet {
		t.Fatalf("want GET then PUT, got %+v", *reqs)
	}
	dep, _ := (*reqs)[1].body["deployment"].(map[string]any)
	if dep["firmware"] != "fw-current" || dep["archive"] != "ar-1" {
		t.Errorf("release body: %+v", dep)
	}
}

func TestDeploymentUpdateRequiredNeedsRelease(t *testing.T) {
	_, err := execCmd(t, "",
		"deployment", "update", "production", "--required",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "--required marks a new release") {
		t.Errorf("expected required-needs-release error, got %v", err)
	}
}

func TestDeploymentShowExtraFields(t *testing.T) {
	srv, _ := deploymentFake(t, `{"data":{
		"name":"production","state":"on","is_active":true,
		"firmware_uuid":"fw-1","archive_uuid":"ar-1","notes":"summer batch\nsecond line",
		"conditions":{"tags":["a","b"],"version":"","tag_operator":"or"},
		"current_release":{"number":2,"required":true,"firmware":{"uuid":"fw-1","version":"1.0.0"}}
	}}`)

	out, err := execCmd(t, "",
		"deployment", "show", "production",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"Tag operator:", "or (any tag)", "Archive:", "ar-1", "Release required:", "Notes:", "summer batch"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q, got:\n%s", want, out)
		}
	}
	// A second line of notes gets its own row, indented under the first.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "second line") && !strings.HasPrefix(line, " ") {
			t.Errorf("notes continuation should be indented, got %q", line)
		}
	}
	if !strings.Contains(out, "second line") {
		t.Errorf("notes second line missing:\n%s", out)
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
