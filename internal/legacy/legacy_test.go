package legacy

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// etfConfig builds an :erlang.term_to_binary-style map with atom keys and
// binary values, matching what the old nerves_hub_cli wrote to
// nerves-hub.config.
func etfConfig(pairs [][2]string) []byte {
	b := []byte{etfVersion, etfTagMap}
	b = binary.BigEndian.AppendUint32(b, uint32(len(pairs)))
	for _, p := range pairs {
		// key: SMALL_ATOM_UTF8_EXT
		b = append(b, etfTagSmallAtomU, byte(len(p[0])))
		b = append(b, p[0]...)
		// value: BINARY_EXT
		b = append(b, etfTagBinary)
		b = binary.BigEndian.AppendUint32(b, uint32(len(p[1])))
		b = append(b, p[1]...)
	}
	return b
}

// legacyPriv wraps envelope bytes the way an old .priv file stores them:
// standard base64 of an Erlang term (leading version byte 131).
func legacyPriv(envelope []byte) string {
	return base64.StdEncoding.EncodeToString(envelope)
}

func TestReadConfig(t *testing.T) {
	dir := t.TempDir()
	raw := etfConfig([][2]string{
		{"uri", "https://example.test"},
		{"org", "acme"},
		{"product", "widget"},
		{"token", "nhu_fake_token"},
	})
	if err := os.WriteFile(filepath.Join(dir, configFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.URI != "https://example.test" || cfg.Org != "acme" || cfg.Product != "widget" || cfg.Token != "nhu_fake_token" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestReadConfigMissing(t *testing.T) {
	cfg, found, err := ReadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if found {
		t.Fatal("expected found=false for missing file")
	}
	if !cfg.IsEmpty() {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestReadConfigPartial(t *testing.T) {
	dir := t.TempDir()
	// Only uri and token present, as the old CLI writes before an org is set.
	raw := etfConfig([][2]string{
		{"uri", "https://example.test"},
		{"token", "nhu_fake"},
	})
	if err := os.WriteFile(filepath.Join(dir, configFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, found, err := ReadConfig(dir)
	if err != nil || !found {
		t.Fatalf("ReadConfig: found=%v err=%v", found, err)
	}
	if cfg.Org != "" || cfg.Product != "" {
		t.Fatalf("expected empty org/product, got %+v", cfg)
	}
	if cfg.URI == "" || cfg.Token == "" {
		t.Fatalf("expected uri/token set, got %+v", cfg)
	}
}

func TestConvertPrivRoundTrips(t *testing.T) {
	// A stand-in envelope: the real bytes are opaque here; ConvertPriv only
	// requires the ETF version byte and lossless re-encoding.
	envelope := append([]byte{etfVersion, etfTagMap, 0, 0, 0, 0}, []byte("payload+with/std64chars")...)
	url, err := ConvertPriv(legacyPriv(envelope))
	if err != nil {
		t.Fatalf("ConvertPriv: %v", err)
	}
	// The result must be valid base64url (unpadded) and decode to the exact
	// original bytes — the encrypted material must be preserved.
	back, err := base64.RawURLEncoding.DecodeString(url)
	if err != nil {
		t.Fatalf("result is not base64url: %v", err)
	}
	if string(back) != string(envelope) {
		t.Fatalf("round-trip mismatch: %x != %x", back, envelope)
	}
}

func TestConvertPrivRejectsForeign(t *testing.T) {
	// Valid base64, but not an Erlang term.
	if _, err := ConvertPriv(base64.StdEncoding.EncodeToString([]byte("not etf"))); err == nil {
		t.Fatal("expected error for non-ETF key")
	}
	if _, err := ConvertPriv("!!!not base64!!!"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestListKeys(t *testing.T) {
	dir := t.TempDir()
	env := []byte{etfVersion, etfTagMap, 0, 0, 0, 0}

	writePair := func(org, name string, withPub bool) {
		orgDir := filepath.Join(dir, "keys", org)
		if err := os.MkdirAll(orgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(orgDir, name+privExt), []byte(legacyPriv(env)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if withPub {
			if err := os.WriteFile(filepath.Join(orgDir, name+pubExt), []byte("cHVia2V5\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	writePair("beta", "dev", true)
	writePair("acme", "prod", true)
	writePair("acme", "orphan", false) // private key with no matching .pub

	keys, warnings, err := ListKeys(dir)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %+v", len(keys), keys)
	}
	// Sorted by org then name.
	if keys[0].Org != "acme" || keys[0].Name != "prod" {
		t.Fatalf("unexpected first key: %+v", keys[0])
	}
	if keys[1].Org != "beta" || keys[1].Name != "dev" {
		t.Fatalf("unexpected second key: %+v", keys[1])
	}
	if keys[0].Pub != "cHVia2V5" {
		t.Fatalf("public key not trimmed/read: %q", keys[0].Pub)
	}
	if _, err := base64.RawURLEncoding.DecodeString(keys[0].Priv); err != nil {
		t.Fatalf("converted priv not base64url: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for the orphan key, got %v", warnings)
	}
}

func TestListKeysMissingDir(t *testing.T) {
	keys, warnings, err := ListKeys(t.TempDir())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 0 || len(warnings) != 0 {
		t.Fatalf("expected no keys/warnings, got %d/%d", len(keys), len(warnings))
	}
}
