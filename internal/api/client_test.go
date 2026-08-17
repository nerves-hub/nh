package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// newTestClient returns a client pointed at srv with a fixed token.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(srv.URL, "tok-123", WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("", "tok"); err == nil {
		t.Error("empty base URL should error")
	}
	if _, err := NewClient("not-absolute/path", "tok"); err == nil {
		t.Error("relative base URL should error")
	}
}

func TestGetResolvesPrefixAuthAndQuery(t *testing.T) {
	var gotPath, gotAuth, gotUA, gotAccept, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"acme"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	var out struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	q := url.Values{"limit": {"10"}}
	if err := c.Get(context.Background(), "/orgs", q, &out); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if gotPath != "/api/orgs" {
		t.Errorf("path: want /api/orgs, got %q", gotPath)
	}
	if gotQuery != "limit=10" {
		t.Errorf("query: want limit=10, got %q", gotQuery)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth: want %q, got %q", "Bearer tok-123", gotAuth)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("user-agent: want %q, got %q", DefaultUserAgent, gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("accept: got %q", gotAccept)
	}
	if out.Data.Name != "acme" {
		t.Errorf("decoded name: got %q", out.Data.Name)
	}
}

func TestPostEncodesBody(t *testing.T) {
	var gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	var out struct {
		ID int `json:"id"`
	}
	in := map[string]string{"name": "device-1"}
	if err := c.Post(context.Background(), "/devices", in, &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type: got %q", gotCT)
	}
	if gotBody != `{"name":"device-1"}` {
		t.Errorf("body: got %q", gotBody)
	}
	if out.ID != 7 {
		t.Errorf("decoded id: got %d", out.ID)
	}
}

func TestErrorResponseFieldErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":{"name":["can't be blank"],"identifier":["is invalid"]}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.Post(context.Background(), "/devices", map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d", apiErr.StatusCode)
	}
	if got := apiErr.FieldErrors["name"]; len(got) != 1 || got[0] != "can't be blank" {
		t.Errorf("name field errors: got %v", got)
	}
	if msg := apiErr.Error(); msg == "" {
		t.Error("Error() should be non-empty")
	}
}

func TestErrorResponseDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.Get(context.Background(), "/orgs/nope", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T (%v)", err, err)
	}
	if apiErr.Message != "Not Found" {
		t.Errorf("message: got %q", apiErr.Message)
	}
}

func TestDeleteNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Delete(context.Background(), "/devices/1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestGetRawProgress(t *testing.T) {
	body := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set Content-Length explicitly, as real file servers do; bodies this
		// large would otherwise be sent chunked with no length.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	var dst strings.Builder
	var lastRead, lastTotal int64
	err := c.GetRaw(context.Background(), "/firmware", nil, &dst, func(read, total int64) {
		lastRead, lastTotal = read, total
	})
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if dst.String() != body {
		t.Errorf("body: got %d bytes, want %d", dst.Len(), len(body))
	}
	if lastRead != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Errorf("final progress: read=%d total=%d, want %d/%d", lastRead, lastTotal, len(body), len(body))
	}
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Get(ctx, "/orgs", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}
