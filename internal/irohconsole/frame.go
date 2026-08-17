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

// Package irohconsole implements the client half of the IrohConsole wire
// protocol (github.com/nervescloud/iroh_console): a tagged, length-prefixed
// framing over a single bidirectional stream, and the hello → challenge/
// response → ready handshake. It is transport-agnostic — it operates on any
// io.ReadWriteCloser — so the frame codec and handshake can be tested without
// an iroh connection. The iroh transport itself lives in internal/iroh.
package irohconsole

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// ALPN is the Application-Layer Protocol Negotiation value the console stream
// uses, matching IrohConsole.Server's default.
const ALPN = "iroh-console/1"

// protocolVersion is announced in the client's hello frame.
const protocolVersion = 1

// maxPayload bounds a single frame's payload, matching IrohConsole.Frame. A
// larger declared length from the peer is rejected rather than buffered.
const maxPayload = 1024 * 1024

// Frame tags, matching lib/iroh_console/frame.ex.
const (
	tagHello     = 0x00
	tagData      = 0x01
	tagResize    = 0x02
	tagChallenge = 0x03
	tagResponse  = 0x04
	tagReady     = 0x05
	tagError     = 0x06
)

// writeFrame writes a single tagged, length-prefixed frame.
func writeFrame(w io.Writer, tag byte, payload []byte) error {
	if len(payload) > maxPayload {
		return fmt.Errorf("irohconsole: frame payload %d exceeds %d", len(payload), maxPayload)
	}
	var hdr [5]byte
	hdr[0] = tag
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// readFrame reads a single frame, rejecting an implausible length before
// allocating for it.
func readFrame(r *bufio.Reader) (tag byte, payload []byte, err error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(hdr[1:])
	if length > maxPayload {
		return 0, nil, fmt.Errorf("irohconsole: frame too large: %d", length)
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// resizePayload encodes a terminal resize as width/height, matching
// IrohConsole.Frame's <<width::16, height::16>>.
func resizePayload(width, height int) []byte {
	var p [4]byte
	binary.BigEndian.PutUint16(p[0:], clampUint16(width))
	binary.BigEndian.PutUint16(p[2:], clampUint16(height))
	return p[:]
}

func clampUint16(n int) uint16 {
	switch {
	case n < 0:
		return 0
	case n > 0xFFFF:
		return 0xFFFF
	default:
		return uint16(n)
	}
}
