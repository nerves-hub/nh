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
	"sort"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// ownerFilters are the accepted values for `--owner`, matching the owner.type
// rendered in the response.
var ownerFilters = []string{"device", "user", "none"}

// irohEndpointCmd groups organization iroh endpoint commands.
var irohEndpointCmd = &cobra.Command{
	Use:     "iroh-endpoint",
	Aliases: []string{"iroh"},
	Short:   "Manage an organization's iroh endpoint ids",
	Long: `Commands for an organization's iroh endpoint ids — the same registry the
Iroh Endpoints page manages, addressed by the endpoint id itself.

Endpoints are scoped to an organization (set with --org or NERVES_HUB_ORG).`,
}

func init() {
	rootCmd.AddCommand(irohEndpointCmd)
}

// ── list ────────────────────────────────────────────────────────────────────

var irohEndpointListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an organization's iroh endpoint ids",
	Long: `List the iroh endpoint ids registered with an organization, newest first.

--search matches the start of an endpoint id, or part of a device identifier or
member name.`,
	Args: cobra.NoArgs,
	RunE: runIrohEndpointList,
}

func init() {
	irohEndpointListCmd.Flags().String("owner", "", "only endpoints held by this owner type: device, user, or none")
	irohEndpointListCmd.Flags().String("search", "", "match the start of an endpoint id, or part of a device identifier or member name")
	irohEndpointCmd.AddCommand(irohEndpointListCmd)
}

func runIrohEndpointList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	owner := mustString(cmd, "owner")
	if err := validateOwnerFilter(owner); err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	endpoints, err := client.ListIrohEndpoints(cmd.Context(), org, api.IrohEndpointFilter{
		Owner:  owner,
		Search: mustString(cmd, "search"),
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, endpoints)
	}

	if len(endpoints) == 0 {
		fmt.Fprintf(w, "No iroh endpoints found for %s.\n", org)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "IDENTIFIER\tINSTANCE\tSOURCE\tOWNER\tLAST REPORTED")
	for _, e := range endpoints {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.Identifier, orDash(e.Instance), orDash(e.Source), formatOwner(e.Owner), certDate(e.LastReportedAt))
	}
	return tw.Flush()
}

// ── register ────────────────────────────────────────────────────────────────

var irohEndpointRegisterCmd = &cobra.Command{
	Use:   "register <identifier>",
	Short: "Register an iroh endpoint id",
	Long: `Register an iroh endpoint id with an organization.

The identifier is the endpoint id — the public key the endpoint proves it holds,
not a ticket or relay URL. Attach it to a member's machine with --user-email;
omit it for one the organization holds directly, or for a device that will claim
it on its next connection.

An id already registered anywhere is refused.`,
	Args: exactlyOneArg(
		"endpoint identifier missing",
		"too many arguments: provide a single endpoint identifier",
	),
	RunE: runIrohEndpointRegister,
}

func init() {
	irohEndpointRegisterCmd.Flags().String("instance", "", "which endpoint this is, for a device running more than one (default: default)")
	irohEndpointRegisterCmd.Flags().String("user-email", "", "attach the endpoint to this member of the organization")
	irohEndpointRegisterCmd.Flags().StringArray("detail", nil, "metadata to record, as key=value (repeatable); never a secret")
	irohEndpointCmd.AddCommand(irohEndpointRegisterCmd)
}

func runIrohEndpointRegister(cmd *cobra.Command, args []string) error {
	identifier := strings.TrimSpace(args[0])
	if identifier == "" {
		return errors.New("endpoint identifier missing")
	}

	detailPairs, _ := cmd.Flags().GetStringArray("detail")
	details, err := parseDetails(detailPairs)
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

	endpoint, err := client.RegisterIrohEndpoint(cmd.Context(), org, api.IrohEndpointInput{
		Identifier: identifier,
		Instance:   mustString(cmd, "instance"),
		UserEmail:  mustString(cmd, "user-email"),
		Details:    details,
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, endpoint)
	}
	fmt.Fprintf(w, "Registered iroh endpoint %s with %s\n", endpoint.Identifier, org)
	return nil
}

// ── show ────────────────────────────────────────────────────────────────────

