package cmd

import (
	"strings"
	"testing"
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
