package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/api"
)

const devicesResponse = `{"data":[
	{"identifier":"dev-001","connection_status":"connected","online":"online","version":"1.2.3","tags":["prod","eu"]},
	{"identifier":"dev-002","connection_status":"disconnected","online":"offline","version":"","tags":[]}
]}`

func TestDeviceListTable(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(devicesResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok-123",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Both org and product are interpolated into the path.
	if gotPath != "/api/orgs/acme/products/thermostat/devices" {
		t.Errorf("path: want /api/orgs/acme/products/thermostat/devices, got %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth header: want %q, got %q", "Bearer tok-123", gotAuth)
	}

	for _, want := range []string{"IDENTIFIER", "STATUS", "VERSION", "dev-001", "connected", "1.2.3", "dev-002", "disconnected"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(devicesResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"identifier": "dev-001"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

func TestDeviceListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No devices found in acme/thermostat") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestDeviceListPagination(t *testing.T) {
	var gotPage, gotPageSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPage = r.URL.Query().Get("pagination[page]")
		gotPageSize = r.URL.Query().Get("pagination[page_size]")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{"identifier":"dev-001","connection_status":"connected","version":"1.0.0","tags":[]}],
			"pagination":{"current_page":2,"page_size":10,"total_pages":5,"total_count":42}
		}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--page", "2", "--page-size", "10",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The flags are sent through as query parameters.
	if gotPage != "2" || gotPageSize != "10" {
		t.Errorf("query params: got page=%q page_size=%q", gotPage, gotPageSize)
	}

	// Pagination info is shown at the top of the output.
	if !strings.Contains(out, "Page 2 of 5") {
		t.Errorf("output should show the page position, got:\n%s", out)
	}
	if !strings.Contains(out, "42 device(s) total") {
		t.Errorf("output should show the total count, got:\n%s", out)
	}
	// The header precedes the table.
	if i, j := strings.Index(out, "Page 2 of 5"), strings.Index(out, "IDENTIFIER"); i < 0 || j < 0 || i > j {
		t.Errorf("pagination header should precede the table, got:\n%s", out)
	}
}

func TestParseSort(t *testing.T) {
	ok := map[string]struct{ col, dir string }{
		"identifier":                   {"identifier", "asc"},
		"identifier:asc":               {"identifier", "asc"},
		"identifier:desc":              {"identifier", "desc"},
		"connection_last_seen_at:DESC": {"connection_last_seen_at", "desc"},
		"identifier:":                  {"identifier", "asc"}, // trailing colon, no direction
	}
	for in, want := range ok {
		col, dir, err := parseSort(in)
		if err != nil {
			t.Errorf("parseSort(%q): unexpected error %v", in, err)
			continue
		}
		if col != want.col || dir != want.dir {
			t.Errorf("parseSort(%q) = (%q, %q), want (%q, %q)", in, col, dir, want.col, want.dir)
		}
	}

	for _, in := range []string{"", ":desc", "identifier:sideways", "last_communication", "inserted_at:desc"} {
		if _, _, err := parseSort(in); err == nil {
			t.Errorf("parseSort(%q): expected error", in)
		}
	}
}

func TestDeviceListSort(t *testing.T) {
	var gotSort, gotDir string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSort = r.URL.Query().Get("sort")
		gotDir = r.URL.Query().Get("sort_direction")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--sort", "identifier:desc",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotSort != "identifier" || gotDir != "desc" {
		t.Errorf("sort query: got sort=%q sort_direction=%q", gotSort, gotDir)
	}
}

func TestParseFilters(t *testing.T) {
	got, err := parseFilters([]string{"connection:not_seen", "tags:prod"})
	if err != nil {
		t.Fatalf("parseFilters: %v", err)
	}
	if got["connection"] != "not_seen" || got["tags"] != "prod" {
		t.Errorf("got %+v", got)
	}

	if got, err := parseFilters(nil); err != nil || got != nil {
		t.Errorf("empty input should give (nil, nil), got (%v, %v)", got, err)
	}

	for _, bad := range []string{"connection", "connection:", ":prod"} {
		if _, err := parseFilters([]string{bad}); err == nil {
			t.Errorf("parseFilters(%q): expected error", bad)
		}
	}
}

func TestDeviceListFilters(t *testing.T) {
	var gotConnection, gotTag string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotConnection = r.URL.Query().Get("filters[connection]")
		gotTag = r.URL.Query().Get("filters[tags]")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--filter", "connection:not_seen",
		"--filter", "tags:prod",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotConnection != "not_seen" || gotTag != "prod" {
		t.Errorf("filter query: got filters[connection]=%q filters[tags]=%q", gotConnection, gotTag)
	}
}

