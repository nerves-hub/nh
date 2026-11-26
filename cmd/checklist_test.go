package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// checklistResponse is what ProductJSON.utilization/1 renders, cut down to a
// few items of each group and status.
const checklistResponse = `{"data":{
	"score":{"done":5,"total":13},
	"device_auth":[
		{"key":"shared_secret","label":"Shared Secret","level":"warning","devices":2},
		{"key":"local_cert","label":"LocalCert","level":"good","devices":10},
		{"key":"nerves_key","label":"NervesKey","level":"good","devices":0}
	],
	"items":[
		{"key":"device_auth","group":"authentication","title":"Device authentication",
		 "description":"Give each device its own certificate.",
		 "status":"attention","summary":"2 of 12 devices connect with a secret shared by the whole product.",
		 "docs_url":"https://docs.nerves-hub.org/auth/device-certificates"},
		{"key":"signed_firmware","group":"recommended","title":"Signed firmware",
		 "description":"Sign firmware and upload it.",
		 "status":"done","summary":"3 signed firmwares uploaded.",
		 "docs_url":"https://docs.nerves-hub.org/concepts/firmware-signing"},
		{"key":"delta_updates","group":"recommended","title":"Delta updates",
		 "description":"Send only what changed.",
		 "status":"todo","summary":"Delta updates are off for every deployment group that is turned on.",
		 "docs_url":"https://docs.nerves-hub.org/management/delta-updates"},
		{"key":"logging","group":"recommended","title":"Device logs",
		 "description":"Have devices send their logs.",
		 "status":"unavailable","summary":"This server has no ClickHouse, which this extension stores its data in.",
		 "docs_url":"https://docs.nerves-hub.org/extensions/logging"},
		{"key":"archives","group":"optional","title":"Archives",
		 "description":"Ship signed files to devices alongside firmware.",
		 "status":"done","summary":"1 archive uploaded.",
		 "docs_url":"https://docs.nerves-hub.org/updates/archives"},
		{"key":"support_scripts","group":"optional","title":"Support scripts",
		 "description":"Keep the commands your team runs on devices as scripts.",
		 "status":"todo","summary":"There are no support scripts.",
		 "docs_url":"https://docs.nerves-hub.org/management/support-scripts"}
	]
}}`

func checklistServer(t *testing.T, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(checklistResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChecklist(t *testing.T) {
	var gotPath string
	srv := checklistServer(t, &gotPath)

	out, err := execCmd(t, "",
		"checklist", "thermostat",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/utilization" {
		t.Errorf("path: got %q", gotPath)
	}

	want := `Utilization of acme/thermostat  (5/13)

Device authentication
  Shared Secret 2 | LocalCert 10 | NervesKey 0
  !  Device authentication  2 of 12 devices connect with a secret shared by the whole product.
      https://docs.nerves-hub.org/auth/device-certificates

Recommended (1/3)
  ✓  Signed firmware        3 signed firmwares uploaded.
  ○  Delta updates          Delta updates are off for every deployment group that is turned on.
      https://docs.nerves-hub.org/management/delta-updates
  -  Device logs            This server has no ClickHouse, which this extension stores its data in.

Optional bonuses (1 in use, not scored)
  ✓  Archives               1 archive uploaded.
  ○  Support scripts        There are no support scripts.
`
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output not written to a terminal has colour codes:\n%q", out)
	}
}

func TestChecklistProductFromFlag(t *testing.T) {
	var gotPath string
	srv := checklistServer(t, &gotPath)

	if _, err := execCmd(t, "",
		"checklist",
		"--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/utilization" {
		t.Errorf("path: got %q", gotPath)
	}
}

func TestChecklistVerbose(t *testing.T) {
	srv := checklistServer(t, nil)

	out, err := execCmd(t, "",
		"checklist", "thermostat", "--verbose",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"      Sign firmware and upload it.\n      https://docs.nerves-hub.org/concepts/firmware-signing\n",
		"      Have devices send their logs.\n      https://docs.nerves-hub.org/extensions/logging\n",
		"      Ship signed files to devices alongside firmware.\n      https://docs.nerves-hub.org/updates/archives\n",
		"      Keep the commands your team runs on devices as scripts.\n      https://docs.nerves-hub.org/management/support-scripts\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q, got:\n%s", want, out)
		}
	}
}

func TestChecklistColor(t *testing.T) {
	srv := checklistServer(t, nil)

	out, err := execCmd(t, "",
		"checklist", "thermostat", "--color", "always",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for name, want := range map[string]string{
		"shared secret in orange":  "\x1b[38;5;208mShared Secret 2\x1b[0m",
		"LocalCert in green":       "\x1b[32mLocalCert 10\x1b[0m",
		"unused NervesKey dimmed":  "\x1b[2mNervesKey 0\x1b[0m",
		"done mark in green":       "\x1b[32m✓\x1b[0m  Signed firmware",
		"attention mark in orange": "\x1b[38;5;208m!\x1b[0m  Device authentication",
		"todo mark dimmed":         "\x1b[2m○\x1b[0m  Delta updates",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s: output missing %q, got:\n%q", name, want, out)
		}
	}
}

func TestChecklistColorNever(t *testing.T) {
	srv := checklistServer(t, nil)

	out, err := execCmd(t, "",
		"checklist", "thermostat", "--color", "never",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--color never wrote colour codes:\n%q", out)
	}
}

func TestChecklistInvalidColor(t *testing.T) {
	srv := checklistServer(t, nil)

	_, err := execCmd(t, "",
		"checklist", "thermostat", "--color", "sometimes",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), `invalid --color "sometimes"`) {
		t.Fatalf("expected an invalid --color error, got %v", err)
	}
}

func TestChecklistJSON(t *testing.T) {
	srv := checklistServer(t, nil)

	out, err := execCmd(t, "",
		"checklist", "thermostat", "--output", "json",
		"--org", "acme", "--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		`"score": {`, `"done": 5`, `"total": 13`,
		`"device_auth": [`, `"key": "shared_secret"`, `"level": "warning"`, `"devices": 2`,
		`"key": "delta_updates"`, `"group": "recommended"`, `"status": "todo"`,
		`"docs_url": "https://docs.nerves-hub.org/management/delta-updates"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s, got:\n%s", want, out)
		}
	}
}

func TestChecklistNoProduct(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := checklistServer(t, nil)

	_, err := execCmd(t, "", "checklist", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "no product set") {
		t.Fatalf("expected a missing product error, got %v", err)
	}
}

// A server from before the report answers its route with 404, while still
// knowing the product.
func TestChecklistOlderServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/utilization") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"name":"thermostat"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "", "checklist", "thermostat", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "doesn't report product utilization") {
		t.Fatalf("expected an upgrade message, got %v", err)
	}
}

func TestChecklistUnknownProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "", "checklist", "nope", "--org", "acme", "--uri", srv.URL, "--token", "tok")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a 404, got %v", err)
	}
	if strings.Contains(err.Error(), "upgrade") {
		t.Errorf("a missing product was reported as an old server: %v", err)
	}
}
