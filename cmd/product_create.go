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
	"unicode"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// productCreateCmd implements `nh product create <name>`.
var productCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a product",
	Long: `Create a product in the organization (set with --org or NERVES_HUB_ORG).

The name must not contain whitespace.`,
	Args: productNameArgs,
	RunE: runProductCreate,
}

func init() {
	productCmd.AddCommand(productCreateCmd)
}

func runProductCreate(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	name := args[0]
	if strings.ContainsFunc(name, unicode.IsSpace) {
		return errors.New("product name must not contain whitespace")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	product, err := client.CreateProduct(cmd.Context(), org, name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, product)
	}

	fmt.Fprintf(w, "Created product %s in %s\n", name, org)
	return nil
}
