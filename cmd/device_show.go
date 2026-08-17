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
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceShowCmd implements `nh device show <identifier>`.
var deviceShowCmd = &cobra.Command{
	Use:   "show <identifier>",
	Short: "Show details for a device",
	Long:  "Show details for a single device by its identifier.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceShow,
}

func init() {
	deviceCmd.AddCommand(deviceShowCmd)
}

func runDeviceShow(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.GetDevice(cmd.Context(), org, product, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	return renderDevice(w, device)
}

// renderDevice writes a device's details as a label/value table.
func renderDevice(w io.Writer, device *api.Device) error {
	tw := newTableWriter(w)
	row := func(label, value string) {
		fmt.Fprintf(tw, "%s\t%s\n", label, value)
	}
	row("Identifier:", device.Identifier)
	row("Status:", device.ConnectionStatus)
	if device.Online != "" {
		row("Online:", device.Online)
	}
	row("Version:", orDash(device.Version))
	if len(device.Tags) > 0 {
		row("Tags:", strings.Join(device.Tags, ", "))
	}
	if device.Description != "" {
		row("Description:", device.Description)
	}
	row("Updates enabled:", fmt.Sprintf("%t", device.UpdatesEnabled))
	row("Last communication:", formatTimestamp(device.LastCommunication))
	if fw := device.FirmwareMetadata; fw != nil && (fw.Version != "" || fw.UUID != "") {
		row("Firmware:", strings.TrimSpace(fw.Version+" "+bracket(fw.UUID)))
	}
	if dg := device.DeploymentGroup; dg != nil && dg.Name != "" {
		row("Deployment group:", dg.Name)
	}
	return tw.Flush()
}

// orDash returns s, or "-" when s is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// bracket wraps s in parentheses, or returns "" when s is empty.
func bracket(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

// formatTimestamp renders a nullable API timestamp, using "never" for a
// missing or zero value.
func formatTimestamp(ts *api.Timestamp) string {
	if ts == nil || ts.IsZero() {
		return "never"
	}
	return ts.Format("2006-01-02 15:04:05 MST")
}
