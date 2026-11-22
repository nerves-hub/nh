package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// archiveJSON is an archive as NervesHubWeb.API.ArchiveJSON renders it,
// including the nulls its nullable fields can carry.
const archiveJSON = `{
	"uuid": "d9f8c63a-1234-5678-abcd-ef0123456789",
	"version": "1.0.0",
	"platform": "rpi0",
	"architecture": "arm",
	"author": null,
	"description": "Supporting files",
	"misc": null,
	"vcs_identifier": "a1b2c3d",
	"size": 1048576,
	"inserted_at": "2026-01-15T09:34:27"
}`

func archiveClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok-123", WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListArchives(t *testing.T) {
	c := archiveClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/orgs/acme/products/thermostat/archives" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("Authorization: %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[`+archiveJSON+`]}`)
	})

	archives, err := c.ListArchives(context.Background(), "acme", "thermostat")
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("got %d archives", len(archives))
	}
	a := archives[0]
	if a.UUID != "d9f8c63a-1234-5678-abcd-ef0123456789" || a.Version != "1.0.0" ||
		a.Platform != "rpi0" || a.Architecture != "arm" || a.Author != "" ||
		a.Description != "Supporting files" || a.VCSIdentifier != "a1b2c3d" ||
		a.Size != 1048576 || a.InsertedAt.IsZero() {
		t.Errorf("decoded archive: %+v", a)
	}
}

func TestGetArchive(t *testing.T) {
	c := archiveClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/orgs/acme/products/thermostat/archives/uuid-1" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":`+archiveJSON+`}`)
	})

	a, err := c.GetArchive(context.Background(), "acme", "thermostat", "uuid-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != "1.0.0" {
		t.Errorf("decoded archive: %+v", a)
	}
}

func TestGetArchiveNotFound(t *testing.T) {
	c := archiveClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"errors":{"detail":"Not Found"}}`)
	})

	_, err := c.GetArchive(context.Background(), "acme", "thermostat", "ghost")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a 404 APIError, got %v", err)
	}
}

func TestArchiveRequiresScope(t *testing.T) {
	c, err := NewClient("https://example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.ListArchives(ctx, "", "p"); err == nil || !strings.Contains(err.Error(), "org is required") {
		t.Errorf("ListArchives without org: %v", err)
	}
	if _, err := c.GetArchive(ctx, "o", "", "u"); err == nil || !strings.Contains(err.Error(), "product is required") {
		t.Errorf("GetArchive without product: %v", err)
	}
	if _, err := c.GetArchive(ctx, "o", "p", ""); err == nil || !strings.Contains(err.Error(), "uuid is required") {
		t.Errorf("GetArchive without uuid: %v", err)
	}
	if err := c.DownloadArchive(ctx, "o", "p", "", io.Discard, nil); err == nil || !strings.Contains(err.Error(), "uuid is required") {
		t.Errorf("DownloadArchive without uuid: %v", err)
	}
	if _, err := c.UploadArchive(ctx, "", "p", "a.fw", strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "org is required") {
		t.Errorf("UploadArchive without org: %v", err)
	}
}

func TestUploadArchiveMultipart(t *testing.T) {
	c := archiveClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orgs/acme/products/thermostat/archives" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Errorf("Content-Type: %q", r.Header.Get("Content-Type"))
		}
		// ArchiveController.create pops the file from the "archive" field.
		f, hdr, err := r.FormFile("archive")
		if err != nil {
			t.Errorf("no archive field: %v", err)
		} else {
			b, _ := io.ReadAll(f)
			if string(b) != "ARCHIVE" || hdr.Filename != "extras.fw" {
				t.Errorf("uploaded %q as %q", b, hdr.Filename)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"data":`+archiveJSON+`}`)
	})

	a, err := c.UploadArchive(context.Background(), "acme", "thermostat", "/some/dir/extras.fw", strings.NewReader("ARCHIVE"))
	if err != nil {
		t.Fatal(err)
	}
	if a.UUID != "d9f8c63a-1234-5678-abcd-ef0123456789" {
		t.Errorf("decoded archive: %+v", a)
	}
}

func TestUploadArchiveRejected(t *testing.T) {
	c := archiveClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"errors":{"detail":"The signature does not match any of this organization's signing keys, or the file is corrupt."}}`)
	})

	_, err := c.UploadArchive(context.Background(), "acme", "thermostat", "extras.fw", strings.NewReader("ARCHIVE"))
	if err == nil || !strings.Contains(err.Error(), "signature does not match") {
		t.Fatalf("expected the server's message, got %v", err)
	}
}

// The download endpoint redirects to a signed storage URL. The client follows
// it, and does not send the API token to the storage host.
func TestDownloadArchiveFollowsRedirectWithoutToken(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("storage host received Authorization %q", got)
		}
		if r.URL.Path != "/archives/1/2/uuid-1.fw" || r.URL.Query().Get("sig") != "abc" {
			t.Errorf("storage request: %s", r.URL)
		}
		_, _ = io.WriteString(w, "ARCHIVEDATA")
	}))
	t.Cleanup(storage.Close)

	// Address the storage server as storage.test, a different domain from the
	// API server's 127.0.0.1 as a real storage bucket would be. The client's
	// dialer sends that name to the storage listener.
	storageAddr := storage.Listener.Addr().String()
	storageURL := "http://storage.test/archives/1/2/uuid-1.fw?sig=abc"

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/orgs/acme/products/thermostat/archives/uuid-1/download" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("API host Authorization: %q", got)
		}
		http.Redirect(w, r, storageURL, http.StatusFound)
	}))
	t.Cleanup(api.Close)

	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "storage.test:80" {
				addr = storageAddr
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	c, err := NewClient(api.URL, "tok-123", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	var lastRead, lastTotal int64
	err = c.DownloadArchive(context.Background(), "acme", "thermostat", "uuid-1", &buf, func(read, total int64) {
		lastRead, lastTotal = read, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "ARCHIVEDATA" {
		t.Errorf("body: %q", buf.String())
	}
	if lastRead != int64(len("ARCHIVEDATA")) || lastTotal != int64(len("ARCHIVEDATA")) {
		t.Errorf("progress: read=%d total=%d", lastRead, lastTotal)
	}
}
