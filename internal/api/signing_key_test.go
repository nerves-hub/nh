package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSigningKeyScheme(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orgs/acme/keys" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"name":"esp","key":"PEM","scheme":"secure_boot_v2_rsa"}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	got, err := c.CreateSigningKeyWithScheme(context.Background(), "acme", "esp", "secure_boot_v2_rsa", "PEM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Scheme != "secure_boot_v2_rsa" || got.Name != "esp" || got.Key != "PEM" {
		t.Errorf("decoded: %+v", got)
	}
	if _, err := c.CreateSigningKey(context.Background(), "acme", "ci", "abc="); err != nil {
		t.Fatal(err)
	}

	if bodies[0]["scheme"] != "secure_boot_v2_rsa" || bodies[0]["name"] != "esp" || bodies[0]["key"] != "PEM" {
		t.Errorf("scheme request body: %v", bodies[0])
	}
	// CreateSigningKey sends exactly what it did before schemes: no scheme
	// field, so the server's ed25519 default applies.
	if _, ok := bodies[1]["scheme"]; ok || len(bodies[1]) != 2 {
		t.Errorf("ed25519 request body: %v", bodies[1])
	}
}

func TestSigningKeyDecodesMissingScheme(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"a","key":"k"},{"name":"b","key":"PEM","scheme":"x509_certificate"}]}`))
	}))
	defer srv.Close()
	keys, err := newTestClient(t, srv).ListSigningKeys(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].Scheme != "" || keys[1].Scheme != "x509_certificate" {
		t.Errorf("keys: %+v", keys)
	}
}
