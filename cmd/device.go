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
	"context"
	"fmt"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceCmd groups device commands.
var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage devices",
	Long:  "Commands for working with devices within a product.",
}

func init() {
	rootCmd.AddCommand(deviceCmd)
}

// deviceIdentifierArgs requires exactly one device identifier, with friendly
// messages. Shared by the device subcommands that take an identifier.
var deviceIdentifierArgs = exactlyOneArg(
	"Device identifier missing",
	"too many arguments: provide a single device identifier",
)

// runDeviceAction resolves the org/product scope and an authenticated client,
// invokes a no-body device action (e.g. reboot, reconnect), and reports it.
// action is the human-readable verb used in the confirmation message.
func runDeviceAction(cmd *cobra.Command, identifier, action string,
	call func(*api.Client, context.Context, string, string, string) error) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := call(client, cmd.Context(), org, product, identifier); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s requested for device %s\n", action, identifier)
	return nil
}
