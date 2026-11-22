package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// deploymentRequest is one request a fake deployment server saw.
type deploymentRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// deployment returns the "deployment" wrapper of a PUT body.
func (r deploymentRequest) deployment(t *testing.T) map[string]any {
	t.Helper()
	dep, ok := r.Body["deployment"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s: body should be wrapped in deployment, got %+v", r.Method, r.Path, r.Body)
	}
	return dep
}

// fakeDeploymentServer records each request and answers with respond, which
// gets the request's index and may return a non-2xx status.
func fakeDeploymentServer(t *testing.T, respond func(i int, r deploymentRequest) (int, string)) (*Client, *[]deploymentRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []deploymentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := deploymentRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil && r.ContentLength != 0 {
			_ = json.NewDecoder(r.Body).Decode(&req.Body)
		}
		mu.Lock()
		i := len(reqs)
		reqs = append(reqs, req)
		mu.Unlock()

		status, body := respond(i, req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv), &reqs
}

const (
	testDeploymentsPath = "/api/orgs/acme/products/thermostat/deployments"
	testDeploymentPath  = testDeploymentsPath + "/production"
)

// release returns the flat body of a POST to the group's releases endpoint.
func (r deploymentRequest) release(t *testing.T) map[string]any {
	t.Helper()
	if r.Method != http.MethodPost || r.Path != testDeploymentPath+"/releases" {
		t.Fatalf("want POST %s/releases, got %s %s", testDeploymentPath, r.Method, r.Path)
	}
	return r.Body
}

func deploymentStr(s string) *string { return &s }
func deploymentBool(b bool) *bool    { return &b }

func TestCreateDeploymentAlwaysSendsConditions(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":{"name":"production","state":"off"}}`
	})

	d, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1"})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if d.Name != "production" {
		t.Errorf("deployment: %+v", d)
	}
	if len(*reqs) != 1 {
		t.Fatalf("want 1 request, got %+v", *reqs)
	}
	r := (*reqs)[0]
	if r.Method != http.MethodPost || r.Path != testDeploymentsPath {
		t.Errorf("request: %s %s", r.Method, r.Path)
	}
	// create_changeset does cast_embed(:conditions, required: true).
	cond, ok := r.Body["conditions"].(map[string]any)
	if !ok {
		t.Fatalf("conditions must always be sent, got %+v", r.Body)
	}
	if cond["version"] != "" {
		t.Errorf("conditions.version: %+v", cond)
	}
	if tags, ok := cond["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("conditions.tags should be [], got %+v", cond["tags"])
	}
	if _, ok := cond["tag_operator"]; ok {
		t.Errorf("tag_operator should be left to the server default, got %+v", cond)
	}
	for _, k := range []string{"state", "delta_updatable", "notes", "deployment"} {
		if _, ok := r.Body[k]; ok {
			t.Errorf("unset %q should be omitted, got %+v", k, r.Body)
		}
	}
}

func TestCreateDeploymentSendsCastFields(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":{"name":"production","state":"off"}}`
	})

	tags := []string{"prod", "eu"}
	_, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production", DeploymentInput{
		Firmware:       "fw-1",
		Version:        deploymentStr(">= 1.0.0"),
		Tags:           &tags,
		TagOperator:    "or",
		DeltaUpdatable: deploymentBool(false),
		Notes:          deploymentStr("summer batch"),
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	body := (*reqs)[0].Body
	if body["name"] != "production" || body["firmware"] != "fw-1" ||
		body["delta_updatable"] != false || body["notes"] != "summer batch" {
		t.Errorf("body: %+v", body)
	}
	cond, _ := body["conditions"].(map[string]any)
	if cond["version"] != ">= 1.0.0" || cond["tag_operator"] != "or" {
		t.Errorf("conditions: %+v", cond)
	}
	if got, _ := cond["tags"].([]any); len(got) != 2 || got[0] != "prod" || got[1] != "eu" {
		t.Errorf("conditions.tags: %+v", cond["tags"])
	}
}

