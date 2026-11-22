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
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/firmware"
	"github.com/nerves-hub/nh/internal/mix"
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// fwupBin is the fwup executable used to sign firmware. It is a variable so
// tests can substitute a stub.
var fwupBin = "fwup"

// errAborted signals that the user declined a confirmation. The command turns
// it into a clean exit rather than an error.
var errAborted = errors.New("aborted")

// firmwareUploadCmd implements `nh firmware upload [path]`, also available as
// `nh firmware publish` — the name the Elixir CLI used for the same flow.
var firmwareUploadCmd = &cobra.Command{
	Use:     "upload [path]",
	Aliases: []string{"publish"},
	Short:   "Sign and upload a firmware file",
	Long: `Sign a firmware image and upload it to a product.

The path is optional inside a Nerves project: without one, nh looks for a single
image under _build/<MIX_TARGET>_<MIX_ENV>/nerves/images/ and uses that.

The format is detected from the file's contents, as the server does, not from
its name. NervesHub accepts:

  fwup     .fw     Nerves firmware archives
  esp-idf  .bin    ESP-IDF application images
  atomvm   .avm    AtomVM packbeam archives
  rauc     .raucb  RAUC bundles (verity format)

A product accepts only the formats in its allowed_update_tools (fwup by
default); ` + "`nh product update <product> --allow-tool <format>`" + ` changes that.

Before uploading, nh reads the image's own metadata, shows it, and refuses an
image built for a different product.

Firmware is signed before uploading unless --skip-signing is passed:

  fwup     Signed with fwup, which must be installed (it is needed only for
           fwup images). The Ed25519 key comes from --key <name> (a key made
           with ` + "`nh key create`" + `; supply --password if it has one), --key-file
           <path> (an fwup private key, as written by ` + "`fwup -g`" + `), or the
           NERVES_HUB_PRIVATE_KEY environment variable.
  atomvm   Signed by nh itself with the same Ed25519 keys as fwup, by appending
           a nerves_hub/signature entry. An archive that is already signed has
           its signature replaced.
  esp-idf  Signed with ` + "`espsecure.py sign_data --version 2`" + ` (from esptool)
           using an RSA-3072 Secure Boot v2 key: --key <name> reads
           <data-dir>/keys/<org>/<name>.rsa.pem, or pass --key-file <pem>. An
           image that is already signed is uploaded as is.
  rauc     Bundles are signed when they are built, so they are uploaded as is
           and the signing flags do not apply.

A product can accept unsigned ESP-IDF or AtomVM images (` + "`nh product update`" + `
--allow-unsigned-esp-idf / --allow-unsigned-atomvm); fwup archives are always
verified.

--deploy names a deployment group to point at the new firmware once the upload
succeeds. It may be repeated to update several. When it is not given, the
NERVES_HUB_DEPLOYMENT_NAME environment variable is used if set.`,
	Args: atMostOneArg("too many arguments: provide a single file path"),
	RunE: runFirmwareUpload,
}

func init() {
	firmwareUploadCmd.Flags().String("key", "", "signing key name: <name>.priv, or <name>.rsa.pem for ESP-IDF (fwup/AtomVM default: NERVES_HUB_PRIVATE_KEY)")
	firmwareUploadCmd.Flags().String("key-file", "", "signing key file: an fwup private key, or an RSA PEM for ESP-IDF")
	firmwareUploadCmd.Flags().String("password", "", "passphrase for an encrypted Ed25519 signing key (with --key or --key-file)")
	firmwareUploadCmd.Flags().Bool("skip-signing", false, "upload the firmware without signing it")
	firmwareUploadCmd.Flags().StringArray("deploy", nil, "deployment group to update to this firmware after upload (repeatable)")
	firmwareUploadCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt shown when the firmware path is auto-detected")
	firmwareCmd.AddCommand(firmwareUploadCmd)
}

