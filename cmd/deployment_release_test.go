package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	releaseGroupPath = "/api/orgs/acme/products/thermostat/deployments/production"
	releaseCmdJSON   = `{"number":3,"firmware":{"uuid":"fw-3","version":"1.0.1","platform":"rpi0","architecture":"arm"},
		"archive_uuid":"ar-1","required":true,"description":"Fixes Wi-Fi","notes":"line one\nline two",
		"connecting_code":"Logger.info(:hi)","connecting_code_mode":"first","delta_status":"ready",
		"is_current":true,"created_by":{"type":"user","name":"Jane Doe","email":"jane@example.com"},
		"inserted_at":"2026-11-22T10:00:00Z","updated_at":"2026-11-22T10:05:00Z"}`
)

type releaseRecordedRequest struct {
	method, path string
	body         map[string]any
}

// releaseFake answers release requests with status and body, keyed by
// "METHOD path"; anything else is a 404 like NervesHub's.
func releaseFake(t *testing.T, routes map[string]struct {
	status int
	body   string
}) (*httptest.Server, *[]releaseRecordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []releaseRecordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := releaseRecordedRequest{method: r.Method, path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&rec.body)
		mu.Lock()
		reqs = append(reqs, rec)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		route, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":{"detail":"Resource Not Found or Authorization Insufficient"}}`))
			return
		}
		w.WriteHeader(route.status)
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

type route = struct {
	status int
	body   string
}

func releaseArgs(srv *httptest.Server, args ...string) []string {
	return append(args, "--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok")
}

func TestDeploymentReleaseList(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"GET " + releaseGroupPath + "/releases": {200, `{"data":[` + releaseCmdJSON + `,
			{"number":2,"firmware":{"uuid":"fw-2","version":"1.0.0"},"required":false,"delta_status":"failed",
			 "is_current":false,"created_by":{"type":"api_key","name":"CI"},"inserted_at":"2026-11-01T10:00:00Z"},
			{"number":1,"firmware":{"uuid":"fw-1"},"created_by":null}]}`},
	})

	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "list", "production")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r := (*reqs)[0]; r.method != http.MethodGet || r.path != releaseGroupPath+"/releases" {
		t.Errorf("request: %s %s", r.method, r.path)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("want a header and 3 rows, got:\n%s", out)
	}
	for _, want := range []string{"NUMBER", "CURRENT", "VERSION", "FIRMWARE", "ARCHIVE", "REQUIRED", "DELTA", "CREATED BY", "CREATED"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("header missing %q: %s", want, lines[0])
		}
	}
	for _, want := range []string{"3", "*", "1.0.1", "fw-3", "ar-1", "yes", "ready", "Jane Doe <jane@example.com>", "2026-11-22"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("current release row missing %q: %s", want, lines[1])
		}
	}
	for _, want := range []string{"fw-2", "no", "failed", "API key CI"} {
		if !strings.Contains(lines[2], want) {
			t.Errorf("second row missing %q: %s", want, lines[2])
		}
	}
	if strings.Contains(lines[2], "*") {
		t.Errorf("only the current release is marked: %s", lines[2])
	}
}

func TestDeploymentReleaseListJSONAndEmpty(t *testing.T) {
	srv, _ := releaseFake(t, map[string]route{
		"GET " + releaseGroupPath + "/releases": {200, `{"data":[]}`},
	})
	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "list", "production")...)
	if err != nil || !strings.Contains(out, `Deployment group "production" in acme/thermostat has no releases.`) {
		t.Errorf("empty list: %q (%v)", out, err)
	}

	srv, _ = releaseFake(t, map[string]route{
		"GET " + releaseGroupPath + "/releases": {200, `{"data":[` + releaseCmdJSON + `]}`},
	})
	out, err = execCmd(t, "", releaseArgs(srv, "deployment", "release", "list", "production", "-o", "json")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 1 || got[0]["number"] != float64(3) ||
		got[0]["is_current"] != true {
		t.Errorf("json: %q (%v)", out, err)
	}
}

func TestDeploymentReleaseShow(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"GET " + releaseGroupPath + "/releases/3": {200, `{"data":` + releaseCmdJSON + `}`},
	})

	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "show", "production", "3")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r := (*reqs)[0]; r.method != http.MethodGet || r.path != releaseGroupPath+"/releases/3" {
		t.Errorf("request: %s %s", r.method, r.path)
	}
	for _, want := range []string{
		"Number:", "3", "Current:", "Firmware:", "fw-3", "Version:", "1.0.1", "Platform:", "rpi0",
		"Architecture:", "arm", "Archive:", "ar-1", "Required:", "yes", "Delta status:", "ready",
		"Created by:", "Jane Doe <jane@example.com>", "Created:", "2026-11-22 10:00:00 UTC",
		"Updated:", "2026-11-22 10:05:00 UTC", "Description:", "Fixes Wi-Fi", "line one", "line two",
		"Connecting code mode:", "first", "Connecting code:", "Logger.info(:hi)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	out, err = execCmd(t, "", releaseArgs(srv, "deployment", "release", "show", "production", "3", "-o", "json")...)
	var got map[string]any
	if err != nil || json.Unmarshal([]byte(out), &got) != nil || got["connecting_code"] != "Logger.info(:hi)" {
		t.Errorf("json: %q (%v)", out, err)
	}
}

