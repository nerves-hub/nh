package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/config"
)

func TestLogoutRemovesToken(t *testing.T) {
	resetState(t)
	dir := t.TempDir()
	if err := config.SaveToken(dir, "nhu_secret"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"user", "logout", "--data-dir", dir})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got, _ := config.LoadToken(dir); got != "" {
		t.Errorf("token should be removed, got %q", got)
	}
	if !strings.Contains(out.String(), "Logged out") {
		t.Errorf("output should confirm logout, got %q", out.String())
	}
}

func TestLogoutNoToken(t *testing.T) {
	resetState(t)
	dir := t.TempDir()

	var out bytes.Buffer
	rootCmd.SetArgs([]string{"user", "logout", "--data-dir", dir})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No saved token") {
		t.Errorf("output should note there was nothing to remove, got %q", out.String())
	}
}
