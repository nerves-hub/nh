package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// followServer serves a scripted sequence of responses, one per request, and
// records the queries. The last response repeats once exhausted.
func followServer(t *testing.T, bodies ...string) (*httptest.Server, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var queries []url.Values
	n := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		body := bodies[min(n, len(bodies)-1)]
		n++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), queries...)
	}
}

// runFollow runs a --follow session and stops it once stopAfter has elapsed, by
// which point the scripted responses have been consumed.
func runFollow(t *testing.T, srvURL string, stopAfter time.Duration, extra ...string) string {
	t.Helper()
	resetState(t)

	args := append([]string{
		"device", "logs", "dev1", "--follow", "--interval", "20ms",
		"--org", "acme", "--product", "thermostat",
		"--uri", srvURL, "--token", "tok",
	}, extra...)

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), stopAfter)
	defer cancel()

	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	// Cancellation is the only way a tail ends; it must exit cleanly.
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("follow: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestDeviceLogsFollowPrintsInitialThenNewLines(t *testing.T) {
	initial := `{"data":[
	  {"timestamp":"2026-08-16T09:14:00.000000Z","level":"info","message":"second"},
	  {"timestamp":"2026-08-16T09:13:00.000000Z","level":"info","message":"first"}
	]}`
	poll := `{"data":[{"timestamp":"2026-08-16T09:15:00.000000Z","level":"warning","message":"third"}]}`
	srv, queries := followServer(t, initial, poll, `{"data":[]}`)

	out := runFollow(t, srv.URL, 250*time.Millisecond)

	// The initial page arrives newest-first and must be reversed for reading.
	firstIdx := strings.Index(out, "first")
	secondIdx := strings.Index(out, "second")
	thirdIdx := strings.Index(out, "third")
	if firstIdx < 0 || secondIdx < 0 || thirdIdx < 0 {
		t.Fatalf("missing lines in output:\n%s", out)
	}
	if !(firstIdx < secondIdx && secondIdx < thirdIdx) {
		t.Errorf("lines should print oldest first, got:\n%s", out)
	}
	// The header belongs to the first batch only.
	if n := strings.Count(out, "TIMESTAMP"); n != 1 {
		t.Errorf("header printed %d times, want 1", n)
	}

	q := queries()
	if len(q) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(q))
	}
	if got := q[0].Get("order"); got != "desc" {
		t.Errorf("initial order = %q, want desc (most recent page)", got)
	}
	if got := q[1].Get("order"); got != "asc" {
		t.Errorf("poll order = %q, want asc", got)
	}
	if got := q[1].Get("since"); !strings.HasPrefix(got, "2026-08-16T09:14:00") {
		t.Errorf("poll should resume from the newest line seen, got since=%q", got)
	}
}

// `since` is inclusive, so the boundary line comes back on the next poll. It
// must not be printed twice, and a different line sharing that timestamp must
// still be printed.
func TestDeviceLogsFollowDeduplicatesBoundaryLine(t *testing.T) {
	initial := `{"data":[{"timestamp":"2026-08-16T09:14:00.000000Z","level":"info","message":"boundary"}]}`
	poll := `{"data":[
	  {"timestamp":"2026-08-16T09:14:00.000000Z","level":"info","message":"boundary"},
	  {"timestamp":"2026-08-16T09:14:00.000000Z","level":"error","message":"same-microsecond"},
	  {"timestamp":"2026-08-16T09:16:00.000000Z","level":"info","message":"later"}
	]}`
	srv, _ := followServer(t, initial, poll, `{"data":[]}`)

	out := runFollow(t, srv.URL, 250*time.Millisecond)

	if n := strings.Count(out, "boundary"); n != 1 {
		t.Errorf("boundary line printed %d times, want 1:\n%s", n, out)
	}
	// A distinct line at the same microsecond must not be lost to dedupe.
	if !strings.Contains(out, "same-microsecond") {
		t.Errorf("a different line at the boundary timestamp was dropped:\n%s", out)
	}
	if !strings.Contains(out, "later") {
		t.Errorf("missing the later line:\n%s", out)
	}
}

