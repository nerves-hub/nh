package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scriptsResponse = `{"data":[
	{"id":"1","name":"Clean Disk","tags":"cleanup"},
	{"id":"2","name":"Dim the lights","tags":"lights"}
],"pagination":{"page_number":1,"page_size":20,"total_entries":2,"total_pages":1}}`

func TestScriptList(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(scriptsResponse))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"script", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/scripts" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"ID", "NAME", "TAGS", "Clean Disk", "cleanup", "Dim the lights", "2 script(s) total"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestScriptListAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"support-script", "list",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No support scripts found in acme/thermostat") {
		t.Errorf("expected empty state via alias, got %q", out)
	}
}

func TestScriptShow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"1","name":"Snoot Boop","tags":"snoots","text":"Snoot.boop()","inserted_at":"2026-03-28T08:10:20Z","created_by":{"id":"1","name":"Waffles","email":"w@doggo.com"}}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"script", "show", "1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("path: got %q", gotPath)
	}
	for _, want := range []string{"Name:", "Snoot Boop", "Created by:", "Waffles", "Text:", "Snoot.boop()"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestScriptCreate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody api_SupportScriptBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"7","name":"Clean Disk","tags":"cleanup","text":"Disk.clean()"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"script", "create",
		"--name", "Clean Disk", "--tags", "cleanup", "--text", "Disk.clean()",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermostat/scripts" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotBody.Name != "Clean Disk" || gotBody.Text != "Disk.clean()" || gotBody.Tags != "cleanup" {
		t.Errorf("body: %+v", gotBody)
	}
	if !strings.Contains(out, "Created support script Clean Disk (id 7)") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptCreateTextFile(t *testing.T) {
	var gotBody api_SupportScriptBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"7","name":"FromFile"}}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "script.txt")
	if err := os.WriteFile(path, []byte("Body.from(:file)"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execCmd(t, "",
		"script", "create",
		"--name", "FromFile", "--text-file", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody.Text != "Body.from(:file)" {
		t.Errorf("text from file: got %q", gotBody.Text)
	}
}

func TestScriptCreateRequiresNameAndText(t *testing.T) {
	if _, err := execCmd(t, "",
		"script", "create", "--text", "x",
		"--org", "acme", "--product", "thermostat", "--token", "tok",
	); err == nil || !strings.Contains(err.Error(), "--name is required") {
		t.Errorf("missing name: got %v", err)
	}
	if _, err := execCmd(t, "",
		"script", "create", "--name", "x",
		"--org", "acme", "--product", "thermostat", "--token", "tok",
	); err == nil || !strings.Contains(err.Error(), "script body is required") {
		t.Errorf("missing text: got %v", err)
	}
}

func TestScriptUpdate(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"1","name":"Renamed"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"script", "update", "1", "--name", "Renamed",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	// Only the changed field is sent.
	if _, hasName := gotBody["name"]; !hasName {
		t.Errorf("body should include name: %+v", gotBody)
	}
	if _, hasTags := gotBody["tags"]; hasTags {
		t.Errorf("body should omit unset tags: %+v", gotBody)
	}
	if !strings.Contains(out, "Updated support script Renamed (id 1)") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptUpdateNothing(t *testing.T) {
	_, err := execCmd(t, "",
		"script", "update", "1",
		"--org", "acme", "--product", "thermostat", "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("expected nothing-to-update error, got %v", err)
	}
}

func TestScriptDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "",
		"script", "delete", "1", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(out, "Deleted support script 1") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptDeleteAbort(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := execCmd(t, "\n",
		"script", "delete", "1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("no delete should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output: %q", out)
	}
}

// api_SupportScriptBody mirrors the create/update request body for assertions.
type api_SupportScriptBody struct {
	Name string `json:"name"`
	Tags string `json:"tags"`
	Text string `json:"text"`
}
