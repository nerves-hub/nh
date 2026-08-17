package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrgRemoveMember(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "remove-member", "alice@example.com", "--yes",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: want DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/users/alice@example.com" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Removed alice@example.com from acme") {
		t.Errorf("output: %q", out)
	}
}

func TestOrgRemoveMemberAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "\n",
		"org", "remove-member", "alice@example.com",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no removal should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}

func TestOrgRemoveMemberConfirmYes(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "y\n",
		"org", "remove-member", "alice@example.com",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("removal should be issued after confirmation")
	}
	if !strings.Contains(out, "Remove alice@example.com from acme? [y/N]") {
		t.Errorf("output should show the prompt, got %q", out)
	}
}

func TestOrgRemoveMemberMissingEmail(t *testing.T) {
	_, err := execCmd(t, "",
		"org", "remove-member",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || err.Error() != "Member email missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
