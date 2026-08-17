package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		"identifier":              {"identifier", "asc"},
		"identifier:asc":          {"identifier", "asc"},
		"identifier:desc":         {"identifier", "desc"},
		"last_communication:DESC": {"last_communication", "desc"},
		"identifier:":             {"identifier", "asc"}, // trailing colon, no direction
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

	for _, in := range []string{"", ":desc", "identifier:sideways"} {
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
	got, err := parseFilters([]string{"connection:not_seen", "tag:prod"})
	if err != nil {
		t.Fatalf("parseFilters: %v", err)
	}
	if got["connection"] != "not_seen" || got["tag"] != "prod" {
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
		gotTag = r.URL.Query().Get("filters[tag]")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "list",
		"--org", "acme", "--product", "thermostat",
		"--filter", "connection:not_seen",
		"--filter", "tag:prod",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotConnection != "not_seen" || gotTag != "prod" {
		t.Errorf("filter query: got filters[connection]=%q filters[tag]=%q", gotConnection, gotTag)
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
