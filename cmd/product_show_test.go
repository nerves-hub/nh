package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":7,"name":"thermostat","inserted_at":"2026-01-02 00:00:00Z","updated_at":"2026-02-02 00:00:00Z"}}`))
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
	for _, want := range []string{"Name:", "thermostat", "ID:", "7", "Created:", "2026-01-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestProductShowJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":7,"name":"thermostat"}}`))
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
	if !strings.Contains(out, `"name": "thermostat"`) {
		t.Errorf("json output expected, got %q", out)
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
