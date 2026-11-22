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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceCertLookupCmd implements `nh device certificates lookup <cert>`, for
// POST .../devices/auth.
var deviceCertLookupCmd = &cobra.Command{
	Use:   "lookup <certificate path>",
	Short: "Find the device a certificate belongs to",
	Long: `Find the device a certificate is registered to, anywhere in the
organization, and show it.

The certificate may be PEM or DER. Only the certificate is sent, never a
private key, even when the file holds one.

NervesHub matches the certificate exactly, by its SHA-1 fingerprint; it
doesn't check its signature or signer CA. So a match says the certificate is
registered, not that a device holding it would connect. The match covers
every product in the organization, so the device shown may be in a product
other than the one selected.

The server gives one answer for a certificate it doesn't know, one whose
device is in another organization or deleted, and one whose device your role
can't see, so nh can't tell those apart.`,
	Args: exactlyOneArg(
		"Certificate path missing",
		"too many arguments: provide a single certificate path",
	),
	RunE: runDeviceCertLookup,
}

func init() {
	deviceCertificatesCmd.AddCommand(deviceCertLookupCmd)
}

func runDeviceCertLookup(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading certificate: %w", err)
	}
	cert, err := parseCertificateFile(data)
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}
	device, err := client.LookupDeviceByCertificate(cmd.Context(), org, product, certPEM)
	if errors.Is(err, api.ErrCertificateNotRecognized) {
		return fmt.Errorf("no device in %s you can see has this certificate (fingerprint %s)", org, certFingerprint(cert))
	}
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, device)
	}
	fmt.Fprintf(w, "Certificate %s (fingerprint %s) belongs to:\n\n", formatSerial(cert.SerialNumber.String()), certFingerprint(cert))
	tw := newTableWriter(w)
	fmt.Fprintf(tw, "Product:\t%s\n", orDash(device.ProductName))
	if err := tw.Flush(); err != nil {
		return err
	}
	return renderDevice(w, device)
}

// parseCertificateFile reads a certificate from PEM (the first CERTIFICATE
// block, skipping keys and anything else) or from DER.
func parseCertificateFile(data []byte) (*x509.Certificate, error) {
	rest := data
	sawPEM := false
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		sawPEM = true
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsing certificate: %w", err)
			}
			return cert, nil
		}
	}
	if sawPEM {
		return nil, errors.New("no CERTIFICATE block in the PEM file")
	}
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, errors.New("not a PEM or DER certificate")
	}
	return cert, nil
}

// certFingerprint is the certificate's fingerprint as NervesHub computes it
// and matches certificates on.
func certFingerprint(cert *x509.Certificate) string {
	return api.CertificateFingerprint(cert.Raw)
}
