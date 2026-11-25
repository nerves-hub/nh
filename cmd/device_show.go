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
	"time"

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
	if device.Deleted {
		row("Deleted:", "yes")
	}
	row("Status:", connectionStatusText(device.ConnectionStatus))
	// NervesHub sends the connection status as "online" too; older servers
	// sent something else there, so show it only when it says more.
	if device.Online != "" && device.Online != device.ConnectionStatus {
		row("Online:", device.Online)
	}
	if device.Sleep != nil {
		row("Sleep:", sleepText(device.Sleep))
	}
	row("Version:", orDash(device.Version))
	if len(device.Tags) > 0 {
		row("Tags:", strings.Join(device.Tags, ", "))
	}
	if device.Description != "" {
		row("Description:", device.Description)
	}
	// update_mode says more than updates_enabled (which is just update_mode
	// != off), so show it when the server sends it.
	if device.UpdateMode != "" {
		row("Update mode:", device.UpdateMode)
	} else {
		row("Updates enabled:", fmt.Sprintf("%t", device.UpdatesEnabled))
	}
	if allowed := device.ManagedUpdatesAllowed; allowed != nil {
		row("Managed updates allowed:", fmt.Sprintf("%t", *allowed))
	}
	// NervesHub keeps updates_blocked_until once it has passed, so only a
	// time still to come means the device is in the penalty box.
	if until := device.UpdatesBlockedUntil; until != nil && until.After(time.Now()) {
		row("Updates blocked until:", formatTimestamp(until))
	}
	row("Last communication:", formatTimestamp(device.LastCommunication))
	if fw := device.FirmwareMetadata; fw != nil && (fw.Version != "" || fw.UUID != "") {
		row("Firmware:", strings.TrimSpace(fw.Version+" "+bracket(fw.UUID)))
	}
	if dg := device.DeploymentGroup; dg != nil && dg.Name != "" {
		value := dg.Name
		if dg.FirmwareVersion != "" {
			value += " (firmware " + dg.FirmwareVersion + ")"
		}
		if !dg.IsActive {
			value += ", inactive"
		}
		row("Deployment group:", value)
	}
	return tw.Flush()
}

// connectionStatusText renders a device's connection_status, spelling out
// "not_seen", which NervesHub sends for a device that has never connected.
func connectionStatusText(status string) string {
	if status == "not_seen" {
		return "never connected"
	}
	return orDash(status)
}

// sleepText says what a device's sleep state means, e.g. "deep, back at
// <time>" or "light, overdue: was due back <time>".
func sleepText(sleep *api.DeviceSleep) string {
	wake := sleep.ExpectedWakeAt
	hasWake := wake != nil && !wake.IsZero()
	var text string
	switch sleep.State {
	case "going_to_sleep":
		text = "going to sleep"
		if hasWake {
			text += ", back at " + formatTimestamp(wake)
		}
	case "asleep":
		if hasWake {
			text = "back at " + formatTimestamp(wake)
		} else {
			text = "asleep, no wake time given"
		}
	case "overdue":
		text = "overdue"
		if hasWake {
			text += ": was due back " + formatTimestamp(wake)
		}
	default:
		text = orDash(sleep.State)
	}
	if sleep.Mode != "" {
		text = sleep.Mode + ", " + text
	}
	return text
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
