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
	"strconv"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceWriteHelp explains the device write flags and the server version
// they need, for the create and update help text.
const deviceWriteHelp = `--update-mode is off, automatic or device_managed. --tag takes one tag or
a comma-separated list. --managed-updates-allowed is whether the device may
put itself into device_managed mode. --connecting-code is Elixir code the
device runs each time it connects. --description "" and --connecting-code ""
clear them.

The API can't put a device in a deployment group: NervesHub assigns a device
to a deployment group whose version and tag conditions it matches when the
device connects, so set the tags that group needs with --tag.

These fields need NervesHub v2.5.3 or later: older servers don't apply them.
nh reports an error when the device comes back without the update mode, tags,
description or managed updates setting you set. The server never returns
the connecting code, so nh can't check that one.`

// deviceWriteFlags are the flags that set a device field, as named in errors.
const deviceWriteFlags = "--description, --tag, --update-mode, --managed-updates-allowed or --connecting-code"

// ── create ──────────────────────────────────────────────────────────────────

var deviceCreateCmd = &cobra.Command{
	Use:   "create <identifier>",
	Short: "Create a device",
	Long: `Register a device in a product by its identifier. Optional fields:
` + deviceWriteFlags + `.

` + deviceWriteHelp,
	Args: deviceIdentifierArgs,
	RunE: runDeviceCreate,
}

var deviceUpdateCmd = &cobra.Command{
	Use:   "update <identifier>",
	Short: "Update a device",
	Long: `Update a device. Only the fields you pass are changed:
` + deviceWriteFlags + `.

` + deviceWriteHelp,
	Args: deviceIdentifierArgs,
	RunE: runDeviceUpdate,
}

// removedDeviceFlags are device write flags nh used to have, with why each is
// gone. They stay as hidden flags so that a script still passing one gets this
// explanation instead of "unknown flag".
var removedDeviceFlags = map[string]string{
	"updates-enabled": "--updates-enabled was removed: use --update-mode automatic or " +
		"--update-mode device_managed to enable updates, or --update-mode off to disable them",
	"deployment-group-id": "--deployment-group-id was removed: NervesHub ignores a deployment group " +
		"in a device create or update, and the API has no other way to set one. NervesHub assigns a " +
		"device to a deployment group whose version and tag conditions it matches when the device " +
		"connects, so set the tags that group needs with --tag",
}

func init() {
	for _, c := range []*cobra.Command{deviceCreateCmd, deviceUpdateCmd} {
		c.Flags().String("description", "", `device description; "" clears it`)
		c.Flags().StringArray("tag", nil, "device tag; repeatable")
		c.Flags().String("update-mode", "", "firmware update mode: off, automatic or device_managed")
		c.Flags().Bool("managed-updates-allowed", false, "whether the device may put itself into device_managed mode")
		c.Flags().String("connecting-code", "", `Elixir code the device runs each time it connects; "" clears it`)

		c.Flags().Bool("updates-enabled", false, "removed: use --update-mode")
		c.Flags().String("deployment-group-id", "", "removed: the API can't set a device's deployment group")
		for name := range removedDeviceFlags {
			_ = c.Flags().MarkHidden(name)
		}
		deviceCmd.AddCommand(c)
	}
}

// updateModes are the values NervesHub accepts for a device's update_mode
// (the Ecto.Enum on NervesHub.Devices.Device).
var updateModes = []string{"off", "automatic", "device_managed"}