// signingFlags are the upload flags that choose how, and whether, to sign.
type signingFlags struct {
	skip     bool
	keyName  string
	keyFile  string
	password bool
}

func (f signingFlags) keyGiven() bool { return f.keyName != "" || f.keyFile != "" }

func readSigningFlags(cmd *cobra.Command, format firmware.Format) (signingFlags, error) {
	var f signingFlags
	f.skip, _ = cmd.Flags().GetBool("skip-signing")
	f.keyName, _ = cmd.Flags().GetString("key")
	f.keyFile, _ = cmd.Flags().GetString("key-file")
	f.password = cmd.Flags().Changed("password")

	if f.keyName != "" && f.keyFile != "" {
		return f, errors.New("use only one of --key or --key-file")
	}
	if f.skip && f.keyGiven() {
		return f, errors.New("use only one of --key/--key-file or --skip-signing")
	}
	switch format {
	case firmware.RAUC:
		if f.keyGiven() || f.password {
			return f, errors.New("RAUC bundles are signed when they are built (`rauc bundle --cert --key`), " +
				"so nh uploads them as is: drop --key, --key-file and --password")
		}
	case firmware.ESPIDF:
		if f.password {
			return f, errors.New("--password does not apply to ESP-IDF keys: espsecure reads the PEM itself")
		}
	}
	return f, nil
}

