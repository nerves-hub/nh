package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrgMember(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"Alice","email":"alice@example.com","role":"admin"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "member", "alice@example.com",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/users/alice@example.com" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Name:", "Alice", "Email:", "alice@example.com", "Role:", "admin"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q, got:\n%s", want, out)
		}
	}
}

func TestOrgMemberCustomRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"Carol","email":"carol@example.com","role":null,"custom_role":"deployer"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "member", "carol@example.com",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if role := orgOutputLine(out, "Role:"); !strings.HasSuffix(strings.TrimSpace(role), "deployer (custom)") {
		t.Errorf("custom role line: got %q", role)
	}
}

func TestOrgMemberNoRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"Dan","email":"dan@example.com","role":null,"custom_role":null}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "member", "dan@example.com",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if role := orgOutputLine(out, "Role:"); !strings.HasSuffix(strings.TrimSpace(role), "-") {
		t.Errorf("a member with no role should show '-', got %q", role)
	}
}

func TestOrgMemberJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"Alice","email":"alice@example.com","role":"admin"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "member", "alice@example.com",
		"--org", "acme", "--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"email": "alice@example.com"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

func TestOrgMemberMissingEmail(t *testing.T) {
	_, err := execCmd(t, "", "org", "member", "--org", "acme", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "member email missing") {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestOrgMemberMissingOrg(t *testing.T) {
	clearScopeEnv(t)
	_, err := execCmd(t, "",
		"org", "member", "alice@example.com",
		"--token", "tok", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("expected missing-org error, got %v", err)
	}
}
