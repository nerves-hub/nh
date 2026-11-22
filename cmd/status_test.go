package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// statusServer answers NervesHubWeb.Plugs.ImAlive's health check with code
// and body, recording the request.
func statusServer(t *testing.T, code int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	got := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(r.Context())
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestStatusHealthy(t *testing.T) {
	srv, got := statusServer(t, http.StatusOK, "Hello, Friend!")

	out, err := execCmd(t, "", "status", "--uri", srv.URL, "--token", "secret", "--data-dir", t.TempDir())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Method != http.MethodGet || got.URL.Path != "/status/alive" {
		t.Errorf("request: %s %s", got.Method, got.URL.Path)
	}
	// The health check is unauthenticated: no token is sent.
	if auth := got.Header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization should not be sent, got %q", auth)
	}
	for _, want := range []string{"URI:", srv.URL, "Status:", "up"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestStatusStripsAPIPrefix(t *testing.T) {
	srv, got := statusServer(t, http.StatusOK, "Hello, Friend!")

	// ImAlive is mounted at the host root, not under /api.
	if _, err := execCmd(t, "", "status", "--uri", srv.URL+"/api/", "--data-dir", t.TempDir()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.URL.Path != "/status/alive" {
		t.Errorf("path: got %q", got.URL.Path)
	}
}

func TestStatusDatabaseDown(t *testing.T) {
	srv, _ := statusServer(t, http.StatusInternalServerError, "Sorry, Friend :(")

	out, err := execCmd(t, "", "status", "--uri", srv.URL, "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "can't reach its database") {
		t.Errorf("expected a database error, got %v", err)
	}
	if !strings.Contains(out, "up, but its database is unreachable") {
		t.Errorf("output: %q", out)
	}
}

func TestStatusUnexpectedAnswer(t *testing.T) {
	srv, _ := statusServer(t, http.StatusNotFound, "not here")

	out, err := execCmd(t, "", "status", "--uri", srv.URL, "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "check the uri setting") {
		t.Errorf("expected an unexpected-answer error, got %v", err)
	}
	if !strings.Contains(out, "unexpected answer (404 Not Found)") {
		t.Errorf("output: %q", out)
	}
}

func TestStatusOldServerGreeting(t *testing.T) {
	// NervesHub before v2.3.0 answered without the comma.
	srv, _ := statusServer(t, http.StatusOK, "Hello Friend!")

	if _, err := execCmd(t, "", "status", "--uri", srv.URL, "--data-dir", t.TempDir()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestStatusNotNervesHub(t *testing.T) {
	for name, tc := range map[string]struct {
		code    int
		body    string
		summary string
	}{
		// A web frontend whose catch-all route answers every path.
		"200 from something else": {http.StatusOK, "<!doctype html><title>Welcome</title>", "unexpected answer (200 OK)"},
		// A proxy's own error page, not ImAlive's database answer.
		"500 from a proxy": {http.StatusInternalServerError, "upstream error", "unexpected answer (500 Internal Server Error)"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := statusServer(t, tc.code, tc.body)

			out, err := execCmd(t, "", "status", "--uri", srv.URL, "--data-dir", t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "not NervesHub's health check") {
				t.Errorf("expected a not-NervesHub error, got %v", err)
			}
			if !strings.Contains(out, tc.summary) {
				t.Errorf("output should say %q, got %q", tc.summary, out)
			}
		})
	}
}

func TestStatusUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	uri := srv.URL
	srv.Close()

	out, err := execCmd(t, "", "status", "--uri", uri, "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "server unreachable") {
		t.Errorf("expected an unreachable error, got %v", err)
	}
	if !strings.Contains(out, uri) || !strings.Contains(out, "unreachable") {
		t.Errorf("output should name the URI and say it's unreachable, got %q", out)
	}

	// The JSON report still names the URL that was tried.
	out, _ = execCmd(t, "", "status", "--uri", uri, "--data-dir", t.TempDir(), "-o", "json")
	var got statusReport
	// Cobra prints the error after the report, so decode just the first value.
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("json output %q: %v", out, err)
	}
	if got.URL != uri+"/status/alive" || got.Reachable || got.Error == "" {
		t.Errorf("report: %+v", got)
	}
}

func TestStatusJSON(t *testing.T) {
	srv, _ := statusServer(t, http.StatusOK, "Hello, Friend!")

	out, err := execCmd(t, "", "status", "--uri", srv.URL, "--data-dir", t.TempDir(), "-o", "json")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got statusReport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json output %q: %v", out, err)
	}
	want := statusReport{
		URI: srv.URL, URL: srv.URL + "/status/alive", Reachable: true, Healthy: true,
		StatusCode: http.StatusOK, Message: "Hello, Friend!",
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatusRejectsNonPositiveTimeout(t *testing.T) {
	_, err := execCmd(t, "", "status", "--uri", "http://127.0.0.1:1", "--timeout", "0s", "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "--timeout must be positive") {
		t.Errorf("expected a timeout error, got %v", err)
	}
}