func runFirmwareUpload(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	path, detected, err := resolveFirmwarePath(cmd, args)
	if err != nil {
		return err
	}

	// Check the input exists up front, so file mistakes surface before any
	// key-resolution or signing errors.
	if _, err := os.Stat(path); err != nil {
		return err
	}

	// The server picks the update tool from the file's bytes, so nh does too:
	// everything after this — the preview, signing — depends on it.
	//
	// An unrecognised file is refused, unless --skip-signing says nh has
	// nothing to do to it: then the server, which may accept formats this nh
	// does not know, has the final say.
	format, err := firmware.Detect(path)
	if errors.Is(err, firmware.ErrUnknownFormat) {
		if skip, _ := cmd.Flags().GetBool("skip-signing"); skip {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v; uploading it as is for the server to judge\n", filepath.Base(path), err)
			err = nil
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	flags, err := readSigningFlags(cmd, format)
	if err != nil {
		return err
	}

	// Read the image's own metadata to catch the wrong product before anything
	// is uploaded, and to show what is about to be published.
	if err := reviewFirmware(cmd, cfg, path, format, product, detected); err != nil {
		if errors.Is(err, errAborted) {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		return err
	}

	// Sign to a temp file, which is uploaded in place of the original.
	uploadPath := path
	signedPath, err := signForUpload(cmd, cfg, org, path, format, flags)
	if err != nil {
		return err
	}
	if signedPath != "" {
		defer os.Remove(signedPath)
		uploadPath = signedPath
	}

	file, err := os.Open(uploadPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Firmware images can be large, so use a client without the default
	// per-request timeout; cancellation still flows through the context.
	client, err := newAuthedClient(cfg, api.WithHTTPClient(&http.Client{}))
	if err != nil {
		return err
	}

	// Show a progress bar on stderr when it's an interactive terminal, so
	// piped/JSON output stays clean.
	var body io.Reader = file
	var bar *progressbar.ProgressBar
	if stderrIsTerminal(cmd) {
		if info, statErr := file.Stat(); statErr == nil && info.Size() > 0 {
			bar = newTransferBar(cmd.ErrOrStderr(), "Uploading "+filepath.Base(path), info.Size())
			reader := progressbar.NewReader(file, bar)
			body = &reader
		}
	}

	// The original path is passed so the server sees the original filename
	// even when the bytes come from the signed temp file.
	uploaded, err := client.UploadFirmware(cmd.Context(), org, product, path, body)
	finishBar(bar)
	if err != nil {
		return uploadErrorHint(err, format, product)
	}

	w := cmd.OutOrStdout()
	if cfg.Output != config.OutputJSON {
		if uploaded.UUID != "" || uploaded.Version != "" {
			fmt.Fprintf(w, "Uploaded firmware %s (%s)\n", orDash(uploaded.Version), orDash(uploaded.UUID))
		} else {
			fmt.Fprintf(w, "Uploaded %s\n", filepath.Base(path))
		}
	}

	// Point deployment groups at the new firmware. This runs after the upload
	// has succeeded, so a failure here leaves the firmware in place and says so.
	updated, err := deployFirmware(cmd, client, org, product, uploaded.UUID)
	if err != nil {
		return err
	}

	if cfg.Output == config.OutputJSON {
		return printJSON(w, struct {
			*api.Firmware
			Deployed []string `json:"deployed,omitempty"`
		}{Firmware: uploaded, Deployed: updated})
	}
	return nil
}

// signForUpload signs the image as its format requires, returning the path of
// a signed temporary copy, or "" when the original is to be uploaded as is.
// The caller removes the temporary copy.
func signForUpload(cmd *cobra.Command, cfg *config.Config, org, path string, format firmware.Format, flags signingFlags) (string, error) {
	stderr := cmd.ErrOrStderr()
	name := filepath.Base(path)

	if format == firmware.RAUC {
		fmt.Fprintf(stderr, "RAUC bundles are signed when they are built; uploading %s as is\n", name)
		return "", nil
	}
	if flags.skip {
		return "", nil
	}

	switch format {
	case firmware.ESPIDF:
		signed, err := firmware.ESPIDFSigned(path)
		if err != nil {
			return "", err
		}
		if signed && !flags.keyGiven() {
			fmt.Fprintf(stderr, "%s already carries a Secure Boot v2 signature; uploading it as is\n", name)
			return "", nil
		}
		// Resolve a key that was asked for even when it will not be used, so a
		// wrong path fails now rather than on the first unsigned build.
		keyFile, label, err := resolveESPIDFKey(cfg, org, flags)
		if err != nil {
			return "", err
		}
		if signed {
			fmt.Fprintf(stderr, "%s already carries a Secure Boot v2 signature, so it is not signed again with %s; "+
				"uploading it as is\n", name, label)
			return "", nil
		}
		out, err := signedTempPath(format)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(stderr, "Signing %s with %s\n", name, label)
		if err := firmware.SignESPIDF(cmd.Context(), path, keyFile, out); err != nil {
			os.Remove(out)
			if errors.Is(err, firmware.ErrEspsecureNotFound) {
				return "", errors.New("espsecure.py not found: install esptool (`pip install esptool`) to sign " +
					"ESP-IDF images, or sign the image yourself and upload it with --skip-signing")
			}
			return "", err
		}
		return out, nil

	case firmware.AtomVM:
		priv, label, err := resolveUploadSigningKey(cmd, cfg, org, flags)
		if err != nil {
			return "", err
		}
		out, err := signedTempPath(format)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(stderr, "Signing %s with %s\n", name, label)
		replaced, err := firmware.SignAtomVMFile(path, out, priv)
		if err != nil {
			os.Remove(out)
			return "", fmt.Errorf("signing %s: %w", name, err)
		}
		if replaced {
			fmt.Fprintf(stderr, "Replaced the signature %s already carried\n", name)
		}
		return out, nil

	default: // fwup
		priv, label, err := resolveUploadSigningKey(cmd, cfg, org, flags)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(stderr, "Signing %s with %s\n", name, label)
		return signFirmware(cmd.Context(), path, pki.PrivateKeyString(priv))
	}
}

// signedTempPath creates an empty temporary file named for the format, so a
// signed .avm is not left lying around as a .fw.
func signedTempPath(format firmware.Format) (string, error) {
	out, err := os.CreateTemp("", "nh-signed-*"+format.Extension())
	if err != nil {
		return "", fmt.Errorf("creating temporary file: %w", err)
	}
	out.Close()
	return out.Name(), nil
}

// resolveESPIDFKey picks the RSA-3072 PEM used to sign an ESP-IDF image:
// --key-file as given, or --key <name> under the data directory.
func resolveESPIDFKey(cfg *config.Config, org string, flags signingFlags) (path, label string, err error) {
	if flags.keyFile != "" {
		if _, err := os.Stat(flags.keyFile); err != nil {
			return "", "", fmt.Errorf("signing key file: %w", err)
		}
		return flags.keyFile, "key file " + filepath.Base(flags.keyFile), nil
	}
	if flags.keyName != "" {
		if err := validKeyName(flags.keyName); err != nil {
			return "", "", err
		}
		p := pki.RSAPrivateKeyPath(cfg.DataDir, org, flags.keyName)
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return "", "", fmt.Errorf("ESP-IDF signing key %q not found at %s; pass --key-file <pem> "+
					"to use a key kept elsewhere", flags.keyName, p)
			}
			return "", "", err
		}
		return p, "key " + flags.keyName, nil
	}
	return "", "", errors.New("ESP-IDF images are signed with an RSA-3072 Secure Boot v2 key: pass --key <name> " +
		"or --key-file <pem>, or --skip-signing to upload the image unsigned " +
		"(the product must accept that: `nh product update <product> --allow-unsigned-esp-idf`)")
}

// uploadErrorHint adds what to do next to the server's reasons for refusing
// an upload. The server's own message (errors.detail) is kept as is.
func uploadErrorHint(err error, format firmware.Format, product string) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		return err
	}
	msg := strings.ToLower(apiErr.Message)
	var hint string
	switch {
	case strings.Contains(msg, "does not accept"):
		hint = fmt.Sprintf("an org owner can accept %s firmware with `nh product update %s --allow-tool %s`",
			format, product, format)
	case strings.Contains(msg, "not signed") && format == firmware.AtomVM:
		// The server words this one for ESP-IDF whatever the format.
		hint = fmt.Sprintf("this packbeam is unsigned: upload it without --skip-signing to sign it with --key, "+
			"or accept unsigned AtomVM firmware with `nh product update %s --allow-unsigned-atomvm`", product)
	case strings.Contains(msg, "not signed"):
		hint = fmt.Sprintf("sign the image with --key or --key-file, or accept unsigned ESP-IDF firmware "+
			"with `nh product update %s --allow-unsigned-esp-idf`", product)
	case strings.Contains(msg, "no signing keys"):
		hint = "register a signing key with the organization first (see `nh key create --help`)"
	case strings.Contains(msg, "does not match any of this organization's signing keys"):
		hint = "check the key you signed with is registered to the organization (see `nh key list`)"
	}
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w\nhint: %s", err, hint)
}

