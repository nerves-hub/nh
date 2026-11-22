package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// webhookJSON is a webhook as NervesHubWeb.API.WebhookJSON renders it.
const webhookJSON = `{
	"id": 12,
	"url": "https://hooks.example.com/nerves-hub",
	"description": "Fleet dashboard",
	"events": ["device.connected", "device.disconnected"],
	"all_products": false,
	"products": ["thermostat"],
	"enabled": true,
	"disabled_reason": null,
	"failing_since": null,
	"last_error": null,
	"last_error_at": null,
	"inserted_at": "2026-10-13T09:00:00",
	"updated_at": "2026-10-13T09:00:00"
}`

// webhookWithSecretJSON is the with_secret/1 render: the webhook plus its
// signing secret.
var webhookWithSecretJSON = strings.TrimSuffix(strings.TrimSpace(webhookJSON), "}") +
	`, "secret": "whsec_c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0"}`

// webhookRequest is what a fake server saw.
type webhookRequest struct {
	method string
	path   string
	body   map[string]any
	raw    string
}

// webhookServer answers every request with status and body, recording the
// request.
func webhookServer(t *testing.T, status int, body string) (*httptest.Server, *webhookRequest) {
	t.Helper()
	got := &webhookRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		got.raw = string(raw)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &got.body); err != nil {
				t.Errorf("request body is not JSON: %v: %s", err, raw)
			}
		}
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func (got *webhookRequest) expect(t *testing.T, method, path string) {
	t.Helper()
	if got.method != method || got.path != path {
		t.Errorf("request: got %s %s, want %s %s", got.method, got.path, method, path)
	}
}

func TestWebhookList(t *testing.T) {
	failing := strings.Replace(webhookJSON, `"id": 12`, `"id": 13`, 1)
	failing = strings.Replace(failing, `"failing_since": null`, `"failing_since": "2026-10-14T10:00:00Z"`, 1)
	off := strings.Replace(webhookJSON, `"id": 12`, `"id": 14`, 1)
	off = strings.Replace(off, `"enabled": true`, `"enabled": false`, 1)
	off = strings.Replace(off, `"disabled_reason": null`, `"disabled_reason": "failing for 3 days"`, 1)
	off = strings.Replace(off, `"all_products": false`, `"all_products": true`, 1)
	off = strings.Replace(off, `"products": ["thermostat"]`, `"products": []`, 1)
	srv, got := webhookServer(t, 200, `{"data":[`+webhookJSON+`,`+failing+`,`+off+`]}`)

	out, err := execCmd(t, "", "webhook", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodGet, "/api/orgs/acme/webhooks")
	for _, want := range []string{
		"ID", "URL", "STATE", "PRODUCTS", "EVENTS",
		"https://hooks.example.com/nerves-hub", "thermostat", "Fleet dashboard",
		"failing", "off (failing for 3 days)", "all",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "whsec_") {
		t.Errorf("list must not print a secret:\n%s", out)
	}
}

func TestWebhookListEmpty(t *testing.T) {
	srv, _ := webhookServer(t, 200, `{"data":[]}`)
	out, err := execCmd(t, "", "webhook", "list", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No webhooks found in acme") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestWebhookListJSON(t *testing.T) {
	srv, _ := webhookServer(t, 200, `{"data":[`+webhookJSON+`]}`)
	out, err := execCmd(t, "", "webhook", "list", "-o", "json", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var hooks []map[string]any
	if err := json.Unmarshal([]byte(out), &hooks); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out)
	}
	if len(hooks) != 1 || hooks[0]["id"] != float64(12) || hooks[0]["all_products"] != false {
		t.Errorf("unexpected JSON: %s", out)
	}
	if _, ok := hooks[0]["secret"]; ok {
		t.Errorf("list JSON must not carry a secret key: %s", out)
	}
}

func TestWebhookShow(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookJSON+`}`)
	out, err := execCmd(t, "", "webhook", "show", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodGet, "/api/orgs/acme/webhooks/12")
	for _, want := range []string{
		"ID:", "12", "URL:", "https://hooks.example.com/nerves-hub", "Description:", "Fleet dashboard",
		"State:", "on", "Events:", "device.connected, device.disconnected", "Products:", "thermostat",
		"Created:", "2026-10-13 09:00:00 UTC",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q, got:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Signing secret", "Failing since", "Last error"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("show output should not contain %q, got:\n%s", unwanted, out)
		}
	}
}

func TestWebhookShowRejectsNonNumericID(t *testing.T) {
	_, err := execCmd(t, "", "webhook", "show", "abc", "--org", "acme", "--uri", "http://127.0.0.1:1", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("expected an invalid id error, got %v", err)
	}
}

