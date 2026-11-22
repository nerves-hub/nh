package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// membersResponse mirrors OrgUserJSON: role is null when the member holds a
// custom role, whose name is in custom_role.
const membersResponse = `{"data":[
	{"name":"Alice","email":"alice@example.com","role":"admin","custom_role":null},
	{"name":"Bob","email":"bob@example.com","role":"view","custom_role":null},
	{"name":"Carol","email":"carol@example.com","role":null,"custom_role":"deployer"}
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
	carol := orgOutputLine(out, "carol@example.com")
	if !strings.HasSuffix(strings.TrimSpace(carol), "deployer (custom)") {
		t.Errorf("custom role should fill the ROLE column, got line %q", carol)
	}
}

func TestOrgMembersNoRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"Dan","email":"dan@example.com","role":null,"custom_role":null}]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "members", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	dan := orgOutputLine(out, "dan@example.com")
	if !strings.HasSuffix(strings.TrimSpace(dan), "-") {
		t.Errorf("a member with no role should show '-', got line %q", dan)
	}
}

// orgOutputLine returns the first line of out containing substr, or "".
func orgOutputLine(out, substr string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
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
	for _, want := range []string{
		`"email": "alice@example.com"`,
		`"custom_role": "deployer"`,
		// Nulls pass through rather than becoming empty strings.
		`"role": null`,
		`"custom_role": null`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s, got %q", want, out)
		}
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
