package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// shortCLIPolling speeds up the poll loop for tests and restores the defaults.
func shortCLIPolling(t *testing.T, timeout time.Duration) {
	t.Helper()
	oo, ot := cliPollOptions, cliPollTimeout
	cliPollOptions = api.CLIPollOptions{Interval: time.Millisecond, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}
	cliPollTimeout = timeout
	t.Cleanup(func() { cliPollOptions, cliPollTimeout = oo, ot })
}

// fakeBrowser makes stdout look like a terminal (or not) and records the URLs
// login tries to open.
func fakeBrowser(t *testing.T, terminal bool) *[]string {
	t.Helper()
	var opened []string
	oo, ot := openBrowser, stdoutIsTerminal
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	stdoutIsTerminal = func(*cobra.Command) bool { return terminal }
	t.Cleanup(func() { openBrowser, stdoutIsTerminal = oo, ot })
	return &opened
}

// loginServer fakes the server side of a CLI login. poll answers each
// GET /api/auth/cli_session/sess-tok (n counts from 1); me answers
// GET /api/users/me. Both default to the happy path.
type loginServer struct {
	t       *testing.T
	start   string // POST /api/auth/cli_session response body
	poll    func(w http.ResponseWriter, n int)
	me      func(w http.ResponseWriter, r *http.Request)
	polls   atomic.Int32
	starts  atomic.Int32
	meCalls atomic.Int32
}

const loginStartOK = `{"data":{"token":"sess-tok","url":"https://example.test/auth/cli","confirmation_code":31281247}}`

func newLoginServer(t *testing.T) *loginServer {
	return &loginServer{t: t, start: loginStartOK}
}

func (ls *loginServer) serve() *httptest.Server {
	t := ls.t
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/cli_session":
			ls.starts.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Errorf("starting a login should be unauthenticated, got %q", r.Header.Get("Authorization"))
			}
			var body struct{ Note string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.HasPrefix(body.Note, "nh dev (") {
				t.Errorf("note: want prefix %q, got %q", "nh dev (", body.Note)
			}
			_, _ = w.Write([]byte(ls.start))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/cli_session/sess-tok":
			n := int(ls.polls.Add(1))
			if r.Header.Get("Authorization") != "" {
				t.Errorf("polling should be unauthenticated, got %q", r.Header.Get("Authorization"))
			}
			if ls.poll != nil {
				ls.poll(w, n)
				return
			}
			if n < 3 {
				_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"status":"ready","user_token":"nhu_secret"}}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/users/me":
			ls.meCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer nhu_secret" {
				t.Errorf("/users/me should use the new token, got Authorization %q", got)
			}
			if ls.me != nil {
				ls.me(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUserLogin(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)

	dir := t.TempDir()
	ls := newLoginServer(t)
	ls.me = func(w http.ResponseWriter, _ *http.Request) {
		// The account is looked up before the token is saved.
		if got, _ := config.LoadToken(dir); got != "" {
			t.Errorf("token saved before /users/me: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"name":"alice","email":"alice@example.com"}}`))
	}
	srv := ls.serve()

	out, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}

	if n := ls.starts.Load(); n != 1 {
		t.Errorf("expected exactly one start request, got %d", n)
	}
	if n := ls.polls.Load(); n != 3 {
		t.Errorf("expected to poll until ready (3 polls), got %d", n)
	}
	if n := ls.meCalls.Load(); n != 1 {
		t.Errorf("expected one /users/me call, got %d", n)
	}

	// The user is told to open the URL, sign in and type the code in; the
	// browser never shows the code.
	for _, want := range []string{
		"open this URL in your browser and sign in",
		"https://example.test/auth/cli",
		"Then enter this code:",
		"    3128 1247\n",
		"expires in five minutes",
		"Successfully authenticated as alice <alice@example.com>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"matches the one shown", "confirm the login", "Opened the URL"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output should not contain %q, got:\n%s", unwanted, out)
		}
	}

	saved, err := config.LoadToken(dir)
	if err != nil {
		t.Fatalf("reading saved token: %v", err)
	}
	if saved != "nhu_secret" {
		t.Errorf("saved token: want %q, got %q", "nhu_secret", saved)
	}
}

