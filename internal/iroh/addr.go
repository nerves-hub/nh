/*
Copyright © 2026 NervesHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package iroh

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/netaddr"
)

// ErrNoDeviceAddr means a device reported an iroh identity with nothing in it
// that says where the endpoint can be reached.
var ErrNoDeviceAddr = errors.New("iroh: the device reported no ticket or relay URL to reach it on")

// DeviceAddr is where a device's iroh endpoint can be reached: its endpoint id
// and the relay (and any direct addresses) it was last reachable on.
type DeviceAddr struct {
	addr netaddr.EndpointAddr
}

// EndpointID returns the device endpoint's id as 64 hex characters.
func (a DeviceAddr) EndpointID() string { return a.addr.ID.String() }

// DeviceAddrFromIdentity works out where to dial a device from the network
// identity it reported to NervesHub.
//
// NervesHub stores the endpoint id — the key the device proved — in
// identifier, and anything about where it is in details. IrohConsole reports
// its current ticket as details["ticket"] and its relays as
// details["relay_urls"]. In order, this uses:
//
//  1. details["ticket"], which must name the same endpoint as identifier;
//  2. identifier itself when it is a ticket, as early IrohConsole builds
//     reported it;
//  3. identifier as an endpoint id, with the relays in details["relay_urls"].
func DeviceAddrFromIdentity(identifier string, details map[string]any) (DeviceAddr, error) {
	identifier = strings.TrimSpace(identifier)

	if t, _ := details["ticket"].(string); strings.TrimSpace(t) != "" {
		addr, err := endpointticket.Decode(strings.TrimSpace(t))
		if err != nil {
			return DeviceAddr{}, fmt.Errorf("iroh: decoding the device's ticket: %w", err)
		}
		// identifier is what the device proved to NervesHub; a ticket naming
		// some other endpoint would have us talk to whoever holds that key.
		if id, err := key.ParseEndpointID(identifier); err == nil && id != addr.ID {
			return DeviceAddr{}, fmt.Errorf("iroh: the device's ticket is for endpoint %s, not the %s it reported", addr.ID.Short(), id.Short())
		}
		return DeviceAddr{addr: addr}, nil
	}

	if identifier == "" {
		return DeviceAddr{}, ErrNoDeviceAddr
	}

	// An endpoint id is 64 hex characters; anything else that decodes as a
	// ticket is one.
	id, idErr := key.ParseEndpointID(identifier)
	if idErr != nil {
		addr, err := endpointticket.Decode(identifier)
		if err != nil {
			return DeviceAddr{}, fmt.Errorf("iroh: the device's identifier %q is neither an endpoint id nor a ticket", truncate(identifier, 16))
		}
		return DeviceAddr{addr: addr}, nil
	}

	relays, err := relayURLs(details["relay_urls"])
	if err != nil {
		return DeviceAddr{}, err
	}
	if len(relays) == 0 {
		return DeviceAddr{}, ErrNoDeviceAddr
	}
	addrs := make([]netaddr.TransportAddr, 0, len(relays))
	for _, u := range relays {
		addrs = append(addrs, netaddr.RelayAddr{URL: u})
	}
	return DeviceAddr{addr: netaddr.NewEndpointAddr(id, addrs...)}, nil
}

// DeviceAddrFromTicket decodes an endpoint ticket, such as one copied from a
// device's console banner.
func DeviceAddrFromTicket(ticket string) (DeviceAddr, error) {
	addr, err := endpointticket.Decode(strings.TrimSpace(ticket))
	if err != nil {
		return DeviceAddr{}, fmt.Errorf("iroh: decoding device ticket: %w", err)
	}
	return DeviceAddr{addr: addr}, nil
}

// relayURLs reads details["relay_urls"], which arrives from JSON as a list of
// strings (or, from a device reporting a single relay, one string).
func relayURLs(v any) ([]netaddr.RelayURL, error) {
	var raw []string
	switch v := v.(type) {
	case nil:
		return nil, nil
	case string:
		raw = []string{v}
	case []string:
		raw = v
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("iroh: the device's relay_urls holds a %T, not a URL", e)
			}
			raw = append(raw, s)
		}
	default:
		return nil, fmt.Errorf("iroh: the device's relay_urls is a %T, not a list of URLs", v)
	}

	out := make([]netaddr.RelayURL, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		u, err := netaddr.ParseRelayURL(s)
		if err != nil {
			return nil, fmt.Errorf("iroh: the device's relay URL %q: %w", s, err)
		}
		out = append(out, u)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
