package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWhoami(t *testing.T) {
	resetState(t)
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com"}}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"user", "whoami", "--uri", srv.URL, "--token", "tok-123"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/users/me" {
		t.Errorf("path: want /api/users/me, got %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth header: want %q, got %q", "Bearer tok-123", gotAuth)
	}
	if !strings.Contains(out.String(), "alice@example.com") {
		t.Errorf("output should include email, got %q", out.String())
	}
}

func TestWhoamiJSON(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com"}}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"user", "whoami", "--uri", srv.URL, "--token", "tok", "--output", "json"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"name": "alice"`) {
		t.Errorf("json output expected, got %q", out.String())
	}
}

func TestWhoamiUnauthenticated(t *testing.T) {
	resetState(t)
	// Ensure the developer's shell token does not leak into this case.
	t.Setenv("NERVES_CLOUD_TOKEN", "")
	t.Setenv("NERVES_HUB_TOKEN", "")

	var out bytes.Buffer
	// No token via flag/env and an empty data dir => no saved token.
	rootCmd.SetArgs([]string{"user", "whoami", "--uri", "https://example.com", "--data-dir", t.TempDir()})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention authentication, got %v", err)
	}
}
