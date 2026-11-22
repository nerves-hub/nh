package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const productsResponse = `{"data":[
	{"name":"thermostat","allowed_update_tools":["fwup","esp-idf","atomvm"],"require_unique_firmware_version":true,
	 "allow_unsigned_esp_idf_firmware":true,"allow_unsigned_atomvm_firmware":true,"default_managed_updates_allowed":true},
	{"name":"doorbell","allowed_update_tools":["fwup"],"require_unique_firmware_version":false,
	 "allow_unsigned_esp_idf_firmware":false,"allow_unsigned_atomvm_firmware":false,"default_managed_updates_allowed":false}
]}`

func TestProductListTable(t *testing.T) {
	resetState(t)
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(productsResponse))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"product", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok-123"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The org name is interpolated into the path.
	if gotPath != "/api/orgs/acme/products" {
		t.Errorf("path: want /api/orgs/acme/products, got %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth header: want %q, got %q", "Bearer tok-123", gotAuth)
	}

	s := out.String()
	for _, want := range []string{
		"NAME", "UPDATE TOOLS", "UNIQUE VERSIONS", "UNSIGNED ALLOWED", "DEVICE-MANAGED UPDATES",
		"thermostat", "fwup,esp-idf,atomvm", "esp-idf,atomvm",
		"doorbell", "none",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("table output missing %q, got:\n%s", want, s)
		}
	}
}

func TestProductListJSON(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(productsResponse))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"product", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok", "--output", "json"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{`"name": "thermostat"`, `"allowed_update_tools": [`, `"default_managed_updates_allowed": true`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("json output missing %s, got:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), `"inserted_at"`) {
		t.Errorf("json output has inserted_at, which the server never sends:\n%s", out.String())
	}
}

// A server older than product settings sends names alone; the table falls
// back to the name column rather than a row of dashes.
func TestProductListWithoutSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"thermostat"}]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "", "product", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "thermostat") || strings.Contains(out, "UPDATE TOOLS") {
		t.Errorf("want a name-only table, got:\n%s", out)
	}
}

func TestProductListEmpty(t *testing.T) {
	resetState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"product", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No products found in acme") {
		t.Errorf("expected empty-state message, got %q", out.String())
	}
}

func TestProductListMissingOrg(t *testing.T) {
	resetState(t)
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	var out bytes.Buffer
	// Token provided so this is specifically the missing-org path, not auth.
	rootCmd.SetArgs([]string{"product", "list", "--uri", "https://example.com", "--token", "tok"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no org is set")
	}
	if !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("error should mention the missing org, got %v", err)
	}
}
