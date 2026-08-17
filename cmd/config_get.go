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

// configGetCmd implements `nh config get [key]`: print all persisted
// settings, or just the value of a single key.
var configGetCmd = &cobra.Command{
	Use:       "get [key]",
	Short:     "Show persisted defaults",
	Long:      "Print all persisted settings, or the value of a single key (org or product).",
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: config.SettingKeys,
	RunE:      runConfigGet,
}

func init() {
	configCmd.AddCommand(configGetCmd)
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())

	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	// Single key: print just the value, for easy scripting.
	if len(args) == 1 {
		value, err := settings.Get(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(w, value)
		return nil
	}

	if cfg.Output == config.OutputJSON {
		// `nh config get` reports only the active uri/org/product; it never
		// exposes the saved token or other profiles' credentials.
		public := *settings
		public.Token = ""
		public.Profiles = nil
		return printJSON(w, public)
	}

	tw := newTableWriter(w)
	for _, key := range config.SettingKeys {
		value, _ := settings.Get(key)
		fmt.Fprintf(tw, "%s\t%s\n", key, value)
	}
	return tw.Flush()
}
