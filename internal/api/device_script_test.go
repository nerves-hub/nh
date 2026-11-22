package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendDeviceScriptTimeoutQuery(t *testing.T) {
	var gotQuery []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = append(gotQuery, r.URL.RawQuery)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	for _, d := range []time.Duration{0, 90 * time.Second, 1500 * time.Microsecond} {
		if _, err := c.SendDeviceScript(context.Background(), "acme", "thermostat", "dev-001", "netinfo", d); err != nil {
			t.Fatalf("SendDeviceScript(%v): %v", d, err)
		}
	}
	// Zero leaves the server default; durations go as whole milliseconds,
	// rounding a remainder up so a tiny timeout never becomes 0.
	want := []string{"", "timeout=90000", "timeout=2"}
	for i := range want {
		if gotQuery[i] != want[i] {
			t.Errorf("query %d: got %q, want %q", i, gotQuery[i], want[i])
		}
	}

	if _, err := c.SendDeviceScript(context.Background(), "acme", "thermostat", "dev-001", "netinfo", -time.Second); err == nil {
		t.Error("a negative timeout should be refused")
	}
	// The server's runner arms an Erlang timer for timeout + 1s, capped at 2^32-1 ms.
	if _, err := c.SendDeviceScript(context.Background(), "acme", "thermostat", "dev-001", "netinfo", MaxScriptTimeout+time.Millisecond); err == nil {
		t.Error("a timeout beyond the server's timer limit should be refused")
	}
	if len(gotQuery) != 3 {
		t.Errorf("refused timeouts should send no request: %q", gotQuery)
	}
}

// The default HTTP client timeout (30s) equals the server's default script
// timeout, so a slow script used to be cut off by the client before the
// server answered. The run must get longer than the script timeout.
func TestSendDeviceScriptOutlastsClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("slow output"))
	}))
	defer srv.Close()

	short := srv.Client()
	short.Timeout = 50 * time.Millisecond
	c, err := NewClient(srv.URL, "tok", WithHTTPClient(short))
	if err != nil {
		t.Fatal(err)
	}

	// A plain request on this client times out ...
	if err := c.Get(context.Background(), "/anything", nil, nil); err == nil {
		t.Fatal("expected the 50ms client timeout to fire on a normal request")
	}
	// ... but a script run is allowed its timeout plus a margin.
	out, err := c.SendDeviceScript(context.Background(), "acme", "thermostat", "dev-001", "slow", time.Second)
	if err != nil {
		t.Fatalf("SendDeviceScript: %v", err)
	}
	if out != "slow output" {
		t.Errorf("output: %q", out)
	}
	// The shared client is not modified.
	if short.Timeout != 50*time.Millisecond {
		t.Errorf("client timeout changed to %v", short.Timeout)
	}
}

func TestWithMinTimeout(t *testing.T) {
	none := &Client{httpClient: &http.Client{}}
	if none.withMinTimeout(time.Minute) != none {
		t.Error("a client without a timeout should be used as is")
	}
	long := &Client{httpClient: &http.Client{Timeout: time.Hour}}
	if long.withMinTimeout(time.Minute) != long {
		t.Error("a longer timeout should not be shortened")
	}
	short := &Client{httpClient: &http.Client{Timeout: time.Second}}
	if got := short.withMinTimeout(time.Minute); got.httpClient.Timeout != time.Minute || short.httpClient.Timeout != time.Second {
		t.Errorf("got %v, original %v", got.httpClient.Timeout, short.httpClient.Timeout)
	}
}

func TestListDeviceScriptsRoute(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":3,"name":"n","language":"elixir","tags":null}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	list, err := c.ListDeviceScripts(context.Background(), "", "", "dev-001", ListSupportScriptsOptions{})
	if err != nil {
		t.Fatalf("ListDeviceScripts: %v", err)
	}
	if len(paths) != 1 || paths[0] != "GET /api/devices/dev-001/scripts" {
		t.Errorf("without org/product only the device route is called: %q", paths)
	}
	if len(list.Scripts) != 1 || list.Scripts[0].ID != 3 {
		t.Errorf("scripts: %+v", list.Scripts)
	}

	if _, err := c.ListDeviceScripts(context.Background(), "", "", "", ListSupportScriptsOptions{}); err == nil {
		t.Error("an empty identifier should be refused")
	}
	if _, err := c.ListDeviceScripts(context.Background(), "acme", "", "dev-001", ListSupportScriptsOptions{}); err == nil {
		t.Error("org without product should be refused")
	}

	// With org and product: an existence check, then the product's scripts.
	paths = nil
	if _, err := c.ListDeviceScripts(context.Background(), "acme", "thermostat", "dev-001", ListSupportScriptsOptions{}); err != nil {
		t.Fatalf("ListDeviceScripts scoped: %v", err)
	}
	want := []string{"GET /api/orgs/acme/products/thermostat/devices/dev-001", "GET /api/orgs/acme/products/thermostat/scripts"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("scoped requests: got %q, want %q", paths, want)
	}
}

func TestSupportScriptDecodesServerJSON(t *testing.T) {
	// Shape of NervesHubWeb.API.ScriptJSON.show/1.
	const body = `{"id":12,"name":"Clean","text":"x","language":"shell","tags":["a","b"],
		"inserted_at":"2026-03-28T08:10:20Z","updated_at":"2026-03-28T08:10:20Z",
		"created_by":{"id":4,"name":"W","email":"w@example.com"}}`
	var s SupportScript
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if s.ID != 12 || s.Language != "shell" || len(s.Tags) != 2 || s.CreatedBy == nil || s.CreatedBy.ID != 4 {
		t.Errorf("decoded: %+v", s)
	}

	// A script with no tags has `tags: null`.
	var empty SupportScript
	if err := json.Unmarshal([]byte(`{"id":1,"name":"n","language":"elixir","tags":null}`), &empty); err != nil {
		t.Fatalf("Unmarshal null tags: %v", err)
	}
	if empty.Tags != nil {
		t.Errorf("tags: %+v", empty.Tags)
	}
}

func TestSupportScriptInputBody(t *testing.T) {
	none := ""
	b, err := json.Marshal(SupportScriptInput{Tags: &none, Language: SupportScriptLanguageShell})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"tags":"","language":"shell"}` {
		t.Errorf("body: %s", b)
	}
	b, _ = json.Marshal(SupportScriptInput{Name: "n"})
	if string(b) != `{"name":"n"}` {
		t.Errorf("unset fields should be omitted: %s", b)
	}
}

func TestSupportScriptIDValidation(t *testing.T) {
	c := &Client{}
	if _, err := c.GetSupportScript(context.Background(), "acme", "thermostat", 0); err == nil {
		t.Error("id 0 should be refused")
	}
}
