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

// Package phoenix is a minimal client for the Phoenix Channels protocol (v2
// serializer) over a websocket: enough to join one channel, push events, send
// heartbeats, and receive messages. Frames are JSON arrays of the form
// [join_ref, ref, topic, event, payload].
package phoenix

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/coder/websocket"
)

// Phoenix Channels protocol events and topics used here.
const (
	EventJoin      = "phx_join"
	EventReply     = "phx_reply"
	EventError     = "phx_error"
	EventClose     = "phx_close"
	EventHeartbeat = "heartbeat"

	// TopicPhoenix is the control topic that carries heartbeats.
	TopicPhoenix = "phoenix"
)

// readLimit bounds a single incoming frame. Console output arrives in small
// chunks, but allow generous headroom.
const readLimit = 1 << 20

// Message is a decoded Phoenix Channels v2 frame. JoinRef and Ref are empty
// when the wire value was null.
type Message struct {
	JoinRef string
	Ref     string
	Topic   string
	Event   string
	Payload json.RawMessage
}

// Conn is a Phoenix Channels v2 client over a single websocket. Sends are
// serialized internally; a single goroutine should own Receive.
type Conn struct {
	ws  *websocket.Conn
	mu  sync.Mutex // serializes writes and ref allocation
	ref int64
}

// Dial opens a websocket to rawURL and returns a Phoenix connection.
func Dial(ctx context.Context, rawURL string) (*Conn, error) {
	ws, _, err := websocket.Dial(ctx, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("phoenix: dialing: %w", err)
	}
	ws.SetReadLimit(readLimit)
	return &Conn{ws: ws}, nil
}

// FromWebsocket wraps an already-open websocket. It is primarily for tests.
func FromWebsocket(ws *websocket.Conn) *Conn {
	ws.SetReadLimit(readLimit)
	return &Conn{ws: ws}
}

// Close closes the websocket with a normal-closure status.
func (c *Conn) Close() error {
	return c.ws.Close(websocket.StatusNormalClosure, "")
}

// CloseNow closes the websocket immediately, without the closing handshake.
func (c *Conn) CloseNow() error {
	return c.ws.CloseNow()
}

// Join sends a phx_join for topic and returns the join ref. As in the Phoenix
// JS client, the join frame's join_ref and ref are the same value, and that
// value identifies the channel on later pushes. The reply arrives
// asynchronously via Receive; callers that care can match it by ref.
func (c *Conn) Join(ctx context.Context, topic string, payload any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref := c.nextRefLocked()
	if err := c.writeLocked(ctx, ref, ref, topic, EventJoin, payload); err != nil {
		return "", err
	}
	return ref, nil
}

// Push sends an event on a joined channel, using its join ref, and returns the
// message ref.
func (c *Conn) Push(ctx context.Context, joinRef, topic, event string, payload any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref := c.nextRefLocked()
	if err := c.writeLocked(ctx, joinRef, ref, topic, event, payload); err != nil {
		return "", err
	}
	return ref, nil
}

// Heartbeat sends a Phoenix heartbeat on the control topic.
func (c *Conn) Heartbeat(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref := c.nextRefLocked()
	return c.writeLocked(ctx, "", ref, TopicPhoenix, EventHeartbeat, struct{}{})
}

// Receive reads and decodes the next frame.
func (c *Conn) Receive(ctx context.Context) (Message, error) {
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	return decodeFrame(data)
}

// IsCleanClose reports whether a Receive error represents a normal websocket
// closure (or the peer going away) rather than an unexpected failure.
func IsCleanClose(err error) bool {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return true
	}
	return false
}

func (c *Conn) nextRefLocked() string {
	c.ref++
	return strconv.FormatInt(c.ref, 10)
}

func (c *Conn) writeLocked(ctx context.Context, joinRef, ref, topic, event string, payload any) error {
	frame, err := encodeFrame(joinRef, ref, topic, event, payload)
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, frame); err != nil {
		return fmt.Errorf("phoenix: writing frame: %w", err)
	}
	return nil
}

// encodeFrame marshals a v2 array frame. Empty join_ref/ref are encoded as
// null; a nil payload becomes an empty object.
func encodeFrame(joinRef, ref, topic, event string, payload any) ([]byte, error) {
	if payload == nil {
		payload = struct{}{}
	}
	frame := []any{nullableRef(joinRef), nullableRef(ref), topic, event, payload}
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("phoenix: encoding frame: %w", err)
	}
	return data, nil
}

func decodeFrame(data []byte) (Message, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Message{}, fmt.Errorf("phoenix: decoding frame: %w", err)
	}
	if len(raw) != 5 {
		return Message{}, fmt.Errorf("phoenix: expected a 5-element frame, got %d", len(raw))
	}
	m := Message{
		JoinRef: decodeRef(raw[0]),
		Ref:     decodeRef(raw[1]),
		Payload: raw[4],
	}
	if err := json.Unmarshal(raw[2], &m.Topic); err != nil {
		return Message{}, fmt.Errorf("phoenix: decoding topic: %w", err)
	}
	if err := json.Unmarshal(raw[3], &m.Event); err != nil {
		return Message{}, fmt.Errorf("phoenix: decoding event: %w", err)
	}
	return m, nil
}

func nullableRef(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// decodeRef reads a ref that may be a JSON string or null.
func decodeRef(r json.RawMessage) string {
	if len(r) == 0 || string(r) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(r, &s); err != nil {
		return ""
	}
	return s
}