func TestDeploymentReleaseShowRejectsBadNumber(t *testing.T) {
	for _, arg := range []string{"0", "1.5", "three"} {
		_, err := execCmd(t, "", "deployment", "release", "show", "production", arg,
			"--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "invalid release number") {
			t.Errorf("%s: want an invalid-number error, got %v", arg, err)
		}
	}
}

func TestDeploymentReleaseShowMissingRelease(t *testing.T) {
	srv, _ := releaseFake(t, map[string]route{
		"GET " + releaseGroupPath + "/releases": {200, `{"data":[]}`},
	})
	_, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "show", "production", "7")...)
	if err == nil || !strings.Contains(err.Error(), `deployment group "production" has no release 7`) {
		t.Errorf("want a missing-release error, got %v", err)
	}
}

func TestDeploymentReleaseCreate(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"POST " + releaseGroupPath + "/releases": {201, `{"data":` + releaseCmdJSON + `}`},
	})

	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "create", "production",
		"--firmware", "fw-3", "--archive", "ar-1", "--required",
		"--description", "Fixes Wi-Fi", "--notes", "after the field test",
		"--connecting-code", "Logger.info(:hi)", "--connecting-code-mode", "first")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r := (*reqs)[0]
	if r.method != http.MethodPost || r.path != releaseGroupPath+"/releases" {
		t.Errorf("request: %s %s", r.method, r.path)
	}
	want := map[string]any{
		"firmware": "fw-3", "archive": "ar-1", "required": true, "description": "Fixes Wi-Fi",
		"notes": "after the field test", "connecting_code": "Logger.info(:hi)", "connecting_code_mode": "first",
	}
	if len(r.body) != len(want) {
		t.Errorf("body: %+v", r.body)
	}
	for k, v := range want {
		if r.body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, r.body[k], v)
		}
	}
	if !strings.Contains(out, `Created release 3 of deployment group "production" in acme/thermostat (firmware 1.0.1 (fw-3))`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentReleaseCreateCodeFromStdinAndJSON(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"POST " + releaseGroupPath + "/releases": {201, `{"data":` + releaseCmdJSON + `}`},
	})

	out, err := execCmd(t, "IO.puts(:from_stdin)\n", releaseArgs(srv, "deployment", "release", "create", "production",
		"--firmware", "fw-3", "--connecting-code-file", "-", "-o", "json")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := (*reqs)[0].body
	if len(body) != 2 || body["firmware"] != "fw-3" || body["connecting_code"] != "IO.puts(:from_stdin)\n" {
		t.Errorf("body: %+v", body)
	}
	var got map[string]any
	if json.Unmarshal([]byte(out), &got) != nil || got["number"] != float64(3) {
		t.Errorf("json: %q", out)
	}
}

func TestDeploymentReleaseCreateValidation(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.exs")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		args []string
		want string
	}{
		"no firmware":   {[]string{"--archive", "ar"}, "--firmware is required"},
		"empty archive": {[]string{"--firmware", "fw", "--archive", ""}, "--archive cannot be empty"},
		"both codes":    {[]string{"--firmware", "fw", "--connecting-code", "x", "--connecting-code-file", "f"}, "use only one of"},
		"empty code":    {[]string{"--firmware", "fw", "--connecting-code-file", empty}, "the connecting code is empty"},
		"bad mode":      {[]string{"--firmware", "fw", "--connecting-code-mode", "middle"}, "invalid --connecting-code-mode"},
	}
	for name, tc := range cases {
		args := append([]string{"deployment", "release", "create", "production"}, tc.args...)
		args = append(args, "--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
		if _, err := execCmd(t, "", args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", name, tc.want, err)
		}
	}
}

func TestDeploymentReleaseUpdate(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"PATCH " + releaseGroupPath + "/releases/3": {200, `{"data":` + releaseCmdJSON + `}`},
	})

	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "update", "production", "3",
		"--required=false", "--connecting-code", "IO.puts(1)", "--connecting-code-mode", "override")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r := (*reqs)[0]
	if r.method != http.MethodPatch || r.path != releaseGroupPath+"/releases/3" {
		t.Errorf("request: %s %s", r.method, r.path)
	}
	if len(r.body) != 3 || r.body["required"] != false || r.body["connecting_code"] != "IO.puts(1)" ||
		r.body["connecting_code_mode"] != "override" {
		t.Errorf("body: %+v", r.body)
	}
	if !strings.Contains(out, `Updated release 3 of deployment group "production" in acme/thermostat`) {
		t.Errorf("output: %q", out)
	}
}

