package phoenix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestEncodeFrame(t *testing.T) {
	// join_ref null, ref set.
	got, err := encodeFrame("", "7", "phoenix", "heartbeat", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[null,"7","phoenix","heartbeat",{}]` {
		t.Errorf("heartbeat frame: %s", got)
	}

	// join_ref and ref set, with a payload.
	got, err = encodeFrame("3", "4", "user:console:dev-1", "dn", map[string]string{"data": "ls\n"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `["3","4","user:console:dev-1","dn",{"data":"ls\n"}]` {
		t.Errorf("dn frame: %s", got)
	}
}

func TestDecodeFrame(t *testing.T) {
	m, err := decodeFrame([]byte(`["3","4","user:console:dev-1","up",{"data":"hi"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if m.JoinRef != "3" || m.Ref != "4" || m.Topic != "user:console:dev-1" || m.Event != "up" {
		t.Errorf("decoded: %+v", m)
	}
	var p struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(m.Payload, &p); err != nil || p.Data != "hi" {
		t.Errorf("payload: %q (%v)", p.Data, err)
	}

	// Null refs decode to empty strings.
	m, err = decodeFrame([]byte(`[null,null,"phoenix","phx_reply",{}]`))
	if err != nil {
		t.Fatal(err)
	}
	if m.JoinRef != "" || m.Ref != "" {
		t.Errorf("null refs should be empty, got join=%q ref=%q", m.JoinRef, m.Ref)
	}

	if _, err := decodeFrame([]byte(`["only","three","elements"]`)); err == nil {
		t.Error("a non-5-element frame should error")
	}
}

// dialTestConn starts a websocket echo-ish server that records frames it
// receives and lets the test push frames back. It returns a connected Conn.
func dialTestConn(t *testing.T, handle func(ctx context.Context, server *Conn)) *Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		handle(r.Context(), FromWebsocket(ws))
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + srv.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func TestJoinPushReceive(t *testing.T) {
	type frame struct {
		joinRef, ref, topic, event string
		data                       string
	}
	received := make(chan frame, 4)

	conn := dialTestConn(t, func(ctx context.Context, server *Conn) {
		for {
			msg, err := server.Receive(ctx)
			if err != nil {
				return
			}
			var p struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(msg.Payload, &p)
			received <- frame{msg.JoinRef, msg.Ref, msg.Topic, msg.Event, p.Data}

			switch msg.Event {
			case EventJoin:
				// Reply ok, then push an "up" on the same channel.
				_, _ = server.Push(ctx, "", msg.Topic, EventReply, map[string]any{"status": "ok", "response": map[string]any{}})
				_, _ = server.Push(ctx, msg.JoinRef, msg.Topic, "up", map[string]string{"data": "hello"})
			}
		}
	})

	ctx := context.Background()
	joinRef, err := conn.Join(ctx, "user:console:dev-1", struct{}{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// The join frame uses the same value for join_ref and ref.
	select {
	case f := <-received:
		if f.event != EventJoin || f.joinRef != joinRef || f.ref != joinRef {
			t.Errorf("join frame: %+v (joinRef=%s)", f, joinRef)
		}
		if f.topic != "user:console:dev-1" {
			t.Errorf("join topic: %q", f.topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the join")
	}

	// Read the reply and the "up" pushed by the server.
	sawUp := false
	for !sawUp {
		msg, err := conn.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if msg.Event == "up" {
			var p struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(msg.Payload, &p)
			if p.Data != "hello" {
				t.Errorf("up data: %q", p.Data)
			}
			if msg.JoinRef != joinRef {
				t.Errorf("up join_ref: got %q want %q", msg.JoinRef, joinRef)
			}
			sawUp = true
		}
	}

	// A channel push carries the join ref.
	if _, err := conn.Push(ctx, joinRef, "user:console:dev-1", "dn", map[string]string{"data": "x"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	select {
	case f := <-received:
		if f.event != "dn" || f.joinRef != joinRef || f.data != "x" {
			t.Errorf("dn frame: %+v", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the push")
	}
}

func TestHeartbeat(t *testing.T) {
	received := make(chan Message, 1)
	conn := dialTestConn(t, func(ctx context.Context, server *Conn) {
		for {
			msg, err := server.Receive(ctx)
			if err != nil {
				return
			}
			received <- msg
		}
	})

	if err := conn.Heartbeat(context.Background()); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	select {
	case msg := <-received:
		if msg.Topic != TopicPhoenix || msg.Event != EventHeartbeat {
			t.Errorf("heartbeat: %+v", msg)
		}
		if msg.JoinRef != "" {
			t.Errorf("heartbeat join_ref should be null, got %q", msg.JoinRef)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the heartbeat")
	}
}

func TestIsCleanClose(t *testing.T) {
	conn := dialTestConn(t, func(ctx context.Context, server *Conn) {
		_ = server.Close() // normal closure
	})
	_, err := conn.Receive(context.Background())
	if err == nil {
		t.Fatal("expected a read error after the server closed")
	}
	if !IsCleanClose(err) {
		t.Errorf("normal closure should be clean, got %v", err)
	}
}

// sendFrame writes a frame with exact refs, as a server replying to a join
// does, rather than allocating a fresh ref like Push.
func sendFrame(ctx context.Context, c *Conn, joinRef, ref, topic, event string, payload any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeLocked(ctx, joinRef, ref, topic, event, payload)
}

// joinServer answers the first phx_join it receives by calling reply with the
// join frame, then keeps reading until the client goes away.
func joinServer(t *testing.T, reply func(ctx context.Context, server *Conn, join Message)) *Conn {
	t.Helper()
	return dialTestConn(t, func(ctx context.Context, server *Conn) {
		for {
			msg, err := server.Receive(ctx)
			if err != nil {
				return
			}
			if msg.Event == EventJoin {
				reply(ctx, server, msg)
			}
		}
	})
}

// The console and local-shell channels answer
// {:error, %{reason: "unauthorized"}} when the user may not use the device.
func TestJoinAndWaitRefused(t *testing.T) {
	conn := joinServer(t, func(ctx context.Context, server *Conn, join Message) {
		_ = sendFrame(ctx, server, join.JoinRef, join.Ref, join.Topic, EventReply,
			map[string]any{"status": "error", "response": map[string]string{"reason": "unauthorized"}})
	})

	_, err := conn.JoinAndWait(context.Background(), "user:console:identifier-dev-1", struct{}{}, 2*time.Second)
	var joinErr *JoinError
	if !errors.As(err, &joinErr) {
		t.Fatalf("want a *JoinError, got %v", err)
	}
	if joinErr.Reason != "unauthorized" || joinErr.Topic != "user:console:identifier-dev-1" {
		t.Errorf("join error: %+v", joinErr)
	}
}

// Frames that arrive before the join reply, including replies to other refs,
// are kept for Receive rather than dropped.
func TestJoinAndWaitKeepsEarlyFrames(t *testing.T) {
	conn := joinServer(t, func(ctx context.Context, server *Conn, join Message) {
		_ = sendFrame(ctx, server, "", "99", TopicPhoenix, EventReply, map[string]any{"status": "ok", "response": map[string]any{}})
		_ = sendFrame(ctx, server, join.JoinRef, "", join.Topic, "up", map[string]string{"data": "early"})
		_ = sendFrame(ctx, server, join.JoinRef, join.Ref, join.Topic, EventReply, map[string]any{"status": "ok", "response": map[string]any{}})
		_ = sendFrame(ctx, server, join.JoinRef, "", join.Topic, "up", map[string]string{"data": "late"})
	})

	ctx := context.Background()
	joinRef, err := conn.JoinAndWait(ctx, "user:console:identifier-dev-1", struct{}{}, 2*time.Second)
	if err != nil {
		t.Fatalf("JoinAndWait: %v", err)
	}
	if joinRef == "" {
		t.Fatal("empty join ref")
	}

	var got []string
	for len(got) < 3 {
		msg, err := conn.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		var p struct {
			Data string `json:"data"`
		}
		_ = json.Unmarshal(msg.Payload, &p)
		got = append(got, msg.Event+":"+p.Data)
	}
	want := []string{EventReply + ":", "up:early", "up:late"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d: got %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestJoinAndWaitTimesOut(t *testing.T) {
	conn := joinServer(t, func(context.Context, *Conn, Message) {})

	_, err := conn.JoinAndWait(context.Background(), "user:console:identifier-dev-1", struct{}{}, 50*time.Millisecond)
	if !errors.Is(err, ErrJoinTimeout) {
		t.Fatalf("want ErrJoinTimeout, got %v", err)
	}
}

// Phoenix answers 403 when the socket's connect/3 refuses the token.
func TestDialRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := Dial(context.Background(), "ws"+srv.URL[len("http"):])
	var dialErr *DialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("want a *DialError, got %v", err)
	}
	if dialErr.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", dialErr.StatusCode)
	}
}
