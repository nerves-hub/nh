package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerves-hub/nh/internal/api"
)

func testClient(t *testing.T, url string) *api.Client {
	t.Helper()
	c, err := api.NewClient(url, "tok")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestDeviceIrohAddr(t *testing.T) {
	// What IrohConsole reports: the 64-hex endpoint id as identifier, where to
	// reach it in details.
	const id = "1d5649b6e2e9b98ee0e589845424cdebb5fc9a24ac7f7b4ece9aff2aef4b4448"
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"` + id + `","service":"iroh","instance":"iroh_console",` +
			`"details":{"relay_urls":["https://relay.example.com"]}}]}`))
	}))
	defer srv.Close()

	got, err := deviceIrohAddr(context.Background(), testClient(t, srv.URL), "acme", "thermostat", "dev1", "iroh_console")
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if got.EndpointID() != id {
		t.Errorf("endpoint id = %q, want %q", got.EndpointID(), id)
	}
	if !strings.Contains(gotQuery, "service=iroh") || !strings.Contains(gotQuery, "instance=iroh_console") {
		t.Errorf("query missing service/instance filters: %q", gotQuery)
	}
}

func TestDeviceIrohAddrUndialable(t *testing.T) {
	// An id with nothing saying where it is cannot be dialled; say so rather
	// than claim the device reported nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"1d5649b6e2e9b98ee0e589845424cdebb5fc9a24ac7f7b4ece9aff2aef4b4448","service":"iroh","instance":"iroh_console"}]}`))
	}))
	defer srv.Close()

	_, err := deviceIrohAddr(context.Background(), testClient(t, srv.URL), "acme", "thermostat", "dev1", "iroh_console")
	if err == nil || !strings.Contains(err.Error(), "cannot be dialled") || !strings.Contains(err.Error(), "no ticket or relay URL") {
		t.Fatalf("expected an undialable-endpoint error, got %v", err)
	}
}

func TestDeviceIrohAddrMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	_, err := deviceIrohAddr(context.Background(), testClient(t, srv.URL), "acme", "thermostat", "dev1", "iroh_console")
	if err == nil || !strings.Contains(err.Error(), "has not reported an iroh console endpoint") {
		t.Fatalf("expected missing-endpoint error, got %v", err)
	}
}
