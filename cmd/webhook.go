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
	"strconv"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/spf13/cobra"
)

// webhookCmd groups organization webhook commands.
var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Manage an organization's webhooks",
	Long: `Commands for an organization's webhooks: the HTTP endpoints its products'
device events are sent to. They are the same webhooks the organization's
Integrations settings manage.

Webhooks are scoped to an organization (set with --org or NERVES_HUB_ORG), and
every webhook command needs a role that can manage them.`,
}

func init() {
	rootCmd.AddCommand(webhookCmd)
}

// webhookIDArgs requires exactly one webhook id, with friendly messages.
var webhookIDArgs = exactlyOneArg(
	"Webhook ID missing",
	"too many arguments: provide a single webhook ID",
)

// parseWebhookID parses a webhook id argument, which the server only accepts
// as a positive integer.
func parseWebhookID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid webhook ID %q: must be a positive integer (see `nh webhook list`)", s)
	}
	return id, nil
}

// webhookProducts describes which products' events a webhook sends.
func webhookProducts(wh *api.Webhook) string {
	if wh.AllProducts {
		return "all"
	}
	return orDash(strings.Join(wh.Products, ", "))
}

// webhookState summarises whether a webhook is sending and how deliveries are
// going: "on", "failing", or "off" with the reason NervesHub turned it off.
func webhookState(wh *api.Webhook) string {
	switch {
	case !wh.Enabled && wh.DisabledReason != "":
		return "off (" + wh.DisabledReason + ")"
	case !wh.Enabled:
		return "off"
	case wh.FailingSince != nil && !wh.FailingSince.IsZero():
		return "failing"
	default:
		return "on"
	}
}

// printWebhookDetails writes a webhook as label/value rows, skipping empty
// values. The signing secret is printed only when the response carried one.
func printWebhookDetails(w io.Writer, wh *api.Webhook) error {
	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("ID:", strconv.Itoa(wh.ID))
	row("URL:", wh.URL)
	row("Description:", wh.Description)
	row("State:", webhookState(wh))
	row("Events:", strings.Join(wh.Events, ", "))
	row("Products:", webhookProducts(wh))
	if wh.FailingSince != nil && !wh.FailingSince.IsZero() {
		row("Failing since:", formatTimestamp(wh.FailingSince))
	}
	row("Last error:", wh.LastError)
	if wh.LastErrorAt != nil && !wh.LastErrorAt.IsZero() {
		row("Last error at:", formatTimestamp(wh.LastErrorAt))
	}
	if wh.InsertedAt != nil && !wh.InsertedAt.IsZero() {
		row("Created:", formatTimestamp(wh.InsertedAt))
	}
	if wh.UpdatedAt != nil && !wh.UpdatedAt.IsZero() {
		row("Updated:", formatTimestamp(wh.UpdatedAt))
	}
	row("Signing secret:", wh.Secret)
	return tw.Flush()
}

// printWebhookSecretNotice tells the user to keep the signing secret, which
// the server returns only in the create and regenerate-secret responses.
func printWebhookSecretNotice(w io.Writer, wh *api.Webhook) {
	if wh.Secret == "" {
		return
	}
	fmt.Fprintf(w, "\nSave the signing secret now: NervesHub won't show it again.\n"+
		"If you lose it, run `nh webhook regenerate-secret %d` for a new one.\n", wh.ID)
}
