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

// deploymentCmd groups product deployment-group commands.
var deploymentCmd = &cobra.Command{
	Use:     "deployment",
	Aliases: []string{"deployments"},
	Short:   "Manage deployment groups",
	Long:    "Commands for working with a product's deployment groups, which roll firmware out to matching devices.",
}

func init() {
	rootCmd.AddCommand(deploymentCmd)
}

// ── list ────────────────────────────────────────────────────────────────────

var deploymentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List a product's deployment groups",
	Long:  "List the deployment groups for a product.",
	Args:  cobra.NoArgs,
	RunE:  runDeploymentList,
}

func init() {
	deploymentCmd.AddCommand(deploymentListCmd)
}

func runDeploymentList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	deployments, err := client.ListDeployments(cmd.Context(), org, product)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, deployments)
	}

	if len(deployments) == 0 {
		fmt.Fprintf(w, "No deployment groups found in %s/%s.\n", org, product)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NAME\tSTATE\tACTIVE\tDEVICES\tVERSION\tFIRMWARE")
	for _, d := range deployments {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			d.Name, orDash(d.State), yesNo(d.IsActive), d.DeviceCount,
			orDash(d.Conditions.Version), orDash(d.FirmwareUUID))
	}
	return tw.Flush()
}

// ── show ────────────────────────────────────────────────────────────────────

var deploymentShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a deployment group",
	Long:  "Show details for a single deployment group, by name.",
	Args: exactlyOneArg(
		"deployment name missing",
		"too many arguments: provide a single deployment name",
	),
	RunE: runDeploymentShow,
}

func init() {
	deploymentCmd.AddCommand(deploymentShowCmd)
}

func runDeploymentShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	d, err := client.GetDeployment(cmd.Context(), org, product, name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, d)
	}

	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Name:\t%s\n", d.Name)
	fmt.Fprintf(tw, "State:\t%s\n", orDash(d.State))
	fmt.Fprintf(tw, "Active:\t%s\n", yesNo(d.IsActive))
	fmt.Fprintf(tw, "Devices:\t%d\n", d.DeviceCount)
	fmt.Fprintf(tw, "Releases:\t%d\n", d.ReleasesCount)
	fmt.Fprintf(tw, "Delta updatable:\t%s\n", yesNo(d.DeltaUpdatable))
	fmt.Fprintf(tw, "Version condition:\t%s\n", orDash(d.Conditions.Version))
	fmt.Fprintf(tw, "Tag conditions:\t%s\n", orDash(strings.Join(d.Conditions.Tags, ", ")))
	fmt.Fprintf(tw, "Firmware:\t%s\n", orDash(d.FirmwareUUID))
	if r := d.CurrentRelease; r != nil {
		fmt.Fprintf(tw, "Current release:\t#%d (%s %s)\n",
			r.Number, orDash(r.Firmware.Version), orDash(r.Firmware.UUID))
	}
	return tw.Flush()
}

// ── delete ──────────────────────────────────────────────────────────────────

var deploymentDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a deployment group",
	Long:  "Remove a deployment group from a product, by name.",
	Args: exactlyOneArg(
		"deployment name missing",
		"too many arguments: provide a single deployment name",
	),
	RunE: runDeploymentDelete,
}

func init() {
	deploymentDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	deploymentCmd.AddCommand(deploymentDeleteCmd)
}

func runDeploymentDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete deployment group %q from %s/%s? [y/N]", name, org, product))
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

	if err := client.DeleteDeployment(cmd.Context(), org, product, name); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted deployment group %q from %s/%s\n", name, org, product)
	return nil
}

// ── create ──────────────────────────────────────────────────────────────────

var deploymentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a deployment group",
	Long: `Create a deployment group in a product.

A firmware UUID is required (--firmware). Conditions target which devices the
deployment applies to: --version sets a version requirement and --tag
(repeatable) sets required tags.`,
	Args: exactlyOneArg(
		"deployment name missing",
		"too many arguments: provide a single deployment name",
	),
	RunE: runDeploymentCreate,
}

var deploymentUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a deployment group",
	Long: `Update a deployment group. Only the fields you pass are changed:
--firmware, --state, --version, --tag, --delta-updatable.`,
	Args: exactlyOneArg(
		"deployment name missing",
		"too many arguments: provide a single deployment name",
	),
	RunE: runDeploymentUpdate,
}

func init() {
	for _, c := range []*cobra.Command{deploymentCreateCmd, deploymentUpdateCmd} {
		c.Flags().String("firmware", "", "firmware UUID to deploy")
		c.Flags().String("state", "", "deployment state: on or off")
		c.Flags().String("version", "", "version condition (e.g. \">= 1.0.0\")")
		c.Flags().StringArray("tag", nil, "tag condition; repeatable")
		c.Flags().Bool("delta-updatable", false, "enable delta firmware updates")
		deploymentCmd.AddCommand(c)
	}
}

// deploymentInputFromFlags reads the deployment write flags, returning the
// input, whether any field was provided, and a validation error.
func deploymentInputFromFlags(cmd *cobra.Command) (api.DeploymentInput, bool, error) {
	var in api.DeploymentInput
	changed := false
	if cmd.Flags().Changed("firmware") {
		in.Firmware = mustString(cmd, "firmware")
		changed = true
	}
	if cmd.Flags().Changed("state") {
		in.State = mustString(cmd, "state")
		if in.State != "on" && in.State != "off" {
			return in, false, fmt.Errorf("invalid --state %q (use on or off)", in.State)
		}
		changed = true
	}
	if cmd.Flags().Changed("version") {
		in.Version = mustString(cmd, "version")
		changed = true
	}
	if cmd.Flags().Changed("tag") {
		in.Tags, _ = cmd.Flags().GetStringArray("tag")
		changed = true
	}
	if cmd.Flags().Changed("delta-updatable") {
		v, _ := cmd.Flags().GetBool("delta-updatable")
		in.DeltaUpdatable = &v
		changed = true
	}
	return in, changed, nil
}

func runDeploymentCreate(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	in, _, err := deploymentInputFromFlags(cmd)
	if err != nil {
		return err
	}
	if in.Firmware == "" {
		return errors.New("--firmware is required")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	d, err := client.CreateDeployment(cmd.Context(), org, product, name, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, d)
	}
	fmt.Fprintf(w, "Created deployment group %q in %s/%s\n", d.Name, org, product)
	return nil
}

func runDeploymentUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	in, changed, err := deploymentInputFromFlags(cmd)
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("nothing to update: provide --firmware, --state, --version, --tag, or --delta-updatable")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	d, err := client.UpdateDeployment(cmd.Context(), org, product, name, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, d)
	}
	fmt.Fprintf(w, "Updated deployment group %q in %s/%s\n", d.Name, org, product)
	return nil
}

// requireOrgProduct resolves both the org and product scope, returning the
// first error.
func requireOrgProduct(cfg *config.Config) (org, product string, err error) {
	if org, err = requireOrg(cfg); err != nil {
		return "", "", err
	}
	if product, err = requireProduct(cfg); err != nil {
		return "", "", err
	}
	return org, product, nil
}
