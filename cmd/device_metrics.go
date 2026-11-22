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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// metricsMaxLimit mirrors the server's cap on reports per request, so an
// out-of-range --limit fails here rather than after a round trip.
const metricsMaxLimit = 1000

var deviceMetricsCmd = &cobra.Command{
	Use:   "metrics <identifier>",
	Short: "Show the metric readings a device has reported",
	Long: `Show the metric readings a device has reported over the health or metrics
extension.

The device sends readings in reports: one timestamp and every reading taken at
it. The table prints one row per reading, grouped by metric name, each group in
the order the reports came back. -o json prints the reports as the server
sent them, including the firmware the device was running at the time.

Newest reports first by default; pass --order asc for oldest first. --limit
counts reports, not readings. --since is inclusive and --before is exclusive,
so the oldest timestamp of one page can be passed straight back as the next
--before without repeating that report.

Both accept an ISO 8601 timestamp (2026-08-16T09:14:00Z) or an offset from now
(30m, 2h, 3d).

--key keeps only the named metrics. Names are the device's own and matched
exactly; a name the device never reported matches nothing.

--list-keys prints the metric names found in the reports fetched (the newest
100 unless --since, --before or --limit say otherwise) instead of the
readings. The API has no list of every name a device has used, so this is the
way to see what a device is currently reporting.

--follow prints the most recent readings and then keeps printing new ones
until interrupted, the way tail -f does. The API has no streaming endpoint, so
this is polling; --interval sets how often it asks. With -o json it prints one
report per line; a reading stored after its report was first printed arrives
as another line with the same timestamp, carrying only the new readings.

History is kept for thirty days. A server with no analytics database stores
none, and says so.`,
	Example: `  nh device metrics thermostat-4021
  nh device metrics thermostat-4021 --key cpu_temp,mem_used_percent --since 2h
  nh device metrics thermostat-4021 --list-keys
  nh device metrics thermostat-4021 --key cpu_temp --follow
  nh device metrics thermostat-4021 --order asc --limit 500 -o json`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceMetrics,
}

func init() {
	deviceMetricsCmd.Flags().StringSlice("key", nil, "only these metrics (repeatable, or comma-separated)")
	deviceMetricsCmd.Flags().String("since", "", "only reports at or after this time (ISO 8601, or an offset like 2h)")
	deviceMetricsCmd.Flags().String("before", "", "only reports before this time (ISO 8601, or an offset like 2h)")
	deviceMetricsCmd.Flags().Int("limit", 0, fmt.Sprintf("maximum reports to return, 1-%d (default 100)", metricsMaxLimit))
	deviceMetricsCmd.Flags().String("order", "", "desc for newest first (default), or asc")
	deviceMetricsCmd.Flags().Bool("list-keys", false, "list the metric names in the fetched reports instead of their readings")
	deviceMetricsCmd.Flags().BoolP("follow", "f", false, "print new readings as they arrive, by polling, until interrupted")
	deviceMetricsCmd.Flags().Duration("interval", defaultFollowInterval, "how often --follow polls for new readings")
	deviceCmd.AddCommand(deviceMetricsCmd)
}

func runDeviceMetrics(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	cfg := config.From(cmd.Context())
	org, product, err := requireOrgProduct(cfg)
	if err != nil {
		return err
	}

	filter, err := deviceMetricsFilter(cmd)
	if err != nil {
		return err
	}

	client, err := newAuthedClient(cfg)
	if err != nil {
		return err
	}

	printer := &metricPrinter{
		w:    cmd.OutOrStdout(),
		json: cfg.Output == config.OutputJSON,
	}

	if follow, _ := cmd.Flags().GetBool("follow"); follow {
		interval, _ := cmd.Flags().GetDuration("interval")
		printer.stream = true
		return followDeviceMetrics(cmd, client, printer, org, product, identifier, filter, interval)
	}

	reports, err := client.ListDeviceMetrics(cmd.Context(), org, product, identifier, filter)
	if err != nil {
		return err
	}

	if listKeys, _ := cmd.Flags().GetBool("list-keys"); listKeys {
		keys := metricKeys(reports)
		if printer.json {
			return printJSON(printer.w, keys)
		}
		if len(keys) == 0 {
			fmt.Fprintf(printer.w, "No metrics found for device %s.\n", identifier)
			return nil
		}
		for _, key := range keys {
			fmt.Fprintln(printer.w, key)
		}
		return nil
	}

	if printer.json {
		// Always an array, even an empty one, so a script can parse the
		// output without special-casing "nothing found".
		if reports == nil {
			reports = []api.MetricReport{}
		}
		return printJSON(printer.w, reports)
	}
	if len(reports) == 0 {
		fmt.Fprintf(printer.w, "No metric reports found for device %s.\n", identifier)
		return nil
	}
	return printer.printGrouped(reports)
}

