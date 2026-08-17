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
	"io"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// stderrIsTerminal reports whether the command's stderr is an interactive
// terminal (as opposed to a pipe, file, or test buffer).
func stderrIsTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.ErrOrStderr().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// newTransferBar builds a byte-oriented progress bar for a transfer of total
// bytes (pass <= 0 when the size is unknown, which renders a spinner),
// rendering to w. The bar is cleared from the terminal when finished, leaving
// only the command's own completion message.
func newTransferBar(w io.Writer, description string, total int64) *progressbar.ProgressBar {
	if total <= 0 {
		total = -1
	}
	return progressbar.NewOptions64(total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(w),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(20),
		progressbar.OptionThrottle(90*time.Millisecond),
		progressbar.OptionClearOnFinish(),
	)
}

// finishBar finalizes a progress bar, clearing its line. It is safe to call
// with a nil bar.
func finishBar(bar *progressbar.ProgressBar) {
	if bar != nil {
		_ = bar.Finish()
	}
}
