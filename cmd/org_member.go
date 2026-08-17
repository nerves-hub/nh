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

// orgMemberCmd implements `nh org member <email>`.
var orgMemberCmd = &cobra.Command{
	Use:   "member <email>",
	Short: "Show an organization member",
	Long: `Show membership details (name, email, role) for a single member of an
organization, by email.

The organization comes from --org, NERVES_HUB_ORG, or your saved settings.`,
	Args: exactlyOneArg(
		"member email missing",
		"too many arguments: provide a single member email",
	),
	RunE: runOrgMember,
}

func init() {
	orgCmd.AddCommand(orgMemberCmd)
}

func runOrgMember(cmd *cobra.Command, args []string) error {
	email := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	member, err := client.GetOrgMember(cmd.Context(), org, email)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, member)
	}

	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Name:\t%s\n", orDash(member.Name))
	fmt.Fprintf(tw, "Email:\t%s\n", member.Email)
	fmt.Fprintf(tw, "Role:\t%s\n", member.Role)
	return tw.Flush()
}
