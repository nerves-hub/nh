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

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// firmwareShowCmd implements `nh firmware show <uuid>`.
var firmwareShowCmd = &cobra.Command{
	Use:   "show <uuid>",
	Short: "Show details for a firmware",
	Long:  "Show details for a single firmware by its UUID.",
	Args: exactlyOneArg(
		"Firmware UUID missing",
		"too many arguments: provide a single firmware UUID",
	),
	RunE: runFirmwareShow,
}

func init() {
	firmwareCmd.AddCommand(firmwareShowCmd)
}

func runFirmwareShow(cmd *cobra.Command, args []string) error {
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

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	firmware, err := client.GetFirmware(cmd.Context(), org, product, uuid)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, firmware)
	}

	tw := newTableWriter(w)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("UUID:", firmware.UUID)
	row("Version:", firmware.Version)
	row("Platform:", firmware.Platform)
	row("Architecture:", firmware.Architecture)
	row("Author:", firmware.Author)
	row("Description:", firmware.Description)
	row("Product:", firmware.Product)
	row("VCS identifier:", firmware.VCSIdentifier)
	row("fwup version:", firmware.FwupVersion)
	row("Misc:", firmware.Misc)
	if !firmware.InsertedAt.IsZero() {
		row("Created:", firmware.InsertedAt.Format("2006-01-02 15:04:05 MST"))
	}
	return tw.Flush()
}