// resolveFirmwarePath returns the firmware to upload: the given path, or the
// image built by the surrounding Nerves project when no path was supplied.
// detected reports which it was, so the caller can confirm a guess.
func resolveFirmwarePath(cmd *cobra.Command, args []string) (path string, detected bool, err error) {
	if len(args) == 1 {
		return args[0], false, nil
	}
	if !mix.Available() {
		return "", false, errors.New("firmware file path missing (and the working directory is not a Nerves project to detect one from)")
	}
	path, err = mix.FirmwarePath()
	if err != nil {
		return "", false, err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Detected firmware: %s\n", path)
	return path, true, nil
}

// firmwareMetadata is the key/value metadata fwup reports for an image, with
// the "meta-" prefix stripped (product, version, platform, architecture, uuid).
type firmwareMetadata map[string]string

// readFirmwareMetadata runs `fwup -m -i <path>` and parses its meta-key="value"
// output.
func readFirmwareMetadata(ctx context.Context, path string) (firmwareMetadata, error) {
	fwup, err := exec.LookPath(fwupBin)
	if err != nil {
		return nil, fmt.Errorf("fwup not found: %w", err)
	}
	out, err := exec.CommandContext(ctx, fwup, "-m", "-i", path).Output()
	if err != nil {
		return nil, fmt.Errorf("reading firmware metadata: %w", err)
	}

	meta := firmwareMetadata{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		meta[strings.TrimPrefix(key, "meta-")] = strings.Trim(value, `"`)
	}
	return meta, nil
}

// reviewFirmware validates and previews the image about to be uploaded.
//
// A product mismatch is a hard error: uploading a build of one product to
// another is never intended, and the image's own metadata is the authority.
// Unreadable metadata is only a warning — fwup is optional when --skip-signing
// is used, and failing the upload over a missing preview would be worse than
// proceeding without one.
func reviewFirmware(cmd *cobra.Command, cfg *config.Config, path string, format firmware.Format, product string, detected bool) error {
	stderr := cmd.ErrOrStderr()

	if format == "" {
		// Not a format nh knows, so there is no metadata to read.
		return confirmFirmwareUpload(cmd, cfg, path, product, detected)
	}
	meta, warnings, err := readImageMetadata(cmd.Context(), path, format)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: could not read %s metadata (%v)\n", format, err)
		return confirmFirmwareUpload(cmd, cfg, path, product, detected)
	}

	if meta.Product != "" && meta.Product != product {
		return fmt.Errorf("firmware is built for product %q but you are uploading to %q; "+
			"check --product, or %s", meta.Product, product, productSource(format))
	}

	uuid := orDash(meta.UUID)
	if format == firmware.AtomVM {
		uuid = "(assigned from the uploaded file)"
	}
	fmt.Fprintf(stderr, "\n  format:       %s\n", format)
	fmt.Fprintf(stderr, "  product:      %s\n", orDash(meta.Product))
	fmt.Fprintf(stderr, "  version:      %s\n", orDash(meta.Version))
	fmt.Fprintf(stderr, "  platform:     %s\n", orDash(meta.Platform))
	fmt.Fprintf(stderr, "  architecture: %s\n", orDash(meta.Architecture))
	fmt.Fprintf(stderr, "  uuid:         %s\n\n", uuid)
	for _, w := range warnings {
		fmt.Fprintf(stderr, "Warning: %s\n", w)
	}

	return confirmFirmwareUpload(cmd, cfg, path, product, detected)
}

