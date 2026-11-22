package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDeviceUpgrade(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "upgrade", "dev-001", "fw-uuid-9",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/upgrade" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotBody["uuid"] != "fw-uuid-9" {
		t.Errorf("body: %+v", gotBody)
	}
	if !strings.Contains(out, "Upgrade to fw-uuid-9 requested for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceMove(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001","connection_status":"offline"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "move", "dev-001", "--to-product", "sensor",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/move" {
		t.Errorf("path: got %q", gotPath)
	}
	// Destination passed as query params; org defaults to the current org.
	if gotQuery.Get("new_org_name") != "acme" || gotQuery.Get("new_product_name") != "sensor" {
		t.Errorf("query: %v", gotQuery)
	}
	if !strings.Contains(out, "Moved device dev-001 to acme/sensor") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceMoveRequiresToProduct(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "move", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "--to-product is required") {
		t.Errorf("expected to-product error, got %v", err)
	}
}

func TestDeviceClearPenalty(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "clear-penalty", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/penalty" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Cleared the penalty box for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceRunCode(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "run-code", "dev-001", "NervesHub.version()",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/code" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotBody["code"] != "NervesHub.version()" {
		t.Errorf("body: %+v", gotBody)
	}
	if !strings.Contains(out, "Sent code to device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceScripts(t *testing.T) {
	var paths []string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/orgs/acme/products/thermostat/devices/dev-001":
			_, _ = w.Write([]byte(`{"data":{"identifier":"dev-001"}}`))
		case "/api/orgs/acme/products/thermostat/scripts":
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"data":[{"id":7,"name":"netinfo","language":"shell","tags":["net"]}],"pagination":{"page_number":1,"page_size":25,"total_entries":1,"total_pages":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "scripts", "dev-001", "--page-size", "50",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// With --org/--product the device is checked to be in the product, then
	// the product's scripts are listed: the same list the server's
	// /api/devices/:identifier/scripts gives, without its cross-org lookup.
	want := []string{
		"GET /api/orgs/acme/products/thermostat/devices/dev-001",
		"GET /api/orgs/acme/products/thermostat/scripts",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\ngot  %q\nwant %q", paths, want)
	}
	if gotQuery != "pagination%5Bpage_size%5D=50" {
		t.Errorf("query: got %q", gotQuery)
	}
	for _, want := range []string{"ID", "NAME", "LANGUAGE", "TAGS", "7", "netinfo", "shell", "net", "1 script(s) total"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceScriptsDeviceOutsideProduct(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"device", "scripts", "dev-001",
		"--org", "acme", "--product", "other",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected an error for a device outside the product")
	}
	if len(paths) != 1 {
		t.Errorf("scripts should not be listed once the device lookup fails: %q", paths)
	}
}

func TestDeviceScriptsWithoutOrgProduct(t *testing.T) {
	t.Setenv("NERVES_HUB_ORG", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Chdir(t.TempDir()) // no Mix project to detect an org/product from

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "scripts", "dev-001",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/api/devices/dev-001/scripts" {
		t.Errorf("requests: %q", paths)
	}
	if !strings.Contains(out, "No scripts available for device dev-001") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceScriptsHalfScope(t *testing.T) {
	t.Setenv("NERVES_HUB_ORG", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Chdir(t.TempDir())

	if _, err := execCmd(t, "", "device", "scripts", "dev-001", "--org", "acme", "--uri", "http://127.0.0.1:1", "--token", "tok"); err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Errorf("org only: got %v", err)
	}
	if _, err := execCmd(t, "", "device", "scripts", "dev-001", "--product", "p", "--uri", "http://127.0.0.1:1", "--token", "tok"); err == nil || !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("product only: got %v", err)
	}
}

func TestDeviceRunScript(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("eth0: 10.0.0.5\n"))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "run-script", "dev-001", "netinfo",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001/scripts/netinfo" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// Without --timeout the server's own default (30_000 ms) applies.
	if gotQuery != "" {
		t.Errorf("query should be empty without --timeout, got %q", gotQuery)
	}
	// The plain-text script output is printed verbatim.
	if !strings.Contains(out, "eth0: 10.0.0.5") {
		t.Errorf("output: %q", out)
	}
}

func TestDeviceRunScriptTimeout(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	}))
	defer srv.Close()

	if _, err := execCmd(t, "",
		"device", "run-script", "dev-001", "7", "--timeout", "2m",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// ScriptController.send reads `timeout` in milliseconds.
	if gotQuery != "timeout=120000" {
		t.Errorf("query: got %q", gotQuery)
	}

	for _, bad := range []string{"0s", "-5s", "1500h"} {
		if _, err := execCmd(t, "",
			"device", "run-script", "dev-001", "7", "--timeout", bad,
			"--org", "acme", "--product", "thermostat",
			"--uri", srv.URL, "--token", "tok",
		); err == nil || !strings.Contains(err.Error(), "--timeout must be") {
			t.Errorf("--timeout %s: got %v", bad, err)
		}
	}
}
