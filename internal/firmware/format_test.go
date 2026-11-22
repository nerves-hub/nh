package firmware

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectBytes(t *testing.T) {
	esp, err := os.ReadFile("testdata/esp_idf_unsigned.bin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		head []byte
		want Format
	}{
		{"fwup zip", []byte("PK\x03\x04rest"), Fwup},
		{"esp-idf image", esp, ESPIDF},
		{"atomvm packbeam", append([]byte("#!/usr/bin/env AtomVM\n\x00\x00"), 0, 0, 0, 0), AtomVM},
		{"rauc bundle", []byte("hsqs\x00\x00"), RAUC},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DetectBytes(c.head)
			if err != nil || got != c.want {
				t.Errorf("DetectBytes = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestDetectRejectsUnknownFormats(t *testing.T) {
	for name, head := range map[string][]byte{
		"empty":   nil,
		"text":    []byte("FWDATA"),
		"short":   []byte("PK"),
		"shebang": []byte("#!/usr/bin/env AtomVM\n"), // magic without its NUL padding
		// 0xE9 alone is not enough: the app descriptor magic must follow at 0x20.
		"esp magic only": append([]byte{0xE9}, make([]byte, 400)...),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DetectBytes(head)
			if !errors.Is(err, ErrUnknownFormat) {
				t.Fatalf("want ErrUnknownFormat, got %v", err)
			}
			for _, want := range []string{"fwup (.fw)", "esp-idf (.bin)", "atomvm (.avm)", "rauc (.raucb)"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should list %s, got %q", want, err)
				}
			}
		})
	}
}

func TestDetectFile(t *testing.T) {
	got, err := Detect("testdata/rauc_1.13_signature.raucb")
	if err != nil || got != RAUC {
		t.Errorf("Detect(rauc) = %q, %v", got, err)
	}

	// A file shorter than the ESP-IDF header is read, not refused.
	short := filepath.Join(t.TempDir(), "short.fw")
	if err := os.WriteFile(short, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Detect(short); err != nil || got != Fwup {
		t.Errorf("Detect(short fwup) = %q, %v", got, err)
	}

	if _, err := Detect(filepath.Join(t.TempDir(), "missing")); !os.IsNotExist(err) {
		t.Errorf("Detect(missing) should report the file error, got %v", err)
	}
}

func TestExtensions(t *testing.T) {
	want := map[Format]string{Fwup: ".fw", ESPIDF: ".bin", AtomVM: ".avm", RAUC: ".raucb"}
	for f, ext := range want {
		if f.Extension() != ext {
			t.Errorf("%s.Extension() = %q, want %q", f, f.Extension(), ext)
		}
	}
}

func TestNormaliseVersions(t *testing.T) {
	for raw, want := range map[string]string{
		"1.2.3":      "1.2.3",
		"v1.2.3":     "1.2.3",
		"1.2":        "1.2.0",
		"1.2.3-rc.1": "1.2.3-rc.1",
		"1.2.3.4":    "",
		"1":          "",
		"main":       "",
		" 2.0.0 ":    "2.0.0",
	} {
		got, ok := normaliseAtomVMVersion(raw)
		if (want == "") == ok || got != want {
			t.Errorf("AtomVM %q = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for raw, want := range map[string]string{
		"1.2.3":      "1.2.3",
		"v1.2":       "1.2.0",
		"1":          "1.0.0",
		"1.2.3.4":    "1.2.3+4",
		"1.2.3-rc.1": "1.2.3-rc.1",
		"v5.2-dirty": "",
	} {
		got, ok := normaliseESPIDFVersion(raw)
		if (want == "") == ok || got != want {
			t.Errorf("ESP-IDF %q = %q, %v; want %q", raw, got, ok, want)
		}
	}
}
