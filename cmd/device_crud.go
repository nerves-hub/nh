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

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// ── create ──────────────────────────────────────────────────────────────────

var deviceCreateCmd = &cobra.Command{
	Use:   "create <identifier>",
	Short: "Create a device",
	Long:  "Register a device in a product by its identifier.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceCreate,
}

var deviceUpdateCmd = &cobra.Command{
	Use:   "update <identifier>",
	Short: "Update a device",
	Long: `Update a device. Only the fields you pass are changed: --description,
--tag (repeatable), --updates-enabled, --deployment-group-id.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceUpdate,
}

func init() {
	for _, c := range []*cobra.Command{deviceCreateCmd, deviceUpdateCmd} {
		c.Flags().String("description", "", "device description")
		c.Flags().StringArray("tag", nil, "device tag; repeatable")
		c.Flags().Bool("updates-enabled", false, "whether firmware updates are enabled")
		c.Flags().Int("deployment-group-id", 0, "deployment group ID to assign")
		deviceCmd.AddCommand(c)
	}
}

// deviceInputFromFlags reads the device write flags, returning the input and
// whether any field was provided.
func deviceInputFromFlags(cmd *cobra.Command) (api.DeviceInput, bool) {
	var in api.DeviceInput
	changed := false
	if cmd.Flags().Changed("description") {
		in.Description = mustString(cmd, "description")
		changed = true
	}
	if cmd.Flags().Changed("tag") {
		in.Tags, _ = cmd.Flags().GetStringArray("tag")
		changed = true
	}
	if cmd.Flags().Changed("updates-enabled") {
		v, _ := cmd.Flags().GetBool("updates-enabled")
		in.UpdatesEnabled = &v
		changed = true
	}
	if cmd.Flags().Changed("deployment-group-id") {
		v, _ := cmd.Flags().GetInt("deployment-group-id")
		in.DeploymentGroupID = &v
		changed = true
	}
	return in, changed
}

func runDeviceCreate(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	in, _ := deviceInputFromFlags(cmd)

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.CreateDevice(cmd.Context(), org, product, identifier, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	fmt.Fprintf(w, "Created device %s in %s/%s\n", device.Identifier, org, product)
	return renderDevice(w, device)
}

func runDeviceUpdate(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	in, changed := deviceInputFromFlags(cmd)
	if !changed {
		return errors.New("nothing to update: provide --description, --tag, --updates-enabled, or --deployment-group-id")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.UpdateDevice(cmd.Context(), org, product, identifier, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	fmt.Fprintf(w, "Updated device %s\n", device.Identifier)
	return renderDevice(w, device)
}

// ── delete ──────────────────────────────────────────────────────────────────

var deviceDeleteCmd = &cobra.Command{
	Use:   "delete <identifier>",
	Short: "Delete a device",
	Long:  "Remove a device from a product, by its identifier.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceDelete,
}

func init() {
	deviceDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	deviceCmd.AddCommand(deviceDeleteCmd)
}

func runDeviceDelete(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete device %s from %s/%s? [y/N]", identifier, org, product))
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

	if err := client.DeleteDevice(cmd.Context(), org, product, identifier); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted device %s from %s/%s\n", identifier, org, product)
	return nil
}