// readImageMetadata reads an image's metadata: through `fwup -m` for fwup,
// and natively for every other format.
func readImageMetadata(ctx context.Context, path string, format firmware.Format) (firmware.Metadata, []string, error) {
	if format != firmware.Fwup {
		return firmware.ReadMetadata(format, path)
	}
	meta, err := readFirmwareMetadata(ctx, path)
	if err != nil {
		return firmware.Metadata{}, nil, err
	}
	return firmware.Metadata{
		Product:      meta["product"],
		Version:      meta["version"],
		Platform:     meta["platform"],
		Architecture: meta["architecture"],
		UUID:         meta["uuid"],
	}, nil, nil
}

// productSource names where each format's build declares its product.
func productSource(format firmware.Format) string {
	switch format {
	case firmware.ESPIDF:
		return "PROJECT_NAME in the project's CMakeLists.txt"
	case firmware.AtomVM:
		return "the application name in the project's .app.src"
	case firmware.RAUC:
		return "[meta.nerveshub] product (or [update] compatible) in the bundle's manifest"
	}
	return "the :app/:name in the project's mix.exs"
}

// confirmFirmwareUpload asks before uploading, but only when nh picked the file
// itself. An explicit path is already a statement of intent, and prompting for
// it would break existing non-interactive callers of `nh firmware upload`.
func confirmFirmwareUpload(cmd *cobra.Command, cfg *config.Config, path, product string, detected bool) error {
	if !detected {
		return nil
	}
	if skip, _ := cmd.Flags().GetBool("yes"); skip {
		return nil
	}
	if cfg.NonInteractive || !stdinIsTerminal(cmd) {
		return fmt.Errorf("refusing to upload an auto-detected firmware without confirmation; "+
			"pass the path explicitly or --yes (detected %s)", path)
	}

	confirmed, err := confirm(cmd, fmt.Sprintf("Upload %s to %s? [y/N]", filepath.Base(path), product))
	if err != nil {
		return err
	}
	if !confirmed {
		return errAborted
	}
	return nil
}

