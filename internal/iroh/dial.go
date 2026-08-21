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
	"context"
	"fmt"
	"io"

	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/relay"
)

// Dial connects to a device's IrohConsole using its ticket and returns the
// bidirectional stream to run the console protocol over, plus a cleanup func
// the caller must invoke (after closing the stream) to release the connection
// and endpoint.
//
// It uses the relay advertised in the ticket — NervesCloud devices use a hosted
// relay, not n0's — and forces the relay path: the device is behind NAT, direct
// hole-punching does not complete, and mixing the two prevents the connection
// from establishing. A console is low-bandwidth, so relaying it is fine.
func Dial(ctx context.Context, ticket string, sk key.SecretKey, alpn string) (stream io.ReadWriteCloser, cleanup func(), err error) {
	addr, err := endpointticket.Decode(ticket)
	if err != nil {
		return nil, nil, fmt.Errorf("iroh: decoding device ticket: %w", err)
	}

	mode := relay.ModeDefault()
	if urls := addr.RelayURLs(); len(urls) > 0 {
		mode = relay.ModeCustomURLs(urls...)
	}

	ep, err := iroh.Bind(ctx,
		iroh.WithSecretKey(sk),
		iroh.WithALPNs(alpn),
		iroh.WithRelayMode(mode),
		iroh.WithoutIPTransports(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("iroh: binding endpoint: %w", err)
	}
	// From here on, any failure must shut the endpoint down.
	shutdown := func() { _ = ep.Shutdown(context.Background()) }

	if err := ep.Online(ctx); err != nil {
		shutdown()
		return nil, nil, fmt.Errorf("iroh: connecting to relay: %w", err)
	}

	conn, err := ep.Connect(ctx, addr, alpn)
	if err != nil {
		shutdown()
		return nil, nil, fmt.Errorf("iroh: connecting to device: %w", err)
	}

	s, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "")
		shutdown()
		return nil, nil, fmt.Errorf("iroh: opening stream: %w", err)
	}

	cleanup = func() {
		_ = conn.CloseWithError(0, "")
		shutdown()
	}
	return s, cleanup, nil
}