func TestCreateDeploymentStateOnTurnsGroupOn(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(i int, _ deploymentRequest) (int, string) {
		if i == 0 {
			return http.StatusCreated, `{"data":{"name":"production","state":"off","is_active":false}}`
		}
		return http.StatusOK, `{"data":{"name":"production","state":"on","is_active":true}}`
	})

	d, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1", State: "on"})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if !d.IsActive || d.State != "on" {
		t.Errorf("should return the group after turning it on, got %+v", d)
	}
	if len(*reqs) != 2 {
		t.Fatalf("want POST then PUT, got %+v", *reqs)
	}
	// create_changeset doesn't cast is_active, so state isn't sent to it.
	if _, ok := (*reqs)[0].Body["state"]; ok {
		t.Errorf("create body should not carry state, got %+v", (*reqs)[0].Body)
	}
	put := (*reqs)[1]
	if put.Method != http.MethodPut || put.Path != testDeploymentPath {
		t.Errorf("second request: %s %s", put.Method, put.Path)
	}
	dep := put.deployment(t)
	if len(dep) != 1 || dep["state"] != "on" {
		t.Errorf("turn-on body should be just state, got %+v", dep)
	}
}

func TestCreateDeploymentStateOffSendsOneRequest(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":{"name":"production","state":"off"}}`
	})
	if _, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1", State: "off"}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("a new group is already off; want 1 request, got %+v", *reqs)
	}
}

func TestCreateDeploymentReportsFailedTurnOn(t *testing.T) {
	c, _ := fakeDeploymentServer(t, func(i int, _ deploymentRequest) (int, string) {
		if i == 0 {
			return http.StatusCreated, `{"data":{"name":"production","state":"off"}}`
		}
		return http.StatusForbidden, `{"errors":{"detail":"forbidden"}}`
	})

	d, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1", State: "on"})
	if err == nil {
		t.Fatal("want an error when turning the group on fails")
	}
	if !strings.Contains(err.Error(), "was created but is still off") {
		t.Errorf("error should say the group exists but is off, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("error should wrap the API error, got %#v", err)
	}
	if d == nil || d.Name != "production" {
		t.Errorf("should still return the created group, got %+v", d)
	}
}

func TestCreateDeploymentRejectsReleaseFields(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":{}}`
	})
	for _, in := range []DeploymentInput{
		{Firmware: "fw-1", Archive: "ar-1"},
		{Firmware: "fw-1", Required: deploymentBool(true)},
	} {
		if _, err := c.CreateDeployment(context.Background(), "acme", "thermostat", "production", in); err == nil {
			t.Errorf("%+v: want an error", in)
		}
	}
	if len(*reqs) != 0 {
		t.Errorf("no request should be sent, got %+v", *reqs)
	}
}

func TestUpdateDeploymentSettingsOnly(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","state":"on"}}`
	})

	tags := []string{}
	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production", DeploymentInput{
		State:       "on",
		Version:     deploymentStr(""),
		Tags:        &tags,
		TagOperator: "and",
		Notes:       deploymentStr("why"),
	})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("want 1 request, got %+v", *reqs)
	}
	r := (*reqs)[0]
	if r.Method != http.MethodPut || r.Path != testDeploymentPath {
		t.Errorf("request: %s %s", r.Method, r.Path)
	}
	dep := r.deployment(t)
	if dep["state"] != "on" || dep["notes"] != "why" {
		t.Errorf("deployment: %+v", dep)
	}
	for _, k := range []string{"firmware", "archive", "required", "delta_updatable"} {
		if _, ok := dep[k]; ok {
			t.Errorf("%q must not be sent, got %+v", k, dep)
		}
	}
	// "" and [] clear the conditions, so they must reach the server.
	cond, _ := dep["conditions"].(map[string]any)
	if v, ok := cond["version"]; !ok || v != "" {
		t.Errorf("conditions.version should be sent as \"\", got %+v", cond)
	}
	if got, ok := cond["tags"].([]any); !ok || len(got) != 0 {
		t.Errorf("conditions.tags should be sent as [], got %+v", cond)
	}
	if cond["tag_operator"] != "and" {
		t.Errorf("conditions.tag_operator: %+v", cond)
	}
}

