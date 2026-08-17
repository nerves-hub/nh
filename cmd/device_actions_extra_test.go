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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"7","name":"netinfo","tags":"net"}]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "scripts", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"ID", "NAME", "netinfo", "net"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
}

func TestDeviceRunScript(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
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
	// The plain-text script output is printed verbatim.
	if !strings.Contains(out, "eth0: 10.0.0.5") {
		t.Errorf("output: %q", out)
	}
}
