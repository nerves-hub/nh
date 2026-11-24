package iroh

import (
	"errors"
	"strings"
	"testing"

	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/netaddr"
)

const testRelay = "https://relay.example.com/"

// deviceTicket returns a fresh endpoint's hex id and a ticket for it on
// testRelay, as IrohConsole reports them.
func deviceTicket(t *testing.T) (id, ticket string) {
	t.Helper()
	sk, err := key.GenerateSecretKey()
	if err != nil {
		t.Fatal(err)
	}
	relay, err := netaddr.ParseRelayURL(testRelay)
	if err != nil {
		t.Fatal(err)
	}
	addr := netaddr.NewEndpointAddr(sk.Public().EndpointID(), netaddr.RelayAddr{URL: relay})
	return EndpointIDHex(sk.Public()), endpointticket.Encode(addr)
}

func relayHosts(a DeviceAddr) []string {
	var out []string
	for _, u := range a.addr.RelayURLs() {
		out = append(out, u.Host())
	}
	return out
}

func TestDeviceAddrFromIdentityTicketInDetails(t *testing.T) {
	// What IrohConsole.NervesHubLink reports: the id as identifier, the ticket
	// and relays in details.
	id, ticket := deviceTicket(t)
	addr, err := DeviceAddrFromIdentity(id, map[string]any{
		"ticket":     ticket,
		"relay_urls": []any{testRelay},
	})
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if addr.EndpointID() != id {
		t.Errorf("endpoint id = %s, want %s", addr.EndpointID(), id)
	}
	if got := relayHosts(addr); len(got) != 1 || got[0] != "relay.example.com" {
		t.Errorf("relays = %v", got)
	}
}

func TestDeviceAddrFromIdentityLegacyTicketIdentifier(t *testing.T) {
	// Early builds reported the ticket itself as the identifier.
	id, ticket := deviceTicket(t)
	addr, err := DeviceAddrFromIdentity(ticket, nil)
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if addr.EndpointID() != id {
		t.Errorf("endpoint id = %s, want %s", addr.EndpointID(), id)
	}
}

func TestDeviceAddrFromIdentityIDAndRelays(t *testing.T) {
	id, _ := deviceTicket(t)
	addr, err := DeviceAddrFromIdentity(id, map[string]any{"relay_urls": []any{testRelay, "https://relay2.example.com"}})
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if addr.EndpointID() != id {
		t.Errorf("endpoint id = %s, want %s", addr.EndpointID(), id)
	}
	if got := relayHosts(addr); len(got) != 2 {
		t.Errorf("relays = %v, want both", got)
	}

	// A single relay as a bare string is read too.
	if _, err := DeviceAddrFromIdentity(id, map[string]any{"relay_urls": testRelay}); err != nil {
		t.Errorf("single relay string: %v", err)
	}
}

func TestDeviceAddrFromIdentityTicketForAnotherEndpoint(t *testing.T) {
	id, _ := deviceTicket(t)
	_, otherTicket := deviceTicket(t)
	_, err := DeviceAddrFromIdentity(id, map[string]any{"ticket": otherTicket})
	if err == nil || !strings.Contains(err.Error(), "not the") {
		t.Fatalf("a ticket naming another endpoint must be refused, got %v", err)
	}
}

func TestDeviceAddrFromIdentityNothingToDial(t *testing.T) {
	id, _ := deviceTicket(t)
	for name, details := range map[string]map[string]any{
		"nil":          nil,
		"empty":        {},
		"blank ticket": {"ticket": "  "},
		"no relays":    {"relay_urls": []any{}},
	} {
		if _, err := DeviceAddrFromIdentity(id, details); !errors.Is(err, ErrNoDeviceAddr) {
			t.Errorf("%s: err = %v, want ErrNoDeviceAddr", name, err)
		}
	}
	if _, err := DeviceAddrFromIdentity("", nil); !errors.Is(err, ErrNoDeviceAddr) {
		t.Errorf("blank identifier: err = %v, want ErrNoDeviceAddr", err)
	}
}

func TestDeviceAddrFromIdentityMalformed(t *testing.T) {
	id, _ := deviceTicket(t)
	cases := []struct {
		name       string
		identifier string
		details    map[string]any
		want       string
	}{
		{"bad ticket, nothing to fall back on", id, map[string]any{"ticket": "endpointnotreal"}, "decoding the device's ticket"},
		{"garbage identifier", "not-a-key-or-ticket", nil, "neither an endpoint id nor a ticket"},
		{"relay not a list", id, map[string]any{"relay_urls": 42}, "not a list of URLs"},
		{"relay entry not a string", id, map[string]any{"relay_urls": []any{1}}, "not a URL"},
	}
	for _, tc := range cases {
		_, err := DeviceAddrFromIdentity(tc.identifier, tc.details)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
}

func TestDeviceAddrFromTicket(t *testing.T) {
	id, ticket := deviceTicket(t)
	addr, err := DeviceAddrFromTicket(" " + ticket + "\n")
	if err != nil {
		t.Fatalf("ticket: %v", err)
	}
	if addr.EndpointID() != id {
		t.Errorf("endpoint id = %s, want %s", addr.EndpointID(), id)
	}
	if _, err := DeviceAddrFromTicket("nope"); err == nil {
		t.Error("expected an error for a bad ticket")
	}
}

func TestDeviceAddrFromIdentityUndecodableTicketFallsBackToRelays(t *testing.T) {
	// A ticket this build cannot read must not stop a device that also
	// reported its relays.
	id, _ := deviceTicket(t)
	addr, err := DeviceAddrFromIdentity(id, map[string]any{
		"ticket":     "endpointfromafutureencoding",
		"relay_urls": []any{testRelay},
	})
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if addr.EndpointID() != id {
		t.Errorf("endpoint id = %s, want %s", addr.EndpointID(), id)
	}
}