func TestUpdateDeploymentReleaseOnly(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","firmware_uuid":"fw-2"}}`
	})

	d, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", Archive: "ar-1", Required: deploymentBool(true)})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if d.FirmwareUUID != "fw-2" {
		t.Errorf("deployment: %+v", d)
	}
	if len(*reqs) != 2 {
		t.Fatalf("want the release then a read of the group, got %+v", *reqs)
	}
	rel := (*reqs)[0].release(t)
	if len(rel) != 3 || rel["firmware"] != "fw-2" || rel["archive"] != "ar-1" || rel["required"] != true {
		t.Errorf("release body: %+v", rel)
	}
	if r := (*reqs)[1]; r.Method != http.MethodGet || r.Path != testDeploymentPath {
		t.Errorf("second request: %s %s", r.Method, r.Path)
	}
}

// The controller's release clause ignores everything but the release, so a
// firmware change and setting changes go in separate requests: settings
// first, then the release, then turning the group on.
func TestUpdateDeploymentSplitsReleaseFromSettings(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","state":"on","firmware_uuid":"fw-2"}}`
	})

	tags := []string{"prod"}
	d, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production", DeploymentInput{
		Firmware:       "fw-2",
		State:          "on",
		Tags:           &tags,
		DeltaUpdatable: deploymentBool(true),
	})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if d.State != "on" || d.FirmwareUUID != "fw-2" {
		t.Errorf("deployment: %+v", d)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want settings, release, turn-on; got %+v", *reqs)
	}

	settings := (*reqs)[0].deployment(t)
	for _, k := range []string{"firmware", "archive", "state"} {
		if _, ok := settings[k]; ok {
			t.Errorf("settings request must not carry %q, got %+v", k, settings)
		}
	}
	if settings["delta_updatable"] != true {
		t.Errorf("settings: %+v", settings)
	}
	cond, _ := settings["conditions"].(map[string]any)
	if got, _ := cond["tags"].([]any); len(got) != 1 || got[0] != "prod" {
		t.Errorf("settings conditions: %+v", cond)
	}
	if release := (*reqs)[1].release(t); len(release) != 1 || release["firmware"] != "fw-2" {
		t.Errorf("second request should be the release alone, got %+v", release)
	}
	if on := (*reqs)[2].deployment(t); len(on) != 1 || on["state"] != "on" {
		t.Errorf("last request should only turn the group on, got %+v", on)
	}
}

// Narrowing the conditions must land before the release, or the release goes
// out to devices that only matched the old conditions.
func TestUpdateDeploymentConditionsBeforeRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production"}}`
	})

	tags := []string{"beta"}
	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", Tags: &tags}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want settings, release, read; got %+v", *reqs)
	}
	if _, ok := (*reqs)[0].deployment(t)["conditions"]; !ok {
		t.Errorf("first request should change the conditions, got %+v", (*reqs)[0].Body)
	}
	if got := (*reqs)[1].release(t); got["firmware"] != "fw-2" {
		t.Errorf("second request should be the release, got %+v", got)
	}
}

func TestUpdateDeploymentStateOnAfterRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","state":"on"}}`
	})

	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", State: "on"}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	// Turning the group on answers with the group, so it isn't read again.
	if len(*reqs) != 2 {
		t.Fatalf("want release then turn-on, got %+v", *reqs)
	}
	if got := (*reqs)[0].release(t); len(got) != 1 || got["firmware"] != "fw-2" {
		t.Errorf("first request should be the release, got %+v", got)
	}
	if got := (*reqs)[1].deployment(t); len(got) != 1 || got["state"] != "on" {
		t.Errorf("second request should turn the group on, got %+v", got)
	}
}

