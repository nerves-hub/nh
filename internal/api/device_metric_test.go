package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The request and response shapes follow DeviceMetricController and
// DeviceMetricJSON on the server.
func TestListDeviceMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method: got %s", r.Method)
		}
		// The identifier is escaped as one path segment.
		if got := r.URL.EscapedPath(); got != "/api/orgs/acme/products/thermostat/devices/dev%201/metrics" {
			t.Errorf("path: got %q", got)
		}
		q := r.URL.Query()
		want := map[string]string{
			"key":    "cpu_temp,mem_used_percent",
			"since":  "2026-08-16T09:00:00Z",
			"before": "2026-08-16T10:00:00Z",
			"limit":  "250",
			"order":  "asc",
		}
		for k, v := range want {
			if got := q.Get(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
		  {"timestamp":"2026-08-16T09:14:00.123456Z","firmware_uuid":"8f6c3a1e-2b7d-4c55-9e0a-1d2f3b4c5a6e",
		   "metrics":{"cpu_temp":41.2,"mem_used_percent":38.0}},
		  {"timestamp":"2026-08-16T09:13:00.004210Z","firmware_uuid":null,"metrics":{"cpu_temp":41}}
		]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	reports, err := c.ListDeviceMetrics(context.Background(), "acme", "thermostat", "dev 1", DeviceMetricsFilter{
		// Blank entries are dropped, so a stray comma sends no empty name.
		Keys:   []string{" cpu_temp", "", "mem_used_percent "},
		Since:  "2026-08-16T09:00:00Z",
		Before: "2026-08-16T10:00:00Z",
		Limit:  250,
		Order:  "asc",
	})
	if err != nil {
		t.Fatalf("ListDeviceMetrics: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	first := reports[0]
	wantTS := time.Date(2026, 8, 16, 9, 14, 0, 123456000, time.UTC)
	if !first.Timestamp.Equal(wantTS) {
		t.Errorf("timestamp: got %s", first.Timestamp)
	}
	if first.FirmwareUUID == nil || *first.FirmwareUUID != "8f6c3a1e-2b7d-4c55-9e0a-1d2f3b4c5a6e" {
		t.Errorf("firmware_uuid: got %v", first.FirmwareUUID)
	}
	if first.Metrics["cpu_temp"] != 41.2 || first.Metrics["mem_used_percent"] != 38 {
		t.Errorf("metrics: got %v", first.Metrics)
	}
	if reports[1].FirmwareUUID != nil {
		t.Errorf("null firmware_uuid should decode as nil, got %q", *reports[1].FirmwareUUID)
	}
}

// Unset filters are not sent, so the server applies its own defaults.
func TestListDeviceMetricsOmitsUnsetFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("no query expected, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	reports, err := newTestClient(t, srv).ListDeviceMetrics(context.Background(), "acme", "thermostat", "dev1", DeviceMetricsFilter{Keys: []string{" ", ""}})
	if err != nil {
		t.Fatalf("ListDeviceMetrics: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none", len(reports))
	}
}

// A server with no analytics database answers 501 with an explanation, which
// must reach the user rather than read as an empty history.
func TestListDeviceMetricsAnalyticsDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"errors":{"detail":"This platform has no analytics database configured, so device metric history is not stored."}}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).ListDeviceMetrics(context.Background(), "acme", "thermostat", "dev1", DeviceMetricsFilter{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotImplemented {
		t.Fatalf("want a 501 *APIError, got %v", err)
	}
	if !strings.Contains(err.Error(), "no analytics database") {
		t.Errorf("error should carry the server's reason, got %v", err)
	}
}

func TestListDeviceMetricsRequiresScope(t *testing.T) {
	c, err := NewClient("http://example.invalid", "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ org, product, id, want string }{
		{"", "p", "d", "org"},
		{"o", "", "d", "product"},
		{"o", "p", "", "identifier"},
	} {
		if _, err := c.ListDeviceMetrics(context.Background(), tc.org, tc.product, tc.id, DeviceMetricsFilter{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: want an error naming %s, got %v", tc, tc.want, err)
		}
	}
}
