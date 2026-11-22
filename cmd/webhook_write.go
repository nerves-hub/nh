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
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// webhookEventTypesHelp lists the event types NervesHub.Webhooks.Events
// defines. The server is the authority: it rejects any type it doesn't know,
// and may know more than are listed here.
const webhookEventTypesHelp = `Event types:
  device.connected, device.disconnected
  device.health_reported, device.metrics_reported, device.health_status_changed
  device.firmware_update.scheduled, device.firmware_update.progress,
  device.firmware_update.finished, device.firmware_validated
  error_group.created, error_group.reopened
  deployment_group.workflow_step.started,
  deployment_group.workflow_step.awaiting_approval,
  deployment_group.workflow_step.completed,
  deployment_group.workflow_step.failed,
  deployment_group.workflow_step.skipped`

// webhookCreateCmd implements `nh webhook create`.
var webhookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Add a webhook to an organization",
	Long: `Add a webhook to the organization. --url and at least one --events type are
required.

The webhook sends the events of every product in the organization, including
products added later, unless --products names the ones to send. It is on unless
--enabled=false is given.

NervesHub generates the webhook's signing secret and returns it once, in this
response. Save it then: it is never shown again, and the only way to get a
secret afterwards is ` + "`nh webhook regenerate-secret`" + `.

` + webhookEventTypesHelp,
	Example: `  nh webhook create --url https://hooks.example.com/nerves-hub \
    --events device.connected,device.disconnected --products thermostat \
    --description "Fleet dashboard"`,
	Args: cobra.NoArgs,
	RunE: runWebhookCreate,
}

// webhookUpdateCmd implements `nh webhook update <id>`.
var webhookUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a webhook",
	Long: `Change a webhook's URL, description, events, products, or whether it is on.
Only the flags you pass are sent; everything else is left as it is. The signing
secret is not changed.

--events and --products replace the current lists. --products limits the webhook
to those products; --all-products sends every product's events again. Turning a
webhook on (--enabled) clears the failures deliveries recorded against it.

A description can be changed but not cleared: NervesHub fails on an empty one.

` + webhookEventTypesHelp,
	Example: `  nh webhook update 12 --enabled
  nh webhook update 12 --events device.connected --all-products`,
	Args: webhookIDArgs,
	RunE: runWebhookUpdate,
}

func init() {
	for _, c := range []*cobra.Command{webhookCreateCmd, webhookUpdateCmd} {
		f := c.Flags()
		f.String("url", "", "where events are sent: an https:// or http:// URL")
		f.String("description", "", "what receives these events")
		f.StringSlice("events", nil, "event types to send (comma-separated or repeated)")
		f.StringSlice("products", nil, "only send these products' events (comma-separated or repeated)")
		f.Bool("all-products", false, "send the events of every product, including products added later")
		f.Bool("enabled", false, "turn the webhook on (--enabled) or off (--enabled=false)")
		// A bare --enabled means true; the value only counts when the flag is given.
		f.Lookup("enabled").NoOptDefVal = "true"
		webhookCmd.AddCommand(c)
	}
}

// webhookInputFromFlags builds the request body from the flags that were
// given, leaving the others out.
func webhookInputFromFlags(cmd *cobra.Command) (api.WebhookInput, error) {
	var in api.WebhookInput
	f := cmd.Flags()

	if f.Changed("url") {
		v := strings.TrimSpace(mustString(cmd, "url"))
		if v == "" {
			return in, errors.New("--url must not be empty")
		}
		in.URL = &v
	}
	if f.Changed("description") {
		v := mustString(cmd, "description")
		// The server casts a blank description to nil and then trims it,
		// which fails with a 500 (Webhook.changeset/3), so it can't clear one.
		// On create there is nothing to clear, so leave the flag out instead.
		if strings.TrimSpace(v) == "" {
			return in, errors.New("--description must not be empty: NervesHub can't clear a webhook's description")
		}
		in.Description = &v
	}
	if f.Changed("events") {
		v, _ := f.GetStringSlice("events")
		in.Events = cleanList(v)
		if len(in.Events) == 0 {
			return in, errors.New("--events needs at least one event type")
		}
	}
	if f.Changed("products") {
		v, _ := f.GetStringSlice("products")
		in.Products = cleanList(v)
		if len(in.Products) == 0 {
			return in, errors.New("--products needs at least one product name")
		}
	}
	if f.Changed("all-products") {
		v, _ := f.GetBool("all-products")
		if v && in.Products != nil {
			return in, errors.New("--all-products and --products can't be used together")
		}
		in.AllProducts = &v
	}
	if f.Changed("enabled") {
		v, _ := f.GetBool("enabled")
		in.Enabled = &v
	}
	return in, nil
}

// cleanList trims each entry and drops empty ones, so "a, b," is [a b].
func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func runWebhookCreate(cmd *cobra.Command, _ []string) error {
	in, err := webhookInputFromFlags(cmd)
	if err != nil {
		return err
	}
	if in.URL == nil {
		return errors.New("--url is required")
	}
	if len(in.Events) == 0 {
		return errors.New("--events is required: name at least one event type")
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	wh, err := client.CreateWebhook(cmd.Context(), org, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, wh)
	}
	fmt.Fprintf(w, "Created webhook %d in %s\n\n", wh.ID, org)
	if err := printWebhookDetails(w, wh); err != nil {
		return err
	}
	printWebhookSecretNotice(w, wh)
	return nil
}

func runWebhookUpdate(cmd *cobra.Command, args []string) error {
	id, err := parseWebhookID(args[0])
	if err != nil {
		return err
	}

	in, err := webhookInputFromFlags(cmd)
	if err != nil {
		return err
	}
	if in.IsEmpty() {
		return errors.New("nothing to update: pass --url, --description, --events, --products, --all-products, or --enabled")
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	wh, err := client.UpdateWebhook(cmd.Context(), org, id, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, wh)
	}
	fmt.Fprintf(w, "Updated webhook %d\n\n", wh.ID)
	return printWebhookDetails(w, wh)
}
