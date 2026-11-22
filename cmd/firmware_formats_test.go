package cmd

import (
	"crypto/ed25519"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/firmware"
	"github.com/nerves-hub/nh/internal/pki"
)

const (
	espUnsigned = "../internal/firmware/testdata/esp_idf_unsigned.bin" // product "my_app"
	espSigned   = "../internal/firmware/testdata/esp_idf_signed_rsa3072.bin"
	raucBundle  = "../internal/firmware/testdata/rauc_1.13_signature.raucb" // product "Gateway"
)

// formatUploadServer accepts a firmware upload as POST
// /api/orgs/acme/products/<product>/firmwares with the image in the
// "firmware" multipart field, as FirmwareController.create/2 reads it.
func formatUploadServer(t *testing.T, product string) (srv *httptest.Server, gotName *string, gotContent *[]byte) {
	t.Helper()
	gotName, gotContent = new(string), new([]byte)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orgs/acme/products/"+product+"/firmwares" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		f, hdr, err := r.FormFile("firmware")
		if err != nil {
			t.Errorf("FormFile(firmware): %v", err)
		} else {
			*gotName = hdr.Filename
			*gotContent, _ = io.ReadAll(f)
			f.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"uuid":"new-uuid","version":"1.0.0","tool":"x"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, gotName, gotContent
}

// testPackbeam is an AtomVM packbeam holding {application, App, [{vsn, Vsn}]}.
func testPackbeam(t *testing.T, app, vsn string) string {
	t.Helper()
	atom := func(s string) []byte { return append([]byte{119, byte(len(s))}, s...) }
	term := []byte{131, 104, 3}
	term = append(term, atom("application")...)
	term = append(term, atom(app)...)
	term = append(term, 108, 0, 0, 0, 1, 104, 2)
	term = append(term, atom("vsn")...)
	term = append(term, 107, 0, byte(len(vsn)))
	term = append(term, vsn...)
	term = append(term, 106)

	data := binary.BigEndian.AppendUint32(nil, uint32(len(term)))
	data = append(data, term...)
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	name := []byte(app + "/priv/application.bin\x00")
	for len(name)%4 != 0 {
		name = append(name, 0)
	}

	b := []byte("#!/usr/bin/env AtomVM\n\x00\x00")
	b = binary.BigEndian.AppendUint32(b, uint32(12+len(name)+len(data)))
	b = binary.BigEndian.AppendUint32(b, 4)
	b = binary.BigEndian.AppendUint32(b, 0)
	b = append(b, name...)
	b = append(b, data...)
	b = append(b, make([]byte, 12)...)
	b = append(b, "end\x00"...)

	p := filepath.Join(t.TempDir(), app+".avm")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// verifyPackbeam checks a signed packbeam the way the server does: the
// Ed25519 signature in the nerves_hub/signature entry covers every byte
// before that entry.
func verifyPackbeam(t *testing.T, archive []byte, pub ed25519.PublicKey) {
	t.Helper()
	// The signature entry is the 104 bytes before the 16 byte terminator.
	if len(archive) < 120 {
		t.Fatalf("archive too short to be signed: %d bytes", len(archive))
	}
	entry := archive[len(archive)-120 : len(archive)-16]
	if binary.BigEndian.Uint32(entry) != 104 || !strings.HasPrefix(string(entry[12:]), "nerves_hub/signature\x00") {
		t.Fatalf("no signature entry before the terminator")
	}
	payload := entry[36:]
	if string(payload[:4]) != "NH1\x01" {
		t.Fatalf("payload = %q", payload[:4])
	}
	if !ed25519.Verify(pub, archive[:len(archive)-120], payload[4:]) {
		t.Fatal("signature does not verify")
	}
}

func noSignedTempFiles(t *testing.T) {
	t.Helper()
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "nh-signed-*")); len(matches) != 0 {
		t.Errorf("signed temp files left behind: %v", matches)
	}
}

