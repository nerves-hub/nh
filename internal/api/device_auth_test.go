package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupDeviceByCertificate(t *testing.T) {
	certPEM := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")

	var gotMethod, gotPath, gotCT string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"identifier":"SN1","product_name":"other","org_name":"acme"}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	device, err := c.LookupDeviceByCertificate(context.Background(), "acme", "thermostat", certPEM)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices/auth" || gotCT != "application/json" {
		t.Errorf("request: %s %s (%s)", gotMethod, gotPath, gotCT)
	}
	// DevicesController.auth reads %{"certificate" => cert64} and
	// Base.decode64s it to the PEM.
	if gotBody["certificate"] != base64.StdEncoding.EncodeToString(certPEM) || len(gotBody) != 1 {
		t.Errorf("body: %v", gotBody)
	}
	if device.Identifier != "SN1" || device.ProductName != "other" {
		t.Errorf("device: %+v", device)
	}
}

func TestLookupDeviceByCertificateNotRecognized(t *testing.T) {
	status, body := http.StatusForbidden, `{"status":"Unauthorized"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	// The 403 the controller sends for every failed match.
	if _, err := c.LookupDeviceByCertificate(context.Background(), "acme", "p", []byte("pem")); !errors.Is(err, ErrCertificateNotRecognized) {
		t.Errorf("want ErrCertificateNotRecognized, got %v", err)
	}

	// Not being a member of the org is a 401 from the permission plug, and
	// stays an API error.
	status, body = http.StatusUnauthorized, `{"errors":{"detail":"Unauthorized"}}`
	_, err := c.LookupDeviceByCertificate(context.Background(), "acme", "p", []byte("pem"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || errors.Is(err, ErrCertificateNotRecognized) {
		t.Errorf("want a 401 APIError, got %v", err)
	}

	for _, args := range [][3]string{{"", "p", "x"}, {"o", "", "x"}, {"o", "p", ""}} {
		if _, err := c.LookupDeviceByCertificate(context.Background(), args[0], args[1], []byte(args[2])); err == nil {
			t.Errorf("%q: want a validation error", args)
		}
	}
}
