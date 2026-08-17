package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/mix"
)

// stubMixProject simulates running inside a Mix project that reports the given
// org and product, restoring the real detection afterwards.
func stubMixProject(t *testing.T, org, product string) {
	t.Helper()
	origA, origE := mix.Available, mix.Eval
	t.Cleanup(func() { mix.Available, mix.Eval = origA, origE })
	mix.Available = func() bool { return true }
	mix.Eval = func(expr string) string {
		switch {
		case strings.Contains(expr, ":org"):
			return org
		case strings.Contains(expr, ":name"), strings.Contains(expr, ":app"):
			return product
		}
		return ""
	}
}

// clearScopeEnv removes any org/product environment that would otherwise win
// over auto-detection.
func clearScopeEnv(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_ORG", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")
}

func TestMixAutoDetectScopesDeviceList(t *testing.T) {
	clearScopeEnv(t)
	stubMixProject(t, "acme", "thermostat")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// No --org/--product and no settings: both come from the Mix project.
	if _, err := execCmd(t, "",
		"device", "list",
		"--uri", srv.URL, "--token", "tok", "--data-dir", t.TempDir(),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/devices" {
		t.Errorf("auto-detected scope not used, path = %q", gotPath)
	}
}

func TestMixAutoDetectOverriddenByFlags(t *testing.T) {
	clearScopeEnv(t)
	// The stub fails the test if consulted: explicit flags must take priority.
	origA, origE := mix.Available, mix.Eval
	t.Cleanup(func() { mix.Available, mix.Eval = origA, origE })
	mix.Available = func() bool {
		t.Error("Mix detection should not run when --org/--product are set")
		return false
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	if _, err := execCmd(t, "",
		"device", "list",
		"--org", "globex", "--product", "doorbell",
		"--uri", srv.URL, "--token", "tok", "--data-dir", t.TempDir(),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/globex/products/doorbell/devices" {
		t.Errorf("explicit scope not used, path = %q", gotPath)
	}
}

func TestMixAutoDetectMissingStillErrors(t *testing.T) {
	clearScopeEnv(t)
	// Not a Mix project, nothing configured: the usual error stands.
	origA := mix.Available
	t.Cleanup(func() { mix.Available = origA })
	mix.Available = func() bool { return false }

	_, err := execCmd(t, "",
		"device", "list",
		"--uri", "https://example.com", "--token", "tok", "--data-dir", t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("expected missing-org error, got %v", err)
	}
}
