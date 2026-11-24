package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/api"
)

const deviceResponse = `{"data":{
	"identifier":"dev-001",
	"connection_status":"connected",
	"online":"connected",
	"version":"1.2.3",
	"tags":["prod","eu"],
	"description":"front door unit",
	"updates_enabled":true,
	"last_communication":"never",
	"firmware_metadata":{"uuid":"abc-123","version":"1.2.3"},
	"update_mode":"automatic",
	"managed_updates_allowed":true,
	"updates_blocked_until":"2026-01-01T00:00:00Z",
	"deleted":false,
	"deployment_group":{"name":"stable","firmware_version":"1.2.3","is_active":true}
}}`

// neverSeenDeviceResponse is a device that has never connected and was
// deleted, as NervesHubWeb.API.DeviceJSON renders it.
const neverSeenDeviceResponse = `{"data":{
	"identifier":"dev-002",
	"connection_status":"not_seen",
	"online":"not_seen",
	"version":"unknown",
	"tags":null,
	"description":null,
	"updates_enabled":false,
	"update_mode":"off",
	"managed_updates_allowed":false,
	"updates_blocked_until":"2099-01-02T10:00:00Z",
	"last_communication":"never",
	"firmware_metadata":null,
	"deployment_group":{"name":"canary","firmware_version":"2.0.0","is_active":false},
	"deleted":true
}}`

func TestDeviceShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deviceResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "show", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Identifier:", "dev-001", "Status:", "connected", "1.2.3", "prod, eu",
		"stable (firmware 1.2.3)", "never", "Update mode:", "automatic", "Managed updates allowed:", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
	// online repeats connection_status, so it isn't shown twice, and a
	// penalty box that has passed isn't shown.
	for _, unwanted := range []string{"Online:", "Deleted:", "inactive", "Updates blocked until:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("detail output shouldn't have %q, got:\n%s", unwanted, out)
		}
	}
}

func TestDeviceShowNeverSeenDeletedDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(neverSeenDeviceResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "show", "dev-002",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"Deleted:", "never connected", "Update mode:", "off",
		"Managed updates allowed:", "false", "Updates blocked until:", "2099-01-02 10:00:00 UTC",
		"canary (firmware 2.0.0), inactive"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "not_seen") {
		t.Errorf("not_seen should be spelled out, got:\n%s", out)
	}
}

// sleepingDeviceResponse is a device that said it was going into deep sleep
// for five minutes, from a server with device sleep tracking.
const sleepingDeviceResponse = `{"data":{
	"identifier":"dev-003",
	"connection_status":"disconnected",
	"online":"disconnected",
	"sleep":{"state":"asleep","mode":"deep","expected_wake_at":"2026-11-24T10:05:00Z"},
	"version":"1.2.3",
	"updates_enabled":true,
	"last_communication":"2026-11-24T10:00:00Z",
	"deleted":false
}}`

func TestDeviceShowSleeping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sleepingDeviceResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "show", "dev-003",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The status still says disconnected; the sleep row follows it.
	status := strings.Index(out, "disconnected")
	sleep := strings.Index(out, "Sleep:")
	if status < 0 || sleep < status {
		t.Errorf("want a Sleep: row after the disconnected status, got:\n%s", out)
	}
	if !strings.Contains(out, "deep, back at 2026-11-24 10:05:00 UTC") {
		t.Errorf("sleep row missing the wake time, got:\n%s", out)
	}
}

func TestRenderDeviceSleep(t *testing.T) {
	wake := &api.Timestamp{Time: time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC)}
	cases := []struct {
		name  string
		sleep *api.DeviceSleep
		want  string
	}{
		{"asleep", &api.DeviceSleep{State: "asleep", Mode: "deep", ExpectedWakeAt: wake}, "deep, back at 2026-11-24 10:05:00 UTC"},
		{"asleep without time", &api.DeviceSleep{State: "asleep", Mode: "light"}, "light, asleep, no wake time given"},
		{"overdue", &api.DeviceSleep{State: "overdue", Mode: "deep", ExpectedWakeAt: wake}, "deep, overdue: was due back 2026-11-24 10:05:00 UTC"},
		{"going to sleep", &api.DeviceSleep{State: "going_to_sleep", Mode: "deep"}, "deep, going to sleep"},
		{"going to sleep with time", &api.DeviceSleep{State: "going_to_sleep", Mode: "light", ExpectedWakeAt: wake}, "light, going to sleep, back at 2026-11-24 10:05:00 UTC"},
		{"unknown state", &api.DeviceSleep{State: "dozing", Mode: "deep"}, "deep, dozing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			device := &api.Device{Identifier: "dev-003", ConnectionStatus: "disconnected", Sleep: tc.sleep}
			if err := renderDevice(&buf, device); err != nil {
				t.Fatalf("renderDevice: %v", err)
			}
			var line string
			for _, l := range strings.Split(buf.String(), "\n") {
				if strings.HasPrefix(l, "Sleep:") {
					line = strings.TrimSpace(strings.TrimPrefix(l, "Sleep:"))
				}
			}
			if line != tc.want {
				t.Errorf("Sleep: %q, want %q; output:\n%s", line, tc.want, buf.String())
			}
		})
	}

	// No sleep object, or a null one, means no row.
	var buf bytes.Buffer
	if err := renderDevice(&buf, &api.Device{Identifier: "dev-001", ConnectionStatus: "disconnected"}); err != nil {
		t.Fatalf("renderDevice: %v", err)
	}
	if strings.Contains(buf.String(), "Sleep:") {
		t.Errorf("no Sleep: row expected without a sleep object, got:\n%s", buf.String())
	}
}

func TestDeviceShowJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deviceResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"device", "show", "dev-001",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok", "--output", "json",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"identifier": "dev-001"`) {
		t.Errorf("json output expected, got %q", out)
	}
}

// TestDeviceBareShowsHelp ensures `nh device` with no identifier still acts
// as a command group (shows help) rather than erroring.
func TestDeviceBareShowsHelp(t *testing.T) {
	out, err := execCmd(t, "", "device")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "list") || !strings.Contains(out, "Usage:") {
		t.Errorf("bare `device` should show help with subcommands, got:\n%s", out)
	}
}

func TestDeviceShowMissingIdentifier(t *testing.T) {
	_, err := execCmd(t, "",
		"device", "show",
		"--org", "acme", "--product", "thermostat",
		"--token", "tok",
	)
	if err == nil || err.Error() != "Device identifier missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}

func TestDeviceShowMissingProduct(t *testing.T) {
	t.Setenv("NERVES_CLOUD_PRODUCT", "")
	t.Setenv("NERVES_HUB_PRODUCT", "")

	_, err := execCmd(t, "",
		"device", "show", "dev-001",
		"--org", "acme",
		"--uri", "https://example.com", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Errorf("expected missing-product error, got %v", err)
	}
}