func TestFirmwareUploadAtomVMSignsNatively(t *testing.T) {
	// fwup is not needed, and must not be run, for an AtomVM archive.
	old := fwupBin
	fwupBin = filepath.Join(t.TempDir(), "no-such-fwup")
	t.Cleanup(func() { fwupBin = old })

	srv, gotName, gotContent := formatUploadServer(t, "kiosk")
	dataDir := t.TempDir()
	priv := writeSigningKey(t, dataDir, "acme", "ci", "")
	path := testPackbeam(t, "kiosk", "1.2")

	out, err := execCmd(t, "",
		"firmware", "upload", path, "--key", "ci",
		"--org", "acme", "--product", "kiosk",
		"--data-dir", dataDir, "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	verifyPackbeam(t, *gotContent, priv.Public().(ed25519.PublicKey))
	if *gotName != "kiosk.avm" {
		t.Errorf("uploaded filename: %q", *gotName)
	}
	for _, want := range []string{"format:       atomvm", "product:      kiosk", "version:      1.2.0",
		"platform:     atomvm", "Signing kiosk.avm with key ci"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Warning") {
		t.Errorf("no warnings expected:\n%s", out)
	}
	noSignedTempFiles(t)
}

func TestFirmwareUploadAtomVMKeySources(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)

	t.Run("environment", func(t *testing.T) {
		srv, _, gotContent := formatUploadServer(t, "kiosk")
		t.Setenv("NERVES_HUB_PRIVATE_KEY", pki.PrivateKeyString(priv))
		if out, err := execCmd(t, "", "firmware", "upload", testPackbeam(t, "kiosk", "1.0.0"),
			"--org", "acme", "--product", "kiosk", "--uri", srv.URL, "--token", "tok"); err != nil {
			t.Fatalf("Execute: %v\n%s", err, out)
		}
		verifyPackbeam(t, *gotContent, pub)
	})

	t.Run("fwup key file", func(t *testing.T) {
		srv, _, gotContent := formatUploadServer(t, "kiosk")
		keyFile := filepath.Join(t.TempDir(), "fwup-key.priv")
		if err := os.WriteFile(keyFile, []byte(pki.PrivateKeyString(priv)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := execCmd(t, "", "firmware", "upload", testPackbeam(t, "kiosk", "1.0.0"),
			"--key-file", keyFile,
			"--org", "acme", "--product", "kiosk", "--uri", srv.URL, "--token", "tok"); err != nil {
			t.Fatalf("Execute: %v\n%s", err, out)
		}
		verifyPackbeam(t, *gotContent, pub)
	})
}

func TestFirmwareUploadAtomVMReSigns(t *testing.T) {
	srv, _, gotContent := formatUploadServer(t, "kiosk")
	_, oldKey, _ := ed25519.GenerateKey(nil)
	dataDir := t.TempDir()
	priv := writeSigningKey(t, dataDir, "acme", "ci", "")

	path := testPackbeam(t, "kiosk", "1.0.0")
	archive, _ := os.ReadFile(path)
	signed, _, err := firmware.SignAtomVM(archive, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, signed, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "", "firmware", "upload", path, "--key", "ci",
		"--org", "acme", "--product", "kiosk", "--data-dir", dataDir, "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Replaced the signature") {
		t.Errorf("output should say the signature was replaced:\n%s", out)
	}
	if len(*gotContent) != len(signed) {
		t.Errorf("re-signed archive is %d bytes, want %d: signatures must not nest", len(*gotContent), len(signed))
	}
	verifyPackbeam(t, *gotContent, priv.Public().(ed25519.PublicKey))
}

func TestFirmwareUploadAtomVMProductMismatch(t *testing.T) {
	_, err := execCmd(t, "", "firmware", "upload", testPackbeam(t, "kiosk", "1.0.0"), "--skip-signing",
		"--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), `built for product "kiosk"`) ||
		!strings.Contains(err.Error(), ".app.src") {
		t.Errorf("want a product mismatch naming the .app.src, got %v", err)
	}
}

func TestFirmwareUploadAtomVMSkipSigning(t *testing.T) {
	srv, _, gotContent := formatUploadServer(t, "kiosk")
	path := testPackbeam(t, "kiosk", "1.0.0")
	want, _ := os.ReadFile(path)
	if out, err := execCmd(t, "", "firmware", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "kiosk", "--uri", srv.URL, "--token", "tok"); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if string(*gotContent) != string(want) {
		t.Error("--skip-signing should upload the archive untouched")
	}
}

// writeESPKey puts a PEM where `--key <name>` looks for an ESP-IDF key.
func writeESPKey(t *testing.T, dataDir, org, name string) string {
	t.Helper()
	dir := filepath.Join(dataDir, "keys", org)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name+".rsa.pem")
	if err := os.WriteFile(p, []byte("-----BEGIN RSA PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// stubEspsecure replaces espsecure with a script that records its arguments
// and writes its input plus "SIGNED" to --output.
func stubEspsecure(t *testing.T) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + argsFile + `"` + "\n" +
		`while [ $# -gt 1 ]; do [ "$1" = --output ] && out="$2"; shift; done` + "\n" +
		`cat "$1" > "$out"; printf SIGNED >> "$out"` + "\n"
	bin := filepath.Join(dir, "espsecure.py")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := firmware.EspsecureCommands
	firmware.EspsecureCommands = []string{bin}
	t.Cleanup(func() { firmware.EspsecureCommands = old })
	return argsFile
}

func TestFirmwareUploadESPIDFSignsWithEspsecure(t *testing.T) {
	argsFile := stubEspsecure(t)
	srv, gotName, gotContent := formatUploadServer(t, "my_app")
	dataDir := t.TempDir()
	keyPath := writeESPKey(t, dataDir, "acme", "secure-boot")

	out, err := execCmd(t, "", "firmware", "upload", espUnsigned, "--key", "secure-boot",
		"--org", "acme", "--product", "my_app", "--data-dir", dataDir, "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}

	raw, _ := os.ReadFile(argsFile)
	args := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(args) != 8 || args[0] != "sign_data" || args[1] != "--version" || args[2] != "2" ||
		args[3] != "--keyfile" || args[4] != keyPath || args[5] != "--output" || args[7] != espUnsigned {
		t.Errorf("espsecure args: %v", args)
	}
	if !strings.HasSuffix(args[6], ".bin") {
		t.Errorf("signed temp file should be a .bin, got %s", args[6])
	}
	if !strings.HasSuffix(string(*gotContent), "SIGNED") {
		t.Error("the signed image should be uploaded")
	}
	if *gotName != "esp_idf_unsigned.bin" {
		t.Errorf("uploaded filename: %q", *gotName)
	}
	for _, want := range []string{"format:       esp-idf", "product:      my_app", "platform:     esp32s3",
		"architecture: xtensa", "uuid:         abababab-abab-abab-abab-abababababab"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	noSignedTempFiles(t)
}

func TestFirmwareUploadESPIDFKeyFile(t *testing.T) {
	argsFile := stubEspsecure(t)
	srv, _, _ := formatUploadServer(t, "my_app")
	keyFile := filepath.Join(t.TempDir(), "signing_key.pem")
	if err := os.WriteFile(keyFile, []byte("pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := execCmd(t, "", "firmware", "upload", espUnsigned, "--key-file", keyFile,
		"--org", "acme", "--product", "my_app", "--uri", srv.URL, "--token", "tok"); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(raw), "--keyfile\n"+keyFile+"\n") {
		t.Errorf("espsecure should get the key file, got %q", raw)
	}
}

func TestFirmwareUploadESPIDFAlreadySigned(t *testing.T) {
	argsFile := stubEspsecure(t)
	srv, _, gotContent := formatUploadServer(t, "my_app")
	dataDir := t.TempDir()
	writeESPKey(t, dataDir, "acme", "secure-boot")
	want, _ := os.ReadFile(espSigned)

	out, err := execCmd(t, "", "firmware", "upload", espSigned, "--key", "secure-boot",
		"--org", "acme", "--product", "my_app", "--data-dir", dataDir, "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Error("espsecure must not sign an image twice")
	}
	if string(*gotContent) != string(want) {
		t.Error("an already-signed image should be uploaded as is")
	}
	if !strings.Contains(out, "already carries a Secure Boot v2 signature, so it is not signed again with key secure-boot") {
		t.Errorf("output should say why it was not signed:\n%s", out)
	}
}

func TestFirmwareUploadESPIDFErrors(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		// NERVES_HUB_PRIVATE_KEY is an Ed25519 key and cannot sign ESP-IDF.
		t.Setenv("NERVES_HUB_PRIVATE_KEY", "ignored")
		_, err := execCmd(t, "", "firmware", "upload", espUnsigned,
			"--org", "acme", "--product", "my_app", "--uri", "https://example.com", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "RSA-3072") || !strings.Contains(err.Error(), "--skip-signing") ||
			!strings.Contains(err.Error(), "--allow-unsigned-esp-idf") {
			t.Errorf("want guidance on ESP-IDF keys, got %v", err)
		}
	})

	t.Run("missing named key", func(t *testing.T) {
		_, err := execCmd(t, "", "firmware", "upload", espUnsigned, "--key", "ghost",
			"--org", "acme", "--product", "my_app", "--data-dir", t.TempDir(),
			"--uri", "https://example.com", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "ghost.rsa.pem") {
			t.Errorf("want the path that was looked for, got %v", err)
		}
	})

	t.Run("espsecure missing", func(t *testing.T) {
		old := firmware.EspsecureCommands
		firmware.EspsecureCommands = []string{filepath.Join(t.TempDir(), "nope")}
		t.Cleanup(func() { firmware.EspsecureCommands = old })
		dataDir := t.TempDir()
		writeESPKey(t, dataDir, "acme", "sb")

		_, err := execCmd(t, "", "firmware", "upload", espUnsigned, "--key", "sb",
			"--org", "acme", "--product", "my_app", "--data-dir", dataDir,
			"--uri", "https://example.com", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "pip install esptool") || !strings.Contains(err.Error(), "--skip-signing") {
			t.Errorf("want install guidance, got %v", err)
		}
		noSignedTempFiles(t)
	})

	t.Run("password", func(t *testing.T) {
		_, err := execCmd(t, "", "firmware", "upload", espUnsigned, "--key", "sb", "--password", "x",
			"--org", "acme", "--product", "my_app", "--uri", "https://example.com", "--token", "tok")
		if err == nil || !strings.Contains(err.Error(), "--password does not apply") {
			t.Errorf("want a --password refusal, got %v", err)
		}
	})
}

func TestFirmwareUploadRAUCAsIs(t *testing.T) {
	srv, _, gotContent := formatUploadServer(t, "Gateway")
	want, _ := os.ReadFile(raucBundle)

	out, err := execCmd(t, "", "firmware", "upload", raucBundle,
		"--org", "acme", "--product", "Gateway", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if string(*gotContent) != string(want) {
		t.Error("a RAUC bundle should be uploaded untouched")
	}
	for _, want := range []string{"format:       rauc", "product:      Gateway", "version:      1.4.2",
		"architecture: aarch64", "uuid:         65547c89-8185-3d08-7e73-551be4c47401",
		"signed when they are built"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFirmwareUploadRAUCRefusesSigningFlags(t *testing.T) {
	for _, flag := range [][]string{{"--key", "ci"}, {"--key-file", "k.pem"}, {"--password", "x"}} {
		args := append([]string{"firmware", "upload", raucBundle}, flag...)
		args = append(args, "--org", "acme", "--product", "Gateway", "--uri", "https://example.com", "--token", "tok")
		_, err := execCmd(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "signed when they are built") {
			t.Errorf("%v: want a RAUC signing refusal, got %v", flag, err)
		}
	}
}

func TestFirmwareUploadRAUCProductMismatch(t *testing.T) {
	_, err := execCmd(t, "", "firmware", "upload", raucBundle,
		"--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), `built for product "Gateway"`) ||
		!strings.Contains(err.Error(), "[meta.nerveshub] product") {
		t.Errorf("want a product mismatch naming the manifest, got %v", err)
	}
}

func TestFirmwareUploadUnknownFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("not firmware"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execCmd(t, "", "firmware", "upload", path,
		"--org", "acme", "--product", "thermostat", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "unrecognised firmware format") ||
		!strings.Contains(err.Error(), "atomvm (.avm)") {
		t.Errorf("want the supported formats listed, got %v", err)
	}
}

// With --skip-signing nh has nothing to do to the file, so a format it does
// not know goes to the server, which may accept formats added after this nh.
func TestFirmwareUploadUnknownFormatSkipSigning(t *testing.T) {
	srv, _, gotContent := formatUploadServer(t, "thermostat")
	path := filepath.Join(t.TempDir(), "new.img")
	if err := os.WriteFile(path, []byte("a future format"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCmd(t, "", "firmware", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if string(*gotContent) != "a future format" {
		t.Errorf("uploaded %q", *gotContent)
	}
	if !strings.Contains(out, "Warning:") || !strings.Contains(out, "for the server to judge") {
		t.Errorf("should warn that the format is unknown:\n%s", out)
	}
}

func TestFirmwareUploadKeyFileRefusesPublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	keyFile := filepath.Join(t.TempDir(), "fwup-key.pub")
	if err := os.WriteFile(keyFile, []byte(pki.PublicKeyString(pub)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execCmd(t, "", "firmware", "upload", testPackbeam(t, "kiosk", "1.0.0"), "--key-file", keyFile,
		"--org", "acme", "--product", "kiosk", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "public key") || !strings.Contains(err.Error(), "fwup-key.priv") {
		t.Errorf("want a public-key refusal, got %v", err)
	}
}

func TestFirmwareUploadESPIDFAlreadySignedChecksTheKey(t *testing.T) {
	stubEspsecure(t)
	_, err := execCmd(t, "", "firmware", "upload", espSigned, "--key-file", filepath.Join(t.TempDir(), "missing.pem"),
		"--org", "acme", "--product", "my_app", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "signing key file") {
		t.Errorf("a key that was asked for should still be checked, got %v", err)
	}
}

func TestFirmwareUploadKeyAndKeyFileConflict(t *testing.T) {
	_, err := execCmd(t, "", "firmware", "upload", testPackbeam(t, "kiosk", "1.0.0"),
		"--key", "ci", "--key-file", "k",
		"--org", "acme", "--product", "kiosk", "--uri", "https://example.com", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "only one of --key or --key-file") {
		t.Errorf("want a conflict error, got %v", err)
	}
}

// The server explains a refused upload in errors.detail
// (NervesHubWeb.API.FallbackController); that text must reach the user, with
// what to do about it.
// An image already signed, uploaded with no key at all, goes as is.
func TestFirmwareUploadESPIDFAlreadySignedNoKey(t *testing.T) {
	srv, _, _ := formatUploadServer(t, "my_app")
	out, err := execCmd(t, "", "firmware", "upload", espSigned,
		"--org", "acme", "--product", "my_app", "--uri", srv.URL, "--token", "tok")
	if err != nil || !strings.Contains(out, "uploading it as is") {
		t.Errorf("Execute: %v\n%s", err, out)
	}
}

func TestFirmwareUploadServerRefusalsCarryHints(t *testing.T) {
	cases := []struct {
		name, detail, path, product string
		want                        []string
	}{
		{
			name:    "format not allowed",
			detail:  `The product \"my_app\" does not accept esp-idf firmware. An organization owner can enable it in the product's settings.`,
			path:    espSigned,
			product: "my_app",
			want:    []string{"does not accept esp-idf firmware", "nh product update my_app --allow-tool esp-idf"},
		},
		{
			name:    "unsigned esp-idf",
			detail:  "This ESP-IDF image is not signed. Sign it with `espsecure.py sign_data --version 2` and register the matching public key against your organization, or allow unsigned images in this product's settings.",
			path:    espUnsigned,
			product: "my_app",
			want:    []string{"This ESP-IDF image is not signed", "nh product update my_app --allow-unsigned-esp-idf"},
		},
		{
			name:    "unsigned atomvm",
			detail:  "This ESP-IDF image is not signed. Sign it with `espsecure.py sign_data --version 2` and register the matching public key against your organization, or allow unsigned images in this product's settings.",
			path:    "", // a packbeam, built below
			product: "kiosk",
			want:    []string{"is not signed", "nh product update kiosk --allow-unsigned-atomvm"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"errors":{"detail":"` + c.detail + `"}}`))
			}))
			t.Cleanup(srv.Close)
			path := c.path
			if path == "" {
				path = testPackbeam(t, "kiosk", "1.0.0")
			}
			_, err := execCmd(t, "", "firmware", "upload", path, "--skip-signing",
				"--org", "acme", "--product", c.product, "--uri", srv.URL, "--token", "tok")
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q, got:\n%v", want, err)
				}
			}
		})
	}
}
