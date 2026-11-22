package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// deviceMetricsResponse is shaped like DeviceMetricJSON on the server: reports
// newest first, each a timestamp and the readings taken at it.
const deviceMetricsResponse = `{"data":[
  {"timestamp":"2026-08-16T09:14:00.123456Z","firmware_uuid":"8f6c3a1e-2b7d-4c55-9e0a-1d2f3b4c5a6e",
   "metrics":{"cpu_temp":41.9,"mem_used_percent":38.5}},
  {"timestamp":"2026-08-16T09:13:00.004210Z","firmware_uuid":null,
   "metrics":{"cpu_temp":41.2,"mem_used_percent":38.0,"load_1":0.25}}
]}`

// metricsServer serves the canned response and records the request it got.
func metricsServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	got := new(http.Request)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func metricsArgs(srvURL string, extra ...string) []string {
	args := append([]string{"device", "metrics", "dev1"}, extra...)
	return append(args, "--org", "acme", "--product", "thermostat", "--uri", srvURL, "--token", "tok")
}

func TestDeviceMetricsTableGroupsByKey(t *testing.T) {
	srv, req := metricsServer(t, deviceMetricsResponse)

	out, err := execCmd(t, "", metricsArgs(srv.URL)...)
	if err != nil {
		t.Fatalf("metrics: %v\n%s", err, out)
	}
	if req.Method != http.MethodGet {
		t.Errorf("method: got %s", req.Method)
	}
	if req.URL.Path != "/api/orgs/acme/products/thermostat/devices/dev1/metrics" {
		t.Errorf("path: got %q", req.URL.Path)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("want a header and 5 readings, got:\n%s", out)
	}
	for _, col := range []string{"METRIC", "TIMESTAMP", "VALUE"} {
		if !strings.Contains(lines[0], col) {
			t.Errorf("header missing %s: %q", col, lines[0])
		}
	}
	// Grouped by name, sorted; within a name in the order the reports came
	// back. Every row carries its name, so grep still finds every reading.
	want := []struct{ key, ts, value string }{
		{"cpu_temp", "2026-08-16 09:14:00.123", "41.9"},
		{"cpu_temp", "2026-08-16 09:13:00.004", "41.2"},
		{"load_1", "2026-08-16 09:13:00.004", "0.25"},
		{"mem_used_percent", "2026-08-16 09:14:00.123", "38.5"},
		{"mem_used_percent", "2026-08-16 09:13:00.004", "38"},
	}
	for i, w := range want {
		line := lines[i+1]
		if !strings.HasPrefix(line, w.key+" ") {
			t.Errorf("row %d: want it to start with %s, got %q", i, w.key, line)
		}
		if !strings.Contains(line, w.ts) || !strings.HasSuffix(strings.TrimSpace(line), w.value) {
			t.Errorf("row %d: want %s and %s, got %q", i, w.ts, w.value, line)
		}
	}
}

func TestDeviceMetricsJSON(t *testing.T) {
	srv, _ := metricsServer(t, deviceMetricsResponse)

	out, err := execCmd(t, "", metricsArgs(srv.URL, "-o", "json")...)
	if err != nil {
		t.Fatalf("metrics json: %v\n%s", err, out)
	}
	var reports []map[string]any
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("output is not a JSON array of reports: %v\n%s", err, out)
	}
	if len(reports) != 2 || reports[0]["firmware_uuid"] != "8f6c3a1e-2b7d-4c55-9e0a-1d2f3b4c5a6e" {
		t.Errorf("reports not passed through, got:\n%s", out)
	}
	// A null firmware stays null rather than becoming "".
	if fw, ok := reports[1]["firmware_uuid"]; !ok || fw != nil {
		t.Errorf("null firmware_uuid should stay null, got %v (present %v)", fw, ok)
	}
	if m, _ := reports[1]["metrics"].(map[string]any); m["load_1"] != 0.25 {
		t.Errorf("metrics not passed through, got:\n%s", out)
	}
}

