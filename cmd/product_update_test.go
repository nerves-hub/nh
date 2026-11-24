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

// productUpdateRequest is one request the fake server saw.
type productUpdateRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// newProductUpdateServer fakes GET and PUT /api/orgs/acme/products/thermostat.
// GET returns current; PUT records its body and answers with status and body,
// or with current when body is empty.
func newProductUpdateServer(t *testing.T, current string, status int, body string) (*httptest.Server, *[]productUpdateRequest) {
	t.Helper()
	var reqs []productUpdateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := productUpdateRequest{Method: r.Method, Path: r.URL.Path}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			if err := json.Unmarshal(raw, &req.Body); err != nil {
				t.Errorf("request body is not a JSON object: %s", raw)
			}
		}
		reqs = append(reqs, req)

		if r.URL.Path != "/api/orgs/acme/products/thermostat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(current))
		case http.MethodPut:
			if body == "" {
				_, _ = w.Write([]byte(current))
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func execProductUpdate(t *testing.T, srv *httptest.Server, flags ...string) (string, error) {
	t.Helper()
	args := append([]string{"product", "update", "thermostat", "--org", "acme", "--uri", srv.URL, "--token", "tok"}, flags...)
	return execCmd(t, "", args...)
}

// Only the settings whose flags were given are sent, at the top level of the
// body: ProductController.update rejects any key it cannot set.
func TestProductUpdateSendsOnlyGivenSettings(t *testing.T) {
	srv, reqs := newProductUpdateServer(t, productShowResponse, 0, "")

	out, err := execProductUpdate(t, srv,
		"--allow-unsigned-atomvm=false",
		"--require-unique-firmware-version",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("want one request, got %+v", *reqs)
	}
	got := (*reqs)[0]
	if got.Method != http.MethodPut || got.Path != "/api/orgs/acme/products/thermostat" {
		t.Errorf("request: want PUT /api/orgs/acme/products/thermostat, got %s %s", got.Method, got.Path)
	}
	want := map[string]any{
		"allow_unsigned_atomvm_firmware":  false,
		"require_unique_firmware_version": true,
	}
	if !reflect.DeepEqual(got.Body, want) {
		t.Errorf("body: want %v, got %v", want, got.Body)
	}
	for _, s := range []string{"Updated product thermostat in acme", "Update tools:", "fwup, esp-idf"} {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q:\n%s", s, out)
		}
	}
}

func TestProductUpdateAllBoolSettings(t *testing.T) {
	srv, reqs := newProductUpdateServer(t, productShowResponse, 0, "")

	_, err := execProductUpdate(t, srv,
		"--allow-unsigned-esp-idf",
		"--allow-unsigned-atomvm=true",
		"--require-unique-firmware-version=false",
		"--default-managed-updates-allowed",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[string]any{
		"allow_unsigned_esp_idf_firmware": true,
		"allow_unsigned_atomvm_firmware":  true,
		"require_unique_firmware_version": false,
		"default_managed_updates_allowed": true,
	}
	if got := (*reqs)[0].Body; !reflect.DeepEqual(got, want) {
		t.Errorf("body: want %v, got %v", want, got)
	}
}

// --tools replaces the whole list without reading the current one, and fwup
// is always kept.
func TestProductUpdateTools(t *testing.T) {
	srv, reqs := newProductUpdateServer(t, productShowResponse, 0, "")

	if _, err := execProductUpdate(t, srv, "--tools", "atomvm, rauc", "--tools", "atomvm"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].Method != http.MethodPut {
		t.Fatalf("want a single PUT, got %+v", *reqs)
	}
	want := map[string]any{"allowed_update_tools": []any{"fwup", "atomvm", "rauc"}}
	if got := (*reqs)[0].Body; !reflect.DeepEqual(got, want) {
		t.Errorf("body: want %v, got %v", want, got)
	}
}

// --allow-tool and --disallow-tool edit the product's current list, so the
// command reads it first and sends the whole new list.
func TestProductUpdateAllowAndDisallowTools(t *testing.T) {
	current := `{"data":{"name":"thermostat","allowed_update_tools":["fwup","atomvm"]}}`
	srv, reqs := newProductUpdateServer(t, current, 0, "")

	if _, err := execProductUpdate(t, srv, "--allow-tool", "esp-idf", "--disallow-tool", "atomvm"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*reqs) != 2 || (*reqs)[0].Method != http.MethodGet || (*reqs)[1].Method != http.MethodPut {
		t.Fatalf("want GET then PUT, got %+v", *reqs)
	}
	want := map[string]any{"allowed_update_tools": []any{"fwup", "esp-idf"}}
	if got := (*reqs)[1].Body; !reflect.DeepEqual(got, want) {
		t.Errorf("body: want %v, got %v", want, got)
	}
}

func TestProductUpdateRejectedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  string
	}{
		{"nothing to update", nil, "nothing to update"},
		{"disallow fwup", []string{"--disallow-tool", "fwup"}, "fwup cannot be disallowed"},
		{"allow and disallow", []string{"--allow-tool", "rauc", "--disallow-tool", "rauc"}, "both allowed and disallowed"},
		{"empty tools", []string{"--tools", ""}, "--tools needs at least one format"},
		{"tools with allow-tool", []string{"--tools", "fwup", "--allow-tool", "rauc"}, "none of the others can be"},
		{"tools with disallow-tool", []string{"--tools", "fwup", "--disallow-tool", "rauc"}, "none of the others can be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := newProductUpdateServer(t, productShowResponse, 0, "")
			_, err := execProductUpdate(t, srv, tc.flags...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
			if len(*reqs) != 0 {
				t.Errorf("no request should be made, got %+v", *reqs)
			}
		})
	}
}

