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
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// deviceListCmd implements `nh device list`: show the devices in a product.
var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List devices in a product",
	Long: `List the devices within a product.

The org and product are taken from --org/--product, the NERVES_HUB_ORG/
NERVES_HUB_PRODUCT environment variables, or your saved settings.

Results are paginated; use --page and --page-size to navigate. When omitted,
the server's defaults apply.

Use --sort to order the results, e.g. --sort identifier or
--sort identifier:desc. The direction is optional and defaults to asc.

Use --filter key:value to narrow the results; repeat it for multiple filters,
e.g. --filter connection:not_seen --filter tag:prod. Filters are passed
through to the API as-is, so any server-supported filter works.`,
	Args: cobra.NoArgs,
	RunE: runDeviceList,
}

func init() {
	deviceListCmd.Flags().Int("page", 0, "page number to fetch")
	deviceListCmd.Flags().Int("page-size", 0, "number of devices per page")
	deviceListCmd.Flags().String("sort", "", "sort by column, e.g. identifier or identifier:desc")
	deviceListCmd.Flags().StringArray("filter", nil, "filter results as key:value; repeatable, e.g. --filter connection:not_seen")
	deviceCmd.AddCommand(deviceListCmd)
}

// parseFilters turns repeated "key:value" flag values into a map.
func parseFilters(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	filters := make(map[string]string, len(raw))
	for _, f := range raw {
		key, value, found := strings.Cut(f, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch {
		case !found:
			return nil, fmt.Errorf("invalid --filter %q: expected key:value", f)
		case key == "":
			return nil, fmt.Errorf("invalid --filter %q: missing key", f)
		case value == "":
			return nil, fmt.Errorf("invalid --filter %q: missing value", f)
		}
		filters[key] = value
	}
	return filters, nil
}

// parseSort splits a --sort value of the form "column" or "column:direction".
// The direction is optional and defaults to "asc"; it must be "asc" or "desc".
func parseSort(s string) (column, direction string, err error) {
	column, direction, hasDir := strings.Cut(s, ":")
	column = strings.TrimSpace(column)
	if column == "" {
		return "", "", errors.New("--sort requires a column name, e.g. --sort identifier")
	}
	if !hasDir || strings.TrimSpace(direction) == "" {
		return column, "asc", nil
	}
	switch direction = strings.ToLower(strings.TrimSpace(direction)); direction {
	case "asc", "desc":
		return column, direction, nil
	default:
		return "", "", fmt.Errorf("invalid sort direction %q: use asc or desc", direction)
	}
}

func runDeviceList(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	org, err := requireOrg(cfg)
	if err != nil {
		return err
	}
	product, err := requireProduct(cfg)
	if err != nil {
		return err
	}

	page, _ := cmd.Flags().GetInt("page")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	if page < 0 || pageSize < 0 {
		return errors.New("--page and --page-size must not be negative")
	}

	var sortColumn, sortDirection string
	if s, _ := cmd.Flags().GetString("sort"); s != "" {
		sortColumn, sortDirection, err = parseSort(s)
		if err != nil {
			return err
		}
	}

	rawFilters, _ := cmd.Flags().GetStringArray("filter")
	filters, err := parseFilters(rawFilters)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	result, err := client.ListDevices(cmd.Context(), org, product, api.ListDevicesOptions{
		Page:          page,
		PageSize:      pageSize,
		Sort:          sortColumn,
		SortDirection: sortDirection,
		Filters:       filters,
	})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, result)
	}

	if p := result.Pagination; p != nil && p.HasInfo() {
		fmt.Fprintf(w, "Page %d of %d — %d device(s) total\n\n", p.Page, p.TotalPages, p.TotalCount)
	}

	if len(result.Devices) == 0 {
		fmt.Fprintf(w, "No devices found in %s/%s.\n", org, product)
		return nil
	}

	tw := newTableWriter(w)
	fmt.Fprintln(tw, "IDENTIFIER\tSTATUS\tVERSION")
	for _, d := range result.Devices {
		version := d.Version
		if version == "" {
			version = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", d.Identifier, d.ConnectionStatus, version)
	}
	return tw.Flush()
}
