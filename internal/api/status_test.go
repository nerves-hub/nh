package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://hub.example.com":             "https://hub.example.com/status/alive",
		"https://hub.example.com/":            "https://hub.example.com/status/alive",
		"https://hub.example.com/api":         "https://hub.example.com/status/alive",
		"https://hub.example.com/api/":        "https://hub.example.com/status/alive",
		"https://hub.example.com/hub/api":     "https://hub.example.com/hub/status/alive",
		"https://hub.example.com/rapi":        "https://hub.example.com/rapi/status/alive",
		"https://hub.example.com:8443/?x=1#y": "https://hub.example.com:8443/status/alive",
	} {
		c, err := NewClient(base, "")
		if err != nil {
			t.Fatalf("NewClient(%q): %v", base, err)
		}
		if got := c.rootURL(statusPath); got != want {
			t.Errorf("rootURL(%q): got %q, want %q", base, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Sorry, Friend :(\n"))
	}))
	defer srv.Close()

	// Even a client holding a token doesn't send it to the health check.
	status, err := newTestClient(t, srv).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/status/alive" || gotAuth != "" {
		t.Errorf("request: %s %s (Authorization %q)", gotMethod, gotPath, gotAuth)
	}
	if status.StatusCode != http.StatusInternalServerError || status.Message != "Sorry, Friend :(" || status.Healthy() {
		t.Errorf("status: %+v", status)
	}
}

func TestStatusUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := newTestClient(t, srv)
	srv.Close()

	if _, err := c.Status(context.Background()); err == nil {
		t.Error("expected an error for an unreachable server")
	}
}
