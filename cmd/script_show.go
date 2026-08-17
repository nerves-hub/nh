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

// scriptShowCmd implements `nh script show <id>`.
var scriptShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show details for a support script",
	Long:  "Show the full details, including the script body, for a support script by its id.",
	Args:  scriptIDArgs,
	RunE:  runScriptShow,
}

func init() {
	scriptCmd.AddCommand(scriptShowCmd)
}

func runScriptShow(cmd *cobra.Command, args []string) error {
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

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	script, err := client.GetSupportScript(cmd.Context(), org, product, id)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, script)
	}

	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("ID:", script.ID)
	row("Name:", script.Name)
	row("Tags:", script.Tags)
	if script.CreatedBy != nil {
		row("Created by:", script.CreatedBy.Name)
	}
	if script.InsertedAt != nil && !script.InsertedAt.IsZero() {
		row("Created:", script.InsertedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if script.UpdatedAt != nil && !script.UpdatedAt.IsZero() {
		row("Updated:", script.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
	}
	tw.Flush()

	if script.Text != "" {
		fmt.Fprintf(w, "\nText:\n%s\n", script.Text)
	}
	return nil
}
