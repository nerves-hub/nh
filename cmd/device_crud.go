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
	"slices"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// updateModeHelp explains --update-mode for the create and update help text.
const updateModeHelp = `--update-mode is off, automatic or device_managed. Setting it, or --tag,
needs NervesHub v2.5.3 or later: older servers don't apply them, and nh
reports an error when the device comes back without them.`

// ── create ──────────────────────────────────────────────────────────────────

var deviceCreateCmd = &cobra.Command{
	Use:   "create <identifier>",
	Short: "Create a device",
	Long: `Register a device in a product by its identifier. Optional fields:
--description, --tag (repeatable), --update-mode, --deployment-group-id.

` + updateModeHelp,
	Args: deviceIdentifierArgs,
	RunE: runDeviceCreate,
}

var deviceUpdateCmd = &cobra.Command{
	Use:   "update <identifier>",
	Short: "Update a device",
	Long: `Update a device. Only the fields you pass are changed: --description,
--tag (repeatable), --update-mode, --deployment-group-id.

` + updateModeHelp,
	Args: deviceIdentifierArgs,
	RunE: runDeviceUpdate,
}

func init() {
	for _, c := range []*cobra.Command{deviceCreateCmd, deviceUpdateCmd} {
		c.Flags().String("description", "", "device description")
		c.Flags().StringArray("tag", nil, "device tag; repeatable")
		c.Flags().String("update-mode", "", "firmware update mode: off, automatic or device_managed")
		c.Flags().Int("deployment-group-id", 0, "deployment group ID to assign")
		deviceCmd.AddCommand(c)
	}
}

// updateModes are the values NervesHub accepts for a device's update_mode.
var updateModes = []string{"off", "automatic", "device_managed"}

// deviceInputFromFlags reads the device write flags, returning the input and
// whether any field was provided.
func deviceInputFromFlags(cmd *cobra.Command) (api.DeviceInput, bool, error) {
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
	if cmd.Flags().Changed("update-mode") {
		mode := mustString(cmd, "update-mode")
		if !slices.Contains(updateModes, mode) {
			return in, changed, fmt.Errorf("invalid --update-mode %q: use %s", mode, strings.Join(updateModes, ", "))
		}
		in.UpdateMode = mode
		changed = true
	}
	if cmd.Flags().Changed("deployment-group-id") {
		v, _ := cmd.Flags().GetInt("deployment-group-id")
		in.DeploymentGroupID = &v
		changed = true
	}
	return in, changed, nil
}

// checkApplied reports an error when the device a create or update returned
// doesn't have the update mode or tags that were sent. NervesHub before
// v2.5.3 reads those only from the top level of the body, so it answers the
// nested body nh sends with success but leaves them unchanged.
func checkApplied(in api.DeviceInput, device *api.Device) error {
	const hint = "the server didn't apply %s (sent %s, device has %s): nh needs NervesHub v2.5.3 or later"
	if in.UpdateMode != "" && device.UpdateMode != in.UpdateMode {
		return fmt.Errorf(hint, "--update-mode", in.UpdateMode, orDash(device.UpdateMode))
	}
	if len(in.Tags) > 0 && !sameTags(in.Tags, device.Tags) {
		return fmt.Errorf(hint, "--tag", strings.Join(in.Tags, ","), orDash(strings.Join(device.Tags, ",")))
	}
	return nil
}

// sameTags reports whether two tag lists hold the same tags, ignoring order,
// duplicates and surrounding whitespace.
func sameTags(a, b []string) bool {
	set := func(tags []string) []string {
		out := make([]string, 0, len(tags))
		for _, t := range tags {
			if t = strings.TrimSpace(t); t != "" {
				out = append(out, t)
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(set(a), set(b))
}

func runDeviceCreate(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	in, _, err := deviceInputFromFlags(cmd)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.CreateDevice(cmd.Context(), org, product, identifier, in)
	if err != nil {
		return err
	}
	if err := checkApplied(in, device); err != nil {
		return fmt.Errorf("created device %s, but %w", identifier, err)
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

	in, changed, err := deviceInputFromFlags(cmd)
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("nothing to update: provide --description, --tag, --update-mode, or --deployment-group-id")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.UpdateDevice(cmd.Context(), org, product, identifier, in)
	if err != nil {
		return err
	}
	if err := checkApplied(in, device); err != nil {
		return fmt.Errorf("updated device %s, but %w", identifier, err)
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
