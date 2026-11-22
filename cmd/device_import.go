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
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceImportCmd implements `nh device import <manifest>`.
var deviceImportCmd = &cobra.Command{
	Use:   "import <manifest.json>",
	Short: "Create devices in bulk from a Microchip Trust&Go manifest",
	Long: `Create a device, with its certificate, for every entry in a Microchip
Trust&Go manifest: the JSON file Microchip supplies with pre-provisioned
ATECC608 secure elements. Each entry's header.uniqueId becomes the device
identifier, and the first x5c certificate in its publicKeySet is registered
to the device. Pass - to read the manifest from standard input.

This is the only format NervesHub imports. It does not take CSV.

NervesHub accepts the file and creates the devices in the background, so it
never says which devices it created. A malformed entry makes it abandon the
whole file without saying so. nh therefore checks every entry first, the way
the server reads it, and sends nothing unless every entry is good. --dry-run
does the check and stops; it needs no login, org or product.

Once the import finishes NervesHub posts a notification on the product
(Notifications in the web UI) with how many devices it created and how many
failed. Devices fail there for reasons nh can't see, such as an identifier or
certificate already in use, or a certificate whose signer CA belongs to
another org.

--tag puts the same tags on every new device. The devices are created with
the product's default for managed updates.`,
	Example: `  # Check a manifest without sending it
  nh device import manifest.json --dry-run

  # Import it, tagging every device
  nh device import manifest.json --tag batch-7 --tag eu`,
	Args: exactlyOneArg(
		"Manifest file missing",
		"too many arguments: provide a single manifest file",
	),
	RunE: runDeviceImport,
}

func init() {
	deviceImportCmd.Flags().StringArray("tag", nil, "tag for every imported device; repeatable, or comma-separated")
	deviceImportCmd.Flags().Bool("dry-run", false, "check the manifest locally and send nothing")
	deviceCmd.AddCommand(deviceImportCmd)
}

// deviceImportResult is the -o json shape of `nh device import`.
type deviceImportResult struct {
	Org     string          `json:"org,omitempty"`
	Product string          `json:"product,omitempty"`
	Format  string          `json:"format"`
	Tags    []string        `json:"tags"`
	DryRun  bool            `json:"dry_run"`
	Sent    bool            `json:"sent"`
	Devices int             `json:"devices"`
	Invalid int             `json:"invalid"`
	Rows    []api.ImportRow `json:"rows"`
}

func runDeviceImport(cmd *cobra.Command, args []string) error {
	cfg := config.From(cmd.Context())
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	rawTags, _ := cmd.Flags().GetStringArray("tag")
	tags := importTags(rawTags)
	if err := api.ValidateImportTags(tags); err != nil {
		return fmt.Errorf("invalid --tag: %w", err)
	}

	data, err := readManifest(cmd, args[0])
	if err != nil {
		return err
	}
	manifest, err := api.ParseTrustAndGoManifest(data)
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}

	result := deviceImportResult{
		Format:  manifest.Format,
		Tags:    tags,
		DryRun:  dryRun,
		Devices: len(manifest.Rows),
		Invalid: len(manifest.Invalid()),
		Rows:    manifest.Rows,
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}

	// Scope and credentials are only needed to send: a dry run works
	// offline. They are checked before anything is printed, so a missing
	// --product isn't reported after a table that looks like success.
	var client *api.Client
	if !dryRun && result.Invalid == 0 {
		if result.Org, result.Product, err = requireOrgProduct(cfg); err != nil {
			return err
		}
		if client, err = newAuthedClient(cfg); err != nil {
			return err
		}
		if err := client.ImportDevices(cmd.Context(), result.Org, result.Product, manifest, tags); err != nil {
			return err
		}
		result.Sent = true
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if err := printJSON(w, result); err != nil {
			return err
		}
	} else if err := renderDeviceImport(w, result); err != nil {
		return err
	}

	if result.Invalid > 0 {
		return fmt.Errorf("%d of %d manifest entries have problems: nothing was sent", result.Invalid, result.Devices)
	}
	return nil
}

// readManifest reads the manifest from path, or from standard input for "-".
func readManifest(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("reading manifest from standard input: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return data, nil
}

// importTags splits repeated and comma-separated --tag values into tags,
// trimming each and dropping empty ones, as the server does with the header.
func importTags(values []string) []string {
	var tags []string
	for _, v := range values {
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return tags
}

// renderDeviceImport writes one line per manifest entry and a summary.
func renderDeviceImport(w io.Writer, r deviceImportResult) error {
	tw := newTableWriter(w)
	fmt.Fprintln(tw, "ROW\tIDENTIFIER\tCERTIFICATE SERIAL\tEXPIRES\tSTATUS")
	for _, row := range r.Rows {
		serial, expires := "-", "-"
		if c := row.Certificate; c != nil {
			serial = formatSerial(c.Serial)
			expires = c.NotAfter.Format("2006-01-02")
		}
		status := "ok"
		if row.Error != "" {
			status = "error: " + row.Error
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", row.Row, orDash(row.Identifier), serial, expires, status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)

	devices := pluralDevices(r.Devices)
	switch {
	case r.Invalid > 0:
		fmt.Fprintf(w, "%d of %d entries would not import. Fix them and run the import again; nothing was sent.\n",
			r.Invalid, r.Devices)
	case r.DryRun:
		fmt.Fprintf(w, "No problems found in %s. Dry run: nothing was sent.\n", devices)
	default:
		fmt.Fprintf(w, "Queued %s for import into %s/%s", devices, r.Org, r.Product)
		if len(r.Tags) > 0 {
			fmt.Fprintf(w, " with tags %s", strings.Join(r.Tags, ", "))
		}
		fmt.Fprintln(w, ".")
		fmt.Fprintln(w, "NervesHub creates them in the background and posts a notification on the product")
		fmt.Fprintln(w, "with how many it created and how many failed.")
	}
	return nil
}

func pluralDevices(n int) string {
	if n == 1 {
		return "1 device"
	}
	return strconv.Itoa(n) + " devices"
}
