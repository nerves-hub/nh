package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/spf13/cobra"
)

func testClient(t *testing.T, url string) *api.Client {
	t.Helper()
	c, err := api.NewClient(url, "tok")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestEnsureIrohEndpointRegistered_AlreadyRegistered(t *testing.T) {
	const id = "1d5649b6e2e9b98ee0e589845424cdebb5fc9a24ac7f7b4ece9aff2aef4b4448"
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
		}
		w.Header().Set("Content-Type", "application/json")
		// The list already contains our endpoint id.
		_, _ = w.Write([]byte(`{"data":[{"identifier":"` + id + `","service":"iroh","instance":"default","owner":{"type":"user"}}]}`))
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	cmd.SetErr(io.Discard)
	if err := ensureIrohEndpointRegistered(context.Background(), cmd, testClient(t, srv.URL), "acme", id); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if posted {
		t.Error("must not register an endpoint that already exists")
	}
}

func TestEnsureIrohEndpointRegistered_RegistersWhenMissing(t *testing.T) {
	const id = "1d5649b6e2e9b98ee0e589845424cdebb5fc9a24ac7f7b4ece9aff2aef4b4448"
	var postBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/iroh_endpoints"):
			_, _ = w.Write([]byte(`{"data":[]}`)) // not registered yet
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me"):
			_, _ = w.Write([]byte(`{"data":{"name":"Alex","email":"alex@example.com"}}`))
		case r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &postBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"identifier":"` + id + `","owner":{"type":"user"}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errOut)
	if err := ensureIrohEndpointRegistered(context.Background(), cmd, testClient(t, srv.URL), "acme", id); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if postBody["identifier"] != id {
		t.Errorf("registered identifier = %v, want %s", postBody["identifier"], id)
	}
	if postBody["user_email"] != "alex@example.com" {
		t.Errorf("registered user_email = %v, want alex@example.com", postBody["user_email"])
	}
	if !strings.Contains(errOut.String(), "Registered this machine's iroh endpoint") {
		t.Errorf("expected a registration notice, got %q", errOut.String())
	}
}

func TestDeviceIrohTicket(t *testing.T) {
	const ticket = "endpoint-example-not-a-real-ticket"
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"` + ticket + `","service":"iroh","instance":"iroh_console"}]}`))
	}))
	defer srv.Close()

	got, err := deviceIrohTicket(context.Background(), testClient(t, srv.URL), "acme", "thermostat", "dev1", "iroh_console")
	if err != nil {
		t.Fatalf("ticket: %v", err)
	}
	if got != ticket {
		t.Errorf("ticket = %q, want %q", got, ticket)
	}
	if !strings.Contains(gotQuery, "service=iroh") || !strings.Contains(gotQuery, "instance=iroh_console") {
		t.Errorf("query missing service/instance filters: %q", gotQuery)
	}
}

func TestDeviceIrohTicketMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := deviceIrohTicket(context.Background(), testClient(t, srv.URL), "acme", "thermostat", "dev1", "iroh_console")
	if err == nil || !strings.Contains(err.Error(), "has not reported an iroh console endpoint") {
		t.Fatalf("expected missing-endpoint error, got %v", err)
	}
}