func TestUserLoginRequiresConfirmationCode(t *testing.T) {
	for name, start := range map[string]string{
		"missing": `{"data":{"token":"sess-tok","url":"https://example.test/auth/cli"}}`,
		"zero":    `{"data":{"token":"sess-tok","url":"https://example.test/auth/cli","confirmation_code":0}}`,
	} {
		t.Run(name, func(t *testing.T) {
			shortCLIPolling(t, 5*time.Second)
			fakeBrowser(t, false)
			ls := newLoginServer(t)
			ls.start = start
			srv := ls.serve()

			dir := t.TempDir()
			_, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir)
			if err == nil || !strings.Contains(err.Error(), "no confirmation code") {
				t.Errorf("expected a missing-code error, got %v", err)
			}
			if n := ls.polls.Load(); n != 0 {
				t.Errorf("should not poll without a code, got %d polls", n)
			}
		})
	}
}

func TestUserLoginRefusesBadURL(t *testing.T) {
	for name, url := range map[string]string{
		"file":         "file:///Applications/Calculator.app",
		"path":         "/System/Applications/Calculator.app",
		"other scheme": "smb://evil.test/share",
		"escape codes": "https://example.test/auth/cli\u001b[2K",
		"missing host": "https:///auth/cli",
	} {
		t.Run(name, func(t *testing.T) {
			shortCLIPolling(t, 5*time.Second)
			opened := fakeBrowser(t, true)
			ls := newLoginServer(t)
			b, _ := json.Marshal(url)
			ls.start = `{"data":{"token":"sess-tok","url":` + string(b) + `,"confirmation_code":31281247}}`
			srv := ls.serve()

			out, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("expected an invalid-URL error, got %v", err)
			}
			if len(*opened) != 0 {
				t.Errorf("nothing should be opened, got %v", *opened)
			}
			if strings.Contains(out, "\x1b") {
				t.Errorf("escape codes should not reach the terminal, got %q", out)
			}
			if n := ls.polls.Load(); n != 0 {
				t.Errorf("should not poll, got %d polls", n)
			}
		})
	}
}

func TestUserLoginExpiredSession(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.poll = func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}
	srv := ls.serve()

	dir := t.TempDir()
	_, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir)
	if err == nil {
		t.Fatal("expected an error for an expired login")
	}
	if msg := err.Error(); !strings.Contains(msg, "login expired") || !strings.Contains(msg, "run `nh user login` again") || strings.Contains(msg, "api: 404") {
		t.Errorf("unexpected error: %q", msg)
	}
	if n := ls.polls.Load(); n != 1 {
		t.Errorf("a 404 should stop polling, got %d polls", n)
	}
	if got, _ := config.LoadToken(dir); got != "" {
		t.Errorf("no token should be saved, got %q", got)
	}
}

func TestUserLoginKeepsPollingWhenThrottled(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter){
		// What a proxy, or a later server, sends.
		"429 with Retry-After": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		// What NervesHub's PlugAttack sends today.
		"plugattack 403": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Forbidden\n"))
		},
	}
	for name, throttle := range cases {
		t.Run(name, func(t *testing.T) {
			shortCLIPolling(t, 5*time.Second)
			fakeBrowser(t, false)
			ls := newLoginServer(t)
			ls.poll = func(w http.ResponseWriter, n int) {
				// Throttled well past the transient-error limit, then ready.
				if n <= 6 {
					throttle(w)
					return
				}
				_, _ = w.Write([]byte(`{"data":{"status":"ready","user_token":"nhu_secret"}}`))
			}
			srv := ls.serve()

			dir := t.TempDir()
			if out, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir); err != nil {
				t.Fatalf("Execute: %v\n%s", err, out)
			}
			if n := ls.polls.Load(); n != 7 {
				t.Errorf("expected 7 polls, got %d", n)
			}
			if got, _ := config.LoadToken(dir); got != "nhu_secret" {
				t.Errorf("saved token: want nhu_secret, got %q", got)
			}
		})
	}
}

