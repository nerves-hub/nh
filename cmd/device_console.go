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

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/phoenix"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	// consoleHeartbeatInterval matches Phoenix's default client heartbeat.
	consoleHeartbeatInterval = 30 * time.Second
	// consoleQuitByte is Ctrl-C, which disconnects the local session.
	consoleQuitByte = 0x03
	// consoleJoinTimeout bounds the wait for the server to accept the channel
	// join. It matches the Phoenix JS client's default push timeout.
	consoleJoinTimeout = 10 * time.Second
	// productKeyPrefix marks a product API key, which the user socket refuses.
	productKeyPrefix = "nhpk_"
)

// terminalProtocol captures the channel events and resize payload for an
// interactive session. The console and local-shell channels differ in these.
type terminalProtocol struct {
	name          string // what the session is, for messages: "console"
	outputEvent   string // device -> user
	inputEvent    string // user -> device
	resizeEvent   string // user -> device
	resizePayload func(rows, cols int) any
}

var (
	consoleProtocol = terminalProtocol{
		name:        "console",
		outputEvent: "up",
		inputEvent:  "dn",
		resizeEvent: "window_size",
		resizePayload: func(rows, cols int) any {
			return map[string]int{"height": rows, "width": cols}
		},
	}
	shellProtocol = terminalProtocol{
		name:        "shell",
		outputEvent: "output",
		inputEvent:  "input",
		resizeEvent: "window_size",
		resizePayload: func(rows, cols int) any {
			return map[string]int{"rows": rows, "cols": cols}
		},
	}
)

// errConsoleDone is the clean-exit sentinel (user quit or channel closed).
var errConsoleDone = errors.New("console: disconnected")

