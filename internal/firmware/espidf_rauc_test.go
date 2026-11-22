package firmware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ESP-IDF and RAUC fixtures come from nerves_hub_web's test/fixtures. The
// signed ESP-IDF image was produced by espsecure.py itself. The RAUC files
// keep the real CMS signatures from bundles built by RAUC 1.8 and 1.13, with
// the SquashFS payload cut down to its magic: the manifest lives in the
// signature, so nothing else is read.

func TestReadESPIDFMetadata(t *testing.T) {
	for _, name := range []string{"esp_idf_unsigned.bin", "esp_idf_signed_rsa3072.bin"} {
		meta, warnings, err := ReadMetadata(ESPIDF, filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := Metadata{
			Product:      "my_app",
			Version:      "1.2.3",
			Platform:     "esp32s3",
			Architecture: "xtensa",
			UUID:         "abababab-abab-abab-abab-abababababab",
			Description:  "ESP-IDF v5.2.1",
		}
		if meta != want {
			t.Errorf("%s: metadata = %+v, want %+v", name, meta, want)
		}
		if len(warnings) != 0 {
			t.Errorf("%s: warnings %v", name, warnings)
		}
	}
}

func TestESPIDFSigned(t *testing.T) {
	if signed, err := ESPIDFSigned("testdata/esp_idf_signed_rsa3072.bin"); err != nil || !signed {
		t.Errorf("signed fixture: %v, %v", signed, err)
	}
	if signed, err := ESPIDFSigned("testdata/esp_idf_unsigned.bin"); err != nil || signed {
		t.Errorf("unsigned fixture: %v, %v", signed, err)
	}

	// The block magic alone, without a matching CRC, is a coincidence in an
	// unsigned image rather than a signature.
	img, _ := os.ReadFile("testdata/esp_idf_unsigned.bin")
	img = append(img, make([]byte, 8192)...)
	img[len(img)-4096] = 0xE7
	path := filepath.Join(t.TempDir(), "lookalike.bin")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	if signed, _ := ESPIDFSigned(path); signed {
		t.Error("a stray 0xE7 is not a signature block")
	}
}

// stubEspsecure puts a fake espsecure on the command list that records its
// arguments and copies its input to --output with a marker appended.
func stubEspsecure(t *testing.T, script string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	if script == "" {
		script = "#!/bin/sh\n" +
			`printf '%s\n' "$@" > "` + argsFile + `"` + "\n" +
			`while [ $# -gt 1 ]; do [ "$1" = --output ] && out="$2"; shift; done` + "\n" +
			`cat "$1" > "$out"; printf SIGNED >> "$out"` + "\n"
	}
	bin := filepath.Join(dir, "espsecure")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := EspsecureCommands
	EspsecureCommands = []string{filepath.Join(dir, "missing.py"), bin}
	t.Cleanup(func() { EspsecureCommands = old })
	return argsFile
}

func TestSignESPIDF(t *testing.T) {
	argsFile := stubEspsecure(t, "")
	out := filepath.Join(t.TempDir(), "signed.bin")
	if err := SignESPIDF(context.Background(), "testdata/esp_idf_unsigned.bin", "key.pem", out); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	want := "sign_data\n--version\n2\n--keyfile\nkey.pem\n--output\n" + out + "\ntestdata/esp_idf_unsigned.bin\n"
	if string(args) != want {
		t.Errorf("espsecure args:\n%s\nwant:\n%s", args, want)
	}
	signed, _ := os.ReadFile(out)
	if !strings.HasSuffix(string(signed), "SIGNED") {
		t.Error("output not written")
	}
}

func TestSignESPIDFFailures(t *testing.T) {
	stubEspsecure(t, "#!/bin/sh\necho 'A fatal error occurred: key is not RSA' >&2\nexit 2\n")
	err := SignESPIDF(context.Background(), "in.bin", "key.pem", filepath.Join(t.TempDir(), "o"))
	if err == nil || !strings.Contains(err.Error(), "key is not RSA") {
		t.Errorf("want espsecure's message, got %v", err)
	}

	old := EspsecureCommands
	EspsecureCommands = []string{filepath.Join(t.TempDir(), "nope")}
	t.Cleanup(func() { EspsecureCommands = old })
	if err := SignESPIDF(context.Background(), "in.bin", "key.pem", "out"); !errors.Is(err, ErrEspsecureNotFound) {
		t.Errorf("want ErrEspsecureNotFound, got %v", err)
	}
}

func TestReadRAUCMetadata(t *testing.T) {
	meta, _, err := ReadMetadata(RAUC, "testdata/rauc_1.13_signature.raucb")
	if err != nil {
		t.Fatal(err)
	}
	// The UUID is the first 128 bits of the manifest digest RAUC reports as
	// its bundle hash, 65547c8981853d087e73551be4c474011fbde82a…; the server's
	// test pins the same value.
	want := Metadata{
		Product:      "Gateway",
		Version:      "1.4.2",
		Platform:     "acme-gateway",
		Architecture: "aarch64",
		UUID:         "65547c89-8185-3d08-7e73-551be4c47401",
		Description:  "built by rauc 1.13",
	}
	if meta != want {
		t.Errorf("metadata = %+v, want %+v", meta, want)
	}

	// RAUC 1.8 drops [meta.nerveshub] while signing, and the error says so.
	_, _, err = ReadMetadata(RAUC, "testdata/rauc_1.8_signature.raucb")
	if err == nil || !strings.Contains(err.Error(), "[meta.nerveshub]") || !strings.Contains(err.Error(), "1.9") {
		t.Errorf("want a missing-section error naming RAUC 1.9, got %v", err)
	}
}

func TestReadRAUCMetadataBadFooters(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, b := range map[string][]byte{
		"short":     []byte("hsqs"),
		"zero":      append([]byte("hsqs0000"), 0, 0, 0, 0, 0, 0, 0, 0),
		"too large": append([]byte("hsqs"), 0, 0, 0, 0, 0, 0, 1, 0),
		"not cms":   append([]byte("hsqsXXXX"), 0, 0, 0, 0, 0, 0, 0, 4),
	} {
		if _, _, err := ReadRAUCMetadata(write(name, b)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRAUCManifestMapping(t *testing.T) {
	manifest := "[update]\ncompatible=acme-gateway\nversion=2.0.0\n\n" +
		"# a comment\n[meta.nerveshub]\narchitecture=x86_64\nuuid=9F3A1C22-6F1B-4D0E-9B3A-0D2F7C5E8A41\narchitecture=aarch64\n"
	meta, _, err := raucMetadata([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	// product and platform default to compatible, a declared uuid wins, and
	// the last duplicate key wins as in GLib.
	if meta.Product != "acme-gateway" || meta.Platform != "acme-gateway" ||
		meta.UUID != "9f3a1c22-6f1b-4d0e-9b3a-0d2f7c5e8a41" || meta.Architecture != "aarch64" {
		t.Errorf("metadata = %+v", meta)
	}

	for name, m := range map[string]string{
		"no compatible":   "[update]\nversion=1.0.0\n[meta.nerveshub]\narchitecture=a\n",
		"no version":      "[update]\ncompatible=c\n[meta.nerveshub]\narchitecture=a\n",
		"no architecture": "[update]\ncompatible=c\nversion=1.0.0\n[meta.nerveshub]\nproduct=p\n",
		"bad uuid":        "[update]\ncompatible=c\nversion=1.0.0\n[meta.nerveshub]\narchitecture=a\nuuid=nope\n",
	} {
		if _, _, err := raucMetadata([]byte(m)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCMSContentHandlesIndefiniteLengths(t *testing.T) {
	// ContentInfo { signedData, [0] { SignedData { 1, {}, { data,
	// [0] { constructed OCTET STRING of two chunks } } } } }, all BER
	// indefinite-length, as some encoders write it.
	oid := append([]byte{0x06, byte(len(oidSignedData))}, oidSignedData...)
	dataOID := []byte{0x06, 0x09, 0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x07, 0x01}
	eoc := []byte{0, 0}
	octets := append(append([]byte{0x24, 0x80, 0x04, 0x03}, "abc"...), append([]byte{0x04, 0x02}, "de"...)...)
	octets = append(octets, eoc...)
	econtent := append(append([]byte{0xA0, 0x80}, octets...), eoc...)
	eci := append(append(append([]byte{0x30, 0x80}, dataOID...), econtent...), eoc...)
	sd := append(append([]byte{0x30, 0x80, 0x02, 0x01, 0x01, 0x31, 0x00}, eci...), eoc...)
	explicit := append(append([]byte{0xA0, 0x80}, sd...), eoc...)
	ci := append(append(append([]byte{0x30, 0x80}, oid...), explicit...), eoc...)

	got, err := cmsContent(ci)
	if err != nil || string(got) != "abcde" {
		t.Errorf("cmsContent = %q, %v", got, err)
	}

	// Without an eContent the signature is detached: a plain bundle.
	detachedECI := append([]byte{0x30, byte(len(dataOID))}, dataOID...)
	detachedSD := append([]byte{0x30, byte(5 + len(detachedECI)), 0x02, 0x01, 0x01, 0x31, 0x00}, detachedECI...)
	detached := append(append([]byte{0x30, byte(len(oid) + 2 + len(detachedSD))}, oid...), append([]byte{0xA0, byte(len(detachedSD))}, detachedSD...)...)
	if _, err := cmsContent(detached); err == nil || !strings.Contains(err.Error(), "plain") {
		t.Errorf("detached signature: %v", err)
	}
}
