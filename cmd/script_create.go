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

// scriptCreateCmd implements `nh script create`.
var scriptCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a support script",
	Long: `Create a support script in a product.

--name and the script body (--text or --text-file) are required; --tags is
optional.`,
	Args: cobra.NoArgs,
	RunE: runScriptCreate,
}

func init() {
	addScriptContentFlags(scriptCreateCmd)
	scriptCmd.AddCommand(scriptCreateCmd)
}

func runScriptCreate(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return errors.New("--name is required")
	}
	text, textSet, err := resolveScriptText(cmd)
	if err != nil {
		return err
	}
	if !textSet || text == "" {
		return errors.New("a script body is required: pass --text or --text-file")
	}
	tags, _ := cmd.Flags().GetString("tags")

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	script, err := client.CreateSupportScript(cmd.Context(), org, product, api.SupportScriptInput{
		Name: name,
		Tags: tags,
		Text: text,
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, script)
	}
	fmt.Fprintf(w, "Created support script %s (id %s)\n", script.Name, script.ID)
	return nil
}
