package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const deviceLogsResponse = `{"data":[
  {"timestamp":"2026-08-16T09:14:00.123456Z","level":"error",
   "message":"Failed to reach the sensor bus",
   "meta":{"file":"lib/thermostat/sensors.ex","line":"42"}},
  {"timestamp":"2026-08-16T09:13:59.998211Z","level":"info",
   "message":"Booted","meta":{}}
]}`

// logsServer serves the canned response and records the query it was called
// with.
func logsServer(t *testing.T) (*httptest.Server, *string, *url.Values) {
	t.Helper()
	path := new(string)
	query := new(url.Values)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		*query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deviceLogsResponse))
	}))
	t.Cleanup(srv.Close)
	return srv, path, query
}

func TestDeviceLogsTable(t *testing.T) {
	srv, path, _ := logsServer(t)

	out, err := execCmd(t, "",
		"device", "logs", "dev1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("logs: %v\n%s", err, out)
	}
	if *path != "/api/orgs/acme/products/thermostat/devices/dev1/logs" {
		t.Errorf("path: got %q", *path)
	}
	for _, want := range []string{"TIMESTAMP", "LEVEL", "MESSAGE", "error", "Failed to reach the sensor bus", "Booted"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
	// Metadata is hidden unless asked for.
	if strings.Contains(out, "sensors.ex") {
		t.Errorf("metadata should be hidden without --meta, got:\n%s", out)
	}
}

func TestDeviceLogsMetaFlag(t *testing.T) {
	srv, _, _ := logsServer(t)

	out, err := execCmd(t, "",
		"device", "logs", "dev1", "--meta",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("logs --meta: %v\n%s", err, out)
	}
	// Sorted key=value pairs, so output is stable run to run.
	if !strings.Contains(out, "file=lib/thermostat/sensors.ex line=42") {
		t.Errorf("expected sorted metadata pairs, got:\n%s", out)
	}
}

func TestDeviceLogsJSON(t *testing.T) {
	srv, _, _ := logsServer(t)

	out, err := execCmd(t, "",
		"device", "logs", "dev1", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("logs json: %v\n%s", err, out)
	}
	for _, want := range []string{`"level": "error"`, `"message": "Failed to reach the sensor bus"`, `"file": "lib/thermostat/sensors.ex"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %s, got:\n%s", want, out)
		}
	}
}

func TestDeviceLogsFiltersSentAsQuery(t *testing.T) {
	srv, _, query := logsServer(t)

	_, err := execCmd(t, "",
		"device", "logs", "dev1",
		"--level", "error", "--level", "warning",
		"--limit", "50", "--order", "asc",
		"--since", "2026-08-16T09:00:00Z",
		"--before", "2026-08-16T10:00:00Z",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	q := *query
	// Repeated --level is sent as the comma-separated list the API expects.
	if got := q.Get("level"); got != "error,warning" {
		t.Errorf("level = %q, want error,warning", got)
	}
	if got := q.Get("limit"); got != "50" {
		t.Errorf("limit = %q", got)
	}
	if got := q.Get("order"); got != "asc" {
		t.Errorf("order = %q", got)
	}
	if got := q.Get("since"); !strings.HasPrefix(got, "2026-08-16T09:00:00") {
		t.Errorf("since = %q", got)
	}
	if got := q.Get("before"); !strings.HasPrefix(got, "2026-08-16T10:00:00") {
		t.Errorf("before = %q", got)
	}
}

// Unset filters must not be sent at all, so the server applies its own defaults.
func TestDeviceLogsOmitsUnsetFilters(t *testing.T) {
	srv, _, query := logsServer(t)

	if _, err := execCmd(t, "",
		"device", "logs", "dev1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(*query) != 0 {
		t.Errorf("no query params expected, got %v", *query)
	}
}

func TestDeviceLogsRelativeSince(t *testing.T) {
	srv, _, query := logsServer(t)

	before := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := execCmd(t, "",
		"device", "logs", "dev1", "--since", "2h",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs: %v", err)
	}

	got := (*query).Get("since")
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("since %q is not RFC 3339: %v", got, err)
	}
	// Should be ~2h ago; allow a wide window for slow machines.
	if diff := parsed.Sub(before); diff < -time.Minute || diff > time.Minute {
		t.Errorf("since = %q, want roughly 2h ago (%s)", got, before.Format(time.RFC3339))
	}
}

func TestDeviceLogsRelativeDays(t *testing.T) {
	srv, _, query := logsServer(t)

	if _, err := execCmd(t, "",
		"device", "logs", "dev1", "--since", "3d",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs: %v", err)
	}
	got := (*query).Get("since")
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("since %q is not RFC 3339: %v", got, err)
	}
	if days := time.Since(parsed).Hours() / 24; days < 2.9 || days > 3.1 {
		t.Errorf("since = %q, want roughly 3 days ago", got)
	}
}

func TestDeviceLogsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "logs", "dev1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !strings.Contains(out, "No log lines found for device dev1") {
		t.Errorf("expected an empty message, got:\n%s", out)
	}
}

// Bad filters are rejected locally, without troubling the server.
func TestDeviceLogsRejectsBadFilters(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"order", []string{"--order", "sideways"}, "invalid --order"},
		{"limit low", []string{"--limit", "0"}, "invalid --limit"},
		{"limit high", []string{"--limit", "1001"}, "invalid --limit"},
		{"since", []string{"--since", "yesterday"}, "invalid --since"},
		{"before", []string{"--before", "soon"}, "invalid --before"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer srv.Close()

			args := append([]string{"device", "logs", "dev1"}, tc.args...)
			args = append(args, "--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok")

			_, err := execCmd(t, "", args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if called {
				t.Error("no request should be made for an invalid filter")
			}
		})
	}
}
