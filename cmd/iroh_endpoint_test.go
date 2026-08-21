package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const irohListResponse = `{"data":[
  {"identifier":"c8924b6c9b7a8528b1365ebec4b2e43b6edebef684f8521f12b8caaf6e1b2302",
   "service":"iroh","instance":"default","source":"device_reported","details":{},
   "last_reported_at":"2026-08-16T09:14:00Z","inserted_at":"2026-08-14T11:02:31Z","updated_at":"2026-08-16T09:14:00Z",
   "owner":{"type":"device","device_identifier":"example_device","user_name":null,"user_email":null}},
  {"identifier":"5f691e39f55415be337b2e4cc0dd7291586ab7c4356bf32bab60f46fc78f95d5",
   "service":"iroh","instance":"default","source":"operator","details":{},
   "last_reported_at":null,"inserted_at":"2026-08-15T08:41:12Z","updated_at":"2026-08-15T08:41:12Z",
   "owner":{"type":"user","device_identifier":null,"user_name":"Alex Doe","user_email":"member@example.com"}}
]}`

func TestIrohEndpointListTable(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(irohListResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"iroh-endpoint", "list",
		"--owner", "user", "--search", "c89",
		"--org", "acme", "--uri", srv.URL, "--token", "tok-123",
	)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}

	if gotPath != "/api/orgs/acme/iroh_endpoints" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if !strings.Contains(gotQuery, "owner=user") || !strings.Contains(gotQuery, "search=c89") {
		t.Errorf("query should carry owner and search filters, got %q", gotQuery)
	}
	for _, want := range []string{"IDENTIFIER", "OWNER", "device (example_device)", "user (member@example.com)", "2026-08-16", "-"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestIrohEndpointListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(irohListResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"iroh-endpoint", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok", "-o", "json",
	)
	if err != nil {
		t.Fatalf("list json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"identifier": "c8924b6c`) || !strings.Contains(out, `"type": "device"`) {
		t.Errorf("json output unexpected:\n%s", out)
	}
}

func TestIrohEndpointListRejectsUnknownOwner(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	_, err := execCmd(t, "",
		"iroh-endpoint", "list", "--owner", "operator",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid --owner") {
		t.Fatalf("expected invalid --owner error, got %v", err)
	}
	if called {
		t.Error("no request should be made for an invalid owner filter")
	}
}

func TestIrohEndpointRegister(t *testing.T) {
	var gotMethod, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"identifier":"c8924b6c","service":"iroh","instance":"console","source":"operator","details":{"note":"laptop"},"last_reported_at":null,"inserted_at":"2026-08-15T08:41:12Z","updated_at":"2026-08-15T08:41:12Z","owner":{"type":"user","user_email":"member@example.com"}}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"iroh-endpoint", "register", "c8924b6c",
		"--instance", "console", "--user-email", "member@example.com", "--detail", "note=laptop",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("register: %v\n%s", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/iroh_endpoints" {
		t.Errorf("request: got %s %s", gotMethod, gotPath)
	}
	if body["identifier"] != "c8924b6c" || body["instance"] != "console" || body["user_email"] != "member@example.com" {
		t.Errorf("body missing fields: %+v", body)
	}
	details, ok := body["details"].(map[string]any)
	if !ok || details["note"] != "laptop" {
		t.Errorf("body details not sent: %+v", body["details"])
	}
	if !strings.Contains(out, "Registered iroh endpoint c8924b6c") {
		t.Errorf("output unexpected:\n%s", out)
	}
}

func TestIrohEndpointRegisterRejectsBadDetail(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	_, err := execCmd(t, "",
		"iroh-endpoint", "register", "c8924b6c", "--detail", "novalue",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "expected key=value") {
		t.Fatalf("expected detail parse error, got %v", err)
	}
	if called {
		t.Error("no request should be made when a --detail is malformed")
	}
}

func TestIrohEndpointShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"c8924b6c","service":"iroh","instance":"default","source":"device_reported","details":{"relay":"https://r.example.com"},"last_reported_at":"2026-08-16T09:14:00Z","inserted_at":"2026-08-14T11:02:31Z","updated_at":"2026-08-16T09:14:00Z","owner":{"type":"device","device_identifier":"example_device"}}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"iroh-endpoint", "show", "c8924b6c", "--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if gotPath != "/api/orgs/acme/iroh_endpoints/c8924b6c" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Identifier:", "device (example_device)", "Details:", "relay", "https://r.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q, got:\n%s", want, out)
		}
	}
}

func TestIrohEndpointDeleteConfirmed(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "y\n",
		"iroh-endpoint", "delete", "c8924b6c", "--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/iroh_endpoints/c8924b6c" {
		t.Errorf("request: got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Deleted iroh endpoint c8924b6c") {
		t.Errorf("output unexpected:\n%s", out)
	}
}

func TestIrohEndpointDeleteAborted(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	out, err := execCmd(t, "n\n",
		"iroh-endpoint", "delete", "c8924b6c", "--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("delete abort: %v\n%s", err, out)
	}
	if called {
		t.Error("no DELETE should be sent when the user declines")
	}
	if !strings.Contains(out, "Aborted") {
		t.Errorf("expected abort message, got:\n%s", out)
	}
}
