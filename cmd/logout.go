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

// logoutCmd implements `nh user logout`: remove the locally saved API token.
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the saved API token",
	Long:  "Remove the locally saved API token from the settings file in the data directory.",
	Args:  cobra.NoArgs,
	RunE:  runLogout,
}

func init() {
	userCmd.AddCommand(logoutCmd)
}

func runLogout(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())

	token, err := config.LoadToken(cfg.DataDir)
	if err != nil {
		return err
	}

	if err := config.DeleteToken(cfg.DataDir); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if token != "" {
		fmt.Fprintf(w, "Logged out; removed the saved token from %s\n", config.SettingsFilePath(cfg.DataDir))
	} else {
		fmt.Fprintln(w, "No saved token to remove")
	}
	return nil
}