// The changeset error the server returns when the instance has not turned a
// format on (Product.validate_allowed_update_tools/1) gains the setting that
// does. The server names the instance by its friendly platform name, NervesHub
// unless the instance is branded, as NervesCloud is.
func TestProductUpdateToolNotEnabledOnInstance(t *testing.T) {
	for _, instance := range []string{"NervesHub", "NervesCloud"} {
		t.Run(instance, func(t *testing.T) {
			refusal := "not enabled on " + instance + ": esp-idf, rauc"
			srv, _ := newProductUpdateServer(t, productShowResponse, http.StatusUnprocessableEntity,
				`{"errors":{"allowed_update_tools":["`+refusal+`"]}}`)

			_, err := execProductUpdate(t, srv, "--tools", "esp-idf,rauc")
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range []string{
				"422",
				refusal,
				"an administrator must enable the format on " + instance + " first",
				"usually ESP_IDF_FIRMWARE_ENABLED=true",
				"RAUC_FIRMWARE_ENABLED=true",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q:\n%v", want, err)
				}
			}
			if strings.Contains(err.Error(), "ATOMVM") {
				t.Errorf("error names a format that was not refused:\n%v", err)
			}
		})
	}
}

// The server never sees a disallowed name, so a typo is caught here, after
// the current list is read and before anything is sent.
func TestProductUpdateDisallowUnknownTool(t *testing.T) {
	srv, reqs := newProductUpdateServer(t, productShowResponse, 0, "")

	_, err := execProductUpdate(t, srv, "--disallow-tool", "esp_idf")
	if err == nil || !strings.Contains(err.Error(), `unknown update tool "esp_idf"`) {
		t.Errorf("want an unknown-tool error, got %v", err)
	}
	for _, r := range *reqs {
		if r.Method == http.MethodPut {
			t.Errorf("no update should be sent, got %+v", *reqs)
		}
	}
}

func TestProductUpdateUnknownTool(t *testing.T) {
	srv, _ := newProductUpdateServer(t, productShowResponse, http.StatusUnprocessableEntity,
		`{"errors":{"allowed_update_tools":["unknown update tool(s): esp32"]}}`)

	_, err := execProductUpdate(t, srv, "--tools", "esp32")
	if err == nil || !strings.Contains(err.Error(), "known update tools: fwup, esp-idf, atomvm, rauc") {
		t.Errorf("want the known tools listed, got %v", err)
	}
}

func TestProductUpdateJSON(t *testing.T) {
	srv, _ := newProductUpdateServer(t, productShowResponse, 0, "")

	out, err := execProductUpdate(t, srv, "--default-managed-updates-allowed=false", "--output", "json")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got["name"] != "thermostat" || got["default_managed_updates_allowed"] != false {
		t.Errorf("json output: %v", got)
	}
}

func TestProductUpdateMissingName(t *testing.T) {
	_, err := execCmd(t, "", "product", "update", "--org", "acme", "--token", "tok")
	if err == nil || err.Error() != "Product name missing" {
		t.Errorf("want friendly message, got %v", err)
	}
}
