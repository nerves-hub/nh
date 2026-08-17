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

// ── upgrade ─────────────────────────────────────────────────────────────────

var deviceUpgradeCmd = &cobra.Command{
	Use:   "upgrade <identifier> <firmware-uuid>",
	Short: "Upgrade a device to a firmware",
	Long:  "Ask a device to upgrade to a specific firmware, by UUID.",
	Args: exactlyTwoArgs(
		"usage: device upgrade <identifier> <firmware-uuid>",
	),
	RunE: runDeviceUpgrade,
}

func init() {
	deviceCmd.AddCommand(deviceUpgradeCmd)
}

func runDeviceUpgrade(cmd *cobra.Command, args []string) error {
	identifier, firmwareUUID := args[0], args[1]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.UpgradeDevice(cmd.Context(), org, product, identifier, firmwareUUID); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Upgrade to %s requested for device %s\n", firmwareUUID, identifier)
	return nil
}

// ── move ────────────────────────────────────────────────────────────────────

var deviceMoveCmd = &cobra.Command{
	Use:   "move <identifier>",
	Short: "Move a device to another product",
	Long:  "Move a device to a different product, given by --to-org and --to-product.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceMove,
}

func init() {
	deviceMoveCmd.Flags().String("to-org", "", "destination organization (default: current org)")
	deviceMoveCmd.Flags().String("to-product", "", "destination product (required)")
	deviceCmd.AddCommand(deviceMoveCmd)
}

func runDeviceMove(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	toProduct := mustString(cmd, "to-product")
	if toProduct == "" {
		return errors.New("--to-product is required")
	}
	toOrg := mustString(cmd, "to-org")
	if toOrg == "" {
		toOrg = org
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	device, err := client.MoveDevice(cmd.Context(), org, product, identifier, toOrg, toProduct)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	fmt.Fprintf(w, "Moved device %s to %s/%s\n", identifier, toOrg, toProduct)
	return nil
}

// ── clear-penalty ───────────────────────────────────────────────────────────

var deviceClearPenaltyCmd = &cobra.Command{
	Use:   "clear-penalty <identifier>",
	Short: "Clear a device's penalty box",
	Long:  "Clear the penalty box for a device, allowing it to reconnect immediately.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceClearPenalty,
}

func init() {
	deviceCmd.AddCommand(deviceClearPenaltyCmd)
}

func runDeviceClearPenalty(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.ClearDevicePenalty(cmd.Context(), org, product, identifier); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Cleared the penalty box for device %s\n", identifier)
	return nil
}

// ── run-code ────────────────────────────────────────────────────────────────

var deviceRunCodeCmd = &cobra.Command{
	Use:   "run-code <identifier> <code>",
	Short: "Run Elixir code on a device",
	Long: `Ask a device to run Elixir code in its console connection. Output appears in
the device's console, not in this command's output.`,
	Args: exactlyTwoArgs(
		"usage: device run-code <identifier> <code>",
	),
	RunE: runDeviceRunCode,
}

func init() {
	deviceCmd.AddCommand(deviceRunCodeCmd)
}

func runDeviceRunCode(cmd *cobra.Command, args []string) error {
	identifier, code := args[0], args[1]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	if err := client.RunDeviceCode(cmd.Context(), org, product, identifier, code); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Sent code to device %s\n", identifier)
	return nil
}

// ── scripts (list + run) ────────────────────────────────────────────────────

var deviceScriptsCmd = &cobra.Command{
	Use:   "scripts <identifier>",
	Short: "List the support scripts available to a device",
	Long:  "List the support scripts that can be run on a device.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceScripts,
}

var deviceRunScriptCmd = &cobra.Command{
	Use:   "run-script <identifier> <name-or-id>",
	Short: "Run a support script on a device",
	Long:  "Run a support script on a device and print its output.",
	Args: exactlyTwoArgs(
		"usage: device run-script <identifier> <name-or-id>",
	),
	RunE: runDeviceRunScript,
}

func init() {
	deviceCmd.AddCommand(deviceScriptsCmd)
	deviceCmd.AddCommand(deviceRunScriptCmd)
}

func runDeviceScripts(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	scripts, err := client.ListDeviceScripts(cmd.Context(), org, product, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, scripts)
	}

	if len(scripts) == 0 {
		fmt.Fprintf(w, "No scripts available for device %s.\n", identifier)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "ID\tNAME\tTAGS")
	for _, s := range scripts {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", orDash(s.ID), orDash(s.Name), orDash(s.Tags))
	}
	return tw.Flush()
}

func runDeviceRunScript(cmd *cobra.Command, args []string) error {
	identifier, nameOrID := args[0], args[1]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	output, err := client.SendDeviceScript(cmd.Context(), org, product, identifier, nameOrID)
	if err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), output)
	return nil
}
