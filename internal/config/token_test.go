package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()

	if err := SaveToken(dir, "  nhu_secret\n"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := LoadToken(dir)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got != "nhu_secret" {
		t.Errorf("token: want %q, got %q", "nhu_secret", got)
	}

	// The token lives in the settings file, which must not be world/group
	// readable.
	info, err := os.Stat(SettingsFilePath(dir))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings file perm: want 0600, got %o", perm)
	}
}

func TestSaveTokenPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSettings(dir, &Settings{Org: "acme", Product: "thermostat"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveToken(dir, "nhu_secret"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != "nhu_secret" || s.Org != "acme" || s.Product != "thermostat" {
		t.Errorf("SaveToken should keep other settings, got %+v", s)
	}
}

func TestLoadTokenMissingFile(t *testing.T) {
	got, err := LoadToken(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadToken on missing dir should not error, got %v", err)
	}
	if got != "" {
		t.Errorf("want empty token, got %q", got)
	}
}

func TestSaveTokenRejectsEmpty(t *testing.T) {
	if err := SaveToken(t.TempDir(), "   "); err == nil {
		t.Error("SaveToken should reject an empty token")
	}
}

func TestDeleteToken(t *testing.T) {
	dir := t.TempDir()
	if err := SaveToken(dir, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteToken(dir); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if got, _ := LoadToken(dir); got != "" {
		t.Errorf("token should be cleared, got %q", got)
	}
	// Deleting again is a no-op.
	if err := DeleteToken(dir); err != nil {
		t.Errorf("DeleteToken on missing token should be nil, got %v", err)
	}
}