var deviceConsoleCmd = &cobra.Command{
	Use:   "console <identifier>",
	Short: "Connect to a device's console",
	Long: `Open an interactive console to a device over a websocket.

Input is sent to the device and its output is shown locally. The session stays
open until the device console ends or you press Ctrl-C. Requires an interactive
terminal.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceConsole,
}

func init() {
	deviceCmd.AddCommand(deviceConsoleCmd)
}

func runDeviceConsole(cmd *cobra.Command, args []string) error {
	return runDeviceTerminalSession(cmd, consoleTopic(args[0]), args[0], consoleProtocol)
}

// consoleTopic is the Phoenix topic for a device's IEx console.
func consoleTopic(identifier string) string {
	return "user:console:identifier-" + identifier
}

// runDeviceTerminalSession opens an interactive websocket session on the given
// Phoenix topic, bridging the local terminal to the device. label names the
// device in status messages.
func runDeviceTerminalSession(cmd *cobra.Command, topic, label string, proto terminalProtocol) error {
	cfg := config.From(cmd.Context())
	if err := requireToken(cfg); err != nil {
		return err
	}

	in, out, ok := consoleTerminal(cmd)
	if !ok {
		return errors.New("an interactive terminal is required")
	}

	socketURL, err := deviceSocketURL(cfg.URI, cfg.Token)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	conn, joinRef, err := openTerminalChannel(ctx, socketURL, cfg.Token, topic, label, proto.name, consoleJoinTimeout)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	fmt.Fprintf(cmd.ErrOrStderr(), "Connected to %s. Press Ctrl-C to disconnect.\n", label)

	oldState, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("entering raw terminal mode: %w", err)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = term.Restore(int(in.Fd()), oldState)
			restored = true
		}
	}
	defer restore()

	// Report the terminal size up front and whenever it changes.
	sendSize := func() {
		cols, rows, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return
		}
		_, _ = conn.Push(ctx, joinRef, topic, proto.resizeEvent, proto.resizePayload(rows, cols))
	}
	sendSize()
	stopResize := onResize(sendSize)
	defer stopResize()

	errc := make(chan error, 3)
	go consoleReadLoop(ctx, conn, out, proto.outputEvent, errc)
	go consoleWriteLoop(ctx, conn, in, joinRef, topic, proto.inputEvent, errc)
	go consoleHeartbeatLoop(ctx, conn, errc)

	err = <-errc
	cancel()
	restore()

	fmt.Fprintf(cmd.ErrOrStderr(), "\nDisconnected from %s\n", label)
	if err == nil || errors.Is(err, errConsoleDone) {
		return nil
	}
	return err
}

// openTerminalChannel dials the user socket and joins topic, waiting for the
// server to accept the join so a refusal is reported before the terminal goes
// into raw mode rather than leaving a silent, dead session.
func openTerminalChannel(ctx context.Context, socketURL, token, topic, label, name string, joinTimeout time.Duration) (*phoenix.Conn, string, error) {
	conn, err := phoenix.Dial(ctx, socketURL)
	if err != nil {
		return nil, "", terminalDialError(err, token, name)
	}

	joinRef, err := conn.JoinAndWait(ctx, topic, struct{}{}, joinTimeout)
	if err != nil {
		_ = conn.CloseNow()
		return nil, "", terminalJoinError(err, label, name)
	}
	return conn, joinRef, nil
}

// terminalDialError explains a websocket the server would not open. The user
// socket refuses any token that is not a user API token with a 403.
func terminalDialError(err error, token, name string) error {
	var dialErr *phoenix.DialError
	if !errors.As(err, &dialErr) {
		return err
	}
	switch dialErr.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		if strings.HasPrefix(token, productKeyPrefix) {
			return fmt.Errorf("cannot open a %s with a product API key (%s...): the server only accepts user tokens here; run `nh user auth` or set NERVES_HUB_TOKEN to a user token", name, productKeyPrefix)
		}
		return fmt.Errorf("the server refused the %s connection (HTTP %d): the token is invalid, expired or revoked; run `nh user auth` to sign in again", name, dialErr.StatusCode)
	default:
		return fmt.Errorf("the server refused the %s connection with HTTP %d", name, dialErr.StatusCode)
	}
}

// terminalJoinError explains a channel join the server refused or never
// answered.
func terminalJoinError(err error, label, name string) error {
	var joinErr *phoenix.JoinError
	switch {
	case errors.As(err, &joinErr) && joinErr.Reason == "unauthorized":
		// The channels give the one reason for every refusal, so name them all.
		return fmt.Errorf("the server refused the %s for device %s (unauthorized): the device does not exist, your role in its org does not allow a %s, or the org requires two-factor authentication you have not set up", name, label, name)
	case errors.As(err, &joinErr):
		reason := joinErr.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return fmt.Errorf("the server refused the %s for device %s: %s", name, label, reason)
	case errors.Is(err, phoenix.ErrJoinTimeout):
		return fmt.Errorf("the server did not answer the request to open a %s for device %s: %w", name, label, err)
	default:
		return err
	}
}

// consoleReadLoop forwards device output to the local terminal.
func consoleReadLoop(ctx context.Context, conn *phoenix.Conn, out *os.File, outputEvent string, errc chan<- error) {
	for {
		msg, err := conn.Receive(ctx)
		if err != nil {
			errc <- consoleReceiveError(err)
			return
		}
		switch msg.Event {
		case outputEvent:
			var p struct {
				Data string `json:"data"`
			}
			if json.Unmarshal(msg.Payload, &p) == nil && p.Data != "" {
				_, _ = out.WriteString(p.Data)
			}
		case phoenix.EventError, phoenix.EventClose:
			errc <- errConsoleDone
			return
		}
	}
}

// consoleWriteLoop forwards local keystrokes to the device, quitting on Ctrl-C.
func consoleWriteLoop(ctx context.Context, conn *phoenix.Conn, in *os.File, joinRef, topic, inputEvent string, errc chan<- error) {
	buf := make([]byte, 4096)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			data := buf[:n]
			if i := bytes.IndexByte(data, consoleQuitByte); i >= 0 {
				if i > 0 {
					_, _ = conn.Push(ctx, joinRef, topic, inputEvent, consoleData(data[:i]))
				}
				errc <- errConsoleDone
				return
			}
			if _, serr := conn.Push(ctx, joinRef, topic, inputEvent, consoleData(data)); serr != nil {
				errc <- serr
				return
			}
		}
		if err != nil {
			errc <- err
			return
		}
	}
}

// consoleHeartbeatLoop keeps the socket alive.
func consoleHeartbeatLoop(ctx context.Context, conn *phoenix.Conn, errc chan<- error) {
	t := time.NewTicker(consoleHeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := conn.Heartbeat(ctx); err != nil {
				errc <- err
				return
			}
		}
	}
}

func consoleData(b []byte) map[string]string {
	return map[string]string{"data": string(b)}
}

// consoleReceiveError maps a websocket read error to the clean-exit sentinel
// when it is a normal closure or context cancellation, and passes through any
// genuine failure.
func consoleReceiveError(err error) error {
	if phoenix.IsCleanClose(err) || errors.Is(err, context.Canceled) {
		return errConsoleDone
	}
	return err
}

// consoleTerminal returns stdin and stdout as files when both are interactive
// terminals.
func consoleTerminal(cmd *cobra.Command) (in, out *os.File, ok bool) {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil, nil, false
	}
	return in, out, true
}

// deviceSocketURL derives the device console websocket URL from the API base
// URL: the scheme is switched to ws/wss, the path becomes /api/socket/websocket,
// and the token is carried as a query parameter.
func deviceSocketURL(apiBase, token string) (string, error) {
	u, err := url.Parse(apiBase)
	if err != nil {
		return "", fmt.Errorf("invalid URI %q: %w", apiBase, err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "":
		return "", fmt.Errorf("URI %q must include a scheme", apiBase)
	default:
		return "", fmt.Errorf("unsupported URI scheme %q", u.Scheme)
	}
	u.Path = "/api/socket/websocket"
	// vsn selects the v2 JSON serializer (array frames); without it the server
	// defaults to v1 and cannot parse our messages.
	u.RawQuery = url.Values{"token": {token}, "vsn": {"2.0.0"}}.Encode()
	u.Fragment = ""
	return u.String(), nil
}
