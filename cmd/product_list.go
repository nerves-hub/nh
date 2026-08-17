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

// productListCmd implements `nh product list`: show the products in an
// organization.
var productListCmd = &cobra.Command{
	Use:   "list",
	Short: "List products in an organization",
	Long:  "List the products within an organization (set with --org or NERVES_HUB_ORG).",
	Args:  cobra.NoArgs,
	RunE:  runProductList,
}

func init() {
	productCmd.AddCommand(productListCmd)
}

func runProductList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	products, err := client.ListProducts(cmd.Context(), org)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, products)
	}

	if len(products) == 0 {
		fmt.Fprintf(w, "No products found in %s.\n", org)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME")
	for _, p := range products {
		fmt.Fprintf(tw, "%s\n", p.Name)
	}
	return tw.Flush()
}
