package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const orgsWithProducts = `{"data":[
	{"name":"acme","inserted_at":"2026-01-02 00:00:00Z","updated_at":"2026-02-02 00:00:00Z","products":[{"id":1,"name":"thermostat"},{"id":2,"name":"doorbell"}]},
	{"name":"globex","inserted_at":"2026-03-04 00:00:00Z","updated_at":"2026-03-05 00:00:00Z"}
]}`

func TestOrgShow(t *testing.T) {
	var gotPath, gotInclude string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInclude = r.URL.Query().Get("include")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsWithProducts))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "show", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotInclude != "products" {
		t.Errorf("expected include=products, got %q", gotInclude)
	}
	for _, want := range []string{"Name:", "acme", "Created:", "Products:", "thermostat", "doorbell"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestOrgShowJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsWithProducts))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "show", "acme",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"name": "acme"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

func TestOrgShowNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsWithProducts))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"org", "show", "nope",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestOrgShowDefaultsToConfiguredOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsWithProducts))
	}))
	defer srv.Close()

	// No name argument: it should default to --org.
	out, err := execCmd(t, "",
		"org", "show",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "thermostat") {
		t.Errorf("should show the default org, got:\n%s", out)
	}
}

func TestOrgShowNoOrgGiven(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	_, err := execCmd(t, "",
		"org", "show",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no organization given") {
		t.Errorf("want no-org error, got %v", err)
	}
}