func TestDeviceMetricsFiltersSentAsQuery(t *testing.T) {
	srv, req := metricsServer(t, deviceMetricsResponse)

	if _, err := execCmd(t, "", metricsArgs(srv.URL,
		"--key", "cpu_temp", "--key", "mem_used_percent,load_1",
		"--limit", "50", "--order", "ASC",
		"--since", "2026-08-16T09:00:00Z",
		"--before", "2026-08-16T10:00:00Z",
	)...); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	q := req.URL.Query()
	// Repeated --key is sent as the comma-separated list the API expects.
	if got := q.Get("key"); got != "cpu_temp,mem_used_percent,load_1" {
		t.Errorf("key = %q", got)
	}
	if got := q.Get("limit"); got != "50" {
		t.Errorf("limit = %q", got)
	}
	if got := q.Get("order"); got != "asc" {
		t.Errorf("order = %q", got)
	}
	if got := q.Get("since"); got != "2026-08-16T09:00:00Z" {
		t.Errorf("since = %q", got)
	}
	if got := q.Get("before"); got != "2026-08-16T10:00:00Z" {
		t.Errorf("before = %q", got)
	}
}

func TestDeviceMetricsOmitsUnsetFilters(t *testing.T) {
	srv, req := metricsServer(t, deviceMetricsResponse)

	if _, err := execCmd(t, "", metricsArgs(srv.URL)...); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if req.URL.RawQuery != "" {
		t.Errorf("no query params expected, got %q", req.URL.RawQuery)
	}
}

func TestDeviceMetricsRelativeSince(t *testing.T) {
	srv, req := metricsServer(t, deviceMetricsResponse)

	if _, err := execCmd(t, "", metricsArgs(srv.URL, "--since", "3d")...); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	got := req.URL.Query().Get("since")
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("since %q is not RFC 3339: %v", got, err)
	}
	if days := time.Since(parsed).Hours() / 24; days < 2.9 || days > 3.1 {
		t.Errorf("since = %q, want roughly 3 days ago", got)
	}
}

func TestDeviceMetricsEmpty(t *testing.T) {
	srv, _ := metricsServer(t, `{"data":[]}`)

	out, err := execCmd(t, "", metricsArgs(srv.URL)...)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if !strings.Contains(out, "No metric reports found for device dev1") {
		t.Errorf("expected an empty message, got:\n%s", out)
	}

	out, err = execCmd(t, "", metricsArgs(srv.URL, "-o", "json")...)
	if err != nil {
		t.Fatalf("metrics json: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty JSON output should be [], got %q", out)
	}
}

func TestDeviceMetricsListKeys(t *testing.T) {
	srv, req := metricsServer(t, deviceMetricsResponse)

	out, err := execCmd(t, "", metricsArgs(srv.URL, "--list-keys", "--since", "1h")...)
	if err != nil {
		t.Fatalf("metrics --list-keys: %v\n%s", err, out)
	}
	if out != "cpu_temp\nload_1\nmem_used_percent\n" {
		t.Errorf("want the distinct names sorted, one per line, got:\n%s", out)
	}
	// The window still applies, since the names come from the reports in it.
	if req.URL.Query().Get("since") == "" {
		t.Error("--since should still be sent with --list-keys")
	}

	out, err = execCmd(t, "", metricsArgs(srv.URL, "--list-keys", "-o", "json")...)
	if err != nil {
		t.Fatalf("metrics --list-keys json: %v", err)
	}
	var keys []string
	if err := json.Unmarshal([]byte(out), &keys); err != nil || len(keys) != 3 {
		t.Errorf("want a JSON array of 3 names, got %q (%v)", out, err)
	}
}

// A server with no analytics database stores no history; its explanation
// must reach the user instead of an empty table.
func TestDeviceMetricsAnalyticsDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"errors":{"detail":"This platform has no analytics database configured, so device metric history is not stored."}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "", metricsArgs(srv.URL)...)
	if err == nil || !strings.Contains(err.Error(), "no analytics database") {
		t.Fatalf("want the server's 501 reason, got %v", err)
	}
}

// Bad flags are rejected locally, without troubling the server.
func TestDeviceMetricsRejectsBadFlags(t *testing.T) {
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
		{"follow order", []string{"--follow", "--order", "asc"}, "--order cannot be used with --follow"},
		{"follow before", []string{"--follow", "--before", "1h"}, "--before cannot be used with --follow"},
		{"follow list-keys", []string{"--follow", "--list-keys"}, "--list-keys cannot be used with --follow"},
		{"interval alone", []string{"--interval", "1s"}, "--interval only applies with --follow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer srv.Close()

			_, err := execCmd(t, "", metricsArgs(srv.URL, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if called {
				t.Error("no request should be made for an invalid flag")
			}
		})
	}
}