// deployFirmware points each requested deployment group at uuid, returning the
// groups updated. UpdateDeployment creates each release with
// POST .../releases where the server has it, and the release PUT otherwise.
func deployFirmware(cmd *cobra.Command, client *api.Client, org, product, uuid string) ([]string, error) {
	names := deployTargets(cmd)
	if len(names) == 0 {
		return nil, nil
	}
	if uuid == "" {
		return nil, errors.New("firmware uploaded, but the server returned no UUID to deploy")
	}

	w := cmd.OutOrStdout()
	updated := make([]string, 0, len(names))
	for _, name := range names {
		if _, err := client.UpdateDeployment(cmd.Context(), org, product, name, api.DeploymentInput{Firmware: uuid}); err != nil {
			// Be explicit that the upload itself is done, so the fix is to
			// retry the deployment rather than the whole publish.
			return updated, fmt.Errorf("firmware %s uploaded, but updating deployment group %q failed: %w", uuid, name, err)
		}
		updated = append(updated, name)
		fmt.Fprintf(w, "Updated deployment group %s to firmware %s\n", name, uuid)
	}
	return updated, nil
}

// deployTargets returns the deployment groups to update: --deploy when given,
// otherwise NERVES_HUB_DEPLOYMENT_NAME / NERVES_CLOUD_DEPLOYMENT_NAME.
func deployTargets(cmd *cobra.Command) []string {
	if names, _ := cmd.Flags().GetStringArray("deploy"); len(names) > 0 {
		return names
	}
	if name := config.Env("DEPLOYMENT_NAME"); name != "" {
		return []string{name}
	}
	return nil
}

// resolveUploadSigningKey picks the Ed25519 key used to sign an fwup or
// AtomVM upload: the named stored key for --key, the file for --key-file,
// otherwise an unencrypted key from the NERVES_HUB_PRIVATE_KEY environment
// variable. It returns a human-readable label for status output too.
func resolveUploadSigningKey(cmd *cobra.Command, cfg *config.Config, org string, flags signingFlags) (ed25519.PrivateKey, string, error) {
	if flags.keyName != "" {
		k, err := resolveSigningKey(cmd, cfg, org, flags.keyName)
		return k, "key " + flags.keyName, err
	}
	if flags.keyFile != "" {
		k, err := readSigningKeyFile(cmd, cfg, flags.keyFile)
		return k, "key file " + filepath.Base(flags.keyFile), err
	}

	if env := config.Env("PRIVATE_KEY"); env != "" {
		priv, err := pki.ParsePrivateKeyString(env)
		if err != nil {
			return nil, "", fmt.Errorf("invalid NERVES_HUB_PRIVATE_KEY: %w", err)
		}
		return priv, "key from NERVES_HUB_PRIVATE_KEY", nil
	}

	return nil, "", errors.New("no signing key: pass --key <name> or --key-file <path>, set NERVES_HUB_PRIVATE_KEY, " +
		"or pass --skip-signing to upload the firmware as is")
}

// resolveSigningKey loads and decrypts the named signing key for org from the
// data directory.
func resolveSigningKey(cmd *cobra.Command, cfg *config.Config, org, name string) (ed25519.PrivateKey, error) {
	if err := validKeyName(name); err != nil {
		return nil, err
	}
	privPath := filepath.Join(cfg.DataDir, "keys", org, name+".priv")
	blob, err := os.ReadFile(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("signing key %q not found at %s (create one with `nh key create %s`)", name, privPath, name)
		}
		return nil, err
	}
	priv, err := decryptSigningKey(cmd, cfg, string(blob))
	if err != nil {
		return nil, fmt.Errorf("decrypting signing key %q (wrong --password?): %w", name, err)
	}
	return priv, nil
}

