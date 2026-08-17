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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// firmwareListCmd implements `nh firmware list`: show the firmwares in a
// product.
var firmwareListCmd = &cobra.Command{
	Use:   "list",
	Short: "List firmware in a product",
	Long: `List the firmware within a product.

The org and product are taken from --org/--product, the NERVES_HUB_ORG/
NERVES_HUB_PRODUCT environment variables, or your saved settings.`,
	Args: cobra.NoArgs,
	RunE: runFirmwareList,
}

func init() {
	firmwareCmd.AddCommand(firmwareListCmd)
}

func runFirmwareList(cmd *cobra.Command, _ []string) error {
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

	firmwares, err := client.ListFirmwares(cmd.Context(), org, product)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, firmwares)
	}

	if len(firmwares) == 0 {
		fmt.Fprintf(w, "No firmware found in %s/%s.\n", org, product)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "VERSION\tPLATFORM\tARCHITECTURE\tUUID")
	for _, f := range firmwares {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			orDash(f.Version), orDash(f.Platform), orDash(f.Architecture), f.UUID)
	}
	return tw.Flush()
}
