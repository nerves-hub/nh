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
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/iroh"
	"github.com/nerves-hub/nh/internal/irohconsole"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// irohConsoleService is fixed: this command speaks to an IrohConsole endpoint.
const irohConsoleService = "iroh"

// irohDetachByte is Ctrl-], which detaches the local session. Unlike Ctrl-C it
// passes nothing to the device, so Ctrl-C reaches the remote IEx (its break
// menu) as usual — matching iroh_console's own client.
const irohDetachByte = 0x1d

var deviceIrohConsoleCmd = &cobra.Command{
	Use:   "iroh-console <identifier>",
	Short: "Open a remote IEx console to a device over iroh",
	Long: `Open an interactive IEx console to a device running an IrohConsole endpoint,
over a peer-to-peer iroh connection.

nh reads where to reach the device from its reported network identities, ensures
this machine's iroh endpoint is registered with the organization (so the hosted
relay authorizes it), connects, and bridges your terminal to the device.

nh keeps one iroh key per organization in its data directory, because NervesHub
lets a key belong to only one organization. Each is registered to you under an
instance named for this machine ("nh-<host>-<key prefix>"), so your other
machines register their own.

Devices authenticate however their IrohConsole.Auth adapter is configured —
supply the credential with --auth, or you will be prompted when the device
challenges. Press Ctrl-] to detach. Requires an interactive terminal.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceIrohConsole,
}

func init() {
	deviceIrohConsoleCmd.Flags().String("auth", "", "response to the device's auth challenge (password, TOTP code, …)")
	deviceIrohConsoleCmd.Flags().String("instance", "iroh_console", "which iroh endpoint of the device to connect to")
	deviceCmd.AddCommand(deviceIrohConsoleCmd)
}

func runDeviceIrohConsole(cmd *cobra.Command, args []string) error {
	identifier := args[0]
	instance := mustString(cmd, "instance")

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	// A console is only useful attached to a real terminal; fail before doing
	// any network work if there isn't one.
	in, out, ok := consoleTerminal(cmd)
	if !ok {
		return errors.New("an interactive terminal is required")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sk, err := iroh.EnsureOrgEndpoint(ctx, client, iroh.EndpointOptions{
		DataDir: cfg.DataDir,
		Org:     org,
		Notify:  cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}

	addr, err := deviceIrohAddr(ctx, client, org, product, identifier, instance)
	if err != nil {
		return err
	}

	stderr := cmd.ErrOrStderr()
	fmt.Fprintf(stderr, "Connecting to %s over iroh…\n", identifier)

	stream, cleanup, err := iroh.Dial(ctx, addr, sk, irohconsole.ALPN)
	if err != nil {
		return err
	}
	defer cleanup()

	session, err := irohconsole.Connect(stream, irohConsoleResponder(cmd))
	if err != nil {
		return err
	}
	defer session.Close()

	return runIrohTerminal(cmd, session, in, out, identifier)
}

// deviceIrohAddr works out where to dial the device's iroh console from the
// network identity it reported for instance. See iroh.DeviceAddrFromIdentity
// for which fields that reads.
func deviceIrohAddr(ctx context.Context, client *api.Client, org, product, identifier, instance string) (iroh.DeviceAddr, error) {
	ids, err := client.ListDeviceNetworkIdentities(ctx, org, product, identifier, api.DeviceNetworkIdentityFilter{
		Service:  irohConsoleService,
		Instance: instance,
	})
	if err != nil {
		return iroh.DeviceAddr{}, err
	}
	var firstErr error
	for _, id := range ids {
		addr, err := iroh.DeviceAddrFromIdentity(id.Identifier, id.Details)
		if err == nil {
			return addr, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return iroh.DeviceAddr{}, fmt.Errorf("device %s reported an iroh console endpoint (instance %q) that cannot be dialled: %w", identifier, instance, firstErr)
	}
	return iroh.DeviceAddr{}, fmt.Errorf("device %s has not reported an iroh console endpoint (instance %q); is IrohConsole running on it?", identifier, instance)
}

// irohConsoleResponder answers the device's auth challenge from --auth, or, when
// that is not set, by prompting — only if the device actually challenges.
func irohConsoleResponder(cmd *cobra.Command) irohconsole.Responder {
	if cmd.Flags().Changed("auth") {
		return irohconsole.StaticResponder(mustString(cmd, "auth"))
	}
	return func([]byte) (string, error) {
		return promptPassword(cmd, "Device console auth: ", false)
	}
}

// runIrohTerminal puts the local terminal in raw mode and bridges it to the
// session until the device closes or the user presses Ctrl-].
func runIrohTerminal(cmd *cobra.Command, session *irohconsole.Session, in, out *os.File, label string) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "Connected to %s. Press Ctrl-] to detach.\n", label)

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

	sendSize := func() {
		if w, h, err := term.GetSize(int(out.Fd())); err == nil {
			_ = session.Resize(w, h)
		}
	}
	sendSize()
	stopResize := onResize(sendSize)
	defer stopResize()

	errc := make(chan error, 2)
	go irohConsoleReadLoop(session, out, errc)
	go irohConsoleWriteLoop(session, in, errc)

	err = <-errc
	restore()

	fmt.Fprintf(cmd.ErrOrStderr(), "\nDisconnected from %s\n", label)
	if err == nil || errors.Is(err, errConsoleDone) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func irohConsoleReadLoop(session *irohconsole.Session, out *os.File, errc chan<- error) {
	for {
		data, err := session.Output()
		if err != nil {
			errc <- err
			return
		}
		if _, err := out.Write(data); err != nil {
			errc <- err
			return
		}
	}
}

func irohConsoleWriteLoop(session *irohconsole.Session, in *os.File, errc chan<- error) {
	buf := make([]byte, 4096)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			data := buf[:n]
			if i := bytes.IndexByte(data, irohDetachByte); i >= 0 {
				if i > 0 {
					_ = session.SendInput(data[:i])
				}
				errc <- errConsoleDone
				return
			}
			if serr := session.SendInput(data); serr != nil {
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
