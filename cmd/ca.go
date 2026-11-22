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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/api"
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
with the organization, proving ownership with a verification certificate, and
the --jitp-* flags give it a just-in-time provisioning profile (see
` + "`nh ca upload --help`" + `).`,
	Args: cobra.NoArgs,
	RunE: runCAGenerate,
}

func init() {
	caGenerateCmd.Flags().String("name", "", "common name for the CA certificate (default: organization name)")
	caGenerateCmd.Flags().String("valid-for", "31y", "validity of the CA certificate (e.g. 31y, 90d)")
	caGenerateCmd.Flags().Bool("upload", false, "register the CA with the organization after generating")
	caGenerateCmd.Flags().String("description", "", "description recorded with the CA certificate (with --upload)")
	addJITPFlags(caGenerateCmd, " (with --upload)")
	caCmd.AddCommand(caGenerateCmd)
}

func runCAGenerate(cmd *cobra.Command, _ []string) error {
	doUpload, _ := cmd.Flags().GetBool("upload")
	description := mustString(cmd, "description")
	if !doUpload && cmd.Flags().Changed("description") {
		return errors.New("--description requires --upload")
	}
	if !doUpload && jitpFlagsChanged(cmd) {
		return errors.New("the --jitp-* flags require --upload")
	}
	jitp, err := jitpFromFlags(cmd)
	if err != nil {
		return err
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
	if err := uploadCA(cmd, cfg, org, certPEM, keyPEM, description, jitp); err != nil {
		// The CA was registered, just without its JITP profile: saying
		// registering failed would send the user to upload it again.
		var notSaved *jitpNotSavedError
		if errors.As(err, &notSaved) {
			return err
		}
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
both cases.

Just-in-time provisioning (JITP): with --jitp-product, a device that connects
with a certificate signed by this CA, and that NervesHub doesn't know yet, is
created in that product (which must belong to the organization) on its first
connection. --jitp-tag and --jitp-description set the new device's tags and
description. Naming the product needs NervesHub v2.5.0 or later.`,
	Example: `  # Register a CA whose devices join the "sensor" product, tagged "prod"
  nh ca upload 2026-06-11-16-41-10 --jitp-product sensor --jitp-tag prod`,
	Args: atMostOneArg("too many arguments: provide a single CA name"),
	RunE: runCAUpload,
}

func init() {
	caUploadCmd.Flags().String("cert", "", "path to the CA certificate (for a CA not managed by nh)")
	caUploadCmd.Flags().String("key", "", "path to the CA private key (required with --cert)")
	caUploadCmd.Flags().String("description", "", "description recorded with the CA certificate")
	addJITPFlags(caUploadCmd, "")
	caCmd.AddCommand(caUploadCmd)
}

func runCAUpload(cmd *cobra.Command, args []string) error {
	description := mustString(cmd, "description")
	jitp, err := jitpFromFlags(cmd)
	if err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	certPEM, keyPEM, err := resolveCAMaterial(cmd, cfg, org, args)
	if err != nil {
		return err
	}

	return uploadCA(cmd, cfg, org, certPEM, keyPEM, description, jitp)
}

// jitpFlags are the flags that build a CA's JITP profile.
var jitpFlags = []string{"jitp-product", "jitp-tag", "jitp-description"}

// addJITPFlags adds the JITP profile flags to c. suffix is appended to each
// flag's usage.
func addJITPFlags(c *cobra.Command, suffix string) {
	c.Flags().String("jitp-product", "", "product that devices signed by this CA join on first connect (enables JITP)"+suffix)
	c.Flags().StringArray("jitp-tag", nil, "tag given to devices created by JITP; repeatable or comma-separated"+suffix)
	c.Flags().String("jitp-description", "", "description given to devices created by JITP"+suffix)
}

