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
	"io"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// productShowCmd implements `nh product show <name>`.
var productShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details for a product",
	Long: `Show a single product by its name, with its settings: the firmware
formats it accepts, whether firmware versions must be unique, whether unsigned
ESP-IDF and AtomVM firmware is accepted, and whether new devices may manage
their own updates. Change them with ` + "`nh product update`" + `.`,
	Args: productNameArgs,
	RunE: runProductShow,
}

func init() {
	productCmd.AddCommand(productShowCmd)
}

func runProductShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	product, err := client.GetProduct(cmd.Context(), org, name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, product)
	}

	return printProductDetail(w, product)
}

// printProductDetail writes a product's name and settings as a two-column
// table. Shared by show and update. A setting the server did not report
// (one older than product settings) is left out rather than shown as off.
func printProductDetail(w io.Writer, product *api.Product) error {
	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("Name:", product.Name)
	if product.AllowedUpdateTools != nil {
		row("Update tools:", updateToolsText(product.AllowedUpdateTools, ", "))
	}
	row("Unique firmware versions:", boolWord(product.RequireUniqueFirmwareVersion, "required", "not required"))
	row("Unsigned ESP-IDF firmware:", boolWord(product.AllowUnsignedESPIDFFirmware, "allowed", "refused"))
	row("Unsigned AtomVM firmware:", boolWord(product.AllowUnsignedAtomVMFirmware, "allowed", "refused"))
	row("Device-managed updates (new devices):", boolWord(product.DefaultManagedUpdatesAllowed, "allowed", "not allowed"))
	return tw.Flush()
}

// updateToolsText joins tools with sep. A list the server reports but leaves
// empty means the product accepts no firmware, so it reads "none" rather than
// disappearing.
func updateToolsText(tools []string, sep string) string {
	if len(tools) == 0 {
		return "none"
	}
	return strings.Join(tools, sep)
}

// boolWord renders an optional setting: yes or no by its value, or "" when
// the server did not report it.
func boolWord(v *bool, yes, no string) string {
	switch {
	case v == nil:
		return ""
	case *v:
		return yes
	default:
		return no
	}
}
