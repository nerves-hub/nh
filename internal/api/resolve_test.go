package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		path  string
		query url.Values
		want  string
	}{
		{"plain path", "https://hub.example.com", "/orgs/acme/products", nil, "https://hub.example.com/api/orgs/acme/products"},
		{"escaped slash kept", "https://hub.example.com", "/orgs/" + url.PathEscape("a/b") + "/products", nil, "https://hub.example.com/api/orgs/a%2Fb/products"},
		{"escaped space and percent", "https://hub.example.com", "/orgs/" + url.PathEscape("a b%c"), nil, "https://hub.example.com/api/orgs/a%20b%25c"},
		{"base with path prefix", "https://example.com/hub/", "/orgs/" + url.PathEscape("x/y"), nil, "https://example.com/hub/api/orgs/x%2Fy"},
		{"base already has api", "https://example.com/api", "/orgs", nil, "https://example.com/api/orgs"},
		{"escaped base path", "https://example.com/my%2Fhub", "/orgs", nil, "https://example.com/my%2Fhub/api/orgs"},
		{"duplicate slashes collapsed", "https://hub.example.com/", "/orgs//acme/", nil, "https://hub.example.com/api/orgs/acme"},
		{"query merged", "https://hub.example.com", "/orgs?page=2", url.Values{"limit": {"10"}}, "https://hub.example.com/api/orgs?limit=10&page=2"},
		{"base query kept", "https://hub.example.com?x=1", "/orgs/" + url.PathEscape("a/b") + "?page=2", nil, "https://hub.example.com/api/orgs/a%2Fb?page=2&x=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewClient(tt.base, "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.resolve(tt.path, tt.query)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolve(%q):\n got %s\nwant %s", tt.path, got, tt.want)
			}
		})
	}
}

// An identifier holding "/" must reach the server as one escaped segment, not
// as two path segments that hit a different route.
func TestGetKeepsEscapedSlashOnTheWire(t *testing.T) {
	var gotRawPath, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath, gotPath = r.URL.EscapedPath(), r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Get(context.Background(), "/orgs/"+url.PathEscape("a/b")+"/products", nil, nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotRawPath != "/api/orgs/a%2Fb/products" {
		t.Errorf("escaped path: want /api/orgs/a%%2Fb/products, got %q", gotRawPath)
	}
	if gotPath != "/api/orgs/a/b/products" {
		t.Errorf("decoded path: got %q", gotPath)
	}
}
