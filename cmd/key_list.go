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

// keyListCmd implements `nh key list`: show an organization's signing keys.
var keyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List signing keys in an organization",
	Long:  "List the signing keys for the organization (set with --org or NERVES_HUB_ORG).",
	Args:  cobra.NoArgs,
	RunE:  runKeyList,
}

func init() {
	keyCmd.AddCommand(keyListCmd)
}

func runKeyList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	keys, err := client.ListSigningKeys(cmd.Context(), org)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, keys)
	}

	if len(keys) == 0 {
		fmt.Fprintf(w, "No signing keys found in %s.\n", org)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME\tPUBLIC KEY")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", k.Name, orDash(k.Key))
	}
	return tw.Flush()
}
