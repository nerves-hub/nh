package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures below follow NervesHubWeb.API.ScriptJSON on the server: ids are
// integers, tags are a JSON array (or null), and every script has a language.
// The published OpenAPI spec shows ids and tags as strings, which is wrong.

const scriptsResponse = `{"data":[
	{"id":1,"name":"Clean Disk","language":"elixir","tags":["cleanup","disk"]},
	{"id":2,"name":"Dim the lights","language":"shell","tags":null}
],"pagination":{"page_number":1,"page_size":20,"total_entries":2,"total_pages":1}}`

const scriptShowResponse = `{"data":{"id":1,"name":"Snoot Boop","text":"Snoot.boop()","language":"elixir","tags":["snoots"],"inserted_at":"2026-03-28T08:10:20Z","updated_at":"2026-06-23T08:10:20Z","created_by":{"id":5,"name":"Waffles","email":"w@doggo.com"}}}`

// scriptServer serves resp for one request and records its method, path,
// query, and decoded JSON body.
type scriptServer struct {
	method, path, query string
	body                map[string]any
}

func (s *scriptServer) start(t *testing.T, status int, resp string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method, s.path, s.query = r.Method, r.URL.Path, r.URL.RawQuery
		s.body = nil
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&s.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestScriptList(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, scriptsResponse)

	out, err := execCmd(t, "",
		"script", "list", "--page", "2", "--page-size", "5",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != http.MethodGet || s.path != "/api/orgs/acme/products/thermostat/scripts" {
		t.Errorf("request: %s %s", s.method, s.path)
	}
	if s.query != "pagination%5Bpage%5D=2&pagination%5Bpage_size%5D=5" {
		t.Errorf("query: got %q", s.query)
	}
	for _, want := range []string{"ID", "NAME", "LANGUAGE", "TAGS", "Clean Disk", "elixir", "cleanup,disk", "Dim the lights", "shell", "2 script(s) total"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestScriptListJSON(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, scriptsResponse)

	out, err := execCmd(t, "",
		"script", "list", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got struct {
		Data []struct {
			ID       int64    `json:"id"`
			Language string   `json:"language"`
			Tags     []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(got.Data) != 2 || got.Data[0].ID != 1 || got.Data[0].Language != "elixir" ||
		strings.Join(got.Data[0].Tags, ",") != "cleanup,disk" || got.Data[1].ID != 2 {
		t.Errorf("decoded: %+v", got)
	}
}

func TestScriptListAlias(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, `{"data":[]}`)

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
	var s scriptServer
	srv := s.start(t, http.StatusOK, scriptShowResponse)

	out, err := execCmd(t, "",
		"script", "show", "1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != http.MethodGet || s.path != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("request: %s %s", s.method, s.path)
	}
	for _, want := range []string{"ID:", "Name:", "Snoot Boop", "Language:", "elixir", "Tags:", "snoots", "Created by:", "Waffles", "Text:", "Snoot.boop()"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q, got:\n%s", want, out)
		}
	}
}

func TestScriptShowJSON(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, scriptShowResponse)

	out, err := execCmd(t, "",
		"script", "show", "1", "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got["id"] != float64(1) || got["language"] != "elixir" {
		t.Errorf("id/language: %+v", got)
	}
	if tags, ok := got["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "snoots" {
		t.Errorf("tags should stay a JSON array: %+v", got["tags"])
	}
	if by, ok := got["created_by"].(map[string]any); !ok || by["id"] != float64(5) {
		t.Errorf("created_by: %+v", got["created_by"])
	}
}

func TestScriptIDMustBePositiveInteger(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, scriptShowResponse)

	for _, args := range [][]string{
		{"script", "show", "abc"},
		{"script", "show", "0"},
		{"script", "update", "7a", "--name", "x"},
		{"script", "delete", "1.5", "--yes"},
	} {
		args = append(args, "--org", "acme", "--product", "thermostat", "--uri", srv.URL, "--token", "tok")
		_, err := execCmd(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "ids are positive integers") {
			t.Errorf("%v: got %v", args[:3], err)
		}
	}
	if s.method != "" {
		t.Errorf("no request should be sent for an invalid id, got %s %s", s.method, s.path)
	}
}

func TestScriptCreate(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, `{"data":{"id":7,"name":"Clean Disk","language":"shell","tags":["cleanup"],"text":"rm -rf /tmp/cache"}}`)

	out, err := execCmd(t, "",
		"script", "create",
		"--name", "Clean Disk", "--tags", "cleanup", "--language", "shell", "--text", "rm -rf /tmp/cache",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != http.MethodPost || s.path != "/api/orgs/acme/products/thermostat/scripts" {
		t.Errorf("request: %s %s", s.method, s.path)
	}
	// ScriptController.create casts name, text, language, and tags from the
	// top level of the body; tags as a comma-separated string.
	want := map[string]any{"name": "Clean Disk", "text": "rm -rf /tmp/cache", "language": "shell", "tags": "cleanup"}
	if len(s.body) != len(want) {
		t.Errorf("body: got %+v, want %+v", s.body, want)
	}
	for k, v := range want {
		if s.body[k] != v {
			t.Errorf("body[%q]: got %v, want %v", k, s.body[k], v)
		}
	}
	if !strings.Contains(out, "Created support script Clean Disk (id 7)") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptCreateOmitsUnsetFields(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, `{"data":{"id":7,"name":"FromFile","language":"elixir","tags":null}}`)

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
	if s.body["text"] != "Body.from(:file)" {
		t.Errorf("text from file: got %v", s.body["text"])
	}
	// Without --tags or --language the server keeps its defaults (no tags,
	// elixir), so neither key is sent.
	for _, k := range []string{"tags", "language"} {
		if _, ok := s.body[k]; ok {
			t.Errorf("body should omit %q: %+v", k, s.body)
		}
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
	if _, err := execCmd(t, "",
		"script", "create", "--name", "x", "--text", "x", "--language", "",
		"--org", "acme", "--product", "thermostat", "--token", "tok",
	); err == nil || !strings.Contains(err.Error(), "--language must not be empty") {
		t.Errorf("empty language: got %v", err)
	}
}

func TestScriptCreateWarnsWhenLanguageIgnored(t *testing.T) {
	var s scriptServer
	// A server from before script languages sends no language field.
	srv := s.start(t, http.StatusOK, `{"data":{"id":7,"name":"Old","tags":null}}`)

	stdout, stderr, err := scriptExecSplit(t, "",
		"script", "create", "--name", "Old", "--text", "echo hi", "--language", "shell",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr, "may not support --language") {
		t.Errorf("stderr should warn, got %q", stderr)
	}
	if !strings.Contains(stdout, "Created support script Old (id 7)") {
		t.Errorf("stdout: %q", stdout)
	}
}

func TestScriptUpdate(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, `{"data":{"id":1,"name":"Renamed","language":"elixir","tags":["a"]}}`)

	out, err := execCmd(t, "",
		"script", "update", "1", "--name", "Renamed",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != http.MethodPut || s.path != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("request: %s %s", s.method, s.path)
	}
	// Only the changed field is sent.
	if len(s.body) != 1 || s.body["name"] != "Renamed" {
		t.Errorf("body should hold only name: %+v", s.body)
	}
	if !strings.Contains(out, "Updated support script Renamed (id 1)") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptUpdateLanguageAndClearTags(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusOK, `{"data":{"id":1,"name":"x","language":"shell","tags":null}}`)

	if _, err := execCmd(t, "",
		"script", "update", "1", "--language", "shell", "--tags", "",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// An empty --tags is sent as "", which the server casts to no tags.
	if tags, ok := s.body["tags"]; !ok || tags != "" {
		t.Errorf("tags should be sent empty to clear them: %+v", s.body)
	}
	if s.body["language"] != "shell" {
		t.Errorf("language: %+v", s.body)
	}
	if _, ok := s.body["name"]; ok {
		t.Errorf("unset name should be omitted: %+v", s.body)
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
	var s scriptServer
	srv := s.start(t, http.StatusNoContent, "")

	out, err := execCmd(t, "",
		"script", "delete", "1", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != http.MethodDelete || s.path != "/api/orgs/acme/products/thermostat/scripts/1" {
		t.Errorf("request: %s %s", s.method, s.path)
	}
	if !strings.Contains(out, "Deleted support script 1") {
		t.Errorf("output: %q", out)
	}
}

func TestScriptDeleteAbort(t *testing.T) {
	var s scriptServer
	srv := s.start(t, http.StatusNoContent, "")

	out, err := execCmd(t, "\n",
		"script", "delete", "1",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.method != "" {
		t.Error("no delete should be issued when declined")
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("output: %q", out)
	}
}

// scriptExecSplit runs the CLI like execCmd but keeps stdout and stderr apart.
func scriptExecSplit(t *testing.T, in string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetState(t)

	var out, errOut bytes.Buffer
	rootCmd.SetArgs(args)
	if in != "" {
		rootCmd.SetIn(strings.NewReader(in))
	}
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	t.Cleanup(func() { rootCmd.SetErr(nil) })

	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}
