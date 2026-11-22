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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// webhookDeleteCmd implements `nh webhook delete <id>`.
var webhookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a webhook",
	Long: `Delete a webhook from the organization. Deliveries already queued for it are
dropped.`,
	Args: webhookIDArgs,
	RunE: runWebhookDelete,
}

// webhookRegenerateSecretCmd implements `nh webhook regenerate-secret <id>`.
var webhookRegenerateSecretCmd = &cobra.Command{
	Use:   "regenerate-secret <id>",
	Short: "Replace a webhook's signing secret",
	Long: `Replace a webhook's signing secret and print the new one.

The old secret stops working at once, for retries of earlier events too, so
update the receiver straight away. The new secret is returned only in this
response: save it, because NervesHub won't show it again.`,
	Args: webhookIDArgs,
	RunE: runWebhookRegenerateSecret,
}

func init() {
	webhookDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	webhookRegenerateSecretCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	webhookCmd.AddCommand(webhookDeleteCmd)
	webhookCmd.AddCommand(webhookRegenerateSecretCmd)
}

// confirmWebhookAction asks before a change that can't be undone, unless
// --yes was given. It reports false when the user declines.
func confirmWebhookAction(cmd *cobra.Command, cfg *config.Config, verb, prompt string) (bool, error) {
	if skip, _ := cmd.Flags().GetBool("yes"); skip {
		return true, nil
	}
	if cfg.NonInteractive {
		return false, fmt.Errorf("refusing to %s without confirmation; pass --yes to proceed non-interactively", verb)
	}
	confirmed, err := confirm(cmd, prompt)
	if err != nil {
		return false, err
	}
	if !confirmed {
		fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
	}
	return confirmed, nil
}

func runWebhookDelete(cmd *cobra.Command, args []string) error {
	id, err := parseWebhookID(args[0])
	if err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	ok, err := confirmWebhookAction(cmd, cfg, "delete",
		fmt.Sprintf("Delete webhook %d from %s? [y/N]", id, org))
	if err != nil || !ok {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.DeleteWebhook(cmd.Context(), org, id); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted webhook %d\n", id)
	return nil
}

func runWebhookRegenerateSecret(cmd *cobra.Command, args []string) error {
	id, err := parseWebhookID(args[0])
	if err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	ok, err := confirmWebhookAction(cmd, cfg, "regenerate the signing secret",
		fmt.Sprintf("Replace the signing secret of webhook %d in %s? The current secret stops working at once. [y/N]", id, org))
	if err != nil || !ok {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	wh, err := client.RegenerateWebhookSecret(cmd.Context(), org, id)
	if err != nil {
		return err
	}
	if wh.Secret == "" {
		return errors.New("the server did not return a new signing secret")
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, wh)
	}
	fmt.Fprintf(w, "Regenerated the signing secret of webhook %d\n\n", wh.ID)
	if err := printWebhookDetails(w, wh); err != nil {
		return err
	}
	printWebhookSecretNotice(w, wh)
	return nil
}
