package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListOrgMembersDecodesRoles(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// The shape OrgUserJSON renders: role is null for a custom-role member.
		_, _ = w.Write([]byte(`{"data":[
			{"name":"Alice","email":"alice@example.com","role":"admin","custom_role":null},
			{"name":"Carol","email":"carol@example.com","role":null,"custom_role":"deployer"},
			{"name":"Dan","email":"dan@example.com","role":null,"custom_role":null}
		]}`))
	}))
	defer srv.Close()

	members, err := newTestClient(t, srv).ListOrgMembers(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListOrgMembers: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/orgs/acme/users" {
		t.Errorf("request: got %s %s", gotMethod, gotPath)
	}
	if len(members) != 3 {
		t.Fatalf("want 3 members, got %d", len(members))
	}

	tests := []struct {
		email      string
		wantName   string
		wantCustom bool
	}{
		{"alice@example.com", "admin", false},
		{"carol@example.com", "deployer", true},
		{"dan@example.com", "", false},
	}
	for i, tt := range tests {
		m := members[i]
		if m.Email != tt.email {
			t.Fatalf("member %d: got email %q, want %q", i, m.Email, tt.email)
		}
		name, custom := m.EffectiveRole()
		if name != tt.wantName || custom != tt.wantCustom {
			t.Errorf("%s: EffectiveRole() = (%q, %v), want (%q, %v)", tt.email, name, custom, tt.wantName, tt.wantCustom)
		}
	}
	if members[1].Role != nil {
		t.Errorf("null role should decode to nil, got %q", *members[1].Role)
	}
}

func TestGetOrgDecodesProductNames(t *testing.T) {
	var gotInclude string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotInclude = r.URL.Query().Get("include")
		w.Header().Set("Content-Type", "application/json")
		// ProductJSON.product/1 renders products without an id.
		_, _ = w.Write([]byte(`{"data":[{"name":"acme","products":[{"name":"thermostat","allowed_update_tools":["fwup"]}]}]}`))
	}))
	defer srv.Close()

	org, err := newTestClient(t, srv).GetOrg(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	if gotInclude != "products" {
		t.Errorf("include: got %q", gotInclude)
	}
	if len(org.Products) != 1 || org.Products[0].Name != "thermostat" {
		t.Errorf("products: got %+v", org.Products)
	}
}