var irohEndpointShowCmd = &cobra.Command{
	Use:   "show <identifier>",
	Short: "Show an iroh endpoint id",
	Long:  "Show details for one of an organization's iroh endpoint ids.",
	Args: exactlyOneArg(
		"endpoint identifier missing",
		"too many arguments: provide a single endpoint identifier",
	),
	RunE: runIrohEndpointShow,
}

func init() {
	irohEndpointCmd.AddCommand(irohEndpointShowCmd)
}

func runIrohEndpointShow(cmd *cobra.Command, args []string) error {
	identifier := strings.TrimSpace(args[0])

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	endpoint, err := client.GetIrohEndpoint(cmd.Context(), org, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, endpoint)
	}

	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Identifier:\t%s\n", endpoint.Identifier)
	fmt.Fprintf(tw, "Service:\t%s\n", orDash(endpoint.Service))
	fmt.Fprintf(tw, "Instance:\t%s\n", orDash(endpoint.Instance))
	fmt.Fprintf(tw, "Source:\t%s\n", orDash(endpoint.Source))
	fmt.Fprintf(tw, "Owner:\t%s\n", formatOwner(endpoint.Owner))
	fmt.Fprintf(tw, "Last reported:\t%s\n", formatTimestamp(&endpoint.LastReportedAt))
	fmt.Fprintf(tw, "Created:\t%s\n", formatTimestamp(&endpoint.InsertedAt))
	fmt.Fprintf(tw, "Updated:\t%s\n", formatTimestamp(&endpoint.UpdatedAt))
	writeDetails(tw, endpoint.Details)
	return tw.Flush()
}

// ── delete ──────────────────────────────────────────────────────────────────

var irohEndpointDeleteCmd = &cobra.Command{
	Use:   "delete <identifier>",
	Short: "Delete an iroh endpoint id",
	Long: `Remove an iroh endpoint id from an organization.

An endpoint a device reported is removed too, but the device records it again on
its next connection — deleting one is not a way to stop a device holding a key.`,
	Args: exactlyOneArg(
		"endpoint identifier missing",
		"too many arguments: provide a single endpoint identifier",
	),
	RunE: runIrohEndpointDelete,
}

func init() {
	irohEndpointDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	irohEndpointCmd.AddCommand(irohEndpointDeleteCmd)
}

func runIrohEndpointDelete(cmd *cobra.Command, args []string) error {
	identifier := strings.TrimSpace(args[0])

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete iroh endpoint %s from %s? [y/N]", identifier, org))
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

	if err := client.DeleteIrohEndpoint(cmd.Context(), org, identifier); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted iroh endpoint %s from %s\n", identifier, org)
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

// validateOwnerFilter checks a --owner value against the accepted set, giving a
// clear local error rather than relying on the server's 422.
func validateOwnerFilter(owner string) error {
	if owner == "" {
		return nil
	}
	for _, v := range ownerFilters {
		if owner == v {
			return nil
		}
	}
	return fmt.Errorf("invalid --owner %q: must be one of %s", owner, strings.Join(ownerFilters, ", "))
}

// formatOwner renders an endpoint's owner for display: the identifying detail
// alongside the type, so a table row says whose a key is at a glance.
func formatOwner(o api.IrohEndpointOwner) string {
	switch o.Type {
	case "device":
		return "device (" + orDash(o.DeviceIdentifier) + ")"
	case "user":
		return "user (" + orDash(o.UserEmail) + ")"
	case "none", "":
		return "none"
	default:
		return o.Type
	}
}

// parseDetails turns repeated key=value flags into a details map. It errors on
// an entry without an "=" so a typo is caught rather than silently dropped.
func parseDetails(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	details := make(map[string]any, len(pairs))
	for _, p := range pairs {
		key, value, found := strings.Cut(p, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("invalid --detail %q: expected key=value", p)
		}
		details[key] = value
	}
	return details, nil
}

// writeDetails prints a details map to the show table, sorted for stable output.
func writeDetails(tw io.Writer, details map[string]any) {
	if len(details) == 0 {
		return
	}
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(tw, "Details:")
	for _, k := range keys {
		fmt.Fprintf(tw, "  %s:\t%v\n", k, details[k])
	}
}
