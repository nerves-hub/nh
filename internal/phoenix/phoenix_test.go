package phoenix

import (
	"context"
	"encoding/json"
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
