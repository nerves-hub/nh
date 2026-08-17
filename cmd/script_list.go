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

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// scriptListCmd implements `nh script list`.
var scriptListCmd = &cobra.Command{
	Use:   "list",
	Short: "List support scripts in a product",
	Long: `List the support scripts within a product.

The org and product are taken from --org/--product, the NERVES_HUB_ORG/
NERVES_HUB_PRODUCT environment variables, or your saved settings.`,
	Args: cobra.NoArgs,
	RunE: runScriptList,
}

func init() {
	scriptListCmd.Flags().Int("page", 0, "page number to fetch")
	scriptListCmd.Flags().Int("page-size", 0, "number of scripts per page")
	scriptCmd.AddCommand(scriptListCmd)
}

func runScriptList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	page, _ := cmd.Flags().GetInt("page")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	if page < 0 || pageSize < 0 {
		return errors.New("--page and --page-size must not be negative")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	result, err := client.ListSupportScripts(cmd.Context(), org, product, api.ListSupportScriptsOptions{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, result)
	}

	if p := result.Pagination; p != nil && p.HasInfo() {
		fmt.Fprintf(w, "Page %d of %d — %d script(s) total\n\n", p.Page, p.TotalPages, p.TotalCount)
	}

	if len(result.Scripts) == 0 {
		fmt.Fprintf(w, "No support scripts found in %s/%s.\n", org, product)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "ID\tNAME\tTAGS")
	for _, s := range result.Scripts {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.ID, s.Name, orDash(s.Tags))
	}
	return tw.Flush()
}