func TestUpdateDeploymentTurnsOffBeforeRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","state":"off"}}`
	})

	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", State: "off"}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want turn-off, release, read; got %+v", *reqs)
	}
	if got := (*reqs)[0].deployment(t); got["state"] != "off" {
		t.Errorf("first request should turn the group off, got %+v", got)
	}
	if got := (*reqs)[1].release(t); got["firmware"] != "fw-2" {
		t.Errorf("second request should be the release, got %+v", got)
	}
}

func TestUpdateDeploymentArchiveKeepsCurrentFirmware(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(_ int, r deploymentRequest) (int, string) {
		if r.Method == http.MethodGet {
			return http.StatusOK, `{"data":{"name":"production","firmware_uuid":"fw-current"}}`
		}
		return http.StatusOK, `{"data":{"name":"production","archive_uuid":"ar-1"}}`
	})

	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Archive: "ar-1"}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want GET, release, GET; got %+v", *reqs)
	}
	if r := (*reqs)[0]; r.Method != http.MethodGet || r.Path != testDeploymentPath {
		t.Errorf("first request: %s %s", r.Method, r.Path)
	}
	rel := (*reqs)[1].release(t)
	if rel["firmware"] != "fw-current" || rel["archive"] != "ar-1" {
		t.Errorf("release body: %+v", rel)
	}
}

func TestUpdateDeploymentReportsPartialFailure(t *testing.T) {
	c, _ := fakeDeploymentServer(t, func(i int, _ deploymentRequest) (int, string) {
		if i == 0 {
			return http.StatusOK, `{"data":{"name":"production"}}`
		}
		return http.StatusUnprocessableEntity, `{"errors":{"firmware":["The current release already has this firmware and archive"]}}`
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1", Notes: deploymentStr("n")})
	if err == nil || !strings.Contains(err.Error(), "its settings were updated, but creating the new release failed") {
		t.Errorf("want a partial-failure error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("error should wrap the API error, got %#v", err)
	}
}

func TestUpdateDeploymentReportsFailedTurnOn(t *testing.T) {
	c, _ := fakeDeploymentServer(t, func(i int, _ deploymentRequest) (int, string) {
		if i < 2 {
			return http.StatusOK, `{"data":{"name":"production"}}`
		}
		return http.StatusForbidden, `{"errors":{"detail":"forbidden"}}`
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", State: "on", Notes: deploymentStr("n")})
	want := "its settings were updated and the new release was created, but turning it on failed"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("want %q, got %v", want, err)
	}
}

// A failed first request changed nothing, so its error is passed on as is.
func TestUpdateDeploymentFirstStepFailure(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusForbidden, `{"errors":{"detail":"forbidden"}}`
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", State: "off"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || strings.Contains(err.Error(), "but") {
		t.Errorf("want the bare API error, got %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("should stop after the failed request, got %+v", *reqs)
	}
}

func TestUpdateDeploymentValidation(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{}}`
	})
	for _, in := range []DeploymentInput{
		{},
		{Required: deploymentBool(true)},
		{Required: deploymentBool(true), State: "on"},
	} {
		if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production", in); err == nil {
			t.Errorf("%+v: want an error", in)
		}
	}
	if len(*reqs) != 0 {
		t.Errorf("no request should be sent, got %+v", *reqs)
	}
}

func TestDeploymentDecodesShownFields(t *testing.T) {
	c, _ := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","archive_uuid":"ar-1","notes":"why",
			"conditions":{"version":"","tags":["a"],"tag_operator":"or"},
			"current_release":{"number":2,"required":true,"firmware":{"uuid":"fw-1"}}}}`
	})
	d, err := c.GetDeployment(context.Background(), "acme", "thermostat", "production")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if d.ArchiveUUID != "ar-1" || d.Notes != "why" || d.Conditions.TagOperator != "or" ||
		d.CurrentRelease == nil || !d.CurrentRelease.Required {
		t.Errorf("decoded: %+v (release %+v)", d, d.CurrentRelease)
	}
}
