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
	"strings"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// configLoadCmd implements `nh config load <name>`.
var configLoadCmd = &cobra.Command{
	Use:   "load <name>",
	Short: "Switch the active configuration to a saved profile",
	Long: `Make a profile saved with ` + "`nh config save`" + ` the active configuration,
replacing the current uri, org, product, and token.`,
	Args: exactlyOneArg(
		"profile name missing",
		"too many arguments: provide a single profile name",
	),
	RunE: runConfigLoad,
}

func init() {
	configCmd.AddCommand(configLoadCmd)
}

func runConfigLoad(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])

	cfg := config.From(cmd.Context())
	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	if err := settings.LoadProfile(name); err != nil {
		if names := settings.ProfileNames(); len(names) > 0 {
			return fmt.Errorf("%w (available: %s)", err, strings.Join(names, ", "))
		}
		return err
	}
	if err := config.SaveSettings(cfg.DataDir, settings); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Switched to profile %q\n", name)
	return nil
}
