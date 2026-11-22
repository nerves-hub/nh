package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// deviceJSON is a device as NervesHubWeb.API.DeviceJSON renders one that has
// never connected, was soft deleted and is in a deployment group.
const deviceJSON = `{"data":{
	"identifier":"dev-001",
	"description":null,
	"tags":["prod","eu"],
	"online":"not_seen",
	"connection_status":"not_seen",
	"firmware_metadata":null,
	"version":"unknown",
	"deployment_group":{"firmware_uuid":"fw-1","firmware_version":"1.2.3","is_active":true,"name":"stable"},
	"updates_enabled":true,
	"update_mode":"device_managed",
	"managed_updates_allowed":true,
	"updates_blocked_until":"2026-11-22T10:00:00Z",
	"org_name":"acme",
	"product_name":"thermostat",
	"last_communication":"never",
	"deleted":true
}}`

func TestCreateDevicePostsToDevicesCollection(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(deviceJSON))
	}))
	defer srv.Close()

	desc, code, allowed := "edge unit", "IO.puts(:hi)", false
	in := DeviceInput{
		Description:           &desc,
		Tags:                  []string{"prod", "eu"},
		UpdateMode:            "off",
		ManagedUpdatesAllowed: &allowed,
		ConnectingCode:        &code,
	}
	if _, err := newTestClient(t, srv).CreateDevice(context.Background(), "acme", "thermostat", "dev-001", in); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	// The router has post("/", DeviceController, :create) under
	// /orgs/:org/products/:product/devices; there is no POST to a device.
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/devices" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	want := map[string]any{
		"identifier":              "dev-001",
		"description":             "edge unit",
		"tags":                    "prod,eu",
		"update_mode":             "off",
		"managed_updates_allowed": false,
		"connecting_code":         "IO.puts(:hi)",
	}
	dev := gotBody["device"]
	if len(gotBody) != 1 || len(dev) != len(want) {
		t.Fatalf("body: want only device with %d fields, got %+v", len(want), gotBody)
	}
	for k, v := range want {
		if dev[k] != v {
			t.Errorf("device.%s: want %v, got %v", k, v, dev[k])
		}
	}
}

// DeviceController.update answers 201, not 200.
func TestUpdateDeviceAccepts201(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(deviceJSON))
	}))
	defer srv.Close()

	empty := ""
	device, err := newTestClient(t, srv).UpdateDevice(context.Background(), "acme", "thermostat", "dev-001",
		DeviceInput{Description: &empty})
	if err != nil {
		t.Fatalf("UpdateDevice: %v", err)
	}
	if device.Identifier != "dev-001" {
		t.Errorf("device: %+v", device)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/acme/products/thermostat/devices/dev-001" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// An empty description is sent, so that it clears the description; the
	// fields that weren't set are left out.
	dev := gotBody["device"]
	if len(dev) != 1 || dev["description"] != "" {
		t.Errorf("body: want only an empty description, got %+v", gotBody)
	}
}

func TestDeviceDecodesDeviceJSON(t *testing.T) {
	var resp struct {
		Data Device `json:"data"`
	}
	if err := json.Unmarshal([]byte(deviceJSON), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	d := resp.Data
	if d.ConnectionStatus != "not_seen" || d.Online != "not_seen" {
		t.Errorf("status: connection_status %q, online %q", d.ConnectionStatus, d.Online)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "prod" {
		t.Errorf("tags: %v", d.Tags)
	}
	if d.UpdateMode != "device_managed" || !d.UpdatesEnabled {
		t.Errorf("update mode: %q, updates enabled %t", d.UpdateMode, d.UpdatesEnabled)
	}
	if d.ManagedUpdatesAllowed == nil || !*d.ManagedUpdatesAllowed {
		t.Errorf("managed_updates_allowed: %v", d.ManagedUpdatesAllowed)
	}
	if d.LastCommunication == nil || !d.LastCommunication.IsZero() {
		t.Errorf(`last_communication "never" should decode as the zero time, got %v`, d.LastCommunication)
	}
	if d.UpdatesBlockedUntil == nil || d.UpdatesBlockedUntil.IsZero() {
		t.Errorf("updates_blocked_until: %v", d.UpdatesBlockedUntil)
	}
	if !d.Deleted {
		t.Error("deleted should be true")
	}
	if dg := d.DeploymentGroup; dg == nil || dg.Name != "stable" || dg.FirmwareVersion != "1.2.3" || !dg.IsActive {
		t.Errorf("deployment group: %+v", dg)
	}
}

// A device from a server that doesn't send managed_updates_allowed decodes
// it as nil, not false.
func TestDeviceManagedUpdatesAllowedAbsent(t *testing.T) {
	var d Device
	if err := json.Unmarshal([]byte(`{"identifier":"dev-001"}`), &d); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if d.ManagedUpdatesAllowed != nil {
		t.Errorf("want nil, got %v", *d.ManagedUpdatesAllowed)
	}
}
