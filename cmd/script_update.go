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

// scriptUpdateCmd implements `nh script update <id>`.
var scriptUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a support script",
	Long: `Update a support script's name, tags, or body.

Only the fields you supply (--name, --tags, --text/--text-file) are changed.`,
	Args: scriptIDArgs,
	RunE: runScriptUpdate,
}

func init() {
	addScriptContentFlags(scriptUpdateCmd)
	scriptCmd.AddCommand(scriptUpdateCmd)
}

func runScriptUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	// Only send the fields the user actually provided.
	var in api.SupportScriptInput
	changed := false
	if cmd.Flags().Changed("name") {
		in.Name, _ = cmd.Flags().GetString("name")
		changed = true
	}
	if cmd.Flags().Changed("tags") {
		in.Tags, _ = cmd.Flags().GetString("tags")
		changed = true
	}
	if text, textSet, err := resolveScriptText(cmd); err != nil {
		return err
	} else if textSet {
		in.Text = text
		changed = true
	}
	if !changed {
		return errors.New("nothing to update: pass --name, --tags, --text, or --text-file")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	script, err := client.UpdateSupportScript(cmd.Context(), org, product, id, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, script)
	}
	fmt.Fprintf(w, "Updated support script %s (id %s)\n", script.Name, script.ID)
	return nil
}