func TestWebhookShowNotFound(t *testing.T) {
	srv, _ := webhookServer(t, 404, `{"errors":{"detail":"Not Found"}}`)
	_, err := execCmd(t, "", "webhook", "show", "99", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a 404 error, got %v", err)
	}
}

func TestWebhookCreate(t *testing.T) {
	srv, got := webhookServer(t, 201, `{"data":`+webhookWithSecretJSON+`}`)
	out, err := execCmd(t, "",
		"webhook", "create",
		"--url", " https://hooks.example.com/nerves-hub ",
		"--events", "device.connected, device.disconnected",
		"--products", "thermostat",
		"--description", "Fleet dashboard",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodPost, "/api/orgs/acme/webhooks")
	want := map[string]any{
		"url":         "https://hooks.example.com/nerves-hub",
		"description": "Fleet dashboard",
		"events":      []any{"device.connected", "device.disconnected"},
		"products":    []any{"thermostat"},
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body: got %v, want %v", got.body, want)
	}
	for _, s := range []string{
		"Created webhook 12 in acme",
		"Signing secret:", "whsec_c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0",
		"won't show it again", "nh webhook regenerate-secret 12",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("create output missing %q, got:\n%s", s, out)
		}
	}
}

func TestWebhookCreateAllFlags(t *testing.T) {
	srv, got := webhookServer(t, 201, `{"data":`+webhookWithSecretJSON+`}`)
	_, err := execCmd(t, "",
		"webhook", "create",
		"--url", "https://hooks.example.com/x",
		"--events", "device.connected", "--events", "error_group.created",
		"--all-products", "--enabled=false",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[string]any{
		"url":          "https://hooks.example.com/x",
		"events":       []any{"device.connected", "error_group.created"},
		"all_products": true,
		"enabled":      false,
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body: got %v, want %v", got.body, want)
	}
}

func TestWebhookCreateJSONIncludesSecret(t *testing.T) {
	srv, _ := webhookServer(t, 201, `{"data":`+webhookWithSecretJSON+`}`)
	out, err := execCmd(t, "",
		"webhook", "create", "-o", "json",
		"--url", "https://hooks.example.com/nerves-hub", "--events", "device.connected",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var hook map[string]any
	if err := json.Unmarshal([]byte(out), &hook); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if hook["secret"] != "whsec_c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0" {
		t.Errorf("JSON output should carry the secret, got %s", out)
	}
}

func TestWebhookCreateValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no url", []string{"--events", "device.connected"}, "--url is required"},
		{"no events", []string{"--url", "https://x.example"}, "--events is required"},
		{"empty events", []string{"--url", "https://x.example", "--events", " , "}, "at least one event"},
		{"conflicting products", []string{"--url", "https://x.example", "--events", "device.connected",
			"--all-products", "--products", "thermostat"}, "can't be used together"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"webhook", "create"}, tc.args...)
			args = append(args, "--org", "acme", "--uri", "http://127.0.0.1:1", "--token", "tok")
			_, err := execCmd(t, "", args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestWebhookCreateFieldErrors(t *testing.T) {
	srv, _ := webhookServer(t, 422, `{"errors":{"events":["contains an event that does not exist"]}}`)
	_, err := execCmd(t, "",
		"webhook", "create", "--url", "https://x.example", "--events", "device.exploded",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "contains an event that does not exist") {
		t.Fatalf("expected the field error, got %v", err)
	}
}

func TestWebhookUpdateSendsOnlyGivenFlags(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookJSON+`}`)
	out, err := execCmd(t, "",
		"webhook", "update", "12", "--enabled", "--description", "Lab bench",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodPut, "/api/orgs/acme/webhooks/12")
	want := map[string]any{"enabled": true, "description": "Lab bench"}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body: got %v (%s), want %v", got.body, got.raw, want)
	}
	if !strings.Contains(out, "Updated webhook 12") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if strings.Contains(out, "Signing secret") {
		t.Errorf("update must not print a secret:\n%s", out)
	}
}

func TestWebhookUpdateProductsAndEvents(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookJSON+`}`)
	_, err := execCmd(t, "",
		"webhook", "update", "12",
		"--url", "https://new.example.com/hook",
		"--events", "device.connected",
		"--products", "thermostat,doorbell",
		"--enabled=false",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[string]any{
		"url":      "https://new.example.com/hook",
		"events":   []any{"device.connected"},
		"products": []any{"thermostat", "doorbell"},
		"enabled":  false,
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body: got %v, want %v", got.body, want)
	}
}

func TestWebhookUpdateAllProducts(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookJSON+`}`)
	_, err := execCmd(t, "",
		"webhook", "update", "12", "--all-products",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := map[string]any{"all_products": true}; !reflect.DeepEqual(got.body, want) {
		t.Errorf("body: got %v, want %v", got.body, want)
	}
}

