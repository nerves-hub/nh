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

// configSaveCmd implements `nh config save <name>`.
var configSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save the active configuration as a named profile",
	Long: `Save the active configuration (uri, org, product, and token) as a named
profile. Restore it later with ` + "`nh config load <name>`" + `. An existing
profile with the same name is replaced.`,
	Args: exactlyOneArg(
		"profile name missing",
		"too many arguments: provide a single profile name",
	),
	RunE: runConfigSave,
}

func init() {
	configCmd.AddCommand(configSaveCmd)
}

func runConfigSave(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if name == "" {
		return errors.New("profile name missing")
	}

	cfg := config.From(cmd.Context())
	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	settings.SaveProfile(name)
	if err := config.SaveSettings(cfg.DataDir, settings); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Saved the active configuration as profile %q\n", name)
	return nil
}
