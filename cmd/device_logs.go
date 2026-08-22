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
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// logsMaxLimit mirrors the server's cap, so an out-of-range --limit fails here
// rather than after a round trip.
const logsMaxLimit = 1000

// logOrders are the accepted --order values.
var logOrders = []string{"desc", "asc"}

// relativeDays matches a plain day offset ("3d"), which time.ParseDuration does
// not accept but is the natural way to ask for logs from the last few days.
var relativeDays = regexp.MustCompile(`^(\d+)d$`)

var deviceLogsCmd = &cobra.Command{
	Use:   "logs <identifier>",
	Short: "Show the log lines a device has sent",
	Long: `Show the log lines a device has sent over the logging extension.

Newest first by default; pass --order asc for oldest first. --since is
inclusive and --before is exclusive, so the oldest timestamp of one page can be
passed straight back as the next --before without repeating that line.

Both accept an ISO 8601 timestamp (2026-08-16T09:14:00Z) or an offset from now
(30m, 2h, 3d).

Log lines are dropped three days after they were logged, and are only collected
while the logging extension is enabled for the product or device.`,
	Example: `  nh device logs thermostat-4021
  nh device logs thermostat-4021 --level error,warning --limit 50
  nh device logs thermostat-4021 --since 2h --order asc
  nh device logs thermostat-4021 -o json`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceLogs,
}

func init() {
	deviceLogsCmd.Flags().StringSlice("level", nil, "only lines at these levels (repeatable, or comma-separated)")
	deviceLogsCmd.Flags().String("since", "", "only lines at or after this time (ISO 8601, or an offset like 2h)")
	deviceLogsCmd.Flags().String("before", "", "only lines before this time (ISO 8601, or an offset like 2h)")
	deviceLogsCmd.Flags().Int("limit", 0, fmt.Sprintf("maximum lines to return, 1-%d (default 100)", logsMaxLimit))
	deviceLogsCmd.Flags().String("order", "", "desc for newest first (default), or asc")
	deviceLogsCmd.Flags().Bool("meta", false, "append the metadata the device attached to each line")
	deviceCmd.AddCommand(deviceLogsCmd)
}

func runDeviceLogs(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	filter, err := deviceLogsFilter(cmd)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	lines, err := client.ListDeviceLogs(cmd.Context(), org, product, identifier, filter)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		return printJSON(w, lines)
	}

	if len(lines) == 0 {
		fmt.Fprintf(w, "No log lines found for device %s.\n", identifier)
		return nil
	}

	showMeta, _ := cmd.Flags().GetBool("meta")
	tw := newTableWriter(w)
	fmt.Fprintln(tw, "TIMESTAMP\tLEVEL\tMESSAGE")
	for _, line := range lines {
		message := line.Message
		if showMeta {
			if meta := formatLogMeta(line.Meta); meta != "" {
				message += "  " + meta
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", formatLogTime(line.Timestamp), orDash(line.Level), message)
	}
	return tw.Flush()
}

// deviceLogsFilter builds the query from the flags, validating locally what the
// server would reject anyway so mistakes surface without a round trip.
func deviceLogsFilter(cmd *cobra.Command) (api.DeviceLogsFilter, error) {
	var filter api.DeviceLogsFilter

	levels, _ := cmd.Flags().GetStringSlice("level")
	filter.Levels = levels

	since, err := resolveLogTime(mustString(cmd, "since"))
	if err != nil {
		return filter, fmt.Errorf("invalid --since: %w", err)
	}
	filter.Since = since

	before, err := resolveLogTime(mustString(cmd, "before"))
	if err != nil {
		return filter, fmt.Errorf("invalid --before: %w", err)
	}
	filter.Before = before

	limit, _ := cmd.Flags().GetInt("limit")
	if cmd.Flags().Changed("limit") && (limit < 1 || limit > logsMaxLimit) {
		return filter, fmt.Errorf("invalid --limit %d: must be between 1 and %d", limit, logsMaxLimit)
	}
	filter.Limit = limit

	order := strings.ToLower(strings.TrimSpace(mustString(cmd, "order")))
	if order != "" && !slices.Contains(logOrders, order) {
		return filter, fmt.Errorf("invalid --order %q: must be one of %s", order, strings.Join(logOrders, ", "))
	}
	filter.Order = order

	return filter, nil
}

// resolveLogTime accepts either an ISO 8601 timestamp, passed through, or an
// offset from now ("30m", "2h", "3d"), resolved to an absolute UTC timestamp.
// An empty value stays empty so the server applies no bound.
func resolveLogTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if m := relativeDays.FindStringSubmatch(value); m != nil {
		days, err := strconv.Atoi(m[1])
		if err != nil {
			return "", fmt.Errorf("%q is not a valid day offset", value)
		}
		return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339), nil
	}

	if d, err := time.ParseDuration(value); err == nil {
		if d < 0 {
			return "", fmt.Errorf("%q must be a positive offset", value)
		}
		return time.Now().UTC().Add(-d).Format(time.RFC3339), nil
	}

	// Not an offset, so it must be a timestamp. Parse it here rather than
	// letting the server 422, and normalise it to RFC 3339.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("%q is neither an ISO 8601 timestamp (2026-08-16T09:14:00Z) nor an offset (30m, 2h, 3d)", value)
}

// formatLogTime renders a log timestamp keeping millisecond precision, which
// ordering by hand depends on.
func formatLogTime(ts api.Timestamp) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02 15:04:05.000 MST")
}

// formatLogMeta renders metadata as sorted key=value pairs, so repeated runs
// read the same way.
func formatLogMeta(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%v", k, meta[k]))
	}
	return strings.Join(pairs, " ")
}
