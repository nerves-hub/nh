package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/config"
)

func TestUserAuthSavesToken(t *testing.T) {
	resetState(t)
	var gotPath, gotEmail, gotPassword, gotAuthHeader, gotNote string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthHeader = r.Header.Get("Authorization")
		var body struct {
			Email, Password, Note string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotEmail, gotPassword, gotNote = body.Email, body.Password, body.Note
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"token":"nhu_secret","name":"alice"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	var out bytes.Buffer

	rootCmd.SetArgs([]string{
		"user", "auth",
		"--email", "alice@example.com",
		"--uri", srv.URL,
		"--data-dir", dir,
		"--non-interactive",
	})
	rootCmd.SetIn(strings.NewReader("hunter2\n"))
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetIn(nil); rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The auth endpoint is hit unauthenticated, with the right credentials.
	if gotPath != "/api/users/login" {
		t.Errorf("path: want /api/users/login, got %q", gotPath)
	}
	if gotAuthHeader != "" {
		t.Errorf("auth request should be unauthenticated, got header %q", gotAuthHeader)
	}
	if gotEmail != "alice@example.com" || gotPassword != "hunter2" {
		t.Errorf("credentials: got email=%q password=%q", gotEmail, gotPassword)
	}
	if !strings.HasPrefix(gotNote, "nh dev (") {
		t.Errorf("note: want prefix %q, got %q", "nh dev (", gotNote)
	}

	// The returned token is persisted to the settings file.
	saved, err := config.LoadToken(dir)
	if err != nil {
		t.Fatalf("reading saved token: %v", err)
	}
	if saved != "nhu_secret" {
		t.Errorf("saved token: want %q, got %q", "nhu_secret", saved)
	}

	if !strings.Contains(out.String(), "Successfully authenticated as alice") {
		t.Errorf("output should confirm sign-in, got %q", out.String())
	}
}

func TestUserAuthAPIError(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":{"detail":"invalid credentials"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	rootCmd.SetArgs([]string{
		"user", "auth",
		"--email", "alice@example.com",
		"--uri", srv.URL,
		"--data-dir", dir,
		"--non-interactive",
	})
	rootCmd.SetIn(strings.NewReader("wrong\n"))
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	t.Cleanup(func() { rootCmd.SetIn(nil); rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected auth to fail on 401")
	}

	// No token should be saved on failure.
	if got, _ := config.LoadToken(dir); got != "" {
		t.Errorf("no token expected on failure, got %q", got)
	}
}
