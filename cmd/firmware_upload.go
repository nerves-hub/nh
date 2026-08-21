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
	Long: `Sign a firmware image (.fw) and upload it to a product.

The path is optional inside a Nerves project: without one, nh looks for a single
image under _build/<MIX_TARGET>_<MIX_ENV>/nerves/images/ and uses that.

Firmware is signed before uploading unless --skip-signing is passed. Signing
runs fwup, which must be installed. The private key comes from, in order:

  1. --key <name>, a signing key created with ` + "`nh key create`" + ` (if it is
     password protected, supply --password or you will be prompted), or
  2. NERVES_HUB_PRIVATE_KEY, an unencrypted base64 private key.

--deploy names a deployment group to point at the new firmware once the upload
succeeds. It may be repeated to update several. When it is not given, the
NERVES_HUB_DEPLOYMENT_NAME environment variable is used if set.`,
	Args: atMostOneArg("too many arguments: provide a single file path"),
	RunE: runFirmwareUpload,
}

func init() {
	firmwareUploadCmd.Flags().String("key", "", "signing key name (default: NERVES_HUB_PRIVATE_KEY)")
	firmwareUploadCmd.Flags().String("password", "", "passphrase for the signing key (with --key)")
	firmwareUploadCmd.Flags().Bool("skip-signing", false, "upload the firmware without signing it")
	firmwareUploadCmd.Flags().StringArray("deploy", nil, "deployment group to update to this firmware after upload (repeatable)")
	firmwareUploadCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt shown when the firmware path is auto-detected")
	firmwareCmd.AddCommand(firmwareUploadCmd)
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

	// Read the image's own metadata to catch the wrong product before anything
	// is uploaded, and to show what is about to be published.
	if err := reviewFirmware(cmd, cfg, path, product, detected); err != nil {
		if errors.Is(err, errAborted) {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		return err
	}

	skipSigning, _ := cmd.Flags().GetBool("skip-signing")
	keyName, _ := cmd.Flags().GetString("key")
	if skipSigning && keyName != "" {
		return errors.New("use only one of --key or --skip-signing")
	}

	// Sign by default: to a temp file, which is uploaded in place of the
	// original.
	uploadPath := path
	if !skipSigning {
		privateKey, keyLabel, err := resolveUploadSigningKey(cmd, cfg, org, keyName)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Signing %s with %s\n", filepath.Base(path), keyLabel)
		signedPath, err := signFirmware(cmd.Context(), path, privateKey)
		if err != nil {
			return err
		}
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
	firmware, err := client.UploadFirmware(cmd.Context(), org, product, path, body)
	finishBar(bar)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output != config.OutputJSON {
		if firmware.UUID != "" || firmware.Version != "" {
			fmt.Fprintf(w, "Uploaded firmware %s (%s)\n", orDash(firmware.Version), orDash(firmware.UUID))
		} else {
			fmt.Fprintf(w, "Uploaded %s\n", filepath.Base(path))
		}
	}

	// Point deployment groups at the new firmware. This runs after the upload
	// has succeeded, so a failure here leaves the firmware in place and says so.
	updated, err := deployFirmware(cmd, client, org, product, firmware.UUID)
	if err != nil {
		return err
	}

	if cfg.Output == config.OutputJSON {
		return printJSON(w, struct {
			*api.Firmware
			Deployed []string `json:"deployed,omitempty"`
		}{Firmware: firmware, Deployed: updated})
	}
	return nil
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
func reviewFirmware(cmd *cobra.Command, cfg *config.Config, path, product string, detected bool) error {
	stderr := cmd.ErrOrStderr()

	meta, err := readFirmwareMetadata(cmd.Context(), path)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: could not read firmware metadata (%v)\n", err)
		return confirmFirmwareUpload(cmd, cfg, path, product, detected)
	}

	if got := meta["product"]; got != "" && got != product {
		return fmt.Errorf("firmware is built for product %q but you are uploading to %q; "+
			"check --product, or the :app/:name in the project's mix.exs", got, product)
	}

	fmt.Fprintf(stderr, "\n  product:      %s\n", orDash(meta["product"]))
	fmt.Fprintf(stderr, "  version:      %s\n", orDash(meta["version"]))
	fmt.Fprintf(stderr, "  platform:     %s\n", orDash(meta["platform"]))
	fmt.Fprintf(stderr, "  architecture: %s\n", orDash(meta["architecture"]))
	fmt.Fprintf(stderr, "  uuid:         %s\n\n", orDash(meta["uuid"]))

	return confirmFirmwareUpload(cmd, cfg, path, product, detected)
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
// groups updated.
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

// resolveUploadSigningKey picks the private key used to sign an upload: the
// named stored key when --key is given, otherwise an unencrypted key from the
// NERVES_HUB_PRIVATE_KEY environment variable. It returns the key in the
// base64 form fwup expects plus a human-readable label for status output.
func resolveUploadSigningKey(cmd *cobra.Command, cfg *config.Config, org, keyName string) (key, label string, err error) {
	if keyName != "" {
		k, err := resolveSigningKey(cmd, cfg, org, keyName)
		return k, "key " + keyName, err
	}

	if env := config.Env("PRIVATE_KEY"); env != "" {
		priv, err := pki.ParsePrivateKeyString(env)
		if err != nil {
			return "", "", fmt.Errorf("invalid NERVES_HUB_PRIVATE_KEY: %w", err)
		}
		return pki.PrivateKeyString(priv), "key from NERVES_HUB_PRIVATE_KEY", nil
	}

	return "", "", errors.New("no signing key: pass --key <name>, set NERVES_HUB_PRIVATE_KEY, or pass --skip-signing to upload the firmware as is")
}

// resolveSigningKey loads and decrypts the named signing key for org from the
// data directory, returning the base64 form fwup expects. The passphrase comes
// from --password when set; otherwise an empty passphrase is tried first, then
// an interactive prompt when possible.
func resolveSigningKey(cmd *cobra.Command, cfg *config.Config, org, name string) (string, error) {
	if err := validKeyName(name); err != nil {
		return "", err
	}
	privPath := filepath.Join(cfg.DataDir, "keys", org, name+".priv")
	blob, err := os.ReadFile(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("signing key %q not found at %s (create one with `nh key create %s`)", name, privPath, name)
		}
		return "", err
	}

	password := ""
	passwordSet := cmd.Flags().Changed("password")
	if passwordSet {
		password, _ = cmd.Flags().GetString("password")
	}

	priv, err := pki.DecryptPrivateKey(string(blob), password)
	if err != nil && !passwordSet && !cfg.NonInteractive && stdinIsTerminal(cmd) {
		// The key is likely password protected; ask once.
		pw, perr := promptPassword(cmd, "Signing key password: ", false)
		if perr != nil {
			return "", perr
		}
		priv, err = pki.DecryptPrivateKey(string(blob), pw)
	}
	if err != nil {
		return "", fmt.Errorf("decrypting signing key %q (wrong --password?): %w", name, err)
	}
	return pki.PrivateKeyString(priv), nil
}

// signFirmware signs the firmware at inPath with fwup, writing the signed
// image to a temporary file whose path is returned. The caller is responsible
// for removing it.
func signFirmware(ctx context.Context, inPath, privateKey string) (string, error) {
	fwup, err := exec.LookPath(fwupBin)
	if err != nil {
		return "", fmt.Errorf("fwup not found: install fwup to sign firmware (%w)", err)
	}

	out, err := os.CreateTemp("", "nh-signed-*.fw")
	if err != nil {
		return "", fmt.Errorf("creating temporary file: %w", err)
	}
	outPath := out.Name()
	out.Close()

	sign := exec.CommandContext(ctx, fwup, "--sign", "-i", inPath, "-o", outPath, "--private-key", privateKey)
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
