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
	"io"
	"net/http"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceSharedSecretsCmd groups the commands for a device's own shared
// secrets (its "nhd_" keys), as opposed to a product's "nhp_" keys.
var deviceSharedSecretsCmd = &cobra.Command{
	Use:     "shared-secrets",
	Aliases: []string{"shared-secret"},
	Short:   "Manage a device's shared secrets",
	Long: `Commands for a device's own shared secrets: the "nhd_" keys and secrets a
device can connect with instead of a certificate, as opposed to a product's
"nhp_" keys that let any device register itself.

A secret is shown once, when it is created, and cannot be fetched again.`,
}

func init() {
	deviceCmd.AddCommand(deviceSharedSecretsCmd)
}

// ── list ────────────────────────────────────────────────────────────────────

var deviceSharedSecretListCmd = &cobra.Command{
	Use:   "list <identifier>",
	Short: "List a device's shared secret keys",
	Long: `List every shared secret key a device has been given, deactivated ones
included. Only keys are listed: the server never returns a secret after it
has been created.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceSharedSecretList,
}

func init() {
	deviceSharedSecretsCmd.AddCommand(deviceSharedSecretListCmd)
}

func runDeviceSharedSecretList(cmd *cobra.Command, args []string) error {
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

	secrets, err := client.ListDeviceSharedSecrets(cmd.Context(), org, product, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if secrets == nil {
			secrets = []api.DeviceSharedSecret{}
		}
		return printJSON(w, secrets)
	}

	if len(secrets) == 0 {
		fmt.Fprintf(w, "No shared secrets found for device %s.\n", identifier)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "KEY\tSTATUS\tLAST USED\tCREATED\tDEACTIVATED")
	for _, s := range secrets {
		status, deactivated := "active", "-"
		if !s.Active() {
			status, deactivated = "deactivated", formatTimestamp(s.DeactivatedAt)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			s.Key, status, formatTimestamp(s.LastUsed), createdAt(s.InsertedAt), deactivated)
	}
	return tw.Flush()
}

// createdAt renders a creation time, using "-" rather than formatTimestamp's
// "never" when the server sent none.
func createdAt(ts api.Timestamp) string {
	if ts.IsZero() {
		return "-"
	}
	return formatTimestamp(&ts)
}

// ── create ──────────────────────────────────────────────────────────────────

var deviceSharedSecretCreateCmd = &cobra.Command{
	Use:   "create <identifier>",
	Short: "Create a shared secret for a device",
	Long: `Create a new shared secret key and secret the device can connect with.

The secret is printed once, in this command's output, and cannot be fetched
again: save it now. If it is lost, create another and deactivate the old key
with ` + "`nh device shared-secrets delete`" + `.

The server refuses this when device shared secrets are not enabled on the
platform.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceSharedSecretCreate,
}

func init() {
	deviceSharedSecretsCmd.AddCommand(deviceSharedSecretCreateCmd)
}

func runDeviceSharedSecretCreate(cmd *cobra.Command, args []string) error {
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

	secret, err := client.CreateDeviceSharedSecret(cmd.Context(), org, product, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if err := printJSON(w, secret); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "Created shared secret for device %s\n", identifier)
		if err := printSharedSecret(w, secret); err != nil {
			return err
		}
	}
	warnSecretShownOnce(cmd, secret)
	return nil
}

// printSharedSecret prints a newly created shared secret's key and, when the
// server returned it, the secret.
func printSharedSecret(w io.Writer, secret *api.DeviceSharedSecret) error {
	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Key:\t%s\n", secret.Key)
	if secret.Secret != "" {
		fmt.Fprintf(tw, "Secret:\t%s\n", secret.Secret)
	}
	return tw.Flush()
}

// warnSecretShownOnce tells the user, on stderr, that a new secret can't be
// fetched again. It goes to stderr so that stdout stays just the result (or
// the JSON) for anything reading it.
func warnSecretShownOnce(cmd *cobra.Command, secret *api.DeviceSharedSecret) {
	errw := cmd.ErrOrStderr()
	if secret.Secret != "" {
		fmt.Fprintln(errw, "\nSave the secret now: it cannot be fetched again.")
	} else {
		fmt.Fprintln(errw, "\nThe server did not return the secret, and it cannot be fetched later.")
	}
}

// ── delete ──────────────────────────────────────────────────────────────────

var deviceSharedSecretDeleteCmd = &cobra.Command{
	Use:     "delete <identifier> <key>",
	Aliases: []string{"deactivate"},
	Short:   "Deactivate a device's shared secret",
	Long: `Deactivate one of a device's active shared secrets, by key, so the device
can no longer connect with it. Connections made with the key are closed; the
device's other connections stay up.

The key is not removed: it stays in ` + "`nh device shared-secrets list`" + ` with
its deactivation time, and cannot be reactivated.`,
	Args: exactlyTwoArgs("usage: device shared-secrets delete <identifier> <key>"),
	RunE: runDeviceSharedSecretDelete,
}

func init() {
	deviceSharedSecretDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	deviceSharedSecretsCmd.AddCommand(deviceSharedSecretDeleteCmd)
}

func runDeviceSharedSecretDelete(cmd *cobra.Command, args []string) error {
	identifier, key := args[0], args[1]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to deactivate without confirmation; pass --yes to proceed non-interactively")
		}
		prompt := fmt.Sprintf("Deactivate shared secret %s on device %s? The device will no longer be able to connect with it. [y/N]", key, identifier)
		confirmed, err := confirm(cmd, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.DeleteDeviceSharedSecret(cmd.Context(), org, product, identifier, key); err != nil {
		// The server answers 404 with a generic message for an unknown
		// device, an unknown key, another device's key, and a key that is
		// already deactivated alike.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w\ndevice %s has no active shared secret %s (the org, product, device or key may not exist, or the key is already deactivated; see `nh device shared-secrets list %s`)", err, identifier, key, identifier)
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deactivated shared secret %s on device %s\n", key, identifier)
	return nil
}
