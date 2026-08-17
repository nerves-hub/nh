package config

import (
	"os"
	"testing"
)

func TestSaveAndLoadSettings(t *testing.T) {
	dir := t.TempDir()

	if err := SaveSettings(dir, &Settings{Org: "acme", Product: "thermostat"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.Org != "acme" || got.Product != "thermostat" {
		t.Errorf("settings: got %+v", got)
	}

	info, err := os.Stat(SettingsFilePath(dir))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings file perm: want 0600, got %o", perm)
	}
}

func TestLoadSettingsMissingFile(t *testing.T) {
	got, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSettings on missing file should not error, got %v", err)
	}
	if got.Org != "" || got.Product != "" {
		t.Errorf("want empty settings, got %+v", got)
	}
}

func TestSettingsGetSet(t *testing.T) {
	var s Settings
	if err := s.Set("org", "acme"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("product", "thermostat"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("org"); v != "acme" {
		t.Errorf("org: got %q", v)
	}
	if v, _ := s.Get("product"); v != "thermostat" {
		t.Errorf("product: got %q", v)
	}

	if err := s.Set("nope", "x"); err == nil {
		t.Error("Set should reject an unknown key")
	}
	if _, err := s.Get("nope"); err == nil {
		t.Error("Get should reject an unknown key")
	}

	// Empty value clears the setting.
	if err := s.Set("org", ""); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("org"); v != "" {
		t.Errorf("org should be cleared, got %q", v)
	}
}

func TestSaveAndLoadProfile(t *testing.T) {
	s := &Settings{URI: "https://a.example", Org: "acme", Product: "thermostat", Token: "tok-a"}

	// Snapshot the active config as "work".
	s.SaveProfile("work")

	// Change the active config and snapshot it as "home".
	s.URI, s.Org, s.Product, s.Token = "https://b.example", "globex", "doorbell", "tok-b"
	s.SaveProfile("home")

	if names := s.ProfileNames(); len(names) != 2 || names[0] != "home" || names[1] != "work" {
		t.Errorf("ProfileNames: got %v", names)
	}

	// Switching to "work" restores its full config, including the token.
	if err := s.LoadProfile("work"); err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if s.URI != "https://a.example" || s.Org != "acme" || s.Product != "thermostat" || s.Token != "tok-a" {
		t.Errorf("after load work: %+v", *s)
	}

	// The profiles themselves are preserved across a load.
	if len(s.Profiles) != 2 {
		t.Errorf("profiles should be preserved, got %v", s.Profiles)
	}

	// An unknown profile is an error.
	if err := s.LoadProfile("nope"); err == nil {
		t.Error("loading an unknown profile should error")
	}
}

func TestSaveProfileRoundTripsThroughFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSettings(dir, &Settings{Org: "acme", Token: "tok"}); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SaveProfile("first")
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := reloaded.Profiles["first"]
	if !ok || p.Org != "acme" || p.Token != "tok" {
		t.Errorf("profile did not round-trip through the file: %+v", reloaded.Profiles)
	}
}
