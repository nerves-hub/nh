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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// webhookListCmd implements `nh webhook list`.
var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an organization's webhooks",
	Long: `List the organization's webhooks, oldest first. Signing secrets are never
listed.

STATE is "on", "failing" while deliveries are being refused, or "off" (with
the reason, when NervesHub turned the webhook off itself).`,
	Args: cobra.NoArgs,
	RunE: runWebhookList,
}

// webhookShowCmd implements `nh webhook show <id>`.
var webhookShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a webhook",
	Long: `Show a webhook and its delivery state, such as when it started failing and
the last error. The signing secret is not shown: it is only returned when the
webhook is created and by ` + "`nh webhook regenerate-secret`" + `.`,
	Args: webhookIDArgs,
	RunE: runWebhookShow,
}

func init() {
	webhookCmd.AddCommand(webhookListCmd)
	webhookCmd.AddCommand(webhookShowCmd)
}

func runWebhookList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	webhooks, err := client.ListWebhooks(cmd.Context(), org)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, webhooks)
	}

	if len(webhooks) == 0 {
		fmt.Fprintf(w, "No webhooks found in %s.\n", org)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "ID\tURL\tSTATE\tPRODUCTS\tEVENTS\tDESCRIPTION")
	for i := range webhooks {
		wh := &webhooks[i]
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\n",
			wh.ID, wh.URL, webhookState(wh), webhookProducts(wh), len(wh.Events), orDash(wh.Description))
	}
	return tw.Flush()
}

func runWebhookShow(cmd *cobra.Command, args []string) error {
	id, err := parseWebhookID(args[0])
	if err != nil {
		return err
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

	wh, err := client.GetWebhook(cmd.Context(), org, id)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, wh)
	}
	return printWebhookDetails(w, wh)
}
