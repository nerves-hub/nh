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
	"os"
	"path/filepath"
	"time"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// caVerificationValidity is how long the throwaway verification certificate is
// valid. It is used immediately, so a short window is plenty.
const caVerificationValidity = time.Hour

// caCmd groups organization CA certificate commands.
var caCmd = &cobra.Command{
	Use:   "ca",
	Short: "Manage organization CA certificates",
	Long: `Commands for working with an organization's CA certificates, which sign
device certificates.`,
}

func init() {
	rootCmd.AddCommand(caCmd)
}

// caDir returns the directory holding an organization's local CA material.
func caDir(cfg *config.Config, org string) string {
	return filepath.Join(cfg.DataDir, "ca", org)
}

// caPaths returns the on-disk key and certificate paths for a named CA.
func caPaths(cfg *config.Config, org, name string) (keyPath, certPath string) {
	dir := caDir(cfg, org)
	return filepath.Join(dir, name+"-key.pem"), filepath.Join(dir, name+"-cert.pem")
}

// validCAName rejects names that are unsafe as a filename.
func validCAName(name string) error {
	if name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("invalid CA name %q", name)
	}
	return nil
}

// ── generate ────────────────────────────────────────────────────────────────

var caGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a CA certificate locally",
	Long: `Generate a CA private key (secp256r1) and self-signed CA certificate on
disk.

The common name defaults to the organization name; override it with --name.
Files are written to <data-dir>/ca/<org>/, named by the generation time
(e.g. 2026-06-11-16-41-10-cert.pem). With --upload the CA is also registered
with the organization, proving ownership with a verification certificate.`,
	Args: cobra.NoArgs,
	RunE: runCAGenerate,
}

func init() {
	caGenerateCmd.Flags().String("name", "", "common name for the CA certificate (default: organization name)")
	caGenerateCmd.Flags().String("valid-for", "31y", "validity of the CA certificate (e.g. 31y, 90d)")
	caGenerateCmd.Flags().Bool("upload", false, "register the CA with the organization after generating")
	caGenerateCmd.Flags().String("description", "", "description recorded with the CA certificate (with --upload)")
	caCmd.AddCommand(caGenerateCmd)
}

func runCAGenerate(cmd *cobra.Command, _ []string) error {
	doUpload, _ := cmd.Flags().GetBool("upload")
	description := mustString(cmd, "description")
	if !doUpload && cmd.Flags().Changed("description") {
		return errors.New("--description requires --upload")
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	commonName := mustString(cmd, "name")
	if commonName == "" {
		commonName = org
	}

	validFor, err := parseValidFor(mustString(cmd, "valid-for"))
	if err != nil {
		return err
	}

	// Files are named by the generation time (UTC), so repeated runs don't
	// overwrite earlier CAs. The timestamp is the reference used by
	// `nh ca upload` and `nh device certificates generate --ca`.
	stamp := time.Now().UTC().Format("2006-01-02-15-04-05")
	keyPath, certPath := caPaths(cfg, org, stamp)
	if err := ensureAbsent(keyPath, certPath); err != nil {
		return err
	}

	keyPEM, certPEM, err := pki.GenerateCA(org, commonName, validFor)
	if err != nil {
		return err
	}
	if err := writeCertFiles(caDir(cfg, org), keyPath, keyPEM, certPath, certPEM); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Generated CA %s for %s\n", commonName, org)
	fmt.Fprintf(w, "  private key: %s\n", keyPath)
	fmt.Fprintf(w, "  certificate: %s\n", certPath)

	if !doUpload {
		fmt.Fprintf(w, "\nNothing was uploaded. Register it with `nh ca upload %s`.\n", stamp)
		return nil
	}

	fmt.Fprintln(w)
	if err := uploadCA(cmd, cfg, org, certPEM, keyPEM, description); err != nil {
		return fmt.Errorf("CA saved to %s but registering it failed: %w", certPath, err)
	}
	return nil
}

// ── upload ──────────────────────────────────────────────────────────────────

var caUploadCmd = &cobra.Command{
	Use:   "upload [name]",
	Short: "Register a CA certificate with the organization",
	Long: `Register a CA certificate with the organization.

Provide the name of a CA created with ` + "`nh ca generate`" + `, or point at an
external CA with --cert and --key. Ownership is proven by signing a
verification certificate with the CA's private key, so the key is needed in
both cases.`,
	Args: atMostOneArg("too many arguments: provide a single CA name"),
	RunE: runCAUpload,
}

func init() {
	caUploadCmd.Flags().String("cert", "", "path to the CA certificate (for a CA not managed by nh)")
	caUploadCmd.Flags().String("key", "", "path to the CA private key (required with --cert)")
	caUploadCmd.Flags().String("description", "", "description recorded with the CA certificate")
	caCmd.AddCommand(caUploadCmd)
}

func runCAUpload(cmd *cobra.Command, args []string) error {
	description := mustString(cmd, "description")

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	certPEM, keyPEM, err := resolveCAMaterial(cmd, cfg, org, args)
	if err != nil {
		return err
	}

	return uploadCA(cmd, cfg, org, certPEM, keyPEM, description)
}

// resolveCAMaterial loads a CA's certificate and private key, either from a
// named local CA or from explicit --cert/--key paths.
func resolveCAMaterial(cmd *cobra.Command, cfg *config.Config, org string, args []string) (certPEM, keyPEM []byte, err error) {
	certPath := mustString(cmd, "cert")
	keyPath := mustString(cmd, "key")

	switch {
	case len(args) == 1:
		if certPath != "" || keyPath != "" {
			return nil, nil, errors.New("provide either a CA name or --cert/--key, not both")
		}
		return loadCA(cfg, org, args[0])
	case certPath != "" || keyPath != "":
		if certPath == "" || keyPath == "" {
			return nil, nil, errors.New("--cert and --key must be provided together")
		}
		if certPEM, err = os.ReadFile(certPath); err != nil {
			return nil, nil, err
		}
		if keyPEM, err = os.ReadFile(keyPath); err != nil {
			return nil, nil, err
		}
		return certPEM, keyPEM, nil
	default:
		return nil, nil, errors.New("provide a CA name, or --cert and --key")
	}
}

// uploadCA proves ownership of a CA and registers it with the organization: it
// fetches a verification token, signs a verification certificate with the CA
// key, and posts both the CA and verification certificates.
func uploadCA(cmd *cobra.Command, cfg *config.Config, org string, certPEM, keyPEM []byte, description string) error {
	cert, err := pki.ValidateCertificatePEM(certPEM)
	if err != nil {
		return fmt.Errorf("CA certificate: %w", err)
	}
	if !cert.IsCA {
		return errors.New("the certificate is not a CA certificate")
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	token, err := client.CACertificateVerificationToken(cmd.Context(), org)
	if err != nil {
		return err
	}

	verificationPEM, err := pki.SignVerificationCert(certPEM, keyPEM, token, caVerificationValidity)
	if err != nil {
		return err
	}

	created, err := client.CreateCACertificate(cmd.Context(), org, certPEM, description, verificationPEM)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, created)
	}
	fmt.Fprintf(w, "Registered CA certificate %s with %s\n", orDash(formatSerial(created.Serial)), org)
	return nil
}

