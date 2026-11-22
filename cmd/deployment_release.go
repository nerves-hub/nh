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
	"slices"
	"strconv"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deploymentReleaseCmd groups the commands for a deployment group's releases.
var deploymentReleaseCmd = &cobra.Command{
	Use:     "release",
	Aliases: []string{"releases"},
	Short:   "Manage a deployment group's releases",
	Long: `Commands for a deployment group's releases. Each release pairs a firmware
(and optionally an archive) with how it's sent, and the newest is the group's
current release. A release is named by its number within the group.

These commands need a NervesHub server with the deployment release API. An
older server says so rather than answering.`,
}

func init() {
	deploymentCmd.AddCommand(deploymentReleaseCmd)
}

// ── list ────────────────────────────────────────────────────────────────────

var deploymentReleaseListCmd = &cobra.Command{
	Use:   "list <group>",
	Short: "List a deployment group's releases",
	Long:  "List a deployment group's releases, newest first. The current release is marked with *.",
	Args: exactlyOneArg(
		"deployment group name missing",
		"too many arguments: provide a single deployment group name",
	),
	RunE: runDeploymentReleaseList,
}

func init() {
	deploymentReleaseCmd.AddCommand(deploymentReleaseListCmd)
}

func runDeploymentReleaseList(cmd *cobra.Command, args []string) error {
	group := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	releases, err := client.ListDeploymentReleases(cmd.Context(), org, product, group)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, releases)
	}

	if len(releases) == 0 {
		fmt.Fprintf(w, "Deployment group %q in %s/%s has no releases.\n", group, org, product)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "NUMBER\tCURRENT\tVERSION\tFIRMWARE\tARCHIVE\tREQUIRED\tDELTA\tCREATED BY\tCREATED")
	for _, r := range releases {
		current := ""
		if r.IsCurrent {
			current = "*"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Number, current, orDash(r.Firmware.Version), orDash(r.Firmware.UUID),
			orDash(r.ArchiveUUID), yesNo(r.Required), orDash(r.DeltaStatus),
			releaseAuthorLabel(r.CreatedBy), createdAt(r.InsertedAt))
	}
	return tw.Flush()
}

// ── show ────────────────────────────────────────────────────────────────────

var deploymentReleaseShowCmd = &cobra.Command{
	Use:   "show <group> <number>",
	Short: "Show one of a deployment group's releases",
	Long:  "Show the details of one release of a deployment group, by its number.",
	Args:  exactlyTwoArgs("usage: nh deployment release show <group> <number>"),
	RunE:  runDeploymentReleaseShow,
}

func init() {
	deploymentReleaseCmd.AddCommand(deploymentReleaseShowCmd)
}

func runDeploymentReleaseShow(cmd *cobra.Command, args []string) error {
	group := args[0]
	number, err := parseReleaseNumber(args[1])
	if err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	r, err := client.GetDeploymentRelease(cmd.Context(), org, product, group, number)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, r)
	}
	return printRelease(w, r)
}

// printRelease writes a release's details as a two-column table.
func printRelease(w io.Writer, r *api.DeploymentRelease) error {
	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Number:\t%d\n", r.Number)
	fmt.Fprintf(tw, "Current:\t%s\n", yesNo(r.IsCurrent))
	fmt.Fprintf(tw, "Firmware:\t%s\n", orDash(r.Firmware.UUID))
	fmt.Fprintf(tw, "Version:\t%s\n", orDash(r.Firmware.Version))
	fmt.Fprintf(tw, "Platform:\t%s\n", orDash(r.Firmware.Platform))
	fmt.Fprintf(tw, "Architecture:\t%s\n", orDash(r.Firmware.Architecture))
	fmt.Fprintf(tw, "Archive:\t%s\n", orDash(r.ArchiveUUID))
	fmt.Fprintf(tw, "Required:\t%s\n", yesNo(r.Required))
	fmt.Fprintf(tw, "Delta status:\t%s\n", orDash(r.DeltaStatus))
	fmt.Fprintf(tw, "Created by:\t%s\n", releaseAuthorLabel(r.CreatedBy))
	fmt.Fprintf(tw, "Created:\t%s\n", createdAt(r.InsertedAt))
	fmt.Fprintf(tw, "Updated:\t%s\n", createdAt(r.UpdatedAt))
	writeMultiline(tw, "Description:", r.Description)
	writeMultiline(tw, "Notes:", r.Notes)
	fmt.Fprintf(tw, "Connecting code mode:\t%s\n", orDash(r.ConnectingCodeMode))
	writeMultiline(tw, "Connecting code:", r.ConnectingCode)
	return tw.Flush()
}

