package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testArchiveJSON is an archive as NervesHubWeb.API.ArchiveJSON renders it.
const testArchiveJSON = `{"uuid":"uuid-1","version":"1.0.0","platform":"rpi0","architecture":"arm",` +
	`"author":null,"description":"Supporting files","misc":null,"vcs_identifier":"a1b2c3d",` +
	`"size":1572864,"inserted_at":"2026-01-15T09:34:27"}`

// archiveAPIServer answers GET requests on path with body, and fails the test
// for any other request.
func archiveAPIServer(t *testing.T, path, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != path {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestArchiveList(t *testing.T) {
	srv := archiveAPIServer(t, "/api/orgs/acme/products/thermostat/archives", `{"data":[`+testArchiveJSON+`]}`)

	out, err := execCmd(t, "",
		"archive", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"VERSION", "SIZE", "1.0.0", "rpi0", "arm", "1.5 MiB", "uuid-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestArchiveListJSON(t *testing.T) {
	srv := archiveAPIServer(t, "/api/orgs/acme/products/thermostat/archives", `{"data":[`+testArchiveJSON+`]}`)

	out, err := execCmd(t, "",
		"archive", "list", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["uuid"] != "uuid-1" || got[0]["size"] != float64(1572864) {
		t.Errorf("JSON output: %v", got)
	}
}

func TestArchiveListEmpty(t *testing.T) {
	srv := archiveAPIServer(t, "/api/orgs/acme/products/thermostat/archives", `{"data":[]}`)

	out, err := execCmd(t, "",
		"archive", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No archives found in acme/thermostat.") {
		t.Errorf("output: %q", out)
	}
}

func TestArchiveListRequiresProduct(t *testing.T) {
	_, err := execCmd(t, "",
		"archive", "list",
		"--org", "acme",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "product") {
		t.Errorf("expected a missing-product error, got %v", err)
	}
}

func TestArchiveShow(t *testing.T) {
	srv := archiveAPIServer(t, "/api/orgs/acme/products/thermostat/archives/uuid-1", `{"data":`+testArchiveJSON+`}`)

	out, err := execCmd(t, "",
		"archive", "show", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"uuid-1", "1.0.0", "rpi0", "arm", "1.5 MiB (1572864 bytes)", "Supporting files", "a1b2c3d", "2026-01-15 09:34:27"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Null fields are left out rather than shown empty.
	if strings.Contains(out, "Author:") || strings.Contains(out, "Misc:") {
		t.Errorf("null fields should be omitted:\n%s", out)
	}
}

func TestArchiveShowArgs(t *testing.T) {
	if _, err := execCmd(t, "", "archive", "show"); err == nil || !strings.Contains(err.Error(), "Archive UUID missing") {
		t.Errorf("no args: %v", err)
	}
	if _, err := execCmd(t, "", "archive", "show", "a", "b"); err == nil || !strings.Contains(err.Error(), "too many arguments") {
		t.Errorf("two args: %v", err)
	}
}

func TestArchiveSize(t *testing.T) {
	cases := map[int64]string{
		0:                      "0 B",
		1023:                   "1023 B",
		1024:                   "1.0 KiB",
		1572864:                "1.5 MiB",
		1048575:                "1.0 MiB",
		1048524:                "1023.9 KiB",
		5 * 1024 * 1024 * 1024: "5.0 GiB",
	}
	for n, want := range cases {
		if got := archiveSize(n); got != want {
			t.Errorf("archiveSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// archiveDownloadServer serves the download endpoint the way
// ArchiveController.download does: a 302 to the archive's storage URL. The
// file itself is served from that URL. A status other than 200 is returned by
// the API endpoint instead.
func archiveDownloadServer(t *testing.T, body string, status int) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case status != http.StatusOK:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
		case r.URL.Path == "/storage/archives/uuid-1.fw":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, body)
		default:
			http.Redirect(w, r, "/storage/archives/uuid-1.fw?sig=abc", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestArchiveDownloadToFile(t *testing.T) {
	srv, paths := archiveDownloadServer(t, "ARCHIVEDATA", http.StatusOK)

	dest := filepath.Join(t.TempDir(), "extras.fw")
	out, err := execCmd(t, "",
		"archive", "download", "uuid-1",
		"--file", dest,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"/api/orgs/acme/products/thermostat/archives/uuid-1/download", "/storage/archives/uuid-1.fw"}
	if strings.Join(*paths, " ") != strings.Join(want, " ") {
		t.Errorf("requests: got %v, want %v", *paths, want)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(data) != "ARCHIVEDATA" {
		t.Errorf("file content: got %q", data)
	}
	if info, err := os.Stat(dest); err != nil {
		t.Errorf("stat: %v", err)
	} else if info.Mode().Perm() != 0o644 {
		t.Errorf("saved file mode: %v, want 0644", info.Mode().Perm())
	}
	if !strings.Contains(out, "Downloaded archive to "+dest) {
		t.Errorf("output: %q", out)
	}
	if strings.Contains(out, "Downloading") {
		t.Errorf("no progress should be drawn without a TTY, got %q", out)
	}
}

func TestArchiveDownloadDefaultName(t *testing.T) {
	srv, _ := archiveDownloadServer(t, "ARCHIVEDATA", http.StatusOK)
	t.Chdir(t.TempDir())

	if _, err := execCmd(t, "",
		"archive", "download", "uuid-1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile("uuid-1.fw")
	if err != nil {
		t.Fatalf("expected uuid-1.fw in cwd: %v", err)
	}
	if string(data) != "ARCHIVEDATA" {
		t.Errorf("file content: got %q", data)
	}
}

func TestArchiveDownloadStdout(t *testing.T) {
	srv, _ := archiveDownloadServer(t, "ARCHIVEDATA", http.StatusOK)

	out, err := execCmd(t, "",
		"archive", "download", "uuid-1",
		"--file", "-",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "ARCHIVEDATA" {
		t.Errorf("stdout should be the raw body only, got %q", out)
	}
}

func TestArchiveDownloadErrorLeavesNoFile(t *testing.T) {
	srv, _ := archiveDownloadServer(t, "", http.StatusNotFound)

	dir := t.TempDir()
	dest := filepath.Join(dir, "extras.fw")
	_, err := execCmd(t, "",
		"archive", "download", "ghost",
		"--file", dest,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("no file should be left on failure, found %v", entries)
	}
}
