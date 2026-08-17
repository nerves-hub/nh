package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrgSetRole(t *testing.T) {
	var gotMethod, gotPath, gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct {
			Role string `json:"role"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotRole = body.Role
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"Alice","email":"alice@example.com","role":"manage"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"org", "set-role", "alice@example.com", "manage",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method: want PUT, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/users/alice@example.com" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotRole != "manage" {
		t.Errorf("request role: got %q", gotRole)
	}
	if !strings.Contains(out, "Set alice@example.com role to manage in acme") {
		t.Errorf("output: %q", out)
	}
}

func TestOrgSetRoleNormalizesCase(t *testing.T) {
	var gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Role string `json:"role"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotRole = body.Role
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	if _, err := execCmd(t, "",
		"org", "set-role", "alice@example.com", "ADMIN",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotRole != "admin" {
		t.Errorf("role should be lowercased, got %q", gotRole)
	}
}

func TestOrgSetRoleInvalid(t *testing.T) {
	_, err := execCmd(t, "",
		"org", "set-role", "alice@example.com", "superuser",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("expected invalid-role error, got %v", err)
	}
}

func TestOrgSetRoleMissingArgs(t *testing.T) {
	_, err := execCmd(t, "",
		"org", "set-role", "alice@example.com",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "set-role <email> <role>") {
		t.Errorf("expected usage error, got %v", err)
	}
}