func TestDeploymentReleaseUpdateClearsCode(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"PATCH " + releaseGroupPath + "/releases/3": {200, `{"data":` + releaseCmdJSON + `}`},
	})

	out, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "update", "production", "3",
		"--clear-connecting-code", "-o", "json")...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := (*reqs)[0].body
	if v, ok := body["connecting_code"]; !ok || v != nil || len(body) != 1 {
		t.Errorf("body should be {connecting_code: null}, got %+v", body)
	}
	var got map[string]any
	if json.Unmarshal([]byte(out), &got) != nil || got["number"] != float64(3) {
		t.Errorf("json: %q", out)
	}
}

func TestDeploymentReleaseUpdateValidation(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"nothing":     {nil, "nothing to update"},
		"clear+code":  {[]string{"--clear-connecting-code", "--connecting-code", "x"}, "use only one of"},
		"empty code":  {[]string{"--connecting-code", ""}, "use --clear-connecting-code to remove it"},
		"bad mode":    {[]string{"--connecting-code-mode", "middle"}, "invalid --connecting-code-mode"},
		"no firmware": {[]string{"--firmware", "fw"}, "unknown flag: --firmware"},
	}
	for name, tc := range cases {
		args := append([]string{"deployment", "release", "update", "production", "3"}, tc.args...)
		args = append(args, "--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
		if _, err := execCmd(t, "", args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", name, tc.want, err)
		}
	}
}

// A server without the release API has the group but none of the release
// routes; every command says the server needs upgrading.
func TestDeploymentReleaseCommandsOnOldServer(t *testing.T) {
	for name, args := range map[string][]string{
		"list":   {"list", "production"},
		"show":   {"show", "production", "2"},
		"create": {"create", "production", "--firmware", "fw"},
		"update": {"update", "production", "2", "--required"},
	} {
		srv, reqs := releaseFake(t, map[string]route{
			"GET " + releaseGroupPath: {200, `{"data":{"name":"production"}}`},
		})
		_, err := execCmd(t, "", releaseArgs(srv, append([]string{"deployment", "release"}, args...)...)...)
		if err == nil || !strings.Contains(err.Error(), "doesn't support deployment group releases; upgrade NervesHub") {
			t.Errorf("%s: want the upgrade message, got %v", name, err)
		}
		for _, r := range *reqs {
			if r.method == http.MethodPut {
				t.Errorf("%s: must not fall back to a PUT: %+v", name, *reqs)
			}
		}
	}
}

func TestDeploymentReleaseMissingGroup(t *testing.T) {
	srv, _ := releaseFake(t, nil)
	_, err := execCmd(t, "", releaseArgs(srv, "deployment", "release", "list", "staging")...)
	if err == nil || !strings.Contains(err.Error(), `deployment group "staging" not found in acme/thermostat`) {
		t.Errorf("want a missing-group error, got %v", err)
	}
}

// deployment update on a server without the release API keeps #25's split
// PUTs: settings, then the release PUT, then turning the group on.
func TestDeploymentUpdateFallsBackWithoutReleaseAPI(t *testing.T) {
	srv, reqs := releaseFake(t, map[string]route{
		"PUT " + releaseGroupPath: {200, `{"data":{"name":"production","state":"on","firmware_uuid":"fw-2"}}`},
	})

	if _, err := execCmd(t, "", releaseArgs(srv, "deployment", "update", "production",
		"--firmware", "fw-2", "--required", "--state", "on", "--notes", "n")...); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []struct{ method, path string }{
		{http.MethodPut, releaseGroupPath},
		{http.MethodPost, releaseGroupPath + "/releases"},
		{http.MethodPut, releaseGroupPath},
		{http.MethodPut, releaseGroupPath},
	}
	if len(*reqs) != len(want) {
		t.Fatalf("want %d requests, got %+v", len(want), *reqs)
	}
	for i, w := range want {
		if r := (*reqs)[i]; r.method != w.method || r.path != w.path {
			t.Errorf("request %d: %s %s, want %s %s", i, r.method, r.path, w.method, w.path)
		}
	}
	if rel, _ := (*reqs)[2].body["deployment"].(map[string]any); len(rel) != 2 || rel["firmware"] != "fw-2" || rel["required"] != true {
		t.Errorf("release PUT body: %+v", (*reqs)[2].body)
	}
	if on, _ := (*reqs)[3].body["deployment"].(map[string]any); len(on) != 1 || on["state"] != "on" {
		t.Errorf("turn-on body: %+v", (*reqs)[3].body)
	}
}
