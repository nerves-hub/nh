package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nerves-hub/nh/internal/phoenix"
)

func TestDeviceSocketURL(t *testing.T) {
	cases := []struct {
		base, token, want string
	}{
		{"https://manage.nervescloud.com", "tok", "wss://manage.nervescloud.com/api/socket/websocket?token=tok&vsn=2.0.0"},
		{"http://localhost:4000", "abc", "ws://localhost:4000/api/socket/websocket?token=abc&vsn=2.0.0"},
		// A path on the base URL is replaced by the socket path.
		{"https://example.com/ignored", "t", "wss://example.com/api/socket/websocket?token=t&vsn=2.0.0"},
		// The token is query-escaped.
		{"https://example.com", "a/b+c=d", "wss://example.com/api/socket/websocket?token=a%2Fb%2Bc%3Dd&vsn=2.0.0"},
	}
	for _, c := range cases {
		got, err := deviceSocketURL(c.base, c.token)
		if err != nil {
			t.Errorf("deviceSocketURL(%q): %v", c.base, err)
			continue
		}
		if got != c.want {
			t.Errorf("deviceSocketURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestDeviceSocketURLRejectsBadScheme(t *testing.T) {
	if _, err := deviceSocketURL("ftp://example.com", "t"); err == nil {
		t.Error("non-http(s) scheme should error")
	}
	if _, err := deviceSocketURL("example.com", "t"); err == nil {
		t.Error("scheme-less URI should error")
	}
}

func TestDeviceConsoleRequiresTerminal(t *testing.T) {
	// execCmd wires stdin/stdout to buffers, so the TTY check fails.
	_, err := execCmd(t, "",
		"device", "console", "dev-001",
		"--org", "acme", "--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("expected interactive-terminal error, got %v", err)
	}
}

func TestDeviceConsoleRequiresToken(t *testing.T) {
	t.Setenv("NERVES_CLOUD_TOKEN", "")
	t.Setenv("NERVES_HUB_TOKEN", "")
	_, err := execCmd(t, "",
		"device", "console", "dev-001",
		"--org", "acme", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("expected not-authenticated error, got %v", err)
	}
}

// fakePhoenixServer accepts the user socket and answers each phx_join with
// reply's status and response, as the console and local-shell channels do. A
// nil reply never answers. It returns the websocket URL deviceSocketURL would
// build for it.
func fakePhoenixServer(t *testing.T, reply func(topic string) (status string, response any)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var frame []json.RawMessage
			if json.Unmarshal(data, &frame) != nil || len(frame) != 5 {
				return
			}
			var topic, event string
			_ = json.Unmarshal(frame[2], &topic)
			_ = json.Unmarshal(frame[3], &event)
			if event != phoenix.EventJoin || reply == nil {
				continue
			}
			status, response := reply(topic)
			// Output can reach the client before the join reply; it must
			// not be mistaken for the reply or lost.
			early, _ := json.Marshal([]any{frame[0], nil, topic, "up", map[string]string{"data": "early"}})
			_ = ws.Write(ctx, websocket.MessageText, early)
			out, _ := json.Marshal([]any{frame[0], frame[1], topic, phoenix.EventReply, map[string]any{"status": status, "response": response}})
			_ = ws.Write(ctx, websocket.MessageText, out)
		}
	}))
	t.Cleanup(srv.Close)

	socketURL, err := deviceSocketURL(srv.URL, "nhu_tok")
	if err != nil {
		t.Fatal(err)
	}
	return socketURL
}

func TestOpenTerminalChannelJoins(t *testing.T) {
	socketURL := fakePhoenixServer(t, func(string) (string, any) { return "ok", map[string]any{} })

	conn, joinRef, err := openTerminalChannel(context.Background(), socketURL, "nhu_tok", consoleTopic("dev-1"), "dev-1", "console", time.Second)
	if err != nil {
		t.Fatalf("openTerminalChannel: %v", err)
	}
	defer conn.CloseNow()
	if joinRef == "" {
		t.Error("empty join ref")
	}

	// The output that arrived before the reply is still delivered.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, err := conn.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if msg.Event != "up" || !strings.Contains(string(msg.Payload), "early") {
		t.Errorf("first frame after join: %+v", msg)
	}
}

// The console and local-shell channels refuse with reason "unauthorized"
// when the device is missing, the role lacks the permission, or 2FA is unmet.
func TestOpenTerminalChannelRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		topic string
	}{
		{"console", consoleTopic("dev-1")},
		{"shell", shellTopic("dev-1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotTopic string
			socketURL := fakePhoenixServer(t, func(topic string) (string, any) {
				gotTopic = topic
				return "error", map[string]string{"reason": "unauthorized"}
			})

			_, _, err := openTerminalChannel(context.Background(), socketURL, "nhu_tok", tc.topic, "dev-1", tc.name, time.Second)
			if err == nil {
				t.Fatal("a refused join must be an error")
			}
			for _, want := range []string{"refused the " + tc.name, "dev-1", "unauthorized", "two-factor"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should mention %q", err, want)
				}
			}
			if gotTopic != tc.topic {
				t.Errorf("joined %q, want %q", gotTopic, tc.topic)
			}
		})
	}
}

func TestOpenTerminalChannelOtherReason(t *testing.T) {
	socketURL := fakePhoenixServer(t, func(string) (string, any) {
		return "error", map[string]string{"reason": "unmatched topic"}
	})

	_, _, err := openTerminalChannel(context.Background(), socketURL, "nhu_tok", consoleTopic("dev-1"), "dev-1", "console", time.Second)
	if err == nil || !strings.Contains(err.Error(), "unmatched topic") {
		t.Errorf("want the server's reason, got %v", err)
	}
}

func TestOpenTerminalChannelJoinTimeout(t *testing.T) {
	socketURL := fakePhoenixServer(t, nil)

	_, _, err := openTerminalChannel(context.Background(), socketURL, "nhu_tok", consoleTopic("dev-1"), "dev-1", "console", 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("want a join timeout error, got %v", err)
	}
}

// APISocket.connect/3 refuses any token it can't find as a user API token, and
// Phoenix answers the upgrade with 403.
func TestOpenTerminalChannelSocketRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		token string
		want  string
	}{
		{"nhu_revoked", "invalid, expired or revoked"},
		{"nhpk_product", "product API key"},
	} {
		socketURL, err := deviceSocketURL(srv.URL, tc.token)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = openTerminalChannel(context.Background(), socketURL, tc.token, consoleTopic("dev-1"), "dev-1", "console", time.Second)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("token %s: want an error mentioning %q, got %v", tc.token, tc.want, err)
		}
		if err != nil && strings.Contains(err.Error(), tc.token) {
			t.Errorf("the error must not echo the token: %v", err)
		}
	}
}