// jitpFlagsChanged reports whether any JITP flag was given.
func jitpFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range jitpFlags {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// jitpFromFlags reads the JITP flags into a profile, or returns nil when none
// were given. NervesHub requires a JITP profile's product, so the tag and
// description flags need --jitp-product.
func jitpFromFlags(cmd *cobra.Command) (*api.CACertificateJITP, error) {
	if !jitpFlagsChanged(cmd) {
		return nil, nil
	}
	product := strings.TrimSpace(mustString(cmd, "jitp-product"))
	if product == "" {
		return nil, errors.New("--jitp-product is required to set up JITP (--jitp-tag and --jitp-description need it)")
	}
	raw, _ := cmd.Flags().GetStringArray("jitp-tag")
	tags := splitTags(raw)
	for _, tag := range tags {
		if strings.ContainsAny(tag, " \t") {
			return nil, fmt.Errorf("invalid --jitp-tag %q: tags cannot contain spaces", tag)
		}
	}
	return &api.CACertificateJITP{
		ProductName: product,
		Tags:        tags,
		Description: mustString(cmd, "jitp-description"),
	}, nil
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
func uploadCA(cmd *cobra.Command, cfg *config.Config, org string, certPEM, keyPEM []byte, description string, jitp *api.CACertificateJITP) error {
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

	created, err := client.CreateCACertificate(cmd.Context(), org, api.CACertificateInput{
		Cert:             certPEM,
		VerificationCert: verificationPEM,
		Description:      description,
		JITP:             jitp,
	})
	if err != nil {
		return jitpVersionHint(err, jitp)
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if err := printJSON(w, created); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "Registered CA certificate %s with %s\n", orDash(formatSerial(created.Serial)), org)
		if created.JITP != nil {
			fmt.Fprintf(w, "New devices it signs join %s on first connect (JITP)\n", orDash(created.JITP.ProductName))
		}
	}
	if jitp != nil && created.JITP == nil {
		return &jitpNotSavedError{serial: created.Serial}
	}
	return nil
}

// jitpNotSavedError reports a CA that was registered without the JITP profile
// it was sent.
type jitpNotSavedError struct {
	serial string
}

func (e *jitpNotSavedError) Error() string {
	return fmt.Sprintf("registered CA certificate %s, but the server didn't save its JITP profile", orDash(formatSerial(e.serial)))
}

// jitpVersionHint explains a registration that failed because the server is
// too old to name a JITP profile's product. NervesHub before v2.5.0 ignores
// jitp.product_name and requires jitp.product_id, so it answers 422 with
// "can't be blank" for product_id. Later servers set product_id from the name,
// so that answer means an old server.
func jitpVersionHint(err error, jitp *api.CACertificateJITP) error {
	var apiErr *api.APIError
	if jitp == nil || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		return err
	}
	body := string(apiErr.Body)
	if strings.Contains(body, "product_id") && strings.Contains(body, "can't be blank") {
		return fmt.Errorf("%w\nnaming the JITP product (--jitp-product) needs NervesHub v2.5.0 or later", err)
	}
	return err
}

// jitpProduct returns the product a CA's JITP profile provisions into, or "-"
// when the CA has no JITP profile.
func jitpProduct(jitp *api.CACertificateJITP) string {
	if jitp == nil {
		return "-"
	}
	return orDash(jitp.ProductName)
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
	fmt.Fprintln(tw, "SERIAL\tDESCRIPTION\tNOT BEFORE\tNOT AFTER\tJITP PRODUCT")
	for _, cert := range certs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			orDash(formatSerial(cert.Serial)), orDash(cert.Description), certDate(cert.NotBefore), certDate(cert.NotAfter),
			jitpProduct(cert.JITP))
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
	if cert.JITP == nil {
		fmt.Fprintf(tw, "JITP:\t%s\n", "off")
	} else {
		fmt.Fprintf(tw, "JITP product:\t%s\n", orDash(cert.JITP.ProductName))
		fmt.Fprintf(tw, "JITP tags:\t%s\n", orDash(strings.Join(cert.JITP.Tags, ", ")))
		fmt.Fprintf(tw, "JITP description:\t%s\n", orDash(cert.JITP.Description))
	}
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
