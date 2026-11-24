package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestUpdateProduct(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"my product","allowed_update_tools":["fwup","rauc"],"require_unique_firmware_version":false}}`))
	}))
	defer srv.Close()

	no := false
	got, err := newTestClient(t, srv).UpdateProduct(context.Background(), "acme", "my product", ProductUpdate{
		AllowedUpdateTools:           []string{"fwup", "rauc"},
		RequireUniqueFirmwareVersion: &no,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/acme/products/my%20product" {
		t.Errorf("request: want PUT /api/orgs/acme/products/my%%20product, got %s %s", gotMethod, gotPath)
	}
	// Top-level fields, no "product" wrapper, nothing that was not set.
	want := map[string]any{
		"allowed_update_tools":            []any{"fwup", "rauc"},
		"require_unique_firmware_version": false,
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body: want %v, got %v", want, gotBody)
	}
	if got.Name != "my product" || !reflect.DeepEqual(got.AllowedUpdateTools, []string{"fwup", "rauc"}) ||
		got.RequireUniqueFirmwareVersion == nil || *got.RequireUniqueFirmwareVersion {
		t.Errorf("decoded product: %+v", got)
	}
	if got.AllowUnsignedESPIDFFirmware != nil {
		t.Errorf("unreported setting should stay nil, got %v", *got.AllowUnsignedESPIDFFirmware)
	}
}

func TestUpdateProductRequiresSomething(t *testing.T) {
	c, err := NewClient("https://example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateProduct(context.Background(), "acme", "p", ProductUpdate{}); err == nil {
		t.Error("want an error for an empty update")
	}
}

func TestUpdateProductKeepsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		// Product.validate_allowed_update_tools/1 on an unbranded instance.
		_, _ = w.Write([]byte(`{"errors":{"allowed_update_tools":["not enabled on NervesHub: atomvm"]}}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).UpdateProduct(context.Background(), "acme", "p", ProductUpdate{
		AllowedUpdateTools: []string{"fwup", "atomvm"},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want a wrapped 422 *APIError, got %v", err)
	}
	if !strings.Contains(err.Error(), "ATOMVM_FIRMWARE_ENABLED=true") {
		t.Errorf("error should name the instance setting: %v", err)
	}
}

func TestWithFwup(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, []string{"fwup"}},
		{[]string{"atomvm"}, []string{"fwup", "atomvm"}},
		{[]string{" rauc ", "", "fwup", "rauc"}, []string{"fwup", "rauc"}},
	}
	for _, tc := range cases {
		if got := WithFwup(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("WithFwup(%q): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestChangeUpdateTools(t *testing.T) {
	cases := []struct {
		name                     string
		current, allow, disallow []string
		want                     []string
		wantErr                  string
	}{
		{name: "unreported list is fwup", allow: []string{"esp-idf"}, want: []string{"fwup", "esp-idf"}},
		{name: "keeps order", current: []string{"fwup", "rauc"}, allow: []string{"atomvm", "rauc"}, want: []string{"fwup", "rauc", "atomvm"}},
		{name: "removes", current: []string{"fwup", "esp-idf", "atomvm"}, disallow: []string{"esp-idf"}, want: []string{"fwup", "atomvm"}},
		{name: "removing an absent tool is a no-op", current: []string{"fwup"}, disallow: []string{"rauc"}, want: []string{"fwup"}},
		{name: "restores fwup", current: []string{"esp-idf"}, want: []string{"fwup", "esp-idf"}},
		{name: "fwup cannot go", disallow: []string{"fwup"}, wantErr: "fwup cannot be disallowed"},
		{name: "contradiction", allow: []string{"rauc"}, disallow: []string{" rauc"}, wantErr: "both allowed and disallowed"},
		{name: "typo in disallow", current: []string{"fwup", "esp-idf"}, disallow: []string{"esp_idf"}, wantErr: `unknown update tool "esp_idf"`},
		{name: "disallow a tool newer than the CLI", current: []string{"fwup", "zephyr"}, disallow: []string{"zephyr"}, want: []string{"fwup"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChangeUpdateTools(tc.current, tc.allow, tc.disallow)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("want error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ChangeUpdateTools: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// The server names the instance by its configured friendly platform name
// (NervesHub.Branding.friendly_platform_name/0), so the hint must not depend
// on which name that is.
func TestNotEnabledTools(t *testing.T) {
	cases := []struct {
		msg            string
		instance, list string
		ok             bool
	}{
		{"not enabled on NervesHub: rauc", "NervesHub", "rauc", true},
		{"not enabled on NervesCloud: esp-idf, rauc", "NervesCloud", "esp-idf, rauc", true},
		{"not enabled on Acme: Fleet: atomvm", "Acme: Fleet", "atomvm", true},
		{"unknown update tool(s): swupdate", "", "", false},
		{"not enabled on NervesHub", "", "", false},
	}
	for _, tc := range cases {
		instance, list, ok := notEnabledTools(tc.msg)
		if instance != tc.instance || list != tc.list || ok != tc.ok {
			t.Errorf("notEnabledTools(%q) = %q, %q, %v; want %q, %q, %v",
				tc.msg, instance, list, ok, tc.instance, tc.list, tc.ok)
		}
	}
}
