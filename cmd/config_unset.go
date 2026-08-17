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

// configUnsetCmd implements `nh config unset <key>`: clear a persisted
// default.
var configUnsetCmd = &cobra.Command{
	Use:       "unset <key>",
	Short:     "Clear a default (org or product)",
	Long:      "Clear a persisted default. Valid keys are org and product.",
	Args:      cobra.ExactArgs(1),
	ValidArgs: config.SettingKeys,
	RunE:      runConfigUnset,
}

func init() {
	configCmd.AddCommand(configUnsetCmd)
}

func runConfigUnset(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	key := args[0]

	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}
	if err := settings.Set(key, ""); err != nil {
		return err
	}
	if err := config.SaveSettings(cfg.DataDir, settings); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Unset %s\n", key)
	return nil
}
