package cmd

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacyDir builds a fake old nerves_hub_cli directory: an ETF config and
// one signing key under org "acme".
func writeLegacyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// ETF map: %{uri: ..., org: ..., token: ...}
	cfg := []byte{131, 116}
	cfg = binary.BigEndian.AppendUint32(cfg, 3)
	for _, kv := range [][2]string{{"uri", "https://legacy.test"}, {"org", "acme"}, {"token", "nhu_legacy"}} {
		cfg = append(cfg, 119, byte(len(kv[0])))
		cfg = append(cfg, kv[0]...)
		cfg = append(cfg, 109)
		cfg = binary.BigEndian.AppendUint32(cfg, uint32(len(kv[1])))
		cfg = append(cfg, kv[1]...)
	}
	if err := os.WriteFile(filepath.Join(dir, "nerves-hub.config"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}

	// One key: a base64url-wrapped ETF envelope (opaque bytes) + a public key.
	envelope := []byte{131, 116, 0, 0, 0, 0}
	keyDir := filepath.Join(dir, "keys", "acme")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	priv := base64.RawURLEncoding.EncodeToString(envelope)
	if err := os.WriteFile(filepath.Join(keyDir, "dev.priv"), []byte(priv+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "dev.pub"), []byte("cHVia2V5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	from := writeLegacyDir(t)
	data := t.TempDir()

	out, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data, "--dry-run")
	if err != nil {
		t.Fatalf("migrate --dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Would import from") || !strings.Contains(out, "dry run") {
		t.Fatalf("unexpected dry-run output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write settings.json")
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Fatalf("dry run wrote files: %v", entries)
	}
}

func TestMigrateApplyImportsSettingsAndKeys(t *testing.T) {
	from := writeLegacyDir(t)
	data := t.TempDir()

	out, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}

	// Settings imported (token value must not appear in output).
	b, err := os.ReadFile(filepath.Join(data, "settings.json"))
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	s := string(b)
	for _, want := range []string{"https://legacy.test", "acme", "nhu_legacy"} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(out, "nhu_legacy") {
		t.Fatalf("token leaked into output:\n%s", out)
	}

	// Key imported into keys/acme/.
	if _, err := os.Stat(filepath.Join(data, "keys", "acme", "dev.priv")); err != nil {
		t.Fatalf("private key not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "keys", "acme", "dev.pub")); err != nil {
		t.Fatalf("public key not imported: %v", err)
	}
	// The data-dir README should have been dropped.
	if _, err := os.Stat(filepath.Join(data, "README.md")); err != nil {
		t.Fatalf("data-dir README not written: %v", err)
	}
}

func TestMigrateIsIdempotentAndForce(t *testing.T) {
	from := writeLegacyDir(t)
	data := t.TempDir()

	if _, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	// Second run skips everything.
	out, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data)
	if err != nil {
		t.Fatalf("second migrate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 imported, 1 skipped") {
		t.Fatalf("expected keys skipped on re-run:\n%s", out)
	}
	if !strings.Contains(out, "uri      skip") {
		t.Fatalf("expected settings skipped on re-run:\n%s", out)
	}

	// --force re-imports.
	out, err = execCmd(t, "", "migrate", "--from", from, "--data-dir", data, "--force")
	if err != nil {
		t.Fatalf("force migrate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 imported, 0 skipped") {
		t.Fatalf("expected keys re-imported with --force:\n%s", out)
	}
}

func TestMigrateMissingDir(t *testing.T) {
	data := t.TempDir()
	out, err := execCmd(t, "", "migrate", "--from", filepath.Join(t.TempDir(), "nope"), "--data-dir", data)
	if err == nil {
		t.Fatalf("expected error for missing legacy dir, got:\n%s", out)
	}
}

func TestMigrateRespectsExistingSettings(t *testing.T) {
	from := writeLegacyDir(t)
	data := t.TempDir()

	// Pre-existing org should not be overwritten without --force.
	if _, err := execCmd(t, "", "config", "set", "org", "keepme", "--data-dir", data); err != nil {
		t.Fatalf("seeding org: %v", err)
	}

	out, err := execCmd(t, "", "migrate", "--from", from, "--data-dir", data)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "org      skip") {
		t.Fatalf("expected existing org to be skipped:\n%s", out)
	}
	b, _ := os.ReadFile(filepath.Join(data, "settings.json"))
	if !strings.Contains(string(b), "keepme") {
		t.Fatalf("existing org was overwritten:\n%s", b)
	}
}
