package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const firmwaresResponse = `{"data":[
	{"uuid":"uuid-1","version":"1.2.3","platform":"rpi4","architecture":"arm","tool":"fwup","size":1048576,"inserted_at":"2026-01-02 00:00:00Z"},
	{"uuid":"uuid-2","version":"1.3.0","platform":"atomvm","architecture":"beam","tool":"atomvm","size":2048,"inserted_at":"2026-02-02 00:00:00Z"}
]}`

func TestFirmwareList(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(firmwaresResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"firmware", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/firmwares" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"VERSION", "FORMAT", "PLATFORM", "ARCHITECTURE", "UUID", "1.2.3", "uuid-1", "1.3.0", "fwup", "atomvm"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
}

func TestFirmwareListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"firmware", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No firmware found in acme/thermostat") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestFirmwareShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"uuid":"uuid-1","version":"1.2.3","platform":"rpi4","architecture":"arm","author":"acme","tool":"esp-idf","size":1432112,"inserted_at":"2026-01-02 00:00:00Z"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"firmware", "show", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/firmwares/uuid-1" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"UUID:", "uuid-1", "Version:", "1.2.3", "Platform:", "rpi4", "Author:", "acme",
		"Format:", "esp-idf", "Size:", "1.4 MiB (1432112 bytes)"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestFirmwareShowJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"uuid":"uuid-1","version":"1.2.3","tool":"rauc","size":10}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"firmware", "show", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{`"uuid": "uuid-1"`, `"tool": "rauc"`, `"size": 10`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s, got %q", want, out)
		}
	}
	// Fields the server never sends are not invented.
	for _, gone := range []string{"fwup_version", "updated_at"} {
		if strings.Contains(out, gone) {
			t.Errorf("json output should not carry %s, got %q", gone, out)
		}
	}
}

func TestFirmwareShowMissingUUID(t *testing.T) {
	_, err := execCmd(t, "",
		"firmware", "show",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Firmware UUID missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestFirmwareListMissingProduct(t *testing.T) {
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")

	_, err := execCmd(t, "",
		"firmware", "list",
		"--org", "acme",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Errorf("expected missing-product error, got %v", err)
	}
}
