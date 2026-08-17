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
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// deviceCertificatesCmd groups device-certificate commands.
var deviceCertificatesCmd = &cobra.Command{
	Use:     "certificates",
	Aliases: []string{"certs"},
	Short:   "Manage a device's certificates",
	Long:    "Commands for working with the X.509 certificates assigned to a device.",
}

func init() {
	deviceCmd.AddCommand(deviceCertificatesCmd)
}

// certSerialArgs requires a device identifier and a certificate serial.
func certSerialArgs(usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 2 {
			return errors.New(usage)
		}
		return nil
	}
}

// ── list ────────────────────────────────────────────────────────────────────

var deviceCertListCmd = &cobra.Command{
	Use:   "list <identifier>",
	Short: "List a device's certificates",
	Long:  "List the X.509 certificates associated with a device by its identifier.",
	Args:  deviceIdentifierArgs,
	RunE:  runDeviceCertList,
}

func init() {
	deviceCertificatesCmd.AddCommand(deviceCertListCmd)
}

func runDeviceCertList(cmd *cobra.Command, args []string) error {
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

	certs, err := client.ListDeviceCertificates(cmd.Context(), org, product, identifier)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, certs)
	}

	if len(certs) == 0 {
		fmt.Fprintf(w, "No certificates found for device %s.\n", identifier)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "SERIAL\tNOT BEFORE\tNOT AFTER")
	for _, cert := range certs {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", orDash(formatSerial(cert.Serial)), certDate(cert.NotBefore), certDate(cert.NotAfter))
	}
	return tw.Flush()
}

// certDate renders a certificate validity date, using "-" for a zero value.
func certDate(ts api.Timestamp) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02")
}

// formatSerial renders an API certificate serial (a decimal string) as
// colon-separated uppercase hex bytes, e.g. "99887766" -> "05:F4:2A:96".
// Values that are not decimal integers are returned unchanged.
func formatSerial(s string) string {
	n, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok || n.Sign() < 0 {
		return s
	}
	b := n.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, ":")
}

// normalizeSerial converts a user-supplied serial to the decimal form the API
// expects. It accepts the hex forms nh displays ("05:F4:2A:96"),
// 0x-prefixed hex, and bare hex containing letters; plain digits are already
// decimal and pass through, as does anything unrecognizable.
func normalizeSerial(s string) string {
	t := strings.TrimSpace(s)
	cleaned := strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(strings.ToUpper(t), "0X"), "0x"), ":", "")
	if cleaned == "" {
		return t
	}

	hasLetter := false
	for _, r := range cleaned {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'A' && r <= 'F':
			hasLetter = true
		default:
			return t // not hex at all; let the server decide
		}
	}

	marked := strings.Contains(t, ":") || strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X")
	if !marked && !hasLetter {
		return t // plain digits: already decimal
	}
	n, ok := new(big.Int).SetString(cleaned, 16)
	if !ok {
		return t
	}
	return n.String()
}

// ── show ────────────────────────────────────────────────────────────────────

var deviceCertShowCmd = &cobra.Command{
	Use:   "show <identifier> <serial>",
	Short: "Show details for a device certificate",
	Long:  "Show details for one of a device's certificates, by serial.",
	Args:  certSerialArgs("usage: device certificates show <identifier> <serial>"),
	RunE:  runDeviceCertShow,
}

func init() {
	deviceCertificatesCmd.AddCommand(deviceCertShowCmd)
}

func runDeviceCertShow(cmd *cobra.Command, args []string) error {
	identifier, serial := args[0], normalizeSerial(args[1])

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

	cert, err := client.GetDeviceCertificate(cmd.Context(), org, product, identifier, serial)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, cert)
	}

	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Serial:\t%s\n", formatSerial(cert.Serial))
	fmt.Fprintf(tw, "Not before:\t%s\n", certDate(cert.NotBefore))
	fmt.Fprintf(tw, "Not after:\t%s\n", certDate(cert.NotAfter))
	return tw.Flush()
}

// ── generate ────────────────────────────────────────────────────────────────

