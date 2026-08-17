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

import "strings"

// Build metadata injected at release time via -ldflags -X (see
// .goreleaser.yaml). The version itself lives in helpers.go, where it also
// feeds the API User-Agent.
var (
	commit = ""
	date   = ""
)

func init() {
	rootCmd.Version = buildVersion()
	// Print just the version line for `nh --version`, without the usage banner.
	rootCmd.SetVersionTemplate("nh {{.Version}}\n")
}

// buildVersion assembles the version string shown by `nh --version`, appending
// the commit and build date when a release injected them.
func buildVersion() string {
	var b strings.Builder
	b.WriteString(version)
	if commit != "" {
		b.WriteString(" (")
		b.WriteString(commit)
		b.WriteString(")")
	}
	if date != "" {
		b.WriteString(" ")
		b.WriteString(date)
	}
	return b.String()
}
