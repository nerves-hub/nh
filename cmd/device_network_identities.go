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
	"fmt"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceNetworkIdentitiesCmd implements
// `nh device network-identities <identifier>`.
var deviceNetworkIdentitiesCmd = &cobra.Command{
	Use:     "network-identities <identifier>",
	Aliases: []string{"identities"},
	Short:   "List the network identities a device holds",
	Long: `List the keys a device has reported holding on other networks — an iroh
endpoint id, a NetBird, Tailscale or WireGuard public key.

--service filters by protocol (e.g. iroh); --instance filters to one endpoint of
that protocol. This view is read-only: a device reports its own identities over
its own connection.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceNetworkIdentities,
}

func init() {
	deviceNetworkIdentitiesCmd.Flags().String("service", "", "only identities for this protocol (e.g. iroh)")
	deviceNetworkIdentitiesCmd.Flags().String("instance", "", "only this endpoint of the protocol")
	deviceCmd.AddCommand(deviceNetworkIdentitiesCmd)
}

func runDeviceNetworkIdentities(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	identities, err := client.ListDeviceNetworkIdentities(cmd.Context(), org, product, identifier, api.DeviceNetworkIdentityFilter{
		Service:  mustString(cmd, "service"),
		Instance: mustString(cmd, "instance"),
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, identities)
	}

	if len(identities) == 0 {
		fmt.Fprintf(w, "No network identities found for device %s.\n", identifier)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "IDENTIFIER\tSERVICE\tINSTANCE\tSOURCE\tLAST REPORTED")
	for _, e := range identities {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.Identifier, orDash(e.Service), orDash(e.Instance), orDash(e.Source), certDate(e.LastReportedAt))
	}
	return tw.Flush()
}
