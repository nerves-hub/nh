package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/config"
)

// shortCLIPolling speeds up the poll loop for tests and restores the defaults.
func shortCLIPolling(t *testing.T, timeout time.Duration) {
	t.Helper()
	oi, ot := cliPollInterval, cliPollTimeout
	cliPollInterval = time.Millisecond
	cliPollTimeout = timeout
	t.Cleanup(func() { cliPollInterval, cliPollTimeout = oi, ot })
}

func TestUserLogin(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)

	var posts, polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/cli_session":
			posts++
			var body struct{ Note string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.HasPrefix(body.Note, "nh dev (") {
				t.Errorf("note: want prefix %q, got %q", "nh dev (", body.Note)
			}
			_, _ = w.Write([]byte(`{"data":{"token":"sess-tok","url":"https://example.test/auth/cli/sess-tok","confirmation_code":438231}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/cli_session/sess-tok":
			polls++
			if polls < 3 {
				_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"status":"ready","user_token":"nhu_secret"}}`))
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	out, err := execCmd(t, "",
		"user", "login",
		"--uri", srv.URL, "--data-dir", dir,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if posts != 1 {
		t.Errorf("expected exactly one start request, got %d", posts)
	}
	if polls < 3 {
		t.Errorf("expected to poll until ready, got %d polls", polls)
	}

	// The verification URL and confirmation code are shown to the user.
	if !strings.Contains(out, "https://example.test/auth/cli/sess-tok") {
		t.Errorf("output should show the login URL, got:\n%s", out)
	}
	if !strings.Contains(out, "Confirmation code: 4 3 8 2 3 1") {
		t.Errorf("output should show the spaced confirmation code, got:\n%s", out)
	}

	// The returned user token is saved.
	saved, err := config.LoadToken(dir)
	if err != nil {
		t.Fatalf("reading saved token: %v", err)
	}
	if saved != "nhu_secret" {
		t.Errorf("saved token: want %q, got %q", "nhu_secret", saved)
	}
}

func TestIsCancelKey(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want bool
	}{
		"lone esc":        {[]byte{0x1b}, true},
		"ctrl-c":          {[]byte{0x03}, true},
		"esc sequence":    {[]byte{0x1b, '[', 'A'}, false}, // up arrow
		"regular key":     {[]byte{'y'}, false},
		"enter":           {[]byte{'\r'}, false},
		"ctrl-c in chunk": {[]byte{'a', 0x03}, true},
		"empty":           {nil, false},
	}
	for name, tc := range cases {
		if got := isCancelKey(tc.in); got != tc.want {
			t.Errorf("%s: isCancelKey(%v) = %v, want %v", name, tc.in, got, tc.want)
		}
	}
}

func TestUserLoginTimeout(t *testing.T) {
	shortCLIPolling(t, 30*time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"token":"sess-tok","url":"https://example.test/x"}}`))
			return
		}
		// Never becomes ready.
		_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, err := execCmd(t, "",
		"user", "login",
		"--uri", srv.URL, "--data-dir", dir,
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got %v", err)
	}
	if got, _ := config.LoadToken(dir); got != "" {
		t.Errorf("no token should be saved on timeout, got %q", got)
	}
}

func TestUserLoginReadyWithoutToken(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"token":"sess-tok","url":"https://example.test/x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"status":"ready"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"user", "login",
		"--uri", srv.URL, "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("expected missing-token error, got %v", err)
	}
}
