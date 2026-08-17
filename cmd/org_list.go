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

// orgListCmd implements `nh org list`: show the organizations the
// authenticated user belongs to.
var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations you belong to",
	Long:  "List the organizations the authenticated user belongs to.",
	Args:  cobra.NoArgs,
	RunE:  runOrgList,
}

func init() {
	orgCmd.AddCommand(orgListCmd)
}

func runOrgList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	orgs, err := client.ListOrgs(cmd.Context())
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, orgs)
	}

	if len(orgs) == 0 {
		fmt.Fprintln(w, "No organizations found.")
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME\tCREATED")
	for _, o := range orgs {
		fmt.Fprintf(tw, "%s\t%s\n", o.Name, o.InsertedAt.Format("2006-01-02"))
	}
	return tw.Flush()
}
