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

// orgRoles are the built-in organization member roles. They are the only
// roles the API's invite and update endpoints accept: custom roles can be
// assigned only in the web UI.
var orgRoles = []string{"admin", "manage", "view"}

// invalidRoleError explains which roles the CLI can assign.
func invalidRoleError(role string) error {
	return fmt.Errorf("invalid role %q: use %s (custom roles can only be assigned in the NervesHub web UI)", role, strings.Join(orgRoles, ", "))
}

// orgSetRoleCmd implements `nh org set-role <email> <role>`.
var orgSetRoleCmd = &cobra.Command{
	Use:   "set-role <email> <role>",
	Short: "Change a member's role in an organization",
	Long: `Change a member's role (admin, manage, or view) in the organization (set
with --org or NERVES_HUB_ORG).

The API only assigns built-in roles. Giving a member who holds a custom role a
built-in one replaces their custom role; to assign a custom role, use the
NervesHub web UI.`,
	Args: func(_ *cobra.Command, args []string) error {
		switch {
		case len(args) < 2:
			return errors.New("usage: org set-role <email> <role>")
		case len(args) > 2:
			return errors.New("too many arguments: provide an email and a role")
		}
		return nil
	},
	RunE: runOrgSetRole,
}

func init() {
	orgCmd.AddCommand(orgSetRoleCmd)
}

func runOrgSetRole(cmd *cobra.Command, args []string) error {
	email := args[0]
	role := strings.ToLower(strings.TrimSpace(args[1]))

	if !isValidRole(role) {
		return invalidRoleError(args[1])
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

	member, err := client.UpdateOrgMemberRole(cmd.Context(), org, email, role)
	if err != nil {
		return err
	}

	if cfg.Output == config.OutputJSON {
		return printJSON(cmd.OutOrStdout(), member)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s role to %s in %s\n", email, role, org)
	return nil
}

func isValidRole(role string) bool {
	for _, r := range orgRoles {
		if r == role {
			return true
		}
	}
	return false
}
