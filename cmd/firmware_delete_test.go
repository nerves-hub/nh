package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirmwareDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"firmware", "delete", "uuid-1", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: want DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/firmwares/uuid-1" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Firmware deleted successfully") {
		t.Errorf("output: %q", out)
	}
}

func TestFirmwareDeleteConfirmYes(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// Answer the prompt with "y" on stdin.
	out, err := execCmd(t, "y\n",
		"firmware", "delete", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("delete should have been issued after confirmation")
	}
	if !strings.Contains(out, "Delete firmware uuid-1? [y/N]") {
		t.Errorf("output should show the confirmation prompt, got %q", out)
	}
	if !strings.Contains(out, "Firmware deleted successfully") {
		t.Errorf("output should confirm deletion, got %q", out)
	}
}

func TestFirmwareDeleteAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// Default answer (empty line) declines.
	out, err := execCmd(t, "\n",
		"firmware", "delete", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no delete should be issued when the prompt is declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}

func TestFirmwareDeleteNonInteractiveRequiresYes(t *testing.T) {
	_, err := execCmd(t, "",
		"firmware", "delete", "uuid-1", "--non-interactive",
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected error pointing to --yes, got %v", err)
	}
}

func TestFirmwareDeleteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"firmware", "delete", "ghost", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestFirmwareDeleteMissingUUID(t *testing.T) {
	_, err := execCmd(t, "",
		"firmware", "delete",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Firmware UUID missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
