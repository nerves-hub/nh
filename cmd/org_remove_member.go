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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// orgRemoveMemberCmd implements `nh org remove-member <email>`.
var orgRemoveMemberCmd = &cobra.Command{
	Use:   "remove-member <email>",
	Short: "Remove a member from an organization",
	Long:  "Remove a member, by their email, from the organization (set with --org or NERVES_HUB_ORG).",
	Args: exactlyOneArg(
		"Member email missing",
		"too many arguments: provide a single member email",
	),
	RunE: runOrgRemoveMember,
}

func init() {
	orgRemoveMemberCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	orgCmd.AddCommand(orgRemoveMemberCmd)
}

func runOrgRemoveMember(cmd *cobra.Command, args []string) error {
	email := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to remove a member without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Remove %s from %s? [y/N]", email, org))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.RemoveOrgMember(cmd.Context(), org, email); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s\n", email, org)
	return nil
}
