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
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/spf13/cobra"
)

// scriptCmd groups support-script commands.
var scriptCmd = &cobra.Command{
	Use:     "script",
	Aliases: []string{"support-script"},
	Short:   "Manage support scripts",
	Long:    "Commands for working with a product's support scripts.",
}

func init() {
	rootCmd.AddCommand(scriptCmd)
}

// scriptIDArgs requires exactly one support script id, with friendly messages.
var scriptIDArgs = exactlyOneArg(
	"Support script id missing",
	"too many arguments: provide a single support script id",
)

// parseScriptID parses a support script id argument. The server only looks up
// positive integer ids, so anything else is refused before a request is made.
func parseScriptID(arg string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid support script id %q: ids are positive integers (see `nh script list`)", arg)
	}
	return id, nil
}

// formatScriptTags renders a script's tags the way --tags takes them.
func formatScriptTags(tags []string) string {
	return strings.Join(tags, ",")
}

// writeScriptTable prints scripts as an ID/NAME/LANGUAGE/TAGS table.
func writeScriptTable(w io.Writer, scripts []api.SupportScript) error {
	tw := newTableWriter(w)
	fmt.Fprintln(tw, "ID\tNAME\tLANGUAGE\tTAGS")
	for _, s := range scripts {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", s.ID, orDash(s.Name), orDash(s.Language), orDash(formatScriptTags(s.Tags)))
	}
	return tw.Flush()
}

// warnIfLanguageIgnored warns when --language was sent but the server's reply
// carries no language: servers older than script languages drop the field.
func warnIfLanguageIgnored(cmd *cobra.Command, script *api.SupportScript) {
	if cmd.Flags().Changed("language") && script.Language == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: the server did not report a script language, so it may not support --language and ignored it")
	}
}

// addScriptContentFlags registers the flags shared by `script create` and
// `script update`.
func addScriptContentFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "script name")
	cmd.Flags().String("tags", "", "comma-separated tags (on update, an empty value clears them)")
	cmd.Flags().String("language", "", "how the device runs the body: elixir (server default) or shell")
	cmd.Flags().String("text", "", "script body")
	cmd.Flags().String("text-file", "", "read the script body from a file (use - for stdin)")
}

// resolveScriptText returns the script body from --text or --text-file, and
// whether either was provided.
func resolveScriptText(cmd *cobra.Command) (text string, set bool, err error) {
	textSet := cmd.Flags().Changed("text")
	fileSet := cmd.Flags().Changed("text-file")
	switch {
	case textSet && fileSet:
		return "", false, errors.New("use only one of --text or --text-file")
	case textSet:
		t, _ := cmd.Flags().GetString("text")
		return t, true, nil
	case fileSet:
		path, _ := cmd.Flags().GetString("text-file")
		b, err := readFileOrStdin(cmd, path)
		if err != nil {
			return "", false, err
		}
		return string(b), true, nil
	default:
		return "", false, nil
	}
}

// readFileOrStdin reads path, or stdin when path is "-".
func readFileOrStdin(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}