func TestWebhookUpdateRejectsEmptyDescription(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookJSON+`}`)
	_, err := execCmd(t, "", "webhook", "update", "12", "--description", "  ", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "--description must not be empty") {
		t.Fatalf("expected an empty-description error, got %v", err)
	}
	if got.method != "" {
		t.Errorf("must not send a request, got %s %s", got.method, got.path)
	}
}

func TestWebhookCreateBareEnabled(t *testing.T) {
	srv, got := webhookServer(t, 201, `{"data":`+webhookWithSecretJSON+`}`)
	_, err := execCmd(t, "",
		"webhook", "create", "--url", "https://x.example", "--events", "device.connected", "--enabled",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.body["enabled"] != true {
		t.Errorf("bare --enabled should send enabled: true, got %s", got.raw)
	}
}

func TestWebhookUpdateNothing(t *testing.T) {
	_, err := execCmd(t, "", "webhook", "update", "12", "--org", "acme", "--uri", "http://127.0.0.1:1", "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("expected nothing-to-update error, got %v", err)
	}
}

func TestWebhookDelete(t *testing.T) {
	srv, got := webhookServer(t, 204, "")
	out, err := execCmd(t, "", "webhook", "delete", "12", "--yes", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodDelete, "/api/orgs/acme/webhooks/12")
	if !strings.Contains(out, "Deleted webhook 12") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestWebhookDeleteConfirm(t *testing.T) {
	srv, got := webhookServer(t, 204, "")
	out, err := execCmd(t, "y\n", "webhook", "delete", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodDelete, "/api/orgs/acme/webhooks/12")
	if !strings.Contains(out, "Delete webhook 12 from acme?") || !strings.Contains(out, "Deleted webhook 12") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestWebhookDeleteDeclined(t *testing.T) {
	srv, got := webhookServer(t, 204, "")
	out, err := execCmd(t, "n\n", "webhook", "delete", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.method != "" {
		t.Errorf("declining must not send a request, got %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestWebhookDeleteNonInteractive(t *testing.T) {
	srv, got := webhookServer(t, 204, "")
	_, err := execCmd(t, "", "webhook", "delete", "12", "--non-interactive", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a refusal mentioning --yes, got %v", err)
	}
	if got.method != "" {
		t.Errorf("refusing must not send a request, got %s %s", got.method, got.path)
	}
}

func TestWebhookRegenerateSecret(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookWithSecretJSON+`}`)
	out, err := execCmd(t, "", "webhook", "regenerate-secret", "12", "--yes", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodPost, "/api/orgs/acme/webhooks/12/regenerate_secret")
	if got.raw != "" {
		t.Errorf("regenerate-secret should send no body, got %q", got.raw)
	}
	for _, s := range []string{"Regenerated the signing secret of webhook 12", "whsec_", "won't show it again"} {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q, got:\n%s", s, out)
		}
	}
}

func TestWebhookRegenerateSecretConfirm(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":`+webhookWithSecretJSON+`}`)
	out, err := execCmd(t, "n\n", "webhook", "regenerate-secret", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.method != "" {
		t.Errorf("declining must not send a request, got %s %s", got.method, got.path)
	}
	if !strings.Contains(out, "stops working at once") || !strings.Contains(out, "Aborted.") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestWebhookTestDelivered(t *testing.T) {
	srv, got := webhookServer(t, 200, `{"data":{"delivered":true,"status":204,"error":null}}`)
	out, err := execCmd(t, "", "webhook", "test", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got.expect(t, http.MethodPost, "/api/orgs/acme/webhooks/12/test")
	if !strings.Contains(out, "Webhook 12 accepted the test event (HTTP 204)") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestWebhookTestNotDelivered(t *testing.T) {
	srv, _ := webhookServer(t, 200, `{"data":{"delivered":false,"status":500,"error":"HTTP 500"}}`)
	_, err := execCmd(t, "", "webhook", "test", "12", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "did not accept the test event: HTTP 500") {
		t.Fatalf("expected a delivery failure, got %v", err)
	}
}

func TestWebhookTestNoResponseJSON(t *testing.T) {
	srv, _ := webhookServer(t, 200, `{"data":{"delivered":false,"status":null,"error":"timeout"}}`)
	out, err := execCmd(t, "", "webhook", "test", "12", "-o", "json", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected a delivery failure, got %v", err)
	}
	var result map[string]any
	if jerr := json.Unmarshal([]byte(out[:strings.Index(out, "}")+1]), &result); jerr != nil {
		t.Fatalf("output does not start with JSON: %v\n%s", jerr, out)
	}
	if result["delivered"] != false || result["status"] != nil || result["error"] != "timeout" {
		t.Errorf("unexpected JSON: %s", out)
	}
}
