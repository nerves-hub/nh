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

package irohconsole

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrRefused is returned when the device rejects the session (bad auth,
// version mismatch, …). The message carries the device's stated reason.
type ErrRefused struct{ Message string }

func (e *ErrRefused) Error() string {
	if e.Message == "" {
		return "irohconsole: session refused by device"
	}
	return "irohconsole: session refused: " + e.Message
}

// Responder produces the response to the device's auth challenge. nonce is the
// challenge payload (the device's IrohConsole.Auth adapter may or may not use
// it — password and TOTP adapters ignore it). It is only called when the device
// issues a challenge, so a device requiring no auth never invokes it.
type Responder func(nonce []byte) (string, error)

// StaticResponder always answers with auth — a password or code supplied up
// front (e.g. via --auth).
func StaticResponder(auth string) Responder {
	return func([]byte) (string, error) { return auth, nil }
}

// Session is an authenticated console over a byte stream. The dialer speaks
// first (a QUIC stream is not signalled to the peer until the opener writes),
// so Connect sends the hello, answers any auth challenge, and waits for ready
// before returning.
type Session struct {
	rw io.ReadWriteCloser
	br *bufio.Reader

	writeMu sync.Mutex
}

// Connect runs the client handshake over rw and returns a ready Session. When
// the device issues an auth challenge, respond is called to produce the answer.
func Connect(rw io.ReadWriteCloser, respond Responder) (*Session, error) {
	s := &Session{rw: rw, br: bufio.NewReaderSize(rw, 64*1024)}

	if err := writeFrame(s.rw, tagHello, []byte{protocolVersion}); err != nil {
		return nil, fmt.Errorf("irohconsole: sending hello: %w", err)
	}
	if err := s.authenticate(respond); err != nil {
		return nil, err
	}
	return s, nil
}

// authenticate answers the device's challenge (if any) and waits for :ready.
func (s *Session) authenticate(respond Responder) error {
	for {
		tag, payload, err := readFrame(s.br)
		if err != nil {
			return fmt.Errorf("irohconsole: reading handshake: %w", err)
		}
		switch tag {
		case tagReady:
			return nil
		case tagChallenge:
			// The peer's identity is already proven by iroh; this is the
			// optional second factor.
			if respond == nil {
				return errors.New("irohconsole: device requires auth but none was provided")
			}
			answer, err := respond(payload)
			if err != nil {
				return fmt.Errorf("irohconsole: producing auth response: %w", err)
			}
			if err := s.writeControl(tagResponse, []byte(answer)); err != nil {
				return fmt.Errorf("irohconsole: sending auth response: %w", err)
			}
		case tagError:
			return &ErrRefused{Message: string(payload)}
		default:
			return fmt.Errorf("irohconsole: unexpected frame 0x%02x during handshake", tag)
		}
	}
}

// Output blocks for the next chunk of device output. It returns io.EOF when the
// stream closes cleanly and *ErrRefused when the device ends the session with a
// message. Non-data frames (a stray ready) are skipped.
func (s *Session) Output() ([]byte, error) {
	for {
		tag, payload, err := readFrame(s.br)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, io.EOF
			}
			return nil, err
		}
		switch tag {
		case tagData:
			return payload, nil
		case tagError:
			return nil, &ErrRefused{Message: string(payload)}
		case tagReady:
			continue // benign; ignore
		default:
			return nil, fmt.Errorf("irohconsole: unexpected frame 0x%02x", tag)
		}
	}
}

// SendInput sends keystrokes to the device, chunked to the frame limit.
func (s *Session) SendInput(data []byte) error {
	for len(data) > 0 {
		n := min(len(data), maxPayload)
		if err := s.writeControl(tagData, data[:n]); err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

// Resize tells the device the terminal dimensions changed.
func (s *Session) Resize(width, height int) error {
	return s.writeControl(tagResize, resizePayload(width, height))
}

// Close tears down the underlying stream; the device sees the close and ends
// its shell.
func (s *Session) Close() error { return s.rw.Close() }

// writeControl serialises writes so input and resize frames from different
// goroutines cannot interleave on the wire.
func (s *Session) writeControl(tag byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeFrame(s.rw, tag, payload)
}
