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

	"github.com/spf13/cobra"
)

// archiveCmd groups archive commands.
var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Manage archives",
	Long: `Commands for working with archives within a product.

An archive is a supporting fwup file attached to a product. A deployment group
release can include one, and devices on that release download it when they
connect. Archives are attached to deployment groups in the web UI.

NervesHub's API has no endpoint for deleting an archive.`,
}

func init() {
	rootCmd.AddCommand(archiveCmd)
}

// archiveSize formats a byte count for display, e.g. "1.5 MiB".
func archiveSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, exp := float64(n)/unit, 0
	// Move up a unit once the value would print as 1024.0, so a size just
	// under 1 MiB reads "1.0 MiB" rather than "1024.0 KiB".
	for v >= unit-0.05 && exp < 5 {
		v /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", v, "KMGTPE"[exp])
}
