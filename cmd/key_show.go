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
	"fmt"
	"strings"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// keyShowCmd implements `nh key show <name>`.
var keyShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details for a signing key",
	Long: `Show details for a single signing key by its name: its scheme, the key
itself, and for a secure_boot_v2_rsa key its Secure Boot v2 eFuse digest or for
an x509_certificate its SHA-256 fingerprint.`,
	Args: keyNameArgs,
	RunE: runKeyShow,
}

func init() {
	keyCmd.AddCommand(keyShowCmd)
}

func runKeyShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	key, err := client.GetSigningKey(cmd.Context(), org, name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, key)
	}

	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	scheme := pki.SchemeOrDefault(key.Scheme)
	row("Name:", key.Name)
	row("Scheme:", string(scheme))
	if digest, ok := pki.KeyDigest(scheme, key.Key); ok {
		row(pki.KeyPreviewLabel(scheme)+":", digest)
	}
	multiline := strings.Contains(strings.TrimSpace(key.Key), "\n")
	if !multiline {
		row(keyValueLabel(scheme), key.Key)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// A PEM key or certificate spans several lines, which would break the
	// table's alignment, so it is printed whole after it.
	if multiline {
		fmt.Fprintf(w, "%s\n%s\n", keyValueLabel(scheme), strings.TrimRight(key.Key, "\n"))
	}
	return nil
}

// keyValueLabel names what a key string holds for scheme.
func keyValueLabel(scheme pki.Scheme) string {
	if scheme == pki.SchemeX509Certificate {
		return "Certificate:"
	}
	return "Public key:"
}
