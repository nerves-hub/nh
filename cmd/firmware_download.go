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
	"net/http"
	"os"
	"path/filepath"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// firmwareDownloadCmd implements `nh firmware download <uuid>`.
var firmwareDownloadCmd = &cobra.Command{
	Use:   "download <uuid>",
	Short: "Download a firmware file",
	Long: `Download a firmware image by its UUID.

By default the image is saved as <uuid>.fw in the current directory. Use --file
to choose a different path, or --file - to stream it to stdout.`,
	Args: exactlyOneArg(
		"Firmware UUID missing",
		"too many arguments: provide a single firmware UUID",
	),
	RunE: runFirmwareDownload,
}

func init() {
	firmwareDownloadCmd.Flags().StringP("file", "f", "", "destination path (default <uuid>.fw; use - for stdout)")
	firmwareCmd.AddCommand(firmwareDownloadCmd)
}

func runFirmwareDownload(cmd *cobra.Command, args []string) error {
	uuid := args[0]

	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	// Firmware images can be large, so use a client without the default
	// per-request timeout; cancellation still flows through the context.
	client, err := newAuthedClient(cfg, api.WithHTTPClient(&http.Client{}))
	if err != nil {
		return err
	}

	dest, _ := cmd.Flags().GetString("file")
	if dest == "" {
		dest = uuid + ".fw"
	}

	// Show a progress bar on stderr when it's an interactive terminal — safe
	// even when the firmware itself streams to stdout.
	label := filepath.Base(dest)
	if dest == "-" {
		label = uuid
	}
	var bar *progressbar.ProgressBar
	var onProgress func(read, total int64)
	if stderrIsTerminal(cmd) {
		// The total (Content-Length) is only known on the first callback, so
		// the bar is created lazily.
		onProgress = func(read, total int64) {
			if bar == nil {
				bar = newTransferBar(cmd.ErrOrStderr(), "Downloading "+label, total)
			}
			_ = bar.Set64(read)
		}
	}
	finish := func() { finishBar(bar) }

	// Stream straight to stdout when requested.
	if dest == "-" {
		err := client.DownloadFirmware(cmd.Context(), org, product, uuid, cmd.OutOrStdout(), onProgress)
		finish()
		return err
	}

	// Write to a temp file in the destination directory and rename on success,
	// so a failed or partial download never clobbers an existing file.
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".nh-download-*")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpName := tmp.Name()

	err = client.DownloadFirmware(cmd.Context(), org, product, uuid, tmp, onProgress)
	finish()
	if err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("saving firmware: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded firmware to %s\n", dest)
	return nil
}