// ── list ────────────────────────────────────────────────────────────────────

var caListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an organization's CA certificates",
	Long:  "List the CA certificates registered with an organization.",
	Args:  cobra.NoArgs,
	RunE:  runCAList,
}

func init() {
	caCmd.AddCommand(caListCmd)
}

func runCAList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	certs, err := client.ListCACertificates(cmd.Context(), org)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, certs)
	}

	if len(certs) == 0 {
		fmt.Fprintf(w, "No CA certificates found for %s.\n", org)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "SERIAL\tDESCRIPTION\tNOT BEFORE\tNOT AFTER")
	for _, cert := range certs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			orDash(formatSerial(cert.Serial)), orDash(cert.Description), certDate(cert.NotBefore), certDate(cert.NotAfter))
	}
	return tw.Flush()
}

// ── show ────────────────────────────────────────────────────────────────────

var caShowCmd = &cobra.Command{
	Use:   "show <serial>",
	Short: "Show a CA certificate",
	Long:  "Show details for one of an organization's CA certificates, by serial.",
	Args: exactlyOneArg(
		"CA certificate serial missing",
		"too many arguments: provide a single serial",
	),
	RunE: runCAShow,
}

func init() {
	caCmd.AddCommand(caShowCmd)
}

func runCAShow(cmd *cobra.Command, args []string) error {
	serial := normalizeSerial(args[0])

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	cert, err := client.GetCACertificate(cmd.Context(), org, serial)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, cert)
	}

	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Serial:\t%s\n", formatSerial(cert.Serial))
	fmt.Fprintf(tw, "Description:\t%s\n", orDash(cert.Description))
	fmt.Fprintf(tw, "Not before:\t%s\n", certDate(cert.NotBefore))
	fmt.Fprintf(tw, "Not after:\t%s\n", certDate(cert.NotAfter))
	return tw.Flush()
}

// ── delete ──────────────────────────────────────────────────────────────────

var caDeleteCmd = &cobra.Command{
	Use:   "delete <serial>",
	Short: "Delete a CA certificate",
	Long:  "Remove a CA certificate from an organization, by serial.",
	Args: exactlyOneArg(
		"CA certificate serial missing",
		"too many arguments: provide a single serial",
	),
	RunE: runCADelete,
}

func init() {
	caDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	caCmd.AddCommand(caDeleteCmd)
}

func runCADelete(cmd *cobra.Command, args []string) error {
	serial := normalizeSerial(args[0])

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	if skip, _ := cmd.Flags().GetBool("yes"); !skip {
		if cfg.NonInteractive {
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete CA certificate %s from %s? [y/N]", formatSerial(serial), org))
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

	if err := client.DeleteCACertificate(cmd.Context(), org, serial); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted CA certificate %s from %s\n", formatSerial(serial), org)
	return nil
}