// deviceInputFromFlags reads the device write flags, returning the input and
// whether any field was provided.
func deviceInputFromFlags(cmd *cobra.Command) (api.DeviceInput, bool, error) {
	var in api.DeviceInput
	for _, name := range []string{"updates-enabled", "deployment-group-id"} {
		if cmd.Flags().Changed(name) {
			return in, false, errors.New(removedDeviceFlags[name])
		}
	}

	changed := false
	if cmd.Flags().Changed("description") {
		v := mustString(cmd, "description")
		in.Description = &v
		changed = true
	}
	if cmd.Flags().Changed("tag") {
		raw, _ := cmd.Flags().GetStringArray("tag")
		in.Tags = splitTags(raw)
		if len(in.Tags) == 0 {
			return in, changed, errors.New("--tag needs a tag name: nh can't clear a device's tags")
		}
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
	if cmd.Flags().Changed("managed-updates-allowed") {
		v, _ := cmd.Flags().GetBool("managed-updates-allowed")
		in.ManagedUpdatesAllowed = &v
		changed = true
	}
	if cmd.Flags().Changed("connecting-code") {
		v := mustString(cmd, "connecting-code")
		in.ConnectingCode = &v
		changed = true
	}
	return in, changed, nil
}

// checkApplied reports an error when the device a create or update returned
// doesn't have the fields that were sent. NervesHub before v2.5.3 reads device
// fields only from the top level of the body, so it answers the nested body nh
// sends with success but leaves them unchanged. The connecting code isn't in
// the response, so it can't be checked.
func checkApplied(in api.DeviceInput, device *api.Device) error {
	const hint = "the server didn't apply %s (sent %s, device has %s): nh needs NervesHub v2.5.3 or later"
	if in.UpdateMode != "" && device.UpdateMode != in.UpdateMode {
		return fmt.Errorf(hint, "--update-mode", in.UpdateMode, orDash(device.UpdateMode))
	}
	if len(in.Tags) > 0 && !slices.Equal(tagSet(in.Tags), tagSet(device.Tags)) {
		return fmt.Errorf(hint, "--tag", strings.Join(in.Tags, ","), orDash(strings.Join(device.Tags, ",")))
	}
	// Ecto saves a blank string as nil, so a blank description comes back
	// as none.
	if in.Description != nil && device.Description != *in.Description &&
		(strings.TrimSpace(*in.Description) != "" || device.Description != "") {
		return fmt.Errorf(hint, "--description", strconv.Quote(*in.Description), strconv.Quote(device.Description))
	}
	if want := in.ManagedUpdatesAllowed; want != nil {
		if got := device.ManagedUpdatesAllowed; got == nil || *got != *want {
			return fmt.Errorf(hint, "--managed-updates-allowed", strconv.FormatBool(*want), formatOptionalBool(got))
		}
	}
	return nil
}

// formatOptionalBool renders a bool the server may not have sent.
func formatOptionalBool(b *bool) string {
	if b == nil {
		return "-"
	}
	return strconv.FormatBool(*b)
}

// splitTags reads --tag values the way NervesHub reads a tags string: each is
// split on commas and trimmed, and empty entries are dropped.
func splitTags(values []string) []string {
	var tags []string
	for _, v := range values {
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return tags
}

// tagSet returns tags sorted and without duplicates, so two lists that hold
// the same tags compare equal.
func tagSet(tags []string) []string {
	out := splitTags(tags)
	slices.Sort(out)
	return slices.Compact(out)
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

	// The device exists even when a field wasn't applied, so show it before
	// reporting that.
	if err := showWrittenDevice(cmd, cfg, device, fmt.Sprintf("Created device %s in %s/%s", device.Identifier, org, product)); err != nil {
		return err
	}
	if err := checkApplied(in, device); err != nil {
		return fmt.Errorf("created device %s, but %w", identifier, err)
	}
	return nil
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
		return errors.New("nothing to update: provide " + deviceWriteFlags)
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.UpdateDevice(cmd.Context(), org, product, identifier, in)
	if err != nil {
		return err
	}

	if err := showWrittenDevice(cmd, cfg, device, "Updated device "+device.Identifier); err != nil {
		return err
	}
	if err := checkApplied(in, device); err != nil {
		return fmt.Errorf("updated device %s, but %w", identifier, err)
	}
	return nil
}

// showWrittenDevice prints the device a create or update returned: as JSON,
// or as a headline followed by its details.
func showWrittenDevice(cmd *cobra.Command, cfg *config.Config, device *api.Device, headline string) error {
	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	fmt.Fprintln(w, headline)
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