// followDeviceMetrics prints the most recent readings and then polls for new
// reports until the context is cancelled or the user interrupts.
//
// It follows the same shape as followDeviceLogs: the first page is fetched
// newest-first and reversed, and every poll after that asks oldest-first from
// the last timestamp seen.
func followDeviceMetrics(cmd *cobra.Command, client *api.Client, printer *metricPrinter, org, product, identifier string, filter api.DeviceMetricsFilter, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("invalid --interval %s: must be positive", interval)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	initial := filter
	initial.Order = "desc"
	reports, err := client.ListDeviceMetrics(ctx, org, product, identifier, initial)
	if err != nil {
		return followError(ctx, err)
	}
	slices.Reverse(reports)

	cursor := &metricCursor{}
	if err := printer.printReports(reports); err != nil {
		return err
	}
	cursor.advance(reports)

	if cursor.last.IsZero() {
		cursor.last = time.Now().UTC()
	}

	// `since` is inclusive, so every poll returns the newest report already
	// printed. A page of one could then never move past it, so polls ask for
	// at least two.
	pageSize := filter.Limit
	if pageSize == 0 {
		pageSize = serverDefaultLimit
	}
	pageSize = max(pageSize, 2)

	for {
		poll := filter
		poll.Order = "asc"
		poll.Before = ""
		poll.Limit = pageSize
		poll.Since = cursor.last.UTC().Format(time.RFC3339Nano)

		reports, err := client.ListDeviceMetrics(ctx, org, product, identifier, poll)
		if err != nil {
			return followError(ctx, err)
		}

		fresh := cursor.unseen(reports)
		if err := printer.printReports(fresh); err != nil {
			return err
		}
		cursor.advance(fresh)

		// A full page that moved the cursor means there is probably more
		// waiting, so catch up immediately instead of pacing behind the
		// interval.
		if len(reports) >= pageSize && len(fresh) > 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// metricCursor tracks the newest report printed so polling can resume from
// it.
//
// The API's `since` is inclusive, so resuming from the last timestamp returns
// that report again. The readings already printed at that timestamp are
// remembered and skipped, rather than nudging the timestamp forward, so a
// reading the server stored after the report was first read is still shown.
type metricCursor struct {
	last time.Time
	seen map[string]bool
}

// unseen returns the reports newer than the cursor, with the boundary report
// trimmed to the readings not printed yet.
func (c *metricCursor) unseen(reports []api.MetricReport) []api.MetricReport {
	fresh := make([]api.MetricReport, 0, len(reports))
	for _, report := range reports {
		ts := report.Timestamp.Time
		switch {
		case ts.Before(c.last):
			continue
		case ts.Equal(c.last):
			trimmed := report
			trimmed.Metrics = map[string]float64{}
			for key, value := range report.Metrics {
				if !c.seen[key] {
					trimmed.Metrics[key] = value
				}
			}
			if len(trimmed.Metrics) == 0 {
				continue
			}
			report = trimmed
		}
		fresh = append(fresh, report)
	}
	return fresh
}

// advance moves the cursor past the given reports.
func (c *metricCursor) advance(reports []api.MetricReport) {
	for _, report := range reports {
		ts := report.Timestamp.Time
		if ts.After(c.last) {
			c.last = ts
			c.seen = nil
		}
		if ts.Equal(c.last) {
			if c.seen == nil {
				c.seen = map[string]bool{}
			}
			for key := range report.Metrics {
				c.seen[key] = true
			}
		}
	}
}

// metricPrinter renders metric reports in the configured output format.
type metricPrinter struct {
	w    io.Writer
	json bool
	// stream is set while following. It switches JSON output to one report
	// per line, because a tail has no end at which to close an array.
	stream bool
	// headerWritten keeps the table header to the first batch, so a followed
	// tail does not repeat it on every poll.
	headerWritten bool
}

// printGrouped writes one row per reading, grouped by metric name and, within
// a name, in the order the reports came back. Every row carries its name, so
// the table still works piped through grep or awk.
func (p *metricPrinter) printGrouped(reports []api.MetricReport) error {
	tw := newTableWriter(p.w)
	fmt.Fprintln(tw, "METRIC\tTIMESTAMP\tVALUE")
	for _, key := range metricKeys(reports) {
		for _, report := range reports {
			value, ok := report.Metrics[key]
			if !ok {
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", key, formatLogTime(report.Timestamp), formatMetricValue(value))
		}
	}
	return tw.Flush()
}

// printReports writes reports in arrival order, for --follow: a tail cannot
// group by name without waiting for the end. Within a report the readings are
// sorted by name so repeated reports line up.
func (p *metricPrinter) printReports(reports []api.MetricReport) error {
	if len(reports) == 0 {
		return nil
	}

	if p.json {
		enc := json.NewEncoder(p.w)
		for _, report := range reports {
			if err := enc.Encode(report); err != nil {
				return err
			}
		}
		return nil
	}

	tw := newTableWriter(p.w)
	if !p.headerWritten {
		fmt.Fprintln(tw, "METRIC\tTIMESTAMP\tVALUE")
		p.headerWritten = true
	}
	for _, report := range reports {
		for _, key := range sortedMetricNames(report.Metrics) {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", key, formatLogTime(report.Timestamp), formatMetricValue(report.Metrics[key]))
		}
	}
	return tw.Flush()
}

// metricKeys returns the distinct metric names across the reports, sorted.
func metricKeys(reports []api.MetricReport) []string {
	set := map[string]bool{}
	for _, report := range reports {
		for key := range report.Metrics {
			set[key] = true
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMetricNames(metrics map[string]float64) []string {
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// formatMetricValue prints a reading as briefly as it can be without losing
// precision: 41.2 stays 41.2 and 38.0 becomes 38.
func formatMetricValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// deviceMetricsFilter builds the query from the flags, validating locally
// what the server would reject anyway so mistakes surface without a round
// trip.
func deviceMetricsFilter(cmd *cobra.Command) (api.DeviceMetricsFilter, error) {
	var filter api.DeviceMetricsFilter

	follow, _ := cmd.Flags().GetBool("follow")
	if follow {
		if cmd.Flags().Changed("order") {
			return filter, errors.New("--order cannot be used with --follow, which always prints oldest first")
		}
		if cmd.Flags().Changed("before") {
			return filter, errors.New("--before cannot be used with --follow, which prints readings as they arrive")
		}
		if cmd.Flags().Changed("list-keys") {
			return filter, errors.New("--list-keys cannot be used with --follow")
		}
	} else if cmd.Flags().Changed("interval") {
		return filter, errors.New("--interval only applies with --follow")
	}

	keys, _ := cmd.Flags().GetStringSlice("key")
	filter.Keys = keys

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
	if cmd.Flags().Changed("limit") && (limit < 1 || limit > metricsMaxLimit) {
		return filter, fmt.Errorf("invalid --limit %d: must be between 1 and %d", limit, metricsMaxLimit)
	}
	filter.Limit = limit

	order := strings.ToLower(strings.TrimSpace(mustString(cmd, "order")))
	if order != "" && !slices.Contains(logOrders, order) {
		return filter, fmt.Errorf("invalid --order %q: must be one of %s", order, strings.Join(logOrders, ", "))
	}
	filter.Order = order

	return filter, nil
}