var deviceCertGenerateCmd = &cobra.Command{
	Use:   "generate <identifier>",
	Short: "Generate a device key and CSR or signed certificate locally",
	Long: `Generate a device private key (secp256r1) and, by default, a certificate
signing request on disk, without uploading anything.

With --ca <name> a TLS client certificate signed by one of your local CA
certificates (created with ` + "`nh ca generate`" + `) is produced instead of a
CSR. With --self-signed a self-signed TLS client certificate is produced. In
both cases --valid-for controls validity (e.g. 31y, 90d, 12h) and --upload
registers the certificate with the device immediately.

The CSR or certificate carries the device identifier as the common name and
the organization name. Files are written to <data-dir>/certificates/<org>/.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceCertGenerate,
}

func init() {
	deviceCertGenerateCmd.Flags().String("ca", "", "local CA name to sign the certificate with")
	deviceCertGenerateCmd.Flags().Bool("self-signed", false, "produce a self-signed certificate")
	deviceCertGenerateCmd.Flags().String("valid-for", "31y", "validity of the signed certificate (e.g. 31y, 90d, 12h)")
	deviceCertGenerateCmd.Flags().Bool("upload", false, "register the signed certificate with the device after generating")
	deviceCertificatesCmd.AddCommand(deviceCertGenerateCmd)
}

func runDeviceCertGenerate(cmd *cobra.Command, args []string) error {
	identifier := args[0]
	if identifier == "." || identifier == ".." || filepath.Base(identifier) != identifier {
		return fmt.Errorf("invalid device identifier %q", identifier)
	}

	caName := mustString(cmd, "ca")
	selfSigned, _ := cmd.Flags().GetBool("self-signed")
	doUpload, _ := cmd.Flags().GetBool("upload")

	if caName != "" && selfSigned {
		return errors.New("use only one of --ca or --self-signed")
	}
	signing := caName != "" || selfSigned
	if !signing {
		if cmd.Flags().Changed("valid-for") {
			return errors.New("--valid-for requires --ca or --self-signed")
		}
		if doUpload {
			return errors.New("--upload requires --ca or --self-signed (a CSR cannot be registered)")
		}
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	// Resolve everything the upload needs before generating, so a missing
	// product or token fails before any files are written.
	var client *api.Client
	var product string
	if doUpload {
		if product, err = requireProduct(cfg); err != nil {
			return err
		}
		if client, err = newAuthedClient(cfg); err != nil {
			return err
		}
	}

	// Stamp the filenames with the generation time (UTC, YYYYMMDDHHMM) so
	// repeated runs for the same device don't overwrite earlier material.
	dir := filepath.Join(cfg.DataDir, "certificates", org)
	base := identifier + "-" + time.Now().UTC().Format("200601021504")
	keyPath := filepath.Join(dir, base+"-key.pem")

	w := cmd.OutOrStdout()
	if !signing {
		csrPath := filepath.Join(dir, base+"-csr.pem")
		if err := ensureAbsent(keyPath, csrPath); err != nil {
			return err
		}
		keyPEM, csrPEM, err := pki.GenerateDeviceKeyAndCSR(org, identifier)
		if err != nil {
			return err
		}
		if err := writeCertFiles(dir, keyPath, keyPEM, csrPath, csrPEM); err != nil {
			return err
		}
		fmt.Fprintf(w, "Generated key and CSR for device %s\n", identifier)
		fmt.Fprintf(w, "  private key: %s\n", keyPath)
		fmt.Fprintf(w, "  CSR:         %s\n", csrPath)
		fmt.Fprintln(w, "\nNothing was uploaded. Once the CSR is signed, register the certificate with `nh device certificates upload`.")
		return nil
	}

	validFor, err := parseValidFor(mustString(cmd, "valid-for"))
	if err != nil {
		return err
	}

	certPath := filepath.Join(dir, base+"-cert.pem")
	if err := ensureAbsent(keyPath, certPath); err != nil {
		return err
	}

	// Produce the certificate: self-signed, or signed by a named on-disk CA.
	var keyPEM, certPEM []byte
	var signedBy string
	if selfSigned {
		keyPEM, certPEM, err = pki.GenerateSelfSignedDeviceCertificate(org, identifier, validFor)
		if err != nil {
			return err
		}
		signedBy = "self-signed"
	} else {
		caCertPEM, caKeyPEM, err := loadCA(cfg, org, caName)
		if err != nil {
			return err
		}
		keyPEM, certPEM, err = pki.SignDeviceCertificate(caCertPEM, caKeyPEM, org, identifier, validFor)
		if err != nil {
			return err
		}
		signedBy = "signed by CA " + caName
	}

	if err := writeCertFiles(dir, keyPath, keyPEM, certPath, certPEM); err != nil {
		return err
	}
	fmt.Fprintf(w, "Generated key and certificate (%s) for device %s\n", signedBy, identifier)
	fmt.Fprintf(w, "  private key: %s\n", keyPath)
	fmt.Fprintf(w, "  certificate: %s\n", certPath)

	if !doUpload {
		fmt.Fprintln(w, "\nNothing was uploaded. Register it with `nh device certificates upload` or rerun with --upload.")
		return nil
	}

	cert, err := client.CreateDeviceCertificate(cmd.Context(), org, product, identifier, certPEM)
	if err != nil {
		return fmt.Errorf("certificate saved to %s but registering it failed: %w", certPath, err)
	}
	fmt.Fprintf(w, "\nUploaded certificate %s for device %s\n", orDash(formatSerial(cert.Serial)), identifier)
	return nil
}

// loadCA reads a named local CA's certificate and key from the data directory.
func loadCA(cfg *config.Config, org, name string) (certPEM, keyPEM []byte, err error) {
	if err := validCAName(name); err != nil {
		return nil, nil, err
	}
	keyPath, certPath := caPaths(cfg, org, name)
	certPEM, err = os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("CA %q not found at %s (create one with `nh ca generate %s`)", name, certPath, name)
		}
		return nil, nil, err
	}
	keyPEM, err = os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// writeCertFiles creates dir and writes the private key (0600) and its
// companion CSR/certificate (0644).
func writeCertFiles(dir, keyPath string, keyPEM []byte, pubPath string, pubPEM []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating certificate directory: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(pubPath), err)
	}
	return nil
}

// mustString returns the value of a string flag that is known to exist.
func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// parseValidFor parses a certificate validity duration. On top of Go duration
// syntax (e.g. "12h"), it accepts day and year suffixes (e.g. "90d", "31y";
// a year counts as 365 days).
func parseValidFor(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	parseScaled := func(v string, unit time.Duration) (time.Duration, error) {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid --valid-for %q", s)
		}
		return time.Duration(n * float64(unit)), nil
	}

	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "y"):
		d, err = parseScaled(strings.TrimSuffix(s, "y"), 365*24*time.Hour)
	case strings.HasSuffix(s, "d"):
		d, err = parseScaled(strings.TrimSuffix(s, "d"), 24*time.Hour)
	default:
		if d, err = time.ParseDuration(s); err != nil {
			err = fmt.Errorf("invalid --valid-for %q (use e.g. 31y, 90d, or 12h)", s)
		}
	}
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("--valid-for must be positive, got %q", s)
	}
	return d, nil
}

// ── upload ──────────────────────────────────────────────────────────────────

var deviceCertUploadCmd = &cobra.Command{
	Use:   "upload <identifier> <certificate path>",
	Short: "Upload a certificate for a device",
	Long:  "Register a PEM-encoded X.509 certificate with a device.",
	Args:  certSerialArgs("usage: device certificates upload <identifier> <certificate path>"),
	RunE:  runDeviceCertUpload,
}

func init() {
	deviceCertificatesCmd.AddCommand(deviceCertUploadCmd)
}

func runDeviceCertUpload(cmd *cobra.Command, args []string) error {
	identifier, path := args[0], args[1]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	pemData, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := pki.ValidateCertificatePEM(pemData); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	cert, err := client.CreateDeviceCertificate(cmd.Context(), org, product, identifier, pemData)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, cert)
	}
	fmt.Fprintf(w, "Uploaded certificate %s for device %s\n", orDash(formatSerial(cert.Serial)), identifier)
	return nil
}

// ── delete ──────────────────────────────────────────────────────────────────

var deviceCertDeleteCmd = &cobra.Command{
	Use:   "delete <identifier> <serial>",
	Short: "Delete a device certificate",
	Long:  "Remove a certificate from a device, by serial.",
	Args:  certSerialArgs("usage: device certificates delete <identifier> <serial>"),
	RunE:  runDeviceCertDelete,
}

func init() {
	deviceCertDeleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	deviceCertificatesCmd.AddCommand(deviceCertDeleteCmd)
}

func runDeviceCertDelete(cmd *cobra.Command, args []string) error {
	identifier, serial := args[0], normalizeSerial(args[1])

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
			return errors.New("refusing to delete without confirmation; pass --yes to proceed non-interactively")
		}
		confirmed, err := confirm(cmd, fmt.Sprintf("Delete certificate %s from device %s? [y/N]", formatSerial(serial), identifier))
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

	if err := client.DeleteDeviceCertificate(cmd.Context(), org, product, identifier, serial); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted certificate %s from device %s\n", formatSerial(serial), identifier)
	return nil
}
