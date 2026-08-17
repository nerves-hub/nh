package irohconsole

import (
	"bufio"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// fakeDevice speaks the server half of the protocol over one end of a net.Pipe.
type fakeDevice struct {
	conn net.Conn
	br   *bufio.Reader
}

func newPipe(t *testing.T) (client net.Conn, dev *fakeDevice) {
	t.Helper()
	c, s := net.Pipe()
	t.Cleanup(func() { c.Close(); s.Close() })
	// Keep tests from hanging forever if the protocol logic is wrong.
	deadline := time.Now().Add(5 * time.Second)
	_ = c.SetDeadline(deadline)
	_ = s.SetDeadline(deadline)
	return c, &fakeDevice{conn: s, br: bufio.NewReader(s)}
}

func (d *fakeDevice) recv(t *testing.T) (byte, []byte) {
	t.Helper()
	tag, payload, err := readFrame(d.br)
	if err != nil {
		t.Fatalf("device recv: %v", err)
	}
	return tag, payload
}

func (d *fakeDevice) send(t *testing.T, tag byte, payload []byte) {
	t.Helper()
	if err := writeFrame(d.conn, tag, payload); err != nil {
		t.Fatalf("device send: %v", err)
	}
}

func (d *fakeDevice) expectHello(t *testing.T) {
	t.Helper()
	tag, payload, err := readFrame(d.br)
	if err != nil {
		t.Fatalf("reading hello: %v", err)
	}
	if tag != tagHello || len(payload) != 1 || payload[0] != protocolVersion {
		t.Fatalf("bad hello: tag=%#x payload=%v", tag, payload)
	}
}

func TestConnectNoAuth(t *testing.T) {
	client, dev := newPipe(t)
	go func() {
		dev.expectHello(t)
		dev.send(t, tagReady, nil)
	}()

	s, err := Connect(client, StaticResponder(""))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = s.Close()
}

func TestConnectPasswordAuth(t *testing.T) {
	client, dev := newPipe(t)
	gotResp := make(chan string, 1)
	go func() {
		dev.expectHello(t)
		dev.send(t, tagChallenge, []byte("nonce-bytes"))
		tag, payload := dev.recv(t)
		if tag != tagResponse {
			t.Errorf("expected response frame, got %#x", tag)
		}
		gotResp <- string(payload)
		dev.send(t, tagReady, nil)
	}()

	s, err := Connect(client, StaticResponder("hunter2"))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer s.Close()

	if got := <-gotResp; got != "hunter2" {
		t.Errorf("device received response %q, want %q", got, "hunter2")
	}
}

func TestConnectRefused(t *testing.T) {
	client, dev := newPipe(t)
	go func() {
		dev.expectHello(t)
		dev.send(t, tagError, []byte("authentication failed"))
	}()

	_, err := Connect(client, StaticResponder("wrong"))
	var refused *ErrRefused
	if !errors.As(err, &refused) {
		t.Fatalf("expected *ErrRefused, got %v", err)
	}
	if refused.Message != "authentication failed" {
		t.Errorf("refusal message: %q", refused.Message)
	}
}

func TestOutputInputAndResize(t *testing.T) {
	client, dev := newPipe(t)
	serverDone := make(chan struct{})
	var gotInput []byte
	var gotW, gotH int
	go func() {
		defer close(serverDone)
		dev.expectHello(t)
		dev.send(t, tagReady, nil)
		dev.send(t, tagData, []byte("iex(1)> "))
		// Read the client's input, then its resize.
		tag, payload := dev.recv(t)
		if tag == tagData {
			gotInput = payload
		}
		tag, payload = dev.recv(t)
		if tag == tagResize && len(payload) == 4 {
			gotW = int(payload[0])<<8 | int(payload[1])
			gotH = int(payload[2])<<8 | int(payload[3])
		}
	}()

	s, err := Connect(client, StaticResponder(""))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer s.Close()

	out, err := s.Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "iex(1)> " {
		t.Errorf("output: %q", out)
	}
	if err := s.SendInput([]byte("1 + 1\r")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	<-serverDone
	if string(gotInput) != "1 + 1\r" {
		t.Errorf("device got input %q", gotInput)
	}
	if gotW != 120 || gotH != 40 {
		t.Errorf("device got resize %dx%d, want 120x40", gotW, gotH)
	}
}

func TestOutputEOF(t *testing.T) {
	client, dev := newPipe(t)
	go func() {
		dev.expectHello(t)
		dev.send(t, tagReady, nil)
		dev.conn.Close()
	}()

	s, err := Connect(client, StaticResponder(""))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := s.Output(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after close, got %v", err)
	}
}
