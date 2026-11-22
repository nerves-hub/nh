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
	"github.com/nerves-hub/nh/internal/pki"
	"github.com/nerves-hub/nh/internal/semver"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// archiveUploadCmd implements `nh archive upload <path>`.
var archiveUploadCmd = &cobra.Command{
	Use:   "upload <path>",
	Short: "Sign and upload an archive file",
	Long: `Sign an fwup archive (.fw) and upload it to a product.

Before anything is uploaded, nh reads the archive's metadata with ` + "`fwup -m`" + `.
NervesHub requires meta-product, meta-platform, meta-architecture, meta-uuid and
meta-version, and meta-version must be a SemVer version such as 1.0.0, so an
archive missing any of these is refused here rather than by the server.

The archive is signed before uploading unless --skip-signing is passed. Signing
runs fwup, which must be installed. The private key comes from, in order:

  1. --key <name>, a signing key created with ` + "`nh key create`" + `, or
     --key-file <path>, an fwup private key such as ` + "`fwup -g`" + ` writes (if
     either is password protected, supply --password or you will be
     prompted), or
  2. NERVES_HUB_PRIVATE_KEY, an unencrypted base64 private key.

NervesHub only accepts archives signed with one of the organization's keys.`,
	Args: exactlyOneArg(
		"Archive file path missing",
		"too many arguments: provide a single archive file path",
	),
	RunE: runArchiveUpload,
}

func init() {
	archiveUploadCmd.Flags().String("key", "", "signing key name (default: NERVES_HUB_PRIVATE_KEY)")
	archiveUploadCmd.Flags().String("key-file", "", "signing key file: an fwup private key, or a key as `nh key create` stores it")
	archiveUploadCmd.Flags().String("password", "", "passphrase for an encrypted signing key (with --key or --key-file)")
	archiveUploadCmd.Flags().Bool("skip-signing", false, "upload the archive without signing it")
	archiveCmd.AddCommand(archiveUploadCmd)
}

func runArchiveUpload(cmd *cobra.Command, args []string) error {
	path := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	// Check the input exists up front, so file mistakes surface before any
	// key-resolution or signing errors.
	if _, err := os.Stat(path); err != nil {
		return err
	}

	var flags signingFlags
	flags.skip, _ = cmd.Flags().GetBool("skip-signing")
	flags.keyName, _ = cmd.Flags().GetString("key")
	flags.keyFile, _ = cmd.Flags().GetString("key-file")
	flags.password = cmd.Flags().Changed("password")
	if flags.keyName != "" && flags.keyFile != "" {
		return errors.New("use only one of --key or --key-file")
	}
	if flags.skip && flags.keyGiven() {
		return errors.New("use only one of --key/--key-file or --skip-signing")
	}

	if err := reviewArchive(cmd, path, flags.skip); err != nil {
		return err
	}

	uploadPath := path
	if !flags.skip {
		if !flags.keyGiven() && config.Env("PRIVATE_KEY") == "" {
			return errors.New("no signing key: pass --key <name> or --key-file <path>, set NERVES_HUB_PRIVATE_KEY, " +
				"or pass --skip-signing to upload the archive as is")
		}
		privateKey, keyLabel, err := resolveUploadSigningKey(cmd, cfg, org, flags)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Signing %s with %s\n", filepath.Base(path), keyLabel)
		signedPath, err := signFirmware(cmd.Context(), path, pki.PrivateKeyString(privateKey))
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

	// Archives can be large, so use a client without the default per-request
	// timeout; cancellation still flows through the context.
	client, err := newAuthedClient(cfg, api.WithHTTPClient(&http.Client{}))
	if err != nil {
		return err
	}

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
	archive, err := client.UploadArchive(cmd.Context(), org, product, path, body)
	finishBar(bar)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, archive)
	}
	fmt.Fprintf(w, "Uploaded archive %s (%s)\n", orDash(archive.Version), orDash(archive.UUID))
	return nil
}

// archiveRequiredMeta are the metadata keys NervesHub needs in an archive:
// NervesHub.Fwup.metadata/1 refuses a file without them.
var archiveRequiredMeta = []string{"product", "platform", "architecture", "uuid", "version"}

// reviewArchive reads the archive's metadata with fwup, refuses one NervesHub
// would reject, and previews the rest on stderr.
//
// Without fwup the check cannot run. Signing needs fwup anyway, so that is an
// error unless --skip-signing is set, in which case the upload goes ahead with
// a warning and the server makes the same checks.
func reviewArchive(cmd *cobra.Command, path string, skipSigning bool) error {
	stderr := cmd.ErrOrStderr()

	if _, err := exec.LookPath(fwupBin); err != nil {
		if skipSigning {
			fmt.Fprintf(stderr, "Warning: fwup not found, so the archive's metadata was not checked (%v)\n", err)
			return nil
		}
		return fmt.Errorf("fwup not found: install fwup to sign archives (%w)", err)
	}

	meta, err := readArchiveMetadata(cmd.Context(), path)
	if err != nil {
		return err
	}

	if err := checkArchiveMetadata(meta); err != nil {
		return err
	}

	fmt.Fprintf(stderr, "\n  product:      %s\n", meta["product"])
	fmt.Fprintf(stderr, "  version:      %s\n", meta["version"])
	fmt.Fprintf(stderr, "  platform:     %s\n", meta["platform"])
	fmt.Fprintf(stderr, "  architecture: %s\n", meta["architecture"])
	fmt.Fprintf(stderr, "  uuid:         %s\n\n", meta["uuid"])
	return nil
}

// readArchiveMetadata runs `fwup -m -i <path>` and parses its meta-key="value"
// output, like readFirmwareMetadata. It differs in its errors: fwup reports an
// unreadable file on stdout (for example "fwup: Unrecognized archive format"),
// and that message is the useful part, so it is kept.
func readArchiveMetadata(ctx context.Context, path string) (firmwareMetadata, error) {
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, fwupBin, "-m", "-i", path)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s is not a readable fwup archive: %s", path, msg)
	}

	meta := firmwareMetadata{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || !strings.HasPrefix(key, "meta-") {
			continue
		}
		meta[strings.TrimPrefix(key, "meta-")] = strings.Trim(value, `"`)
	}
	return meta, nil
}

// checkArchiveMetadata applies NervesHub's archive checks to fwup metadata:
// the required keys are present and meta-version is SemVer.
func checkArchiveMetadata(meta firmwareMetadata) error {
	var missing []string
	for _, key := range archiveRequiredMeta {
		if meta[key] == "" {
			missing = append(missing, "meta-"+key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("archive metadata is missing %s, which NervesHub requires", strings.Join(missing, ", "))
	}
	if v := meta["version"]; !semver.Valid(v) {
		return fmt.Errorf("archive meta-version %q is not a SemVer version (MAJOR.MINOR.PATCH, such as 1.0.0 or 1.0.0-rc.1), which NervesHub requires", v)
	}
	return nil
}
