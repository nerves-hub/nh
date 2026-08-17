package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConsoleAndShellTopics(t *testing.T) {
	if got := consoleTopic("dev-001"); got != "user:console:identifier-dev-001" {
		t.Errorf("consoleTopic = %q", got)
	}
	if got := shellTopic("dev-001"); got != "user:local_shell:identifier-dev-001" {
		t.Errorf("shellTopic = %q", got)
	}
}

func TestTerminalProtocols(t *testing.T) {
	// The console channel: up/dn with a {height,width} window_size payload.
	if consoleProtocol.outputEvent != "up" || consoleProtocol.inputEvent != "dn" {
		t.Errorf("console events: %+v", consoleProtocol)
	}
	if got := mustJSON(t, consoleProtocol.resizePayload(24, 80)); got != `{"height":24,"width":80}` {
		t.Errorf("console resize payload: %s", got)
	}

	// The local-shell channel: output/input with a {rows,cols} window_size payload.
	if shellProtocol.outputEvent != "output" || shellProtocol.inputEvent != "input" {
		t.Errorf("shell events: %+v", shellProtocol)
	}
	if got := mustJSON(t, shellProtocol.resizePayload(24, 80)); got != `{"cols":80,"rows":24}` {
		t.Errorf("shell resize payload: %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDeviceShellRequiresTerminal(t *testing.T) {
	// execCmd wires stdin/stdout to buffers, so the TTY check fails.
	_, err := execCmd(t, "",
		"device", "shell", "dev-001",
		"--org", "acme", "--token", "tok", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("expected interactive-terminal error, got %v", err)
	}
}

func TestDeviceShellRequiresToken(t *testing.T) {
	t.Setenv("NERVES_CLOUD_TOKEN", "")
	t.Setenv("NERVES_HUB_TOKEN", "")
	_, err := execCmd(t, "",
		"device", "shell", "dev-001",
		"--org", "acme", "--uri", "https://example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("expected not-authenticated error, got %v", err)
	}
}