// writeMultiline writes free text as a labelled row per line, so a newline
// doesn't break the table.
func writeMultiline(w io.Writer, label, text string) {
	for i, line := range strings.Split(orDash(strings.TrimRight(text, "\n")), "\n") {
		if i > 0 {
			label = ""
		}
		fmt.Fprintf(w, "%s\t%s\n", label, strings.TrimRight(line, "\r"))
	}
}

// releaseAuthorLabel describes who created a release.
func releaseAuthorLabel(a *api.ReleaseAuthor) string {
	switch {
	case a == nil:
		return "-"
	case a.Type == "api_key":
		return "API key " + orDash(a.Name)
	case a.Email != "" && a.Name != "":
		return a.Name + " <" + a.Email + ">"
	default:
		return orDash(firstNonEmptyString(a.Name, a.Email))
	}
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ── create ──────────────────────────────────────────────────────────────────

var deploymentReleaseCreateCmd = &cobra.Command{
	Use:   "create <group>",
	Short: "Create a release, which becomes the group's current release",
	Long: `Create a release in a deployment group. It becomes the group's current
release, and devices in the group are updated to it.

--firmware is required. The release has no archive unless you pass --archive,
whatever the previous release had. --required makes devices install it on
the way to any later release.

--connecting-code (or --connecting-code-file, - for stdin) is Elixir the
device runs when it connects while this is its release. --connecting-code-mode
says where it runs relative to the group's connecting code: first, last (the
server's default) or override.

The server refuses a release with the same firmware and archive as the
current one. To change whether a release is required or its connecting code,
use "nh deployment release update".`,
	Args: exactlyOneArg(
		"deployment group name missing",
		"too many arguments: provide a single deployment group name",
	),
	RunE: runDeploymentReleaseCreate,
}

var deploymentReleaseUpdateCmd = &cobra.Command{
	Use:   "update <group> <number>",
	Short: "Change whether a release is required, or its connecting code",
	Long: `Change a release of a deployment group. Only the fields you pass are changed.

--required (or --required=false) says whether devices install the release on
the way to any later release. --connecting-code or --connecting-code-file
(- for stdin) replaces the release's connecting code, and
--clear-connecting-code removes it. --connecting-code-mode is first, last or
override.

A release's firmware, archive, description and notes can't change once it's
made. Create a new release to send different firmware.`,
	Args: exactlyTwoArgs("usage: nh deployment release update <group> <number> [flags]"),
	RunE: runDeploymentReleaseUpdate,
}

func init() {
	f := deploymentReleaseCreateCmd.Flags()
	f.String("firmware", "", "firmware UUID for the release (required)")
	f.String("archive", "", "archive UUID for the release")
	f.Bool("required", false, "mark the release required")
	f.String("description", "", "short description of the release")
	f.String("notes", "", "notes about the release")
	deploymentReleaseCmd.AddCommand(deploymentReleaseCreateCmd)

	u := deploymentReleaseUpdateCmd.Flags()
	u.Bool("required", false, "whether the release is required (--required=false to unmark it)")
	u.Bool("clear-connecting-code", false, "remove the release's connecting code")
	deploymentReleaseCmd.AddCommand(deploymentReleaseUpdateCmd)

	for _, c := range []*cobra.Command{deploymentReleaseCreateCmd, deploymentReleaseUpdateCmd} {
		c.Flags().String("connecting-code", "", "Elixir the device runs when it connects")
		c.Flags().String("connecting-code-file", "", "read the connecting code from a file (use - for stdin)")
		c.Flags().String("connecting-code-mode", "", "where the connecting code runs relative to the group's: first, last or override")
	}
}

// connectingCodeFromFlags returns the code from --connecting-code or
// --connecting-code-file, and whether either was given. An empty code is
// refused: on update it would silently remove the code, which
// --clear-connecting-code says outright.
func connectingCodeFromFlags(cmd *cobra.Command) (string, bool, error) {
	flags := cmd.Flags()
	codeSet, fileSet := flags.Changed("connecting-code"), flags.Changed("connecting-code-file")
	var code string
	switch {
	case codeSet && fileSet:
		return "", false, errors.New("use only one of --connecting-code or --connecting-code-file")
	case codeSet:
		code = mustString(cmd, "connecting-code")
	case fileSet:
		path := mustString(cmd, "connecting-code-file")
		if path == "" {
			return "", false, errors.New("--connecting-code-file cannot be empty")
		}
		b, err := readFileOrStdin(cmd, path)
		if err != nil {
			return "", false, fmt.Errorf("reading the connecting code: %w", err)
		}
		code = string(b)
	default:
		return "", false, nil
	}
	if strings.TrimSpace(code) == "" {
		msg := "the connecting code is empty"
		if flags.Lookup("clear-connecting-code") != nil {
			msg += "; use --clear-connecting-code to remove it"
		}
		return "", false, errors.New(msg)
	}
	return code, true, nil
}

// connectingCodeModeFromFlags returns --connecting-code-mode, checked.
func connectingCodeModeFromFlags(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed("connecting-code-mode") {
		return "", nil
	}
	mode := mustString(cmd, "connecting-code-mode")
	if !slices.Contains(api.ConnectingCodeModes, mode) {
		return "", fmt.Errorf("invalid --connecting-code-mode %q (use first, last or override)", mode)
	}
	return mode, nil
}

func runDeploymentReleaseCreate(cmd *cobra.Command, args []string) error {
	group := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	flags := cmd.Flags()
	in := api.ReleaseInput{
		Firmware:    mustString(cmd, "firmware"),
		Archive:     mustString(cmd, "archive"),
		Description: mustString(cmd, "description"),
		Notes:       mustString(cmd, "notes"),
	}
	if in.Firmware == "" {
		return errors.New("--firmware is required")
	}
	if flags.Changed("archive") && in.Archive == "" {
		return errors.New("--archive cannot be empty")
	}
	if flags.Changed("required") {
		v, _ := flags.GetBool("required")
		in.Required = &v
	}
	if in.ConnectingCode, _, err = connectingCodeFromFlags(cmd); err != nil {
		return err
	}
	if in.ConnectingCodeMode, err = connectingCodeModeFromFlags(cmd); err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	r, err := client.CreateDeploymentRelease(cmd.Context(), org, product, group, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, r)
	}
	fmt.Fprintf(w, "Created release %d of deployment group %q in %s/%s (firmware %s)\n",
		r.Number, group, org, product, releaseFirmwareLabel(r))
	return nil
}

