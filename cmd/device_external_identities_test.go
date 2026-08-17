package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const deviceIdentitiesResponse = `{"data":[
  {"identifier":"c8924b6c9b7a8528b1365ebec4b2e43b6edebef684f8521f12b8caaf6e1b2302",
   "service":"iroh","instance":"default","source":"device_reported",
   "details":{"relay":"https://relay.example.com"},
   "last_reported_at":"2026-08-16T09:14:00Z","inserted_at":"2026-08-14T11:02:31Z","updated_at":"2026-08-16T09:14:00Z"}
]}`

func TestDeviceExternalIdentitiesTable(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deviceIdentitiesResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "external-identities", "dev1",
		"--service", "iroh", "--instance", "default",
		"--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("external-identities: %v\n%s", err, out)
	}

	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev1/external_identities" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotQuery.Get("service") != "iroh" || gotQuery.Get("instance") != "default" {
		t.Errorf("query filters not sent: %v", gotQuery)
	}
	for _, want := range []string{"IDENTIFIER", "SERVICE", "iroh", "default", "device_reported", "2026-08-16"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceExternalIdentitiesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "external-identities", "dev1",
		"--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("external-identities: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No external identities found for device dev1") {
		t.Errorf("expected empty message, got:\n%s", out)
	}
}

func TestDeviceExternalIdentitiesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deviceIdentitiesResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "external-identities", "dev1",
		"--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok", "-o", "json",
	)
	if err != nil {
		t.Fatalf("external-identities json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"identifier": "c8924b6c`) || !strings.Contains(out, `"service": "iroh"`) {
		t.Errorf("json output unexpected:\n%s", out)
	}
}