// readSigningKeyFile reads an Ed25519 private key from a file: an fwup key
// (base64 of the 64 byte secret key, as `fwup -g` writes) or a
// password-protected key as `nh key create` stores it.
//
// A bare 32 byte key is refused rather than read as a seed: that is the shape
// of the fwup-key.pub `fwup -g` writes beside the private key, and signing with
// it would only fail later, at the server, as a signature nothing matches.
func readSigningKeyFile(cmd *cobra.Command, cfg *config.Config, path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("signing key file: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	if priv, err := pki.ParsePrivateKeyString(text); err == nil {
		return priv, nil
	}
	if raw, err := base64.StdEncoding.DecodeString(text); err == nil && len(raw) == ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s holds a 32 byte key, which is an Ed25519 public key (such as fwup-key.pub); "+
			"pass the private key (fwup-key.priv) instead", path)
	}
	priv, err := decryptSigningKey(cmd, cfg, text)
	if err != nil {
		return nil, fmt.Errorf("%s is not an fwup private key, and could not be decrypted as an nh key "+
			"(wrong --password?): %w", path, err)
	}
	return priv, nil
}

// decryptSigningKey decrypts a password-protected private key. The passphrase
// comes from --password when set; otherwise an empty passphrase is tried
// first, then an interactive prompt when possible.
func decryptSigningKey(cmd *cobra.Command, cfg *config.Config, blob string) (ed25519.PrivateKey, error) {
	password := ""
	passwordSet := cmd.Flags().Changed("password")
	if passwordSet {
		password, _ = cmd.Flags().GetString("password")
	}

	priv, err := pki.DecryptPrivateKey(blob, password)
	if err != nil && !passwordSet && !cfg.NonInteractive && stdinIsTerminal(cmd) {
		// The key is likely password protected; ask once.
		pw, perr := promptPassword(cmd, "Signing key password: ", false)
		if perr != nil {
			return nil, perr
		}
		priv, err = pki.DecryptPrivateKey(blob, pw)
	}
	return priv, err
}

// signFirmware signs the firmware at inPath with fwup, writing the signed
// image to a temporary file whose path is returned. The caller is responsible
// for removing it.
//
// The key goes to fwup in a private temporary file (-s) rather than as
// --private-key, where any local user could read it from the process list.
func signFirmware(ctx context.Context, inPath, privateKey string) (string, error) {
	fwup, err := exec.LookPath(fwupBin)
	if err != nil {
		return "", fmt.Errorf("fwup not found: install fwup to sign fwup images (%w)", err)
	}

	keyFile, err := os.CreateTemp("", "nh-key-*.priv")
	if err != nil {
		return "", fmt.Errorf("creating temporary key file: %w", err)
	}
	defer os.Remove(keyFile.Name())
	if _, err := keyFile.WriteString(privateKey + "\n"); err != nil {
		keyFile.Close()
		return "", fmt.Errorf("writing temporary key file: %w", err)
	}
	if err := keyFile.Close(); err != nil {
		return "", fmt.Errorf("writing temporary key file: %w", err)
	}

	outPath, err := signedTempPath(firmware.Fwup)
	if err != nil {
		return "", err
	}

	sign := exec.CommandContext(ctx, fwup, "--sign", "-i", inPath, "-o", outPath, "-s", keyFile.Name())
	var stderr bytes.Buffer
	sign.Stderr = &stderr
	if err := sign.Run(); err != nil {
		os.Remove(outPath)
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("fwup signing failed: %s", msg)
		}
		return "", fmt.Errorf("fwup signing failed: %w", err)
	}
	return outPath, nil
}