// runMetricsFollow runs a --follow session and stops it once stopAfter has
// elapsed, by which point the scripted responses have been consumed.
func runMetricsFollow(t *testing.T, srvURL string, stopAfter time.Duration, extra ...string) string {
	t.Helper()
	resetState(t)

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), stopAfter)
	defer cancel()

	rootCmd.SetArgs(metricsArgs(srvURL, append([]string{"--follow", "--interval", "20ms"}, extra...)...))
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("follow: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestDeviceMetricsFollowPrintsInitialThenNewReports(t *testing.T) {
	initial := `{"data":[
	  {"timestamp":"2026-08-16T09:14:00Z","metrics":{"cpu_temp":2}},
	  {"timestamp":"2026-08-16T09:13:00Z","metrics":{"cpu_temp":1}}
	]}`
	// `since` is inclusive, so the boundary report comes back. A reading at
	// that timestamp not printed yet must still appear; the rest must not
	// repeat.
	poll := `{"data":[
	  {"timestamp":"2026-08-16T09:14:00Z","metrics":{"cpu_temp":2,"load_1":7}},
	  {"timestamp":"2026-08-16T09:15:00Z","metrics":{"cpu_temp":3}}
	]}`
	srv, queries := followServer(t, initial, poll, `{"data":[]}`)

	out := runMetricsFollow(t, srv.URL, 250*time.Millisecond, "--key", "cpu_temp,load_1")

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("want a header and 4 readings, got:\n%s", out)
	}
	if n := strings.Count(out, "METRIC"); n != 1 {
		t.Errorf("header printed %d times, want 1", n)
	}
	// Oldest first, the initial page reversed.
	for i, want := range []string{"09:13:00.000", "09:14:00.000", "09:14:00.000", "09:15:00.000"} {
		if !strings.Contains(lines[i+1], want) {
			t.Errorf("row %d: want %s, got %q", i, want, lines[i+1])
		}
	}
	if !strings.HasPrefix(lines[3], "load_1") {
		t.Errorf("the new reading at the boundary timestamp was dropped:\n%s", out)
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
	if got := q[1].Get("since"); got != "2026-08-16T09:14:00Z" {
		t.Errorf("poll should resume from the newest report seen, got since=%q", got)
	}
	if got := q[1].Get("key"); got != "cpu_temp,load_1" {
		t.Errorf("poll should keep the key filter, got key=%q", got)
	}
}

// With --limit 1 a poll's one report is always the boundary one, which would
// never move the cursor. Polls ask for at least two, and a page that brought
// nothing new waits for the interval rather than spinning.
func TestDeviceMetricsFollowLimitOne(t *testing.T) {
	initial := `{"data":[{"timestamp":"2026-08-16T09:14:00Z","metrics":{"cpu_temp":2}}]}`
	srv, queries := followServer(t, initial)

	_ = runMetricsFollow(t, srv.URL, 200*time.Millisecond, "--limit", "1")

	q := queries()
	if len(q) < 2 {
		t.Fatalf("expected a poll, got %d requests", len(q))
	}
	if got := q[0].Get("limit"); got != "1" {
		t.Errorf("initial limit = %q, want 1", got)
	}
	if got := q[1].Get("limit"); got != "2" {
		t.Errorf("poll limit = %q, want 2", got)
	}
	// 200ms at a 20ms interval is about ten polls; a spinning loop makes
	// thousands.
	if len(q) > 30 {
		t.Errorf("%d requests in 200ms: polling is not waiting for the interval", len(q))
	}
}

func TestDeviceMetricsFollowJSONIsOnePerLine(t *testing.T) {
	initial := `{"data":[{"timestamp":"2026-08-16T09:14:00Z","metrics":{"cpu_temp":2}}]}`
	srv, _ := followServer(t, initial, `{"data":[]}`)

	out := runMetricsFollow(t, srv.URL, 200*time.Millisecond, "-o", "json")

	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("streamed JSON should not be an array, got:\n%s", out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Errorf("expected one JSON object per line, got %q: %v", out, err)
	}
}
