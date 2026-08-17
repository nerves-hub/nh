package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareUpload(t *testing.T) {
	var gotMethod, gotPath, gotCT, gotName, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")

		f, hdr, err := r.FormFile("firmware")
		if err != nil {
			t.Errorf("FormFile(firmware): %v", err)
		} else {
			gotName = hdr.Filename
			b, _ := io.ReadAll(f)
			gotContent = string(b)
			f.Close()
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"uuid":"new-uuid","version":"1.4.0"}}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(path, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCmd(t, "",
		"firmware", "upload", path, "--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/firmwares" {
		t.Errorf("path: got %q", gotPath)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("content-type: got %q", gotCT)
	}
	if gotName != "image.fw" {
		t.Errorf("uploaded filename: got %q", gotName)
	}
	if gotContent != "FWDATA" {
		t.Errorf("uploaded content: got %q", gotContent)
	}
	if !strings.Contains(out, "1.4.0") || !strings.Contains(out, "new-uuid") {
		t.Errorf("output should report the created firmware, got %q", out)
	}
	// stderr is a buffer here, not a TTY, so no progress bar should render.
	if strings.Contains(out, "Uploading") {
		t.Errorf("no progress should be drawn without a TTY, got %q", out)
	}
}

func TestFirmwareUploadMissingFile(t *testing.T) {
	_, err := execCmd(t, "",
		"firmware", "upload", filepath.Join(t.TempDir(), "nope.fw"),
		"--org", "acme", "--product", "thermostat",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected error for a missing file")
	}
}

func TestFirmwareUploadMissingPathArg(t *testing.T) {
	_, err := execCmd(t, "",
		"firmware", "upload",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Firmware file path missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
