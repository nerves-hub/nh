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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// orgInviteCmd implements `nh org invite <email> <role>`.
var orgInviteCmd = &cobra.Command{
	Use:   "invite <email> <role>",
	Short: "Invite a user to an organization",
	Long:  "Invite a user, by email, to the organization (set with --org or NERVES_HUB_ORG) with a role of admin, manage, or view.",
	Args: func(_ *cobra.Command, args []string) error {
		switch {
		case len(args) < 2:
			return errors.New("usage: org invite <email> <role>")
		case len(args) > 2:
			return errors.New("too many arguments: provide an email and a role")
		}
		return nil
	},
	RunE: runOrgInvite,
}

func init() {
	orgCmd.AddCommand(orgInviteCmd)
}

func runOrgInvite(cmd *cobra.Command, args []string) error {
	email := args[0]
	role := strings.ToLower(strings.TrimSpace(args[1]))

	if !isValidRole(role) {
		return fmt.Errorf("invalid role %q: use %s", args[1], strings.Join(orgRoles, ", "))
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.InviteOrgMember(cmd.Context(), org, email, role); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Invited %s to %s as %s\n", email, org, role)
	return nil
}
