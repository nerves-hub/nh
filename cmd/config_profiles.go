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

// configProfilesCmd implements `nh config profiles`.
var configProfilesCmd = &cobra.Command{
	Use:     "profiles",
	Aliases: []string{"list"},
	Short:   "List saved configuration profiles",
	Long: `List the configuration profiles saved with ` + "`nh config save`" + `. The token
value is never shown — only whether a profile has one.`,
	Args: cobra.NoArgs,
	RunE: runConfigProfiles,
}

func init() {
	configCmd.AddCommand(configProfilesCmd)
}

// profileView is the token-redacted form of a profile used for output.
type profileView struct {
	URI      string `json:"uri,omitempty"`
	Org      string `json:"org,omitempty"`
	Product  string `json:"product,omitempty"`
	HasToken bool   `json:"has_token"`
}

func runConfigProfiles(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}
	names := settings.ProfileNames()

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		view := make(map[string]profileView, len(names))
		for _, name := range names {
			p := settings.Profiles[name]
			view[name] = profileView{URI: p.URI, Org: p.Org, Product: p.Product, HasToken: p.Token != ""}
		}
		return printJSON(w, view)
	}

	if len(names) == 0 {
		fmt.Fprintln(w, "No profiles saved.")
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME\tURI\tORG\tPRODUCT\tTOKEN")
	for _, name := range names {
		p := settings.Profiles[name]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			name, orDash(p.URI), orDash(p.Org), orDash(p.Product), yesNo(p.Token != ""))
	}
	return tw.Flush()
}

// yesNo renders a boolean as "yes"/"no" for table output.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
