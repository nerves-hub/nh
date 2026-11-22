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
	Long: `Generate a new signing keypair for the organization (set with --org or
NERVES_HUB_ORG), upload its public key, and save the keypair under
<data-dir>/keys/<org>/ (default ~/.nh/keys/<org>/).

--scheme picks the kind of key:

  ed25519 (default)    An fwup key for signing .fw archives. Writes
                       <name>.priv and <name>.pub. The private key is encrypted
                       at rest: --password sets the passphrase; it is optional,
                       and you are prompted for one when running interactively.

  secure_boot_v2_rsa   An RSA-3072 key for ESP-IDF Secure Boot v2 images. Writes
                       <name>.rsa.pem (PKCS#1 PEM private key, mode 0600) and
                       <name>.rsa.pub.pem. The private key is NOT encrypted,
                       because espsecure.py sign_data --version 2 --keyfile
                       needs a plain key file; keep it safe. --password is
                       refused for this scheme.

To register a key or certificate you already have (including a RAUC
x509_certificate), use nh key add.`,
	Args: keyNameArgs,
	RunE: runKeyCreate,
}

func init() {
	keyCreateCmd.Flags().String("password", "", "passphrase to encrypt an ed25519 private key (optional)")
	keyCreateCmd.Flags().String("scheme", string(pki.SchemeEd25519), "key scheme: ed25519 or secure_boot_v2_rsa")
	keyCmd.AddCommand(keyCreateCmd)
}

// createdKey is what key create wrote locally, for the human output.
type createdKey struct {
	privPath, pubPath string
	publicKey         string
}

func runKeyCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validKeyName(name); err != nil {
		return err
	}

	schemeFlag, _ := cmd.Flags().GetString("scheme")
	scheme, err := pki.ParseScheme(schemeFlag)
	if err != nil {
		return err
	}
	switch scheme {
	case pki.SchemeX509Certificate:
		return errors.New("nh key create cannot generate an x509_certificate; create the certificate for your RAUC keyring yourself and register it with `nh key add --scheme x509_certificate`")
	case pki.SchemeSecureBootV2RSA:
		if cmd.Flags().Changed("password") {
			return errors.New("--password is not supported for secure_boot_v2_rsa: the private key is stored unencrypted because espsecure.py needs a plain key file")
		}
	}

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}

	// Refuse to clobber any existing key of this name, whatever its scheme:
	// names are unique within an organization.
	if err := ensureAbsent(pki.LocalKeyPaths(cfg.DataDir, org, name)...); err != nil {
		return err
	}

	// Check the credentials before generating anything, so a missing login
	// does not leave key files behind that block the retry.
	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	var local createdKey
	switch scheme {
	case pki.SchemeSecureBootV2RSA:
		local, err = writeRSAKeypair(cfg, org, name)
	default:
		local, err = writeEd25519Keypair(cmd, cfg, org, name)
	}
	if err != nil {
		return err
	}
	dir := pki.KeyDir(cfg.DataDir, org)

	// ed25519 sends no scheme, exactly as before schemes existed, so older
	// servers keep working.
	requestScheme := ""
	if scheme != pki.SchemeEd25519 {
		requestScheme = string(scheme)
	}
	created, err := client.CreateSigningKeyWithScheme(cmd.Context(), org, name, requestScheme, local.publicKey)
	if err != nil {
		return fmt.Errorf("keypair saved to %s but uploading the public key failed: %w", dir, withSchemeHint(err, scheme))
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, created)
	}
	fmt.Fprintf(w, "Created signing key %s in %s\n", name, org)
	if scheme != pki.SchemeEd25519 {
		fmt.Fprintf(w, "  scheme:      %s\n", scheme)
	}
	fmt.Fprintf(w, "  private key: %s\n", local.privPath)
	fmt.Fprintf(w, "  public key:  %s\n", local.pubPath)
	if scheme == pki.SchemeSecureBootV2RSA {
		if digest, ok := rsaDigest(local.publicKey); ok {
			fmt.Fprintf(w, "  eFuse digest: %s\n", digest)
		}
		fmt.Fprintln(w, "The private key is not encrypted; keep it safe.")
	}
	return nil
}

// writeEd25519Keypair generates an fwup Ed25519 keypair and writes it, the
// private key encrypted, to <name>.priv and <name>.pub.
func writeEd25519Keypair(cmd *cobra.Command, cfg *config.Config, org, name string) (createdKey, error) {
	password, err := keyPassword(cmd, cfg)
	if err != nil {
		return createdKey{}, err
	}
	pub, priv, err := pki.GenerateSigningKey()
	if err != nil {
		return createdKey{}, err
	}
	encrypted, err := pki.EncryptPrivateKey(priv, password)
	if err != nil {
		return createdKey{}, err
	}
	k := createdKey{
		privPath:  pki.Ed25519PrivateKeyPath(cfg.DataDir, org, name),
		pubPath:   pki.Ed25519PublicKeyPath(cfg.DataDir, org, name),
		publicKey: pki.PublicKeyString(pub),
	}
	// Write the keypair locally first, so a failed upload never loses the
	// freshly generated private key.
	return k, writeKeyFiles(cfg, org, k, []byte(encrypted+"\n"), []byte(k.publicKey+"\n"))
}

// writeRSAKeypair generates a Secure Boot v2 RSA-3072 keypair and writes the
// unencrypted PKCS#1 private key to <name>.rsa.pem and the public key to
// <name>.rsa.pub.pem.
func writeRSAKeypair(cfg *config.Config, org, name string) (createdKey, error) {
	priv, err := pki.GenerateSecureBootV2Key()
	if err != nil {
		return createdKey{}, err
	}
	publicKey, err := pki.EncodeRSAPublicKeyPEM(&priv.PublicKey)
	if err != nil {
		return createdKey{}, err
	}
	k := createdKey{
		privPath:  pki.RSAPrivateKeyPath(cfg.DataDir, org, name),
		pubPath:   pki.RSAPublicKeyPath(cfg.DataDir, org, name),
		publicKey: publicKey,
	}
	return k, writeKeyFiles(cfg, org, k, pki.EncodeRSAPrivateKeyPEM(priv), []byte(publicKey))
}

// writeKeyFiles writes a keypair into the organization's key directory: the
// private key 0600, the public key 0644.
func writeKeyFiles(cfg *config.Config, org string, k createdKey, priv, pub []byte) error {
	if err := config.EnsureDataDir(cfg.DataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(pki.KeyDir(cfg.DataDir, org), 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}
	if err := writeNewFile(k.privPath, priv, 0o600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}
	if err := writeNewFile(k.pubPath, pub, 0o644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}
	return nil
}

// writeNewFile creates path with perm and writes data, failing if the file
// already exists rather than overwriting a key.
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// rsaDigest returns the Secure Boot v2 eFuse digest of a PEM public key.
func rsaDigest(publicKey string) (string, bool) {
	return pki.KeyDigest(pki.SchemeSecureBootV2RSA, publicKey)
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
