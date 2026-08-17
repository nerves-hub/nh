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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/spf13/cobra"
)

// keyCreateCmd implements `nh key create <name>`.
var keyCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a signing key",
	Long: `Generate a new Ed25519 signing key for the organization (set with --org or
NERVES_HUB_ORG), upload its public key, and save the keypair under
<data-dir>/keys/<org>/ (default ~/.nh/keys/<org>/).

The private key is encrypted at rest. --password sets the passphrase; it is
optional, and you are prompted for one when running interactively.`,
	Args: keyNameArgs,
	RunE: runKeyCreate,
}

func init() {
	keyCreateCmd.Flags().String("password", "", "passphrase to encrypt the private key (optional)")
	keyCmd.AddCommand(keyCreateCmd)
}

func runKeyCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validKeyName(name); err != nil {
		return err
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	password, err := keyPassword(cmd, cfg)
	if err != nil {
		return err
	}

	// Resolve destinations and refuse to clobber an existing key.
	dir := filepath.Join(cfg.DataDir, "keys", org)
	privPath := filepath.Join(dir, name+".priv")
	pubPath := filepath.Join(dir, name+".pub")
	if err := ensureAbsent(privPath, pubPath); err != nil {
		return err
	}

	pub, priv, err := pki.GenerateSigningKey()
	if err != nil {
		return err
	}
	encrypted, err := pki.EncryptPrivateKey(priv, password)
	if err != nil {
		return err
	}
	publicKey := pki.PublicKeyString(pub)

	// Write the keypair locally first, so a failed upload never loses the
	// freshly generated private key.
	if err := config.EnsureDataDir(cfg.DataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}
	if err := os.WriteFile(privPath, []byte(encrypted+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(publicKey+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}
	created, err := client.CreateSigningKey(cmd.Context(), org, name, publicKey)
	if err != nil {
		return fmt.Errorf("keypair saved to %s but uploading the public key failed: %w", dir, err)
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, created)
	}
	fmt.Fprintf(w, "Created signing key %s in %s\n", name, org)
	fmt.Fprintf(w, "  private key: %s\n", privPath)
	fmt.Fprintf(w, "  public key:  %s\n", pubPath)
	return nil
}

// validKeyName rejects names that are unsafe as a filename (path separators,
// "." / "..").
func validKeyName(name string) error {
	if name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("invalid signing key name %q", name)
	}
	return nil
}

// ensureAbsent errors if any of paths already exists.
func ensureAbsent(paths ...string) error {
	for _, p := range paths {
		switch _, err := os.Stat(p); {
		case err == nil:
			return fmt.Errorf("key file already exists: %s", p)
		case !os.IsNotExist(err):
			return err
		}
	}
	return nil
}

// keyPassword resolves the private-key passphrase from --password or, when
// running interactively, an optional (confirmed) prompt.
func keyPassword(cmd *cobra.Command, cfg *config.Config) (string, error) {
	if cmd.Flags().Changed("password") {
		p, _ := cmd.Flags().GetString("password")
		return p, nil
	}
	if cfg.NonInteractive || !stdinIsTerminal(cmd) {
		return "", nil
	}

	first, err := promptPassword(cmd, "Private key password (optional, press enter to skip): ", false)
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", nil
	}
	second, err := promptPassword(cmd, "Confirm password: ", false)
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}
