package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"
)

// TestErrorShapes drives every error body NervesHub renders through the
// client: NervesHubWeb.API.ErrorJSON ({"errors":{"detail":...}}),
// NervesHubWeb.API.ChangesetJSON (traverse_errors output, nested for embeds
// and associations), the bare {"status":"Unauthorized"} from
// DeviceController, PlugAttack's plain-text 403, the maintenance plug's 503,
// and what a proxy may put in front of them.
func TestErrorShapes(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		retryAfter  string
		body        string

		wantMessage    string
		wantFields     map[string][]string
		wantRetryAfter time.Duration
		wantError      string
	}{
		{
			name:        "flat changeset",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"name":["can't be blank"],"identifier":["is invalid","has already been taken"]}}`,
			wantFields: map[string][]string{
				"name":       {"can't be blank"},
				"identifier": {"is invalid", "has already been taken"},
			},
			wantError: "api: 422: Unprocessable Entity\n  identifier: is invalid, has already been taken\n  name: can't be blank",
		},
		{
			// DeploymentGroup embeds_one :conditions.
			name:        "embeds_one",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"conditions":{"version":["is invalid"]}}}`,
			wantFields:  map[string][]string{"conditions.version": {"is invalid"}},
			wantError:   "api: 422: Unprocessable Entity\n  conditions.version: is invalid",
		},
		{
			// CACertificate cast_assoc(:jitp): the parent has no errors of
			// its own, only the nested changeset does.
			name:        "nested assoc only",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"jitp":{"description":["can't be blank"],"product_id":["can't be blank"]}}}`,
			wantFields: map[string][]string{
				"jitp.description": {"can't be blank"},
				"jitp.product_id":  {"can't be blank"},
			},
			wantError: "api: 422: Unprocessable Entity\n  jitp.description: can't be blank\n  jitp.product_id: can't be blank",
		},
		{
			// traverse_errors gives one map per entry of a has_many or
			// embeds_many, with {} for the valid ones.
			name:        "embeds_many",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"devices":[{},{},{"identifier":["can't be blank"]}]}}`,
			wantFields:  map[string][]string{"devices[2].identifier": {"can't be blank"}},
			wantError:   "api: 422: Unprocessable Entity\n  devices[2].identifier: can't be blank",
		},
		{
			name:        "deeply nested and mixed with flat",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body: `{"errors":{
				"name":["can't be blank"],
				"conditions":{"tags":["is invalid"],"rules":[{"op":["is invalid"]},{}]},
				"steps":[{"matching_conditions":{"version":["is invalid"]}}]
			}}`,
			wantFields: map[string][]string{
				"name":                                 {"can't be blank"},
				"conditions.tags":                      {"is invalid"},
				"conditions.rules[0].op":               {"is invalid"},
				"steps[0].matching_conditions.version": {"is invalid"},
			},
			wantError: "api: 422: Unprocessable Entity" +
				"\n  conditions.rules[0].op: is invalid" +
				"\n  conditions.tags: is invalid" +
				"\n  name: can't be blank" +
				"\n  steps[0].matching_conditions.version: is invalid",
		},
		{
			name:        "list positions sort as numbers",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"devices":[{},{},{"tags":["is invalid"]},{},{},{},{},{},{},{},{"tags":["is invalid"]}]}}`,
			wantFields: map[string][]string{
				"devices[2].tags":  {"is invalid"},
				"devices[10].tags": {"is invalid"},
			},
			wantError: "api: 422: Unprocessable Entity\n  devices[2].tags: is invalid\n  devices[10].tags: is invalid",
		},
		{
			name:        "nested changesets with no errors",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"devices":[{},{}],"conditions":{}}}`,
			wantError:   "api: 422: Unprocessable Entity",
		},
		{
			name:        "error detail",
			status:      http.StatusNotFound,
			contentType: "application/json",
			body:        `{"errors":{"detail":"Resource Not Found or Authorization Insufficient"}}`,
			wantMessage: "Resource Not Found or Authorization Insufficient",
			wantError:   "api: 404: Resource Not Found or Authorization Insufficient",
		},
		{
			// ChangesetJSON.error(%{message: ...}).
			name:        "changeset message detail",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"detail":"firmware not found"}}`,
			wantMessage: "firmware not found",
			wantError:   "api: 422: firmware not found",
		},
		{
			// A changeset with a field literally named detail keeps it as
			// a field when it has more than one message.
			name:        "detail with several messages",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"detail":["is too long","is invalid"]}}`,
			wantFields:  map[string][]string{"detail": {"is too long", "is invalid"}},
			wantError:   "api: 422: Unprocessable Entity\n  detail: is too long, is invalid",
		},
		{
			// DeviceController's penalty-box check.
			name:        "status unauthorized",
			status:      http.StatusForbidden,
			contentType: "application/json",
			body:        `{"status":"Unauthorized"}`,
			wantMessage: "Unauthorized",
			wantError:   "api: 403: Unauthorized",
		},
		{
			// PlugAttack's default block_action, used by
			// NervesHubWeb.Plugs.Attack on the CLI login endpoints.
			name:        "plug attack plain text",
			status:      http.StatusForbidden,
			contentType: "text/plain; charset=utf-8",
			body:        "Forbidden\n",
			wantMessage: "Forbidden",
			wantError:   "api: 403: Forbidden",
		},
		{
			name:        "errors as a string",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"errors":"bad things"}`,
			wantMessage: "bad things",
			wantError:   "api: 400: bad things",
		},
		{
			name:        "errors as a list of strings",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"errors":["first","second"]}`,
			wantMessage: "first; second",
			wantError:   "api: 400: first; second",
		},
		{
			name:        "errors as a mixed list",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"errors":["first",{"name":["is bad"]}]}`,
			wantMessage: "first",
			wantFields:  map[string][]string{"[1].name": {"is bad"}},
			wantError:   "api: 400: first\n  [1].name: is bad",
		},
		{
			name:        "keys that flatten to one path",
			status:      http.StatusUnprocessableEntity,
			contentType: "application/json",
			body:        `{"errors":{"a":{"b":["y"]},"a.b":["x"]}}`,
			wantFields:  map[string][]string{"a.b": {"y", "x"}},
			wantError:   "api: 422: Unprocessable Entity\n  a.b: y, x",
		},
		{
			name:           "HTML page without an HTML content type",
			status:         http.StatusTooManyRequests,
			contentType:    "text/plain",
			retryAfter:     "99999999999999999999",
			body:           "\n<!DOCTYPE html><html><body>busy</body></html>",
			wantRetryAfter: time.Duration(1<<63 - 1).Truncate(time.Second),
			wantError:      "api: 429: Too Many Requests (retry after 2562047h47m16s)",
		},
		{
			// NervesHubWeb.Plugs.Maintenance.
			name:           "maintenance",
			status:         http.StatusServiceUnavailable,
			contentType:    "application/json",
			retryAfter:     "300",
			body:           `{"errors":{"detail":"NervesHub is offline for planned maintenance."}}`,
			wantMessage:    "NervesHub is offline for planned maintenance.",
			wantRetryAfter: 5 * time.Minute,
			wantError:      "api: 503: NervesHub is offline for planned maintenance. (retry after 5m)",
		},
		{
			name:           "429 with retry-after seconds",
			status:         http.StatusTooManyRequests,
			retryAfter:     "30",
			wantRetryAfter: 30 * time.Second,
			wantError:      "api: 429: Too Many Requests (retry after 30s)",
		},
		{
			name:           "429 from a proxy with an HTML page and an HTTP date",
			status:         http.StatusTooManyRequests,
			contentType:    "text/html; charset=utf-8",
			retryAfter:     "Sun, 22 Nov 2026 12:01:30 GMT",
			body:           "<html><body><h1>429 Too Many Requests</h1></body></html>",
			wantRetryAfter: 90 * time.Second,
			wantError:      "api: 429: Too Many Requests (retry after 1m30s)",
		},
		{
			name:        "429 without retry-after",
			status:      http.StatusTooManyRequests,
			contentType: "text/plain",
			body:        "slow down",
			wantMessage: "slow down",
			wantError:   "api: 429: slow down",
		},
	}

	fixed := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	origNow := retryAfterNow
	retryAfterNow = func() time.Time { return fixed }
	t.Cleanup(func() { retryAfterNow = origNow })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/orgs/acme" {
					t.Errorf("request: got %s %s", r.Method, r.URL.Path)
				}
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			err := newTestClient(t, srv).Get(context.Background(), "/orgs/acme", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("want *APIError, got %T (%v)", err, err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("status: got %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Message != tc.wantMessage {
				t.Errorf("message: got %q, want %q", apiErr.Message, tc.wantMessage)
			}
			if !reflect.DeepEqual(apiErr.FieldErrors, tc.wantFields) {
				t.Errorf("field errors: got %v, want %v", apiErr.FieldErrors, tc.wantFields)
			}
			if apiErr.RetryAfter != tc.wantRetryAfter {
				t.Errorf("retry after: got %v, want %v", apiErr.RetryAfter, tc.wantRetryAfter)
			}
			if got := apiErr.Error(); got != tc.wantError {
				t.Errorf("Error():\ngot  %q\nwant %q", got, tc.wantError)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"-5", 0},
		{" 120 ", 2 * time.Minute},
		{"soon", 0},
		{"Sun, 22 Nov 2026 12:00:10 GMT", 10 * time.Second},
		{"Sunday, 22-Nov-26 12:00:10 GMT", 10 * time.Second},
		{"Sun Nov 22 12:00:10 2026", 10 * time.Second},
		{"Sun, 22 Nov 2026 11:59:00 GMT", 0},
		{"99999999999999999999", time.Duration(1<<63 - 1).Truncate(time.Second)},
		{"-99999999999999999999", 0},
		{"9223372036854775807", time.Duration(1<<63 - 1).Truncate(time.Second)},
	}
	for _, tc := range tests {
		if got := parseRetryAfter(tc.in, at); got != tc.want {
			t.Errorf("parseRetryAfter(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}

	// A date with a fraction of a second left rounds up, so waiting
	// RetryAfter is always long enough.
	if got := parseRetryAfter("Sun, 22 Nov 2026 12:00:10 GMT", at.Add(500*time.Millisecond)); got != 10*time.Second {
		t.Errorf("fractional date: got %v, want 10s", got)
	}
}

func TestFormatRetryAfter(t *testing.T) {
	tests := map[time.Duration]string{
		time.Second:                "1s",
		30 * time.Second:           "30s",
		100 * time.Second:          "1m40s",
		10 * time.Minute:           "10m",
		time.Hour:                  "1h",
		time.Hour + 30*time.Minute: "1h30m",
		time.Hour + 30*time.Second: "1h0m30s",
		1500 * time.Millisecond:    "2s",
	}
	for d, want := range tests {
		if got := formatRetryAfter(d); got != want {
			t.Errorf("formatRetryAfter(%v): got %q, want %q", d, got, want)
		}
	}
}

func TestNaturalLess(t *testing.T) {
	got := []string{"devices[10].tags", "name", "devices[2].tags", "devices[02].a", "devices", "conditions.version"}
	sort.Slice(got, func(i, j int) bool { return naturalLess(got[i], got[j]) })
	want := []string{"conditions.version", "devices", "devices[02].a", "devices[2].tags", "devices[10].tags", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
