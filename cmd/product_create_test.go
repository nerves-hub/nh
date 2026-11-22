package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductCreate(t *testing.T) {
	var gotMethod, gotPath, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotName = body.Name
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"doorbell","allowed_update_tools":["fwup"],"require_unique_firmware_version":true}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "", "product", "create", "doorbell", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/api/orgs/acme/products" {
		t.Errorf("path: want /api/orgs/acme/products, got %q", gotPath)
	}
	if gotName != "doorbell" {
		t.Errorf("request body name: want doorbell, got %q", gotName)
	}
	if !strings.Contains(out, "Created product doorbell in acme") {
		t.Errorf("output: %q", out)
	}
}

// ProductController.create reads Map.take(params, ["name"]): the name is a
// top-level field. The published OpenAPI spec documents a {"product":{...}}
// wrapper, which the server would drop and then reject as a missing name.
func TestProductCreateSendsTopLevelName(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("content type: want application/json, got %q", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"name":"doorbell"}}`))
	}))
	defer srv.Close()

	if _, err := execCmd(t, "", "product", "create", "doorbell", "--org", "acme", "--uri", srv.URL, "--token", "tok"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(body) != 1 || body["name"] != "doorbell" {
		t.Errorf(`body: want exactly {"name":"doorbell"}, got %v`, body)
	}
}

func TestProductCreateRejectsWhitespace(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	out, err := execCmd(t, "", "product", "create", "my product", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil {
		t.Fatalf("expected error for whitespace in name, output: %q", out)
	}
	if !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("error should mention whitespace, got %v", err)
	}
	if called {
		t.Error("no HTTP request should be made when the name is invalid")
	}
}

func TestProductCreateMissingName(t *testing.T) {
	_, err := execCmd(t, "", "product", "create", "--org", "acme", "--token", "tok")
	if err == nil {
		t.Fatal("expected error when no name is given")
	}
	if err.Error() != "Product name missing" {
		t.Errorf("want friendly message, got %q", err.Error())
	}
}

func TestProductCreateMissingOrg(t *testing.T) {
	t.Setenv("NERVES_CLOUD_ORG", "")
	t.Setenv("NERVES_HUB_ORG", "")

	_, err := execCmd(t, "", "product", "create", "doorbell", "--uri", "https://example.com", "--token", "tok")
	if err == nil {
		t.Fatal("expected error when no org is set")
	}
	if !strings.Contains(err.Error(), "no organization set") {
		t.Errorf("error should mention the missing org, got %v", err)
	}
}
