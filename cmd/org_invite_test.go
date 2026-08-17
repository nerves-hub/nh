package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrgInvite(t *testing.T) {
	var gotMethod, gotPath, gotEmail, gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotEmail, gotRole = body.Email, body.Role
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "invite", "newbie@example.com", "view",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/users/invite" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotEmail != "newbie@example.com" || gotRole != "view" {
		t.Errorf("body: got email=%q role=%q", gotEmail, gotRole)
	}
	if !strings.Contains(out, "Invited newbie@example.com to acme as view") {
		t.Errorf("output: %q", out)
	}
}

func TestOrgInviteInvalidRole(t *testing.T) {
	_, err := execCmd(t, "",
		"org", "invite", "newbie@example.com", "owner",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("expected invalid-role error, got %v", err)
	}
}

func TestOrgInviteMissingArgs(t *testing.T) {
	_, err := execCmd(t, "",
		"org", "invite", "newbie@example.com",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invite <email> <role>") {
		t.Errorf("expected usage error, got %v", err)
	}
}
