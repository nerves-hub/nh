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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// orgCmd groups organization commands.
var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "Manage organizations",
	Long:  "Commands for working with the organizations you belong to.",
}

func init() {
	rootCmd.AddCommand(orgCmd)
}

// orgNameFromArgs resolves an organization name from a single optional
// positional argument, falling back to the configured org scope (and a Mix
// project) and erroring when neither is set.
func orgNameFromArgs(cfg *config.Config, args []string) (string, error) {
	var name string
	if len(args) == 1 {
		name = args[0]
	} else {
		name = resolveOrg(cfg)
	}
	if name == "" {
		return "", errors.New("no organization given: pass a name, or set --org, NERVES_HUB_ORG, or `nh config set org <name>`")
	}
	return name, nil
}
