package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"product", "delete", "thermostat", "--yes",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: want DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products/thermostat" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Deleted product thermostat") {
		t.Errorf("output: %q", out)
	}
}

func TestProductDeleteConfirmYes(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "y\n",
		"product", "delete", "thermostat",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("delete should have been issued after confirmation")
	}
	if !strings.Contains(out, "Delete product thermostat? [y/N]") {
		t.Errorf("output should show the prompt, got %q", out)
	}
}

func TestProductDeleteAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "\n",
		"product", "delete", "thermostat",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no delete should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}

func TestProductDeleteMissingName(t *testing.T) {
	_, err := execCmd(t, "",
		"product", "delete",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || err.Error() != "Product name missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
