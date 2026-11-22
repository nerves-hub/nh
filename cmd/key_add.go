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
	"os"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// keyAddCmd implements `nh key add <name> --public-key <file>`.
var keyAddCmd = &cobra.Command{
	Use:   "add <name> --public-key <file>",
	Short: "Register an existing public key or certificate",
	Long: `Register a public key or certificate you already have as a signing key for the
organization (set with --org or NERVES_HUB_ORG). Nothing is generated and
nothing is written locally. --public-key - reads the file from stdin.

--scheme says what the file holds; without it, nh works it out from the file:

  ed25519              An fwup public key (the .pub from fwup -g): base64 of a
                       32-byte Ed25519 key. Verifies .fw archives.
  secure_boot_v2_rsa   An RSA-3072 PEM public key (espsecure.py
                       extract_public_key --version 2), or a PEM private key
                       (espsecure.py generate_signing_key --version 2), from
                       which only the public half is extracted and sent.
                       Verifies ESP-IDF Secure Boot v2 images.
  x509_certificate     A PEM X.509 certificate, optionally followed by its
                       issuers, used as the trust anchor for RAUC bundles. A
                       file that also holds a private key is refused.

The key is checked locally the same way the server checks it, so a malformed
key, or an RSA key that is not 3072 bits, is reported before anything is sent.`,
	Args: keyNameArgs,
	RunE: runKeyAdd,
}

func init() {
	keyAddCmd.Flags().String("public-key", "", "path to the public key or certificate file (- for stdin)")
	keyAddCmd.Flags().String("scheme", "", "key scheme: "+pki.SchemeNames()+" (default: detected from the file)")
	_ = keyAddCmd.MarkFlagRequired("public-key")
	keyCmd.AddCommand(keyAddCmd)
}

func runKeyAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	path, _ := cmd.Flags().GetString("public-key")
	data, err := readKeyFile(cmd, path)
	if err != nil {
		return err
	}

	var scheme pki.Scheme
	if s, _ := cmd.Flags().GetString("scheme"); s != "" {
		if scheme, err = pki.ParseScheme(s); err != nil {
			return err
		}
	} else if scheme, err = pki.DetectScheme(data); err != nil {
		return err
	}

	key, err := pki.PreparePublicKey(scheme, data)
	if err != nil {
		source := path
		if path == "-" {
			source = "stdin"
		}
		return fmt.Errorf("%s is not a valid %s key: %w", source, scheme, err)
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}
	created, err := client.CreateSigningKeyWithScheme(cmd.Context(), org, name, string(scheme), key)
	if err != nil {
		return withSchemeHint(err, scheme)
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, created)
	}
	fmt.Fprintf(w, "Added %s signing key %s to %s\n", scheme, name, org)
	if digest, ok := pki.KeyDigest(scheme, key); ok {
		fmt.Fprintf(w, "  %s: %s\n", pki.KeyPreviewLabel(scheme), digest)
	}
	return nil
}

// readKeyFile reads path, or stdin when path is "-".
func readKeyFile(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("--public-key is required")
	}
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("reading key from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading key file: %w", err)
	}
	return data, nil
}

// withSchemeHint explains the one confusing failure: a server from before
// signing key schemes ignores the scheme and validates every key as Ed25519.
func withSchemeHint(err error, scheme pki.Scheme) error {
	var apiErr *api.APIError
	if scheme == pki.SchemeEd25519 || !errors.As(err, &apiErr) {
		return err
	}
	for _, msg := range apiErr.FieldErrors["key"] {
		if strings.Contains(msg, "Ed25519") {
			return fmt.Errorf("%w\nthe server validated the key as Ed25519, so it probably predates signing key schemes and cannot store a %s key", err, scheme)
		}
	}
	return err
}
