package cmd

import (
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/config"
)

func TestConfigSaveAndLoadProfiles(t *testing.T) {
	dir := t.TempDir()

	// Set up a "work" config and save it as a profile.
	mustExec(t, "config", "set", "org", "acme", "--data-dir", dir)
	mustExec(t, "config", "set", "product", "thermostat", "--data-dir", dir)
	if err := config.SaveToken(dir, "tok-work"); err != nil {
		t.Fatal(err)
	}
	if out := mustExec(t, "config", "save", "work", "--data-dir", dir); !strings.Contains(out, `profile "work"`) {
		t.Errorf("save output: %q", out)
	}

	// Switch to a different "home" config and save it too.
	mustExec(t, "config", "set", "org", "globex", "--data-dir", dir)
	mustExec(t, "config", "set", "product", "doorbell", "--data-dir", dir)
	if err := config.SaveToken(dir, "tok-home"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, "config", "save", "home", "--data-dir", dir)

	// Loading "work" restores its org, product, and token.
	if out := mustExec(t, "config", "load", "work", "--data-dir", dir); !strings.Contains(out, `Switched to profile "work"`) {
		t.Errorf("load output: %q", out)
	}
	s, err := config.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Org != "acme" || s.Product != "thermostat" || s.Token != "tok-work" {
		t.Errorf("after load work: org=%q product=%q token=%q", s.Org, s.Product, s.Token)
	}
	// Both profiles survive the switch.
	if len(s.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %v", s.ProfileNames())
	}
	if tok, _ := config.LoadToken(dir); tok != "tok-work" {
		t.Errorf("active token should be work's, got %q", tok)
	}
}

func TestConfigLoadUnknownProfile(t *testing.T) {
	dir := t.TempDir()
	mustExec(t, "config", "save", "work", "--data-dir", dir)

	_, err := execCmd(t, "", "config", "load", "ghost", "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), `no profile named "ghost"`) {
		t.Errorf("expected unknown-profile error, got %v", err)
	}
	// The error lists the available profiles.
	if err != nil && !strings.Contains(err.Error(), "available: work") {
		t.Errorf("error should list available profiles, got %v", err)
	}
}

func TestConfigGetHidesProfiles(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{
		Org: "acme",
		Profiles: map[string]config.Profile{
			"work": {Org: "acme", Token: "tok-secret"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "", "config", "get", "--data-dir", dir, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "profiles") || strings.Contains(out, "tok-secret") {
		t.Errorf("config get must not expose profiles or their tokens, got %q", out)
	}
}

// mustExec runs a command through execCmd and fails the test on error,
// returning its combined output.
func mustExec(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execCmd(t, "", args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out
}

func TestConfigProfilesList(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{
		Profiles: map[string]config.Profile{
			"work": {URI: "https://a.example", Org: "acme", Product: "thermostat", Token: "tok-secret"},
			"home": {Org: "globex"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	out := mustExec(t, "config", "profiles", "--data-dir", dir)

	for _, want := range []string{"NAME", "TOKEN", "work", "acme", "thermostat", "home", "globex"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q, got:\n%s", want, out)
		}
	}
	// Token presence is shown as yes/no; the value never appears.
	if strings.Contains(out, "tok-secret") {
		t.Errorf("the token value must not be shown, got:\n%s", out)
	}
	if !strings.Contains(out, "yes") || !strings.Contains(out, "no") {
		t.Errorf("token column should read yes/no, got:\n%s", out)
	}
}

func TestConfigProfilesJSON(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveSettings(dir, &config.Settings{
		Profiles: map[string]config.Profile{
			"work": {Org: "acme", Token: "tok-secret"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	out := mustExec(t, "config", "profiles", "--data-dir", dir, "-o", "json")
	if strings.Contains(out, "tok-secret") {
		t.Errorf("JSON must not contain the token value, got:\n%s", out)
	}
	if !strings.Contains(out, `"has_token": true`) {
		t.Errorf("JSON should report has_token, got:\n%s", out)
	}
}

func TestConfigProfilesEmpty(t *testing.T) {
	out := mustExec(t, "config", "profiles", "--data-dir", t.TempDir())
	if !strings.Contains(out, "No profiles saved") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}
