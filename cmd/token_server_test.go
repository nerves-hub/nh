package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nerves-hub/nh/internal/config"
)

// newUserServer fakes the auth endpoint (returning token) and /users/me,
// recording the Authorization header each /users/me request carried.
func newUserServer(t *testing.T, token string, authHeaders *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/users/auth":
			_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com","token":"` + token + `"}}`))
		case "/api/users/me":
			*authHeaders = append(*authHeaders, r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// countingServer counts every request it receives.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// Signing in with --uri makes that server the default, so the next command
// goes there with the token. Email and password are both piped on stdin.
func TestAuthWithURIThenCommandUsesThatServer(t *testing.T) {
	var headers []string
	srv := newUserServer(t, "nhu_corp", &headers)
	dir := t.TempDir()

	out, err := execCmd(t, "alice@example.com\nhunter2\n", "user", "auth", "--uri", srv.URL, "--data-dir", dir)
	if err != nil {
		t.Fatalf("auth: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Default server set to "+srv.URL) {
		t.Errorf("auth output should say the default server changed:\n%s", out)
	}

	out, err = execCmd(t, "", "user", "whoami", "--data-dir", dir)
	if err != nil {
		t.Fatalf("whoami: %v\n%s", err, out)
	}
	if len(headers) != 1 || headers[0] != "Bearer nhu_corp" {
		t.Errorf("whoami should reach the login server with the token, got %v", headers)
	}
}

func TestSavedTokenNotSentToAnotherServer(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.SaveToken(dir, "https://hub.corp", "nhu_corp"); err != nil {
		t.Fatal(err)
	}
	srv, hits := countingServer(t)

	_, err := execCmd(t, "", "user", "whoami", "--uri", srv.URL, "--data-dir", dir)
	var mismatch *config.TokenMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("want a token mismatch error, got %v", err)
	}
	if !strings.Contains(err.Error(), "saved token is for https://hub.corp") {
		t.Errorf("error should name the token's server: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("no request should be sent, got %d", n)
	}
}

func TestSavedTokenServerCheck(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.SaveToken(dir, "https://hub.corp", "nhu_corp"); err != nil {
		t.Fatal(err)
	}

	t.Run("default is the token's server", func(t *testing.T) {
		c := runRoot(t, "--data-dir", dir)
		if c.URI != "https://hub.corp" || c.Token != "nhu_corp" || c.TokenErr != nil {
			t.Errorf("got uri=%q token=%q err=%v", c.URI, c.Token, c.TokenErr)
		}
	})
	t.Run("same server spelled differently", func(t *testing.T) {
		c := runRoot(t, "--data-dir", dir, "--uri", "https://HUB.corp/")
		if c.Token != "nhu_corp" {
			t.Errorf("got token=%q err=%v", c.Token, c.TokenErr)
		}
	})
	t.Run("other server by flag", func(t *testing.T) {
		c := runRoot(t, "--data-dir", dir, "--uri", "https://other.example.com")
		if c.Token != "" || c.TokenErr == nil {
			t.Errorf("got token=%q err=%v", c.Token, c.TokenErr)
		}
	})
	t.Run("other server by env", func(t *testing.T) {
		t.Setenv("NERVES_HUB_URI", "https://other.example.com")
		c := runRoot(t, "--data-dir", dir)
		if c.Token != "" || c.TokenErr == nil {
			t.Errorf("got token=%q err=%v", c.Token, c.TokenErr)
		}
	})
	t.Run("explicit token flag skips the check", func(t *testing.T) {
		c := runRoot(t, "--data-dir", dir, "--uri", "https://other.example.com", "--token", "nhu_other")
		if c.Token != "nhu_other" || c.TokenErr != nil {
			t.Errorf("got token=%q err=%v", c.Token, c.TokenErr)
		}
	})
	t.Run("token env skips the check", func(t *testing.T) {
		t.Setenv("NERVES_HUB_TOKEN", "nhu_env")
		c := runRoot(t, "--data-dir", dir, "--uri", "https://other.example.com")
		if c.Token != "nhu_env" || c.TokenErr != nil {
			t.Errorf("got token=%q err=%v", c.Token, c.TokenErr)
		}
	})
}

// Settings files from before nh recorded token_uri still work with the server
// they were being used with.
func TestOldSettingsFileWithoutTokenServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(config.SettingsFilePath(dir), []byte(`{"uri":"https://hub.corp","token":"nhu_old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := runRoot(t, "--data-dir", dir); c.Token != "nhu_old" {
		t.Errorf("old file should keep working, got token=%q err=%v", c.Token, c.TokenErr)
	}
	if c := runRoot(t, "--data-dir", dir, "--uri", config.DefaultURI); c.Token != "" {
		t.Errorf("old token must not go to another server, got %q", c.Token)
	}

	dir = t.TempDir()
	if err := os.WriteFile(config.SettingsFilePath(dir), []byte(`{"token":"nhu_old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := runRoot(t, "--data-dir", dir); c.Token != "nhu_old" || c.URI != config.DefaultURI {
		t.Errorf("old default-server file should keep working, got uri=%q token=%q", c.URI, c.Token)
	}
}

func TestConfigSetURIWarnsAboutTokenServer(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.SaveToken(dir, "https://hub.corp", "nhu_corp"); err != nil {
		t.Fatal(err)
	}

	out := mustExec(t, "config", "set", "uri", "https://other.example.com", "--data-dir", dir)
	if !strings.Contains(out, "saved token is for https://hub.corp") {
		t.Errorf("config set uri should warn about the token's server:\n%s", out)
	}
	if c := runRoot(t, "--data-dir", dir); c.Token != "" || c.TokenErr == nil {
		t.Errorf("token must not follow the uri change, got token=%q", c.Token)
	}

	out = mustExec(t, "config", "set", "uri", "https://hub.corp/", "--data-dir", dir)
	if strings.Contains(out, "Note:") {
		t.Errorf("no warning expected when the uri matches the token:\n%s", out)
	}
}

func TestConfigLoadRestoresTokenServer(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.SaveToken(dir, "https://work.example.com", "tok-work"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, "config", "save", "work", "--data-dir", dir)
	if _, err := config.SaveToken(dir, "https://home.example.com", "tok-home"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, "config", "load", "work", "--data-dir", dir)

	c := runRoot(t, "--data-dir", dir)
	if c.URI != "https://work.example.com" || c.Token != "tok-work" {
		t.Errorf("got uri=%q token=%q err=%v", c.URI, c.Token, c.TokenErr)
	}
}
