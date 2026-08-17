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
	"io"
	"os"

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

// addScriptContentFlags registers the flags shared by `script create` and
// `script update`.
func addScriptContentFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "script name")
	cmd.Flags().String("tags", "", "comma-separated tags")
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
