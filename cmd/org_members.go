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

// orgMembersCmd implements `nh org members [name]`.
var orgMembersCmd = &cobra.Command{
	Use:     "members [name]",
	Aliases: []string{"users"},
	Short:   "List members of an organization",
	Long: `List the members of an organization you belong to.

The name is optional; when omitted it defaults to the configured organization
(--org, NERVES_HUB_ORG, or your saved settings).`,
	Args: atMostOneArg("too many arguments: provide a single organization name"),
	RunE: runOrgMembers,
}

func init() {
	orgCmd.AddCommand(orgMembersCmd)
}

func runOrgMembers(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())

	name, err := orgNameFromArgs(cfg, args)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	members, err := client.ListOrgMembers(cmd.Context(), name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, members)
	}

	if len(members) == 0 {
		fmt.Fprintf(w, "No members found in %s.\n", name)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME\tEMAIL\tROLE")
	for _, m := range members {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", orDash(m.Name), m.Email, m.Role)
	}
	return tw.Flush()
}
