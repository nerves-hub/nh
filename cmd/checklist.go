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
	"io"
	"os"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// checklistCmd implements `nh checklist [product]`.
var checklistCmd = &cobra.Command{
	Use:   "checklist [product]",
	Short: "Report which NervesHub features a product is using",
	Long: `Report how much of NervesHub a product is using, as a checklist: the same
report as the Utilization tab of the product's settings.

It opens with the score, the recommended features in use out of those the
server offers, and how the product's devices authenticate: a shared secret is
marked as one to move away from, and certificates (LocalCert, NervesKey) as
where to be. Then each recommended feature, and after those the optional ones,
which are situational and don't count towards the score.

  ✓  in use
  !  set up, but risky or not showing up yet (a warning)
  ○  not in use yet (never a warning)
  -  not available on this server (and so not scored)

Recommended features that aren't in use are followed by a link to their
documentation; --verbose describes every feature and links them all. The product defaults to
the one set with --product, NERVES_HUB_PRODUCT or ` + "`nh config set product`" + `.

The server judges each feature from what it can see of the product, so
firmware configuration that leaves no trace there is not on the list.`,
	Example: `  nh checklist thermostat
  nh checklist thermostat --verbose
  nh checklist thermostat -o json`,
	Args: atMostOneArg("too many arguments: provide a single product name"),
	RunE: runChecklist,
}

func init() {
	checklistCmd.Flags().BoolP("verbose", "v", false, "describe every feature and link its documentation")
	checklistCmd.Flags().String("color", "auto", "colour the report: auto (when writing to a terminal), always or never")
	rootCmd.AddCommand(checklistCmd)
}

func runChecklist(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	var product string
	if len(args) == 1 {
		product = args[0]
	} else if product, err = requireProduct(cfg); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	colorMode, _ := cmd.Flags().GetString("color")
	color, err := useColor(colorMode, w)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	report, err := client.GetUtilization(cmd.Context(), org, product)
	if err != nil {
		return err
	}

	if cfg.Output == config.OutputJSON {
		return printJSON(w, report)
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	return printChecklist(w, org, product, report, checklistStyle{color: color, verbose: verbose})
}

// useColor resolves --color. "auto" colours only a terminal, and never when
// NO_COLOR is set (https://no-color.org).
func useColor(mode string, w io.Writer) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if os.Getenv("NO_COLOR") != "" {
			return false, nil
		}
		f, ok := w.(*os.File)
		return ok && term.IsTerminal(int(f.Fd())), nil
	default:
		return false, fmt.Errorf("invalid --color %q: use auto, always or never", mode)
	}
}

// ANSI colours for the report. Orange is from the 256-colour palette, which
// has no orange among its first sixteen.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiOrange = "\x1b[38;5;208m"
)

type checklistStyle struct {
	color   bool
	verbose bool
}

func (s checklistStyle) paint(code, text string) string {
	if !s.color {
		return text
	}
	return code + text + ansiReset
}

// mark is the symbol for an item's status, coloured to match.
func (s checklistStyle) mark(status string) string {
	switch status {
	case api.UtilizationDone:
		return s.paint(ansiGreen, "✓")
	case api.UtilizationAttention:
		return s.paint(ansiOrange, "!")
	case api.UtilizationUnavailable:
		return s.paint(ansiDim, "-")
	default:
		return s.paint(ansiDim, "○")
	}
}

// printChecklist writes the report: the score, device authentication, the
// recommended items and then the optional ones.
func printChecklist(w io.Writer, org, product string, report *api.Utilization, style checklistStyle) error {
	var b strings.Builder

	fmt.Fprintf(&b, "%s  %s\n",
		style.paint(ansiBold, fmt.Sprintf("Utilization of %s/%s", org, product)),
		style.paint(ansiBold, fmt.Sprintf("(%d/%d)", report.Score.Done, report.Score.Total)))

	width := titleWidth(report.Items)

	auth := itemsIn(report.Items, api.UtilizationGroupAuthentication)
	fmt.Fprintf(&b, "\n%s\n", style.paint(ansiBold, "Device authentication"))
	if len(report.DeviceAuth) > 0 {
		fmt.Fprintf(&b, "  %s\n", authMethods(report.DeviceAuth, style))
	}
	writeItems(&b, auth, width, style)

	recommended := itemsIn(report.Items, api.UtilizationGroupRecommended)
	fmt.Fprintf(&b, "\n%s %s\n",
		style.paint(ansiBold, "Recommended"),
		style.paint(ansiDim, fmt.Sprintf("(%d/%d)", doneCount(recommended), len(recommended))))
	writeItems(&b, recommended, width, style)

	if optional := itemsIn(report.Items, api.UtilizationGroupOptional); len(optional) > 0 {
		fmt.Fprintf(&b, "\n%s %s\n",
			style.paint(ansiBold, "Optional bonuses"),
			style.paint(ansiDim, fmt.Sprintf("(%d in use, not scored)", doneCount(optional))))
		writeItems(&b, optional, width, style)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// authMethods renders how devices authenticate on one line, such as
// "Shared Secret 2 | LocalCert 10 | NervesKey 0": a method in use takes its
// level's colour (orange for a warning, green otherwise), an unused one is dim.
func authMethods(methods []api.UtilizationAuthMethod, style checklistStyle) string {
	parts := make([]string, 0, len(methods))
	for _, m := range methods {
		text := fmt.Sprintf("%s %d", m.Label, m.Devices)
		switch {
		case m.Devices == 0:
			text = style.paint(ansiDim, text)
		case m.Level == api.UtilizationLevelWarning:
			text = style.paint(ansiOrange, text)
		default:
			text = style.paint(ansiGreen, text)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, style.paint(ansiDim, " | "))
}

// writeItems writes one line per item: its mark, title and what the server
// found. A scored feature that isn't in use, and that the server can offer,
// is followed by its documentation link; verbose adds the description and
// links every item, the optional ones included.
func writeItems(b *strings.Builder, items []api.UtilizationItem, width int, style checklistStyle) {
	const indent = "      "
	for _, item := range items {
		// Padded by hand: the tabwriter would count colour codes as width.
		pad := strings.Repeat(" ", width-len([]rune(item.Title)))
		fmt.Fprintf(b, "  %s  %s%s  %s\n", style.mark(item.Status), item.Title, pad, item.Summary)

		if style.verbose && item.Description != "" {
			fmt.Fprintf(b, "%s%s\n", indent, style.paint(ansiDim, item.Description))
		}
		wanted := item.Group != api.UtilizationGroupOptional &&
			(item.Status == api.UtilizationTodo || item.Status == api.UtilizationAttention)
		if item.DocsURL != "" && (style.verbose || wanted) {
			fmt.Fprintf(b, "%s%s\n", indent, style.paint(ansiDim, item.DocsURL))
		}
	}
}

func itemsIn(items []api.UtilizationItem, group string) []api.UtilizationItem {
	var out []api.UtilizationItem
	for _, item := range items {
		if item.Group == group {
			out = append(out, item)
		}
	}
	return out
}

func doneCount(items []api.UtilizationItem) int {
	n := 0
	for _, item := range items {
		if item.Status == api.UtilizationDone {
			n++
		}
	}
	return n
}

func titleWidth(items []api.UtilizationItem) int {
	width := 0
	for _, item := range items {
		if n := len([]rune(item.Title)); n > width {
			width = n
		}
	}
	return width
}
