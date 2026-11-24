package iroh

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/tmc/go-iroh/key"
)

// fakeHub is a NervesHub that holds the two rules registration has to live
// with (NervesHub.Devices.NetworkIdentity): a key belongs to one organization
// anywhere (409), and a member has one endpoint per instance in an
// organization (422 on org_user_id).
type fakeHub struct {
	t     *testing.T
	email string

	mu    sync.Mutex
	rows  []fakeRow
	posts []api.IrohEndpointInput
	// status, when set, is returned for every POST instead.
	status int
	// heldElsewhereRegistersHere makes a 409 also record the key in the org
	// asked for, as when another nh run won the race to register it.
	heldElsewhereRegistersHere bool
}

type fakeRow struct {
	org, identifier, instance, email string
}

func newFakeHub(t *testing.T) (*fakeHub, *api.Client) {
	t.Helper()
	h := &fakeHub{t: t, email: "alex@example.com"}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := api.NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}

func (h *fakeHub) add(org, identifier, instance, email string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rows = append(h.rows, fakeRow{org, identifier, instance, email})
}

func (h *fakeHub) registered(org, identifier string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.rows {
		if r.org == org && r.identifier == identifier {
			return true
		}
	}
	return false
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if strings.HasSuffix(r.URL.Path, "/users/me") {
		_, _ = w.Write([]byte(`{"data":{"name":"Alex","email":"` + h.email + `"}}`))
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[len(parts)-1] != "iroh_endpoints" || parts[len(parts)-3] != "orgs" {
		h.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	org := parts[len(parts)-2]

	switch r.Method {
	case http.MethodGet:
		search, owner := r.URL.Query().Get("search"), r.URL.Query().Get("owner")
		var data []map[string]any
		for _, row := range h.rows {
			if row.org != org || !strings.HasPrefix(row.identifier, search) {
				continue
			}
			if owner == "user" && row.email == "" {
				continue
			}
			data = append(data, map[string]any{
				"identifier": row.identifier,
				"service":    "iroh",
				"instance":   row.instance,
				"owner":      map[string]any{"type": "user", "user_email": row.email},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

	case http.MethodPost:
		var in api.IrohEndpointInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		h.posts = append(h.posts, in)
		if h.status != 0 {
			w.WriteHeader(h.status)
			_, _ = w.Write([]byte(`{"errors":{"detail":"Forbidden"}}`))
			return
		}
		for _, row := range h.rows {
			if row.identifier == in.Identifier {
				if h.heldElsewhereRegistersHere {
					h.rows = append(h.rows, fakeRow{org, in.Identifier, in.Instance, in.UserEmail})
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"errors":{"detail":"That endpoint id is already registered."}}`))
				return
			}
		}
		instance := in.Instance
		if instance == "" {
			instance = "default"
		}
		for _, row := range h.rows {
			if row.org == org && row.email != "" && row.email == in.UserEmail && row.instance == instance {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"errors":{"org_user_id":["has already been taken"]}}`))
				return
			}
		}
		h.rows = append(h.rows, fakeRow{org, in.Identifier, instance, in.UserEmail})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"identifier":"` + in.Identifier + `","instance":"` + instance + `","owner":{"type":"user"}}}`))

	default:
		h.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}
}

func opts(dir, org string) EndpointOptions {
	return EndpointOptions{DataDir: dir, Org: org, Hostname: "Alexs-MacBook-Pro.local"}
}

func idOf(sk key.SecretKey) string { return EndpointIDHex(sk.Public()) }

func writeLegacy(t *testing.T, dir string) key.SecretKey {
	t.Helper()
	sk, err := LoadOrCreateIdentity(dir, LegacyIdentityPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

func TestEnsureOrgEndpointRegistersOnce(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	var notice bytes.Buffer
	o := opts(dir, "acme")
	o.Notify = &notice

	sk, err := EnsureOrgEndpoint(context.Background(), client, o)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(hub.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(hub.posts))
	}
	got := hub.posts[0]
	if got.Identifier != idOf(sk) || got.UserEmail != "alex@example.com" {
		t.Errorf("registered %+v", got)
	}
	if want := "nh-alexs-macbook-pro-" + idOf(sk)[:8]; got.Instance != want {
		t.Errorf("instance = %q, want %q", got.Instance, want)
	}
	if !strings.Contains(notice.String(), "Registered this machine's iroh endpoint with acme") {
		t.Errorf("notice = %q", notice.String())
	}
	if _, err := os.Stat(OrgIdentityPath(dir, "acme")); err != nil {
		t.Errorf("org key not saved: %v", err)
	}
	if _, err := os.Stat(LegacyIdentityPath(dir)); !os.IsNotExist(err) {
		t.Errorf("a new install must not write the legacy key: %v", err)
	}

	// Already registered: the same key, and nothing posted.
	again, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if again.Bytes() != sk.Bytes() || len(hub.posts) != 1 {
		t.Errorf("second run changed key or re-registered (posts=%d)", len(hub.posts))
	}
}

func TestEnsureOrgEndpointSecondOrgGetsItsOwnKey(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()

	a, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err != nil {
		t.Fatalf("acme: %v", err)
	}
	b, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "globex"))
	if err != nil {
		t.Fatalf("globex (the 409 the shared key used to hit): %v", err)
	}
	if a.Bytes() == b.Bytes() {
		t.Fatal("both organizations got the same key; NervesHub refuses a key in two")
	}
	if !hub.registered("acme", idOf(a)) || !hub.registered("globex", idOf(b)) {
		t.Error("each key should be registered with its own organization")
	}
}

func TestEnsureOrgEndpointSecondMachineSameHostname(t *testing.T) {
	hub, client := newFakeHub(t)

	// Two laptops of the same member, both with the default hostname.
	a, err := EnsureOrgEndpoint(context.Background(), client, opts(t.TempDir(), "acme"))
	if err != nil {
		t.Fatalf("first machine: %v", err)
	}
	b, err := EnsureOrgEndpoint(context.Background(), client, opts(t.TempDir(), "acme"))
	if err != nil {
		t.Fatalf("second machine (the 422 a shared instance used to hit): %v", err)
	}
	if hub.posts[0].Instance == hub.posts[1].Instance {
		t.Errorf("both machines registered instance %q", hub.posts[0].Instance)
	}
	if !hub.registered("acme", idOf(a)) || !hub.registered("acme", idOf(b)) {
		t.Error("both machines should be registered")
	}
}

func TestEnsureOrgEndpointKeepsLegacyRegistration(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	legacy := writeLegacy(t, dir)
	// What an earlier nh registered: the machine key, instance "default".
	hub.add("acme", idOf(legacy), "default", "alex@example.com")

	got, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err != nil {
		t.Fatalf("acme: %v", err)
	}
	if got.Bytes() != legacy.Bytes() {
		t.Fatal("the organization the old key is registered with should keep using it")
	}
	if len(hub.posts) != 0 {
		t.Errorf("an existing registration must not be re-posted, got %+v", hub.posts)
	}
	saved, ok, err := ReadIdentity(OrgIdentityPath(dir, "acme"))
	if err != nil || !ok || saved.Bytes() != legacy.Bytes() {
		t.Errorf("legacy key should be saved as acme's key (ok=%v err=%v)", ok, err)
	}

	// Another organization gets a new key, never the old one.
	other, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "globex"))
	if err != nil {
		t.Fatalf("globex: %v", err)
	}
	if other.Bytes() == legacy.Bytes() {
		t.Error("the old key must not be registered with a second organization")
	}
	if len(hub.posts) != 1 || hub.posts[0].Identifier != idOf(other) {
		t.Errorf("posts = %+v, want only globex's new key", hub.posts)
	}

	// And the legacy file is left where an older nh expects it.
	still, ok, err := ReadIdentity(LegacyIdentityPath(dir))
	if err != nil || !ok || still.Bytes() != legacy.Bytes() {
		t.Error("the legacy key file should be left alone")
	}
}

func TestEnsureOrgEndpointLegacyKeyHeldElsewhere(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	legacy := writeLegacy(t, dir)
	hub.add("globex", idOf(legacy), "default", "alex@example.com")

	got, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err != nil {
		t.Fatalf("acme: %v", err)
	}
	if got.Bytes() == legacy.Bytes() {
		t.Error("a key registered with another organization must not be used here")
	}
	for _, p := range hub.posts {
		if p.Identifier == idOf(legacy) {
			t.Error("the legacy key was posted to a second organization")
		}
	}
}

func TestEnsureOrgEndpointKeyHeldByAnotherOrg(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	// acme's key on disk is held by some other organization: a copied data
	// directory, say.
	sk, err := LoadOrCreateIdentity(dir, OrgIdentityPath(dir, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	hub.add("elsewhere", idOf(sk), "default", "")

	_, err = EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err == nil {
		t.Fatal("expected an error for a key held by another organization")
	}
	for _, want := range []string{"already registered outside acme", "only one organization", OrgIdentityPath(dir, "acme"), "409"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestEnsureOrgEndpointConflictFromARace(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	sk, err := LoadOrCreateIdentity(dir, OrgIdentityPath(dir, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	// Another nh run registers the key between our check and our POST.
	hub.add("racing", idOf(sk), "x", "")
	hub.heldElsewhereRegistersHere = true

	if _, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme")); err != nil {
		t.Fatalf("a 409 for a key now registered here should succeed: %v", err)
	}
}

func TestEnsureOrgEndpointInstanceTaken(t *testing.T) {
	hub, client := newFakeHub(t)
	dir := t.TempDir()
	sk, err := LoadOrCreateIdentity(dir, OrgIdentityPath(dir, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	instance := MachineInstance("Alexs-MacBook-Pro.local", idOf(sk))
	const old = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	hub.add("acme", old, instance, "alex@example.com")

	_, err = EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err == nil {
		t.Fatal("expected an error when the instance name is taken")
	}
	for _, want := range []string{"already have an iroh endpoint named", instance, "nh iroh-endpoint delete " + old + " --org acme", "422"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestEnsureOrgEndpointForbidden(t *testing.T) {
	hub, client := newFakeHub(t)
	hub.status = http.StatusForbidden
	dir := t.TempDir()

	_, err := EnsureOrgEndpoint(context.Background(), client, opts(dir, "acme"))
	if err == nil || !strings.Contains(err.Error(), "does not allow registering iroh endpoints") ||
		!strings.Contains(err.Error(), "nh iroh-endpoint register ") {
		t.Fatalf("expected a permission explanation, got %v", err)
	}
}

func TestEnsureOrgEndpointOtherUnprocessable(t *testing.T) {
	hub, client := newFakeHub(t)
	hub.status = http.StatusUnprocessableEntity

	_, err := EnsureOrgEndpoint(context.Background(), client, opts(t.TempDir(), "acme"))
	if err == nil || !strings.Contains(err.Error(), "NervesHub would not register this machine's iroh endpoint") {
		t.Fatalf("expected a registration refusal, got %v", err)
	}
}

func TestMachineInstance(t *testing.T) {
	const id = "1d5649b6e2e9b98ee0e589845424cdebb5fc9a24ac7f7b4ece9aff2aef4b4448"
	tests := []struct{ host, want string }{
		{"Alexs-MacBook-Pro.local", "nh-alexs-macbook-pro-1d5649b6"},
		{"build_box 01", "nh-build-box-01-1d5649b6"},
		{"--weird--", "nh-weird-1d5649b6"},
		{"", "nh-1d5649b6"},
		{"...", "nh-1d5649b6"},
		{strings.Repeat("a", 100), "nh-" + strings.Repeat("a", maxHostLabel) + "-1d5649b6"},
	}
	for _, tt := range tests {
		if got := MachineInstance(tt.host, id); got != tt.want {
			t.Errorf("MachineInstance(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}
