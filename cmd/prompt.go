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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// stdinIsTerminal reports whether the command's stdin is an interactive
// terminal (as opposed to a pipe or file).
func stdinIsTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// confirm asks a yes/no question (defaulting to no) and returns true only for
// an affirmative answer. The prompt should include its own "[y/N]" hint.
func confirm(cmd *cobra.Command, prompt string) (bool, error) {
	line, err := promptLine(cmd, prompt+" ", false)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// promptLine writes prompt to stderr (when not quiet) and reads a single line
// from stdin, returning it with the trailing newline stripped.
func promptLine(cmd *cobra.Command, prompt string, quiet bool) (string, error) {
	if !quiet {
		fmt.Fprint(cmd.ErrOrStderr(), prompt)
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptPassword reads a password from stdin. When stdin is a terminal the
// input is read without echoing; otherwise (piped input) a line is read.
// Prompts are suppressed when quiet is true.
func promptPassword(cmd *cobra.Command, prompt string, quiet bool) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if !quiet {
			fmt.Fprint(cmd.ErrOrStderr(), prompt)
		}
		b, err := term.ReadPassword(int(f.Fd()))
		if !quiet {
			fmt.Fprintln(cmd.ErrOrStderr())
		}
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return promptLine(cmd, prompt, quiet)
}