// asleep is a connection filter value from NervesHub's device sleep
// tracking; nh passes it through, alone or with others.
func TestDeviceListAsleepFilter(t *testing.T) {
	for _, value := range []string{"asleep", "connected,asleep"} {
		t.Run(value, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("filters[connection]")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer srv.Close()

			_, err := execCmd(t, "",
				"device", "list",
				"--org", "acme", "--product", "thermostat",
				"--filter", "connection:"+value,
				"--uri", srv.URL, "--token", "tok",
			)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != value {
				t.Errorf("filters[connection] = %q, want %q", got, value)
			}
		})
	}
}

// The server drops filter keys and values it doesn't know (and a key or sort
// field that isn't an atom on the server is a 500), so nh refuses them before
// asking, naming what would work.
func TestDeviceListRejectsUnknownFilterAndSort(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"tag", []string{"--filter", "tag:prod"}, []string{`unknown --filter key "tag"`, `did you mean "tags"`, "connection_type"}},
		{"made-up key", []string{"--filter", "colour:red"}, []string{`"colour"`, "health_status"}},
		{"state value", []string{"--filter", "state:onlin"}, []string{`"onlin"`, "attention, online, updating, offline"}},
		{"multi value", []string{"--filter", "connection:connected,nope"}, []string{`"nope"`, "comma-separated"}},
		{"deployment id", []string{"--filter", "deployment_id:prod"}, []string{"numeric ID"}},
		{"metric without value", []string{"--filter", "metrics_key:cpu_temp"}, []string{"metrics_value"}},
		{"metric value", []string{"--filter", "metrics_key:cpu_temp", "--filter", "metrics_value:hot"}, []string{"want a number"}},
		{"metric NaN", []string{"--filter", "metrics_key:cpu_temp", "--filter", "metrics_value:NaN"}, []string{"want a number"}},
		{"tag with space", []string{"--filter", "tags:prod east"}, []string{"cannot contain spaces"}},
		{"no tags", []string{"--filter", "tags: , "}, []string{"at least one tag"}},
		{"only commas", []string{"--filter", "connection:,"}, []string{"at least one value"}},
		{"only commas, free text", []string{"--filter", "alarm:,,"}, []string{"at least one value"}},
		{"repeated key", []string{"--filter", "connection:connected", "--filter", "connection:not_seen"}, []string{"more than once", "connection:a,b"}},
		{"sort", []string{"--sort", "inserted_at:desc"}, []string{`invalid --sort "inserted_at"`, "connection_last_seen_at"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer srv.Close()

			args := append([]string{"device", "list", "--org", "acme", "--product", "thermostat"}, tc.args...)
			args = append(args, "--uri", srv.URL, "--token", "tok")
			_, err := execCmd(t, "", args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should mention %q", err, want)
				}
			}
			if called {
				t.Error("no request should be made")
			}
		})
	}
}

// The metric filter only applies with its key, operator and value all sent,
// so a missing operator is filled in with the server's default.
func TestDeviceListMetricFilterDefaultsOperator(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--filter", "metrics_key:cpu_temp",
		"--filter", "metrics_value:70.5",
		"--filter", "connection:connected,not_seen",
		"--sort", "connection_last_seen_at",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for key, want := range map[string]string{
		"filters[metrics_key]":      "cpu_temp",
		"filters[metrics_value]":    "70.5",
		"filters[metrics_operator]": "gt",
		"filters[connection]":       "connected,not_seen",
		"sort":                      "connection_last_seen_at",
	} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
}

