package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/config"
)

func TestConfigSetGetUnset(t *testing.T) {
	dir := t.TempDir()

	// set org and product
	if out, err := execCmd(t, "", "config", "set", "org", "acme", "--data-dir", dir); err != nil {
		t.Fatalf("set org: %v", err)
	} else if !strings.Contains(out, `Set org to "acme"`) {
		t.Errorf("set org output: %q", out)
	}
	if _, err := execCmd(t, "", "config", "set", "product", "thermostat", "--data-dir", dir); err != nil {
		t.Fatalf("set product: %v", err)
	}

	// the file really holds the values
	settings, err := config.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Org != "acme" || settings.Product != "thermostat" {
		t.Fatalf("persisted settings: %+v", settings)
	}

	// get all
	out, err := execCmd(t, "", "config", "get", "--data-dir", dir)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "thermostat") {
		t.Errorf("get output: %q", out)
	}

	// get single key prints just the value
	out, err = execCmd(t, "", "config", "get", "org", "--data-dir", dir)
	if err != nil {
		t.Fatalf("get org: %v", err)
	}
	if strings.TrimSpace(out) != "acme" {
		t.Errorf("get org output: %q", out)
	}

	// unset
	if _, err := execCmd(t, "", "config", "unset", "org", "--data-dir", dir); err != nil {
		t.Fatalf("unset: %v", err)
	}
	settings, _ = config.LoadSettings(dir)
	if settings.Org != "" {
		t.Errorf("org should be cleared, got %q", settings.Org)
	}
	if settings.Product != "thermostat" {
		t.Errorf("product should be untouched, got %q", settings.Product)
	}
}

// TestConfigGetHidesToken verifies the saved auth token, which lives in the
// settings file, is never exposed through `nh config get`.
func TestConfigGetHidesToken(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{Org: "acme", Token: "nhu_secret"}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"config", "get", "--data-dir", dir},
		{"config", "get", "--data-dir", dir, "-o", "json"},
	} {
		out, err := execCmd(t, "", args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out, "nhu_secret") || strings.Contains(out, "token") {
			t.Errorf("%v leaked the token: %q", args, out)
		}
	}
}

func TestConfigSetUnknownKey(t *testing.T) {
	out, err := execCmd(t, "", "config", "set", "bogus", "x", "--data-dir", t.TempDir())
	if err == nil {
		t.Fatalf("expected error for unknown key, output: %q", out)
	}
	if !strings.Contains(err.Error(), "unknown setting") {
		t.Errorf("error should name the problem, got %v", err)
	}
}

// TestConfigSettingsURI verifies a saved uri is used as the request base when
// no --uri flag or env var is given.
func TestConfigSettingsURI(t *testing.T) {
	t.Setenv("NERVES_CLOUD_URI", "")
	t.Setenv("NERVES_HUB_URI", "")

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{URI: srv.URL}); err != nil {
		t.Fatal(err)
	}

	// No --uri passed: the request base must come from the saved settings.
	if _, err := execCmd(t, "", "org", "list", "--token", "tok", "--data-dir", dir); err != nil {
		t.Fatalf("org list: %v", err)
	}
	if !hit {
		t.Error("saved uri should have been used as the request base")
	}
}

// TestConfigSettingsPrecedence verifies a saved default org flows through to a
// scoped command when no --org flag or env var is given.
func TestConfigSettingsPrecedence(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{Org: "acme"}); err != nil {
		t.Fatal(err)
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// No --org passed: it must come from the saved settings.
	_, err := execCmd(t, "", "product", "list", "--uri", srv.URL, "--token", "tok", "--data-dir", dir)
	if err != nil {
		t.Fatalf("product list: %v", err)
	}
	if gotPath != "/api/orgs/acme/products" {
		t.Errorf("saved org should scope the request, got path %q", gotPath)
	}
}

// TestConfigFlagOverridesSettings verifies --org still wins over a saved
// default.
func TestConfigFlagOverridesSettings(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{Org: "acme"}); err != nil {
		t.Fatal(err)
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "", "product", "list", "--org", "globex", "--uri", srv.URL, "--token", "tok", "--data-dir", dir)
	if err != nil {
		t.Fatalf("product list: %v", err)
	}
	if gotPath != "/api/orgs/globex/products" {
		t.Errorf("--org should override saved default, got path %q", gotPath)
	}
}
