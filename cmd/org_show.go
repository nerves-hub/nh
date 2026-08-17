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
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// orgShowCmd implements `nh org show [name]`.
var orgShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show details for an organization",
	Long: `Show details for a single organization you belong to.

The name is optional; when omitted it defaults to the configured organization
(--org, NERVES_HUB_ORG, or your saved settings).`,
	Args: atMostOneArg("too many arguments: provide a single organization name"),
	RunE: runOrgShow,
}

func init() {
	orgCmd.AddCommand(orgShowCmd)
}

func runOrgShow(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())

	name, err := orgNameFromArgs(cfg, args)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	org, err := client.GetOrg(cmd.Context(), name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, org)
	}

	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("Name:", org.Name)
	if !org.InsertedAt.IsZero() {
		row("Created:", org.InsertedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if !org.UpdatedAt.IsZero() {
		row("Updated:", org.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
	}
	row("Products:", orgProductNames(org.Products))
	return tw.Flush()
}

// orgProductNames joins product names for display, or "-" when there are none.
func orgProductNames(products []api.OrgProduct) string {
	if len(products) == 0 {
		return "-"
	}
	names := make([]string, len(products))
	for i, p := range products {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}
