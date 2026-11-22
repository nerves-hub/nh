package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateCACertificateJITP(t *testing.T) {
	var gotMethod, gotPath string
	var body map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"serial":"255","description":"ca","not_before":"2026-01-02T00:00:00Z","not_after":"2027-01-02T00:00:00Z",
			"jitp":{"product_name":"sensor","tags":["prod"],"description":"Field unit"}}}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).CreateCACertificate(context.Background(), "acme", CACertificateInput{
		Cert:             []byte("CERT"),
		VerificationCert: []byte("VERIFY"),
		Description:      "ca",
		JITP:             &CACertificateJITP{ProductName: "sensor", Tags: []string{"prod"}, Description: "Field unit"},
	})
	if err != nil {
		t.Fatalf("CreateCACertificate: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/ca_certificates" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// CACertificateController.create reads cert, verification_cert,
	// description and jitp.
	want := map[string]string{
		"cert":              `"` + base64.StdEncoding.EncodeToString([]byte("CERT")) + `"`,
		"verification_cert": `"` + base64.StdEncoding.EncodeToString([]byte("VERIFY")) + `"`,
		"description":       `"ca"`,
		"jitp":              `{"product_name":"sensor","tags":["prod"],"description":"Field unit"}`,
	}
	if len(body) != len(want) {
		t.Errorf("body has %d fields, want %d: %v", len(body), len(want), body)
	}
	for k, v := range want {
		if string(body[k]) != v {
			t.Errorf("body[%q]: got %s, want %s", k, body[k], v)
		}
	}
	if got.JITP == nil || got.JITP.ProductName != "sensor" || len(got.JITP.Tags) != 1 || got.JITP.Description != "Field unit" {
		t.Errorf("jitp: %+v", got.JITP)
	}
}

func TestCreateCACertificateWithoutJITP(t *testing.T) {
	var body map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"serial":"255","jitp":null}}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).CreateCACertificate(context.Background(), "acme", CACertificateInput{
		Cert: []byte("CERT"), VerificationCert: []byte("VERIFY"),
	})
	if err != nil {
		t.Fatalf("CreateCACertificate: %v", err)
	}
	if _, ok := body["jitp"]; ok {
		t.Errorf("jitp should be left out, body: %v", body)
	}
	if _, ok := body["description"]; ok {
		t.Errorf("an empty description should be left out, body: %v", body)
	}
	if got.JITP != nil {
		t.Errorf("jitp: got %+v, want nil", got.JITP)
	}
}

func TestCreateCACertificateJITPNeedsProduct(t *testing.T) {
	c, _ := NewClient("https://hub.example.com", "tok")
	_, err := c.CreateCACertificate(context.Background(), "acme", CACertificateInput{
		Cert: []byte("CERT"), VerificationCert: []byte("VERIFY"),
		JITP: &CACertificateJITP{Tags: []string{"prod"}},
	})
	if err == nil {
		t.Error("expected an error for a JITP profile without a product")
	}
}
