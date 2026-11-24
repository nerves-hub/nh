package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()

	if _, err := SaveToken(dir, DefaultURI, "  nhu_secret\n"); err != nil {
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
	if _, err := SaveToken(dir, DefaultURI, "nhu_secret"); err != nil {
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
	if _, err := SaveToken(t.TempDir(), DefaultURI, "   "); err == nil {
		t.Error("SaveToken should reject an empty token")
	}
}

func TestDeleteToken(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveToken(dir, DefaultURI, "tok"); err != nil {
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

func TestSaveTokenRecordsServer(t *testing.T) {
	dir := t.TempDir()
	changed, err := SaveToken(dir, "https://hub.corp", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("saving a token for a non-default server should make it the default")
	}
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != "tok" || s.TokenURI != "https://hub.corp" || s.URI != "https://hub.corp" {
		t.Errorf("got token=%q token_uri=%q uri=%q", s.Token, s.TokenURI, s.URI)
	}

	// Signing in again to the same server doesn't change the default.
	if changed, err := SaveToken(dir, "https://HUB.corp/", "tok2"); err != nil || changed {
		t.Errorf("same server: changed=%v err=%v", changed, err)
	}
}

func TestSaveTokenForDefaultServerLeavesURIUnset(t *testing.T) {
	dir := t.TempDir()
	changed, err := SaveToken(dir, DefaultURI, "tok")
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	s, _ := LoadSettings(dir)
	if s.URI != "" || s.TokenURI != DefaultURI {
		t.Errorf("uri=%q token_uri=%q", s.URI, s.TokenURI)
	}
}

func TestSaveTokenRejectsEmptyURI(t *testing.T) {
	if _, err := SaveToken(t.TempDir(), " ", "tok"); err == nil {
		t.Error("SaveToken should reject an empty URI")
	}
}

func TestDeleteTokenClearsServer(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveToken(dir, "https://hub.corp", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteToken(dir); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadSettings(dir)
	if s.Token != "" || s.TokenURI != "" {
		t.Errorf("token=%q token_uri=%q", s.Token, s.TokenURI)
	}
	if s.URI != "https://hub.corp" {
		t.Errorf("logging out should keep the default server, got %q", s.URI)
	}
}

func TestTokenFor(t *testing.T) {
	s := &Settings{Token: "tok", TokenURI: "https://hub.corp"}

	if got, err := s.TokenFor("https://hub.corp/"); err != nil || got != "tok" {
		t.Errorf("same server: got %q, %v", got, err)
	}

	got, err := s.TokenFor(DefaultURI)
	if got != "" {
		t.Errorf("a token must not be returned for another server, got %q", got)
	}
	var mismatch *TokenMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("want *TokenMismatchError, got %v", err)
	}
	for _, want := range []string{"saved token is for https://hub.corp", "--uri https://hub.corp", "nh user login --uri " + DefaultURI} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}

	if got, err := (&Settings{}).TokenFor(DefaultURI); got != "" || err != nil {
		t.Errorf("no token: got %q, %v", got, err)
	}
}

// Settings files written before nh recorded token_uri keep working: the token
// is taken to belong to the saved uri, or the default server.
func TestLoadSettingsPinsTokenServerForOldFiles(t *testing.T) {
	tests := []struct {
		name, file, want string
	}{
		{"saved uri", `{"uri":"https://hub.corp","token":"tok"}`, "https://hub.corp"},
		{"no uri", `{"token":"tok"}`, DefaultURI},
		{"recorded", `{"uri":"https://other","token":"tok","token_uri":"https://hub.corp"}`, "https://hub.corp"},
		{"no token", `{"uri":"https://hub.corp"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(SettingsFilePath(dir), []byte(tt.file), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := LoadSettings(dir)
			if err != nil {
				t.Fatal(err)
			}
			if s.TokenURI != tt.want {
				t.Errorf("token_uri: want %q, got %q", tt.want, s.TokenURI)
			}
		})
	}

	// Once pinned, changing the uri setting doesn't carry the token along.
	dir := t.TempDir()
	if err := os.WriteFile(SettingsFilePath(dir), []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadSettings(dir)
	_ = s.Set("uri", "https://elsewhere")
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	s, _ = LoadSettings(dir)
	if _, err := s.TokenFor("https://elsewhere"); err == nil {
		t.Error("an old token must not follow a later uri change")
	}
}

func TestLoadSettingsPinsProfileTokenServer(t *testing.T) {
	dir := t.TempDir()
	file := `{"profiles":{"work":{"uri":"https://hub.corp","token":"a"},"home":{"token":"b"},"bare":{"org":"x"}}}`
	if err := os.WriteFile(SettingsFilePath(dir), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Profiles["work"].TokenURI; got != "https://hub.corp" {
		t.Errorf("work: %q", got)
	}
	if got := s.Profiles["home"].TokenURI; got != DefaultURI {
		t.Errorf("home: %q", got)
	}
	if got := s.Profiles["bare"].TokenURI; got != "" {
		t.Errorf("bare: %q", got)
	}
}

func TestSameURI(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://hub.corp", "https://hub.corp", true},
		{"https://hub.corp/", "https://HUB.corp", true},
		{"HTTPS://hub.corp:443", "https://hub.corp", true},
		{"http://hub.corp:80/", "http://hub.corp", true},
		{"https://hub.corp/api", "https://hub.corp", true},
		{"https://example.com/hub/api/", "https://example.com/hub", true},
		{"https://[::1]:8443", "https://[::1]:8443/", true},
		{"https://hub.corp", "http://hub.corp", false},
		{"https://hub.corp:8443", "https://hub.corp", false},
		{"https://hub.corp", "https://hub.corp.evil", false},
		{"https://example.com/a", "https://example.com/b", false},
		{"https://[::1]", "https://[::1]:443", true},
	}
	for _, tt := range tests {
		if got := SameURI(tt.a, tt.b); got != tt.want {
			t.Errorf("SameURI(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
