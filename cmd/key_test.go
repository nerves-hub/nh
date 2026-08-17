package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const signingKeysResponse = `{"data":[
	{"name":"production","key":"ntpubkeyAAA"},
	{"name":"staging","key":"ntpubkeyBBB"}
]}`

func TestKeyList(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(signingKeysResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"key", "list",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/keys" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"NAME", "PUBLIC KEY", "production", "ntpubkeyAAA", "staging"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
}

func TestKeyListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"key", "list",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No signing keys found in acme") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestKeyShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"production","key":"ntpubkeyAAA"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"key", "show", "production",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/keys/production" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Name:", "production", "Public key:", "ntpubkeyAAA"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestKeyShowMissingName(t *testing.T) {
	_, err := execCmd(t, "",
		"key", "show",
		"--org", "acme", "--token", "tok",
	)
	if err == nil || err.Error() != "Signing key name missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestKeyDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"key", "delete", "production", "--yes",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: want DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/keys/production" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.Contains(out, "Deleted signing key production") {
		t.Errorf("output: %q", out)
	}
}

func TestKeyDeleteAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "\n",
		"key", "delete", "production",
		"--org", "acme",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no deletion should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output should report the abort, got %q", out)
	}
}
