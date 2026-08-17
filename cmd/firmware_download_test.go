package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func firmwareServer(t *testing.T, body string, status int) (*httptest.Server, *string) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotPath
}

func TestFirmwareDownloadToFile(t *testing.T) {
	srv, gotPath := firmwareServer(t, "FWDATA", http.StatusOK)

	dest := filepath.Join(t.TempDir(), "image.fw")
	out, err := execCmd(t, "",
		"firmware", "download", "uuid-1",
		"--file", dest,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if *gotPath != "/api/orgs/acme/products/thermostat/firmwares/uuid-1/download" {
		t.Errorf("path: got %q", *gotPath)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(data) != "FWDATA" {
		t.Errorf("file content: got %q", string(data))
	}
	if !strings.Contains(out, "Downloaded firmware to "+dest) {
		t.Errorf("output: %q", out)
	}
	// stderr is a buffer here, not a TTY, so no progress bar should render.
	if strings.Contains(out, "Downloading") {
		t.Errorf("no progress should be drawn without a TTY, got %q", out)
	}
}

func TestFirmwareDownloadDefaultName(t *testing.T) {
	srv, _ := firmwareServer(t, "FWDATA", http.StatusOK)

	// Default name <uuid>.fw is written into the working directory.
	t.Chdir(t.TempDir())

	if _, err := execCmd(t, "",
		"firmware", "download", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile("uuid-1.fw")
	if err != nil {
		t.Fatalf("expected uuid-1.fw in cwd: %v", err)
	}
	if string(data) != "FWDATA" {
		t.Errorf("file content: got %q", string(data))
	}
}

func TestFirmwareDownloadStdout(t *testing.T) {
	srv, _ := firmwareServer(t, "FWDATA", http.StatusOK)

	out, err := execCmd(t, "",
		"firmware", "download", "uuid-1",
		"--file", "-",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "FWDATA" {
		t.Errorf("stdout should be the raw body only, got %q", out)
	}
}

func TestFirmwareDownloadErrorLeavesNoFile(t *testing.T) {
	srv, _ := firmwareServer(t, "", http.StatusNotFound)

	dest := filepath.Join(t.TempDir(), "image.fw")
	_, err := execCmd(t, "",
		"firmware", "download", "ghost",
		"--file", dest,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("no file should be written on failure, stat err = %v", statErr)
	}
}
