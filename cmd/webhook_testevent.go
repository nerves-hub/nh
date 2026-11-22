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
	"strconv"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// webhookTestCmd implements `nh webhook test <id>`.
var webhookTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Send a test event to a webhook",
	Long: `Send a webhook.test event to a webhook straight away, whether or not it is on,
and report how its endpoint answered. It can take up to 20 seconds when the
endpoint is slow. The result is not recorded against the webhook.

Exits non-zero when the endpoint did not accept the event (no 2xx answer).`,
	Args: webhookIDArgs,
	RunE: runWebhookTest,
}

func init() {
	webhookCmd.AddCommand(webhookTestCmd)
}

func runWebhookTest(cmd *cobra.Command, args []string) error {
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

	result, err := client.TestWebhook(cmd.Context(), org, id)
	if err != nil {
		return err
	}

	status := "no response"
	if result.Status != nil {
		status = "HTTP " + strconv.Itoa(*result.Status)
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if err := printJSON(w, result); err != nil {
			return err
		}
	} else if result.Delivered {
		fmt.Fprintf(w, "Webhook %d accepted the test event (%s)\n", id, status)
	}

	if !result.Delivered {
		reason := result.Error
		if reason == "" {
			reason = status
		}
		return fmt.Errorf("webhook %d did not accept the test event: %s", id, reason)
	}
	return nil
}
