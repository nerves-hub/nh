package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// productShowResponse is what ProductJSON.show/1 renders: the name and the
// product's settings, with no id or timestamps.
const productShowResponse = `{"data":{
	"name":"thermostat",
	"allowed_update_tools":["fwup","esp-idf"],
	"require_unique_firmware_version":true,
	"allow_unsigned_esp_idf_firmware":true,
	"allow_unsigned_atomvm_firmware":false,
	"default_managed_updates_allowed":false
}}`

func TestProductShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(productShowResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"product", "show", "thermostat",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{
		"Name:", "thermostat",
		"Update tools:", "fwup, esp-idf",
		"Unique firmware versions:", "required",
		"Unsigned ESP-IDF firmware:", "allowed",
		"Unsigned AtomVM firmware:", "refused",
		"Device-managed updates (new devices):", "not allowed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestProductShowJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(productShowResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"product", "show", "thermostat",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		`"name": "thermostat"`,
		`"allowed_update_tools": [`,
		`"esp-idf"`,
		`"require_unique_firmware_version": true`,
		`"allow_unsigned_esp_idf_firmware": true`,
		`"allow_unsigned_atomvm_firmware": false`,
		`"default_managed_updates_allowed": false`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s, got:\n%s", want, out)
		}
	}
	for _, gone := range []string{`"id"`, `"inserted_at"`, `"updated_at"`} {
		if strings.Contains(out, gone) {
			t.Errorf("json output has %s, which the server never sends:\n%s", gone, out)
		}
	}
}

// An empty list the server reports means no firmware is accepted; it must not
// read like a missing setting.
func TestProductShowEmptyToolList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"thermostat","allowed_update_tools":[]}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "", "product", "show", "thermostat", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Update tools:") || !strings.Contains(out, "none") {
		t.Errorf("want Update tools: none, got:\n%s", out)
	}
}

// A server older than product settings sends the name alone. Nothing should
// be shown as off when it was never reported.
func TestProductShowWithoutSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"thermostat"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"product", "show", "thermostat",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "thermostat") {
		t.Errorf("output missing name: %q", out)
	}
	for _, gone := range []string{"Update tools:", "Unique", "Unsigned", "Device-managed"} {
		if strings.Contains(out, gone) {
			t.Errorf("output shows unreported setting %q:\n%s", gone, out)
		}
	}
}

func TestProductShowMissingName(t *testing.T) {
	_, err := execCmd(t, "",
		"product", "show",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || err.Error() != "Product name missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
