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
	"os"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// keyDeleteCmd implements `nh key delete <name>`.
var keyDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a signing key",
	Long: `Delete a signing key from the organization (set with --org or NERVES_HUB_ORG) by its name.

Only the key registered with the server is deleted. Any local key files under
<data-dir>/keys/<org>/ (<name>.priv and <name>.pub for ed25519, <name>.rsa.pem
and <name>.rsa.pub.pem for secure_boot_v2_rsa) are left in place and listed,
so you can still sign with the key or delete them yourself.`,
	Args: keyNameArgs,
	RunE: runKeyDelete,
}

func init() {
	keyDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	keyCmd.AddCommand(keyDeleteCmd)
}

func runKeyDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete signing key %s from %s? [y/N]", name, org))
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

	if err := client.DeleteSigningKey(cmd.Context(), org, name); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Deleted signing key %s\n", name)
	if kept := localKeyFiles(cfg.DataDir, org, name); len(kept) > 0 {
		fmt.Fprintln(w, "Local key files were left in place:")
		for _, p := range kept {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	return nil
}

// localKeyFiles lists the local files nh holds for key name, of any scheme.
// A name that is not a safe filename has none.
func localKeyFiles(dataDir, org, name string) []string {
	if validKeyName(name) != nil {
		return nil
	}
	var found []string
	for _, p := range pki.LocalKeyPaths(dataDir, org, name) {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	return found
}
