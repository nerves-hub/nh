package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const membersResponse = `{"data":[
	{"name":"Alice","email":"alice@example.com","role":"admin"},
	{"name":"Bob","email":"bob@example.com","role":"view"}
]}`

func TestOrgMembers(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(membersResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "members", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/users" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"NAME", "EMAIL", "ROLE", "Alice", "alice@example.com", "admin", "Bob", "view"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
}

func TestOrgMembersAlias(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// The "users" alias resolves to the same command.
	out, err := execCmd(t, "",
		"org", "users", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/users" {
		t.Errorf("alias path: got %q", gotPath)
	}
	if !strings.Contains(out, "No members found in acme") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestOrgMembersDefaultsToConfiguredOrg(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(membersResponse))
	}))
	defer srv.Close()

	// No name argument: defaults to --org.
	if _, err := execCmd(t, "",
		"org", "members",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/users" {
		t.Errorf("path: got %q", gotPath)
	}
}

func TestOrgMembersJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(membersResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "members", "acme",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"email": "alice@example.com"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

func TestOrgMembersNoOrgGiven(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	_, err := execCmd(t, "",
		"org", "members",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no organization given") {
		t.Errorf("want no-org error, got %v", err)
	}
}