// With no history there is nothing to resume from, so following must start at
// now rather than replaying the oldest page.
func TestDeviceLogsFollowWithNoHistoryStartsFromNow(t *testing.T) {
	srv, queries := followServer(t, `{"data":[]}`)

	_ = runFollow(t, srv.URL, 200*time.Millisecond)

	q := queries()
	if len(q) < 2 {
		t.Fatalf("expected a poll after the empty initial page, got %d requests", len(q))
	}
	since := q[1].Get("since")
	if since == "" {
		t.Fatal("poll must be bounded by since, or it would replay old lines")
	}
	ts, err := time.Parse(time.RFC3339Nano, since)
	if err != nil {
		t.Fatalf("since %q is not RFC 3339: %v", since, err)
	}
	if time.Since(ts) > time.Minute {
		t.Errorf("since = %q, want roughly now", since)
	}
}

func TestDeviceLogsFollowJSONIsOnePerLine(t *testing.T) {
	initial := `{"data":[{"timestamp":"2026-08-16T09:14:00.000000Z","level":"info","message":"one"}]}`
	srv, _ := followServer(t, initial, `{"data":[]}`)

	out := runFollow(t, srv.URL, 200*time.Millisecond, "-o", "json")

	// A stream has no closing bracket, so it must not be wrapped in an array.
	if strings.Contains(out, "[") {
		t.Errorf("streamed JSON should not be an array, got:\n%s", out)
	}
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, "{") || !strings.Contains(line, `"message":"one"`) {
		t.Errorf("expected one JSON object per line, got:\n%s", out)
	}
}

func TestDeviceLogsFollowRejectsConflictingFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"order", []string{"--follow", "--order", "asc"}, "--order cannot be used with --follow"},
		{"before", []string{"--follow", "--before", "2026-08-16T09:00:00Z"}, "--before cannot be used with --follow"},
		{"interval without follow", []string{"--interval", "5s"}, "--interval only applies with --follow"},
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
				t.Error("no request should be made for conflicting flags")
			}
		})
	}
}

func TestDeviceLogsSearchSentAsQuery(t *testing.T) {
	srv, _, query := logsServer(t)

	if _, err := execCmd(t, "",
		"device", "logs", "dev1", "--search", "sensor bus",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs --search: %v", err)
	}
	if got := (*query).Get("search"); got != "sensor bus" {
		t.Errorf("search = %q, want %q", got, "sensor bus")
	}
}

// The term is passed through untouched: the server matches it literally, so
// wildcard characters must not be escaped or stripped on the way out.
func TestDeviceLogsSearchPassesTermThroughLiterally(t *testing.T) {
	srv, _, query := logsServer(t)

	if _, err := execCmd(t, "",
		"device", "logs", "dev1", "--search", "100% of _reads_",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs --search: %v", err)
	}
	if got := (*query).Get("search"); got != "100% of _reads_" {
		t.Errorf("search = %q, want it passed through unchanged", got)
	}
}

// A blank search is not a filter, so it must not be sent at all.
func TestDeviceLogsBlankSearchIsOmitted(t *testing.T) {
	srv, _, query := logsServer(t)

	if _, err := execCmd(t, "",
		"device", "logs", "dev1", "--search", "   ",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if _, ok := (*query)["search"]; ok {
		t.Errorf("a blank --search should not be sent, got %v", *query)
	}
}

// Filters must keep applying to the lines a tail picks up, not just the first
// page.
func TestDeviceLogsFollowKeepsSearchOnPolls(t *testing.T) {
	initial := `{"data":[{"timestamp":"2026-08-16T09:14:00.000000Z","level":"error","message":"sensor bus failed"}]}`
	srv, queries := followServer(t, initial, `{"data":[]}`)

	_ = runFollow(t, srv.URL, 200*time.Millisecond, "--search", "sensor bus")

	q := queries()
	if len(q) < 2 {
		t.Fatalf("expected a poll after the initial page, got %d requests", len(q))
	}
	for i, query := range q {
		if got := query.Get("search"); got != "sensor bus" {
			t.Errorf("request %d dropped the search filter: search=%q", i, got)
		}
	}
}