// queryServer answers like NervesHub v2.5.0+: the probe nh sends to check
// advanced query support gets a 422 parse error, and any other request gets
// the device list from devices. It records the last non-probe query.
func queryServer(t *testing.T, devices string, got *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("filters[advanced_query]") == api.AdvancedQueryProbe {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":{"detail":"invalid advanced query: \"nh_support_probe\" is not a valid column (at character 0)"}}`))
			return
		}
		*got = r.URL.Query()
		_, _ = w.Write([]byte(devices))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDeviceListAdvancedQuery(t *testing.T) {
	var got url.Values
	srv := queryServer(t, `{"data":[]}`, &got)

	query := `metric:cpu_temp > 70 and connection = "connected"`
	out, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", "  "+query+"\n",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The expression is sent, trimmed, as the advanced_query filter.
	if q := got.Get("filters[advanced_query]"); q != query {
		t.Errorf("query param: want %q, got %q", query, q)
	}
	if !strings.Contains(out, "No devices in acme/thermostat match the given --query/--filter.") {
		t.Errorf("an empty result should say nothing matched, got %q", out)
	}
}

func TestDeviceListQueryCombinesWithFilters(t *testing.T) {
	var got url.Values
	srv := queryServer(t, `{"data":[]}`, &got)

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", "metric:cpu_temp > 70", "--filter", "tags:prod",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Get("filters[advanced_query]") != "metric:cpu_temp > 70" || got.Get("filters[tags]") != "prod" {
		t.Errorf("both the query and the filter should be sent, got %v", got)
	}
}

func TestDeviceListBlankQuery(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", " \n",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "--query is empty") {
		t.Errorf("want a --query is empty error, got %v", err)
	}
}

// Servers before v2.5.0 drop the unknown filter (and list every device) or
// fail on it; nh refuses instead of printing an unfiltered list.
func TestDeviceListQueryRefusedOnOldServers(t *testing.T) {
	for name, status := range map[string]int{"ignores the filter": http.StatusOK, "fails on it": http.StatusInternalServerError} {
		t.Run(name, func(t *testing.T) {
			listed := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				listed++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"data":[{"identifier":"dev-001","connection_status":"online"}]}`))
			}))
			defer srv.Close()

			out, err := execCmd(t, "",
				"device", "list",
				"--org", "acme", "--product", "thermostat",
				"--query", `health_status = "unhealthy"`,
				"--uri", srv.URL, "--token", "tok",
			)
			if err == nil || !strings.Contains(err.Error(), "needs v2.5.0 or later") {
				t.Errorf("want an unsupported server error, got %v", err)
			}
			if strings.Contains(out, "dev-001") || listed != 1 {
				t.Errorf("only the probe should be sent and nothing listed; requests %d, output %q", listed, out)
			}
		})
	}
}

// A proxy error says nothing about the server's version, so it is reported
// as it is rather than as an old server.
func TestDeviceListQueryProbeGatewayError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", `health_status = "unhealthy"`,
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || strings.Contains(err.Error(), "v2.5.0") || !strings.Contains(err.Error(), "503") {
		t.Errorf("want the 503 reported as is, got %v", err)
	}
}

// A server that passed the check isn't checked again within a day, from the
// same data directory.
func TestDeviceListQueryCheckIsRemembered(t *testing.T) {
	probes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("filters[advanced_query]") == api.AdvancedQueryProbe {
			probes++
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":{"detail":"invalid advanced query"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	for range 2 {
		_, err := execCmd(t, "",
			"device", "list",
			"--org", "acme", "--product", "thermostat",
			"--query", "metric:cpu_temp > 70",
			"--uri", srv.URL, "--token", "tok", "--data-dir", dataDir,
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}
	if probes != 1 {
		t.Errorf("want one support check across two lists, got %d", probes)
	}
}

func TestDeviceListQueryConflictsWithAdvancedQueryFilter(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", "metric:cpu_temp > 70",
		"--filter", "advanced_query:something",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "use one") {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestDeviceListInvalidQueryShowsServerError(t *testing.T) {
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("filters[advanced_query]")
		sent = append(sent, q)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		if q == api.AdvancedQueryProbe {
			_, _ = w.Write([]byte(`{"errors":{"detail":"invalid advanced query: probe rejected"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"errors":{"detail":"invalid advanced query: \"bogus\" is not a valid column (at character 0)"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--query", `bogus = "x"`,
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "not a valid column") {
		t.Errorf("expected the server's parse error for the query to surface, got %v", err)
	}
	if len(sent) != 2 || sent[0] != api.AdvancedQueryProbe || sent[1] != `bogus = "x"` {
		t.Errorf("want the probe and then the query, got %q", sent)
	}
}

func TestDeviceListInvalidFilter(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--filter", "connection",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "expected key:value") {
		t.Errorf("expected invalid-filter error, got %v", err)
	}
}

func TestDeviceListInvalidSort(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--sort", "identifier:sideways",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid sort direction") {
		t.Errorf("expected invalid-direction error, got %v", err)
	}
}

func TestDeviceListNegativePage(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--page", "-1",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Errorf("expected negative-page error, got %v", err)
	}
}

func TestDeviceListMissingOrg(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	_, err := execCmd(t, "",
		"device", "list",
		"--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected error when no org is set")
	}
	if !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("error should mention the missing org, got %v", err)
	}
}

func TestDeviceListMissingProduct(t *testing.T) {
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected error when no product is set")
	}
	if !strings.Contains(err.Error(), "no product set") {
		t.Errorf("error should mention the missing product, got %v", err)
	}
}