func TestUserLoginRetriesTransientErrors(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.poll = func(w http.ResponseWriter, n int) {
		switch n {
		case 1, 2:
			w.WriteHeader(http.StatusBadGateway)
		case 3:
			_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
		case 4, 5, 6:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"data":{"status":"ready","user_token":"nhu_secret"}}`))
		}
	}
	srv := ls.serve()

	if out, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", t.TempDir()); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if n := ls.polls.Load(); n != 7 {
		t.Errorf("expected 7 polls, got %d", n)
	}
}

func TestUserLoginGivesUpOnPersistentErrors(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.poll = func(w http.ResponseWriter, _ int) { w.WriteHeader(http.StatusInternalServerError) }
	srv := ls.serve()

	_, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "api: 500") {
		t.Errorf("expected the 500 to be reported, got %v", err)
	}
	// The first try plus three retries.
	if n := ls.polls.Load(); n != 4 {
		t.Errorf("expected 4 polls, got %d", n)
	}
}

func TestUserLoginTimeout(t *testing.T) {
	shortCLIPolling(t, 30*time.Millisecond)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.poll = func(w http.ResponseWriter, _ int) { _, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`)) }
	srv := ls.serve()

	dir := t.TempDir()
	_, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "login expired") || !strings.Contains(err.Error(), "nh user login") {
		t.Errorf("expected an expiry error, got %v", err)
	}
	if got, _ := config.LoadToken(dir); got != "" {
		t.Errorf("no token should be saved on timeout, got %q", got)
	}
}

func TestUserLoginReadyWithoutToken(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.poll = func(w http.ResponseWriter, _ int) { _, _ = w.Write([]byte(`{"data":{"status":"ready"}}`)) }
	srv := ls.serve()

	_, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("expected missing-token error, got %v", err)
	}
}

func TestUserLoginSavesTokenWhenLookupFails(t *testing.T) {
	shortCLIPolling(t, 5*time.Second)
	fakeBrowser(t, false)
	ls := newLoginServer(t)
	ls.me = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":{"detail":"boom"}}`))
	}
	srv := ls.serve()

	dir := t.TempDir()
	out, err := execCmd(t, "", "user", "login", "--uri", srv.URL, "--data-dir", dir)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Warning: signed in, but couldn't look up the account") || !strings.Contains(out, "boom") {
		t.Errorf("expected a warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Successfully authenticated\n") {
		t.Errorf("expected a plain success line, got:\n%s", out)
	}
	if got, _ := config.LoadToken(dir); got != "nhu_secret" {
		t.Errorf("token should still be saved, got %q", got)
	}
}

func TestUserLoginOpensBrowser(t *testing.T) {
	cases := []struct {
		name     string
		terminal bool
		flags    []string
		want     bool
	}{
		{"terminal", true, nil, true},
		{"not a terminal", false, nil, false},
		{"--no-browser", true, []string{"--no-browser"}, false},
		{"--non-interactive", true, []string{"--non-interactive"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shortCLIPolling(t, 5*time.Second)
			opened := fakeBrowser(t, tc.terminal)
			srv := newLoginServer(t).serve()

			args := append([]string{"user", "login", "--uri", srv.URL, "--data-dir", t.TempDir()}, tc.flags...)
			out, err := execCmd(t, "", args...)
			if err != nil {
				t.Fatalf("Execute: %v\n%s", err, out)
			}
			if tc.want {
				if len(*opened) != 1 || (*opened)[0] != "https://example.test/auth/cli" {
					t.Errorf("expected the login URL to be opened, got %v", *opened)
				}
				if !strings.Contains(out, "Opened the URL in your browser.") {
					t.Errorf("output should say the browser was opened, got:\n%s", out)
				}
			} else if len(*opened) != 0 {
				t.Errorf("browser should not be opened, got %v", *opened)
			}
			// The URL is printed either way.
			if !strings.Contains(out, "https://example.test/auth/cli") {
				t.Errorf("output should show the URL, got:\n%s", out)
			}
		})
	}
}

func TestFormatLoginCode(t *testing.T) {
	cases := map[int]string{
		31281247: "3128 1247",
		10000000: "1000 0000",
		1234:     "1234",
		123456:   "12 3456",
		7:        "7",
	}
	for in, want := range cases {
		if got := formatLoginCode(in); got != want {
			t.Errorf("formatLoginCode(%d) = %q, want %q", in, got, want)
		}
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
