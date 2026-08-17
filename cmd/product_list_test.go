package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const productsResponse = `{"data":[
	{"id":1,"name":"thermostat","inserted_at":"2026-01-02T15:04:05","updated_at":"2026-02-01T00:00:00"},
	{"id":2,"name":"doorbell","inserted_at":"2026-03-04T09:00:00","updated_at":"2026-03-05T00:00:00"}
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
	for _, want := range []string{"NAME", "thermostat", "doorbell"} {
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
	if !strings.Contains(out.String(), `"name": "thermostat"`) {
		t.Errorf("json output expected, got %q", out.String())
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
