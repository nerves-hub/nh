package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Timestamps use the API's real zoneless format (no trailing Z), which is what
// originally broke decoding into time.Time.
const orgsResponse = `{"data":[
	{"name":"acme","inserted_at":"2026-01-02T15:04:05","updated_at":"2026-02-01T00:00:00"},
	{"name":"globex","inserted_at":"2026-03-04T09:00:00","updated_at":"2026-03-05T00:00:00"}
]}`

func TestOrgListTable(t *testing.T) {
	resetState(t)
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsResponse))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"org", "list", "--uri", srv.URL, "--token", "tok-123"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/orgs" {
		t.Errorf("path: want /api/orgs, got %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth header: want %q, got %q", "Bearer tok-123", gotAuth)
	}

	s := out.String()
	for _, want := range []string{"NAME", "CREATED", "acme", "globex", "2026-01-02"} {
		if !strings.Contains(s, want) {
			t.Errorf("table output missing %q, got:\n%s", want, s)
		}
	}
}

func TestOrgListJSON(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orgsResponse))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"org", "list", "--uri", srv.URL, "--token", "tok", "--output", "json"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"name": "acme"`) {
		t.Errorf("json output expected, got %q", out.String())
	}
}

func TestOrgListEmpty(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"org", "list", "--uri", srv.URL, "--token", "tok"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No organizations found") {
		t.Errorf("expected empty-state message, got %q", out.String())
	}
}

func TestOrgListUnauthenticated(t *testing.T) {
	resetState(t)
	t.Setenv("NERVES_CLOUD_TOKEN", "")
	t.Setenv("NERVES_HUB_TOKEN", "")

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"org", "list", "--uri", "https://example.com", "--data-dir", t.TempDir()})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention authentication, got %v", err)
	}
}