func runDeploymentReleaseUpdate(cmd *cobra.Command, args []string) error {
	group := args[0]
	number, err := parseReleaseNumber(args[1])
	if err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	flags := cmd.Flags()
	var in api.ReleaseUpdate
	if flags.Changed("required") {
		v, _ := flags.GetBool("required")
		in.Required = &v
	}
	code, codeSet, err := connectingCodeFromFlags(cmd)
	if err != nil {
		return err
	}
	clearCode, _ := flags.GetBool("clear-connecting-code")
	switch {
	case clearCode && codeSet:
		return errors.New("use only one of --clear-connecting-code, --connecting-code or --connecting-code-file")
	case codeSet:
		in.ConnectingCode = &code
	case clearCode:
		empty := ""
		in.ConnectingCode = &empty
	}
	if in.ConnectingCodeMode, err = connectingCodeModeFromFlags(cmd); err != nil {
		return err
	}
	if in.Required == nil && in.ConnectingCode == nil && in.ConnectingCodeMode == "" {
		return errors.New("nothing to update: provide --required, --connecting-code, --connecting-code-file, --clear-connecting-code, or --connecting-code-mode")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	r, err := client.UpdateDeploymentRelease(cmd.Context(), org, product, group, number, in)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, r)
	}
	fmt.Fprintf(w, "Updated release %d of deployment group %q in %s/%s\n", r.Number, group, org, product)
	return nil
}

// parseReleaseNumber reads a release number argument.
func parseReleaseNumber(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid release number %q: use the release's number from \"nh deployment release list\"", arg)
	}
	return n, nil
}

// releaseFirmwareLabel names a release's firmware by version and UUID.
func releaseFirmwareLabel(r *api.DeploymentRelease) string {
	if r.Firmware.Version == "" {
		return orDash(r.Firmware.UUID)
	}
	return r.Firmware.Version + " (" + r.Firmware.UUID + ")"
}
