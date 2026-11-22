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
	"errors"
	"fmt"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// productUpdateCmd implements `nh product update <name>`.
var productUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Change a product's settings",
	Long: `Change a product's settings. Only the settings you pass are sent; the
rest are left as they are. A product cannot be renamed.

Firmware formats (update tools) are fwup, esp-idf, atomvm and rauc. Every
product accepts fwup, and fwup is always kept. A format other than fwup must
also be enabled on the NervesHub instance (ESP_IDF_FIRMWARE_ENABLED,
ATOMVM_FIRMWARE_ENABLED or RAUC_FIRMWARE_ENABLED) before a product can accept
it.

  --allow-tool / --disallow-tool add or remove formats from the product's
  current list. Both can be repeated or given comma-separated values. The
  command reads the current list and sends the whole new one, so a change
  someone else makes in between is overwritten.

  --tools sets the whole list, so it also removes any format you leave out.
  Pass --tools fwup to accept fwup firmware alone.

Each yes/no setting takes true or false; the flag on its own means true.`,
	Example: `  # Accept ESP-IDF firmware, including images without a Secure Boot signature
  nh product update thermostat --allow-tool esp-idf --allow-unsigned-esp-idf

  # Stop accepting AtomVM firmware
  nh product update thermostat --disallow-tool atomvm

  # Accept exactly fwup and RAUC
  nh product update thermostat --tools fwup,rauc

  # Allow reusing a firmware version
  nh product update thermostat --require-unique-firmware-version=false`,
	Args: productNameArgs,
	RunE: runProductUpdate,
}

func init() {
	f := productUpdateCmd.Flags()
	f.StringSlice("tools", nil, "set the full list of accepted firmware formats (fwup is always kept)")
	f.StringSlice("allow-tool", nil, "accept this firmware format as well (repeatable, or comma-separated)")
	f.StringSlice("disallow-tool", nil, "stop accepting this firmware format (repeatable, or comma-separated)")
	f.Bool("allow-unsigned-esp-idf", false, "accept ESP-IDF images with no Secure Boot v2 signature")
	f.Bool("allow-unsigned-atomvm", false, "accept AtomVM packbeam archives with no signature")
	f.Bool("require-unique-firmware-version", false, "reject firmware whose version already exists for the product")
	f.Bool("default-managed-updates-allowed", false, "let devices created in this product manage their own updates")
	productUpdateCmd.MarkFlagsMutuallyExclusive("tools", "allow-tool")
	productUpdateCmd.MarkFlagsMutuallyExclusive("tools", "disallow-tool")
	productCmd.AddCommand(productUpdateCmd)
}

func runProductUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	flags := cmd.Flags()
	var in api.ProductUpdate
	boolFlag := func(flag string) *bool {
		if !flags.Changed(flag) {
			return nil
		}
		v, _ := flags.GetBool(flag)
		return &v
	}
	in.AllowUnsignedESPIDFFirmware = boolFlag("allow-unsigned-esp-idf")
	in.AllowUnsignedAtomVMFirmware = boolFlag("allow-unsigned-atomvm")
	in.RequireUniqueFirmwareVersion = boolFlag("require-unique-firmware-version")
	in.DefaultManagedUpdatesAllowed = boolFlag("default-managed-updates-allowed")

	if flags.Changed("tools") {
		tools, _ := flags.GetStringSlice("tools")
		if strings.TrimSpace(strings.Join(tools, "")) == "" {
			// Most likely an unset shell variable; resetting to fwup alone
			// should be asked for by name.
			return errors.New("--tools needs at least one format; pass --tools fwup to accept fwup alone")
		}
		in.AllowedUpdateTools = api.WithFwup(tools)
	}
	allow, _ := flags.GetStringSlice("allow-tool")
	disallow, _ := flags.GetStringSlice("disallow-tool")
	changeTools := flags.Changed("allow-tool") || flags.Changed("disallow-tool")
	if changeTools {
		// Fail on a contradictory or fwup-removing request before any request.
		if err := api.CheckUpdateToolChanges(allow, disallow); err != nil {
			return err
		}
	}

	if in.IsEmpty() && !changeTools {
		return errors.New("nothing to update: pass --tools, --allow-tool, --disallow-tool, " +
			"--allow-unsigned-esp-idf, --allow-unsigned-atomvm, " +
			"--require-unique-firmware-version or --default-managed-updates-allowed")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if changeTools {
		// The server takes the whole list, so start from the current one.
		current, err := client.GetProduct(cmd.Context(), org, name)
		if err != nil {
			return err
		}
		in.AllowedUpdateTools, err = api.ChangeUpdateTools(current.AllowedUpdateTools, allow, disallow)
		if err != nil {
			return err
		}
	}

	product, err := client.UpdateProduct(cmd.Context(), org, name, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, product)
	}
	fmt.Fprintf(w, "Updated product %s in %s\n", product.Name, org)
	return printProductDetail(w, product)
}
