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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
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

// defaultFollowInterval is how often --follow asks for new lines. The API has
// no streaming endpoint, so following is polling; a couple of seconds keeps a
// tail feeling live without hammering the server.
const defaultFollowInterval = 2 * time.Second

// serverDefaultLimit mirrors the page size the API applies when no limit is
// given, so follow can tell a full page from a partial one.
const serverDefaultLimit = 100

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

--search keeps only lines whose message contains the given text, ignoring case.
It is matched literally, so % and _ are searched for rather than treated as
wildcards.

--follow prints the most recent lines and then keeps printing new ones until
interrupted, the way tail -f does. The API has no streaming endpoint, so this
is polling; --interval sets how often it asks.

Log lines are dropped three days after they were logged, and are only collected
while the logging extension is enabled for the product or device.`,
	Example: `  nh device logs thermostat-4021
  nh device logs thermostat-4021 --level error,warning --limit 50
  nh device logs thermostat-4021 --search "sensor bus"
  nh device logs thermostat-4021 --since 2h --order asc
  nh device logs thermostat-4021 --follow
  nh device logs thermostat-4021 -o json`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceLogs,
}

func init() {
	deviceLogsCmd.Flags().StringSlice("level", nil, "only lines at these levels (repeatable, or comma-separated)")
	deviceLogsCmd.Flags().String("search", "", "only lines whose message contains this text (case-insensitive, matched literally)")
	deviceLogsCmd.Flags().String("since", "", "only lines at or after this time (ISO 8601, or an offset like 2h)")
	deviceLogsCmd.Flags().String("before", "", "only lines before this time (ISO 8601, or an offset like 2h)")
	deviceLogsCmd.Flags().Int("limit", 0, fmt.Sprintf("maximum lines to return, 1-%d (default 100)", logsMaxLimit))
	deviceLogsCmd.Flags().String("order", "", "desc for newest first (default), or asc")
	deviceLogsCmd.Flags().Bool("meta", false, "append the metadata the device attached to each line")
	deviceLogsCmd.Flags().BoolP("follow", "f", false, "print new lines as they arrive, by polling, until interrupted")
	deviceLogsCmd.Flags().Duration("interval", defaultFollowInterval, "how often --follow polls for new lines")
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

	printer := newLogPrinter(cmd, cfg)

	if follow, _ := cmd.Flags().GetBool("follow"); follow {
		interval, _ := cmd.Flags().GetDuration("interval")
		printer.stream = true
		return followDeviceLogs(cmd, client, printer, org, product, identifier, filter, interval)
	}

	lines, err := client.ListDeviceLogs(cmd.Context(), org, product, identifier, filter)
	if err != nil {
		return err
	}

	if len(lines) == 0 && !printer.json {
		fmt.Fprintf(printer.w, "No log lines found for device %s.\n", identifier)
		return nil
	}
	return printer.print(lines)
}

// followDeviceLogs prints the most recent lines and then polls for new ones
// until the context is cancelled or the user interrupts.
//
// The first page is fetched newest-first so it is the most recent lines, then
// reversed: a tail reads oldest to newest. Every poll after that asks
// oldest-first from the last timestamp seen.
func followDeviceLogs(cmd *cobra.Command, client *api.Client, printer *logPrinter, org, product, identifier string, filter api.DeviceLogsFilter, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("invalid --interval %s: must be positive", interval)
	}

	// Ctrl-C ends the tail cleanly rather than killing the process mid-write.
	// Scoped to this loop, so the rest of the CLI keeps default behaviour.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	pageSize := filter.Limit
	if pageSize == 0 {
		pageSize = serverDefaultLimit
	}

	initial := filter
	initial.Order = "desc"
	lines, err := client.ListDeviceLogs(ctx, org, product, identifier, initial)
	if err != nil {
		return followError(ctx, err)
	}
	slices.Reverse(lines)

	cursor := &logCursor{}
	if err := printer.print(lines); err != nil {
		return err
	}
	cursor.advance(lines)

	// With no history there is nothing to anchor to, so follow from now rather
	// than replaying whatever the server considers the oldest page.
	if cursor.last.IsZero() {
		cursor.last = time.Now().UTC()
	}

	for {
		poll := filter
		poll.Order = "asc"
		poll.Before = ""
		poll.Since = cursor.last.UTC().Format(time.RFC3339Nano)

		lines, err := client.ListDeviceLogs(ctx, org, product, identifier, poll)
		if err != nil {
			return followError(ctx, err)
		}

		fresh := cursor.unseen(lines)
		if err := printer.print(fresh); err != nil {
			return err
		}
		cursor.advance(fresh)

		// A full page means there is probably more waiting, so catch up
		// immediately instead of pacing behind the interval.
		if len(lines) >= pageSize {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// followError turns cancellation into a clean exit; a tail ended by Ctrl-C has
// not failed.
func followError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// logCursor tracks the newest line printed so polling can resume from it.
//
// The API's `since` is inclusive, so resuming from the last timestamp returns
// that line again. Rather than nudging the timestamp forward — which would
// silently drop a line sharing the same microsecond — the lines already printed
// at that exact timestamp are remembered and skipped. Only the boundary
// timestamp is tracked, so this stays small however long the tail runs.
type logCursor struct {
	last time.Time
	seen map[string]bool
}

// unseen returns the lines that have not been printed yet.
func (c *logCursor) unseen(lines []api.LogLine) []api.LogLine {
	fresh := make([]api.LogLine, 0, len(lines))
	for _, line := range lines {
		ts := line.Timestamp.Time
		switch {
		case ts.Before(c.last):
			continue
		case ts.Equal(c.last) && c.seen[logLineKey(line)]:
			continue
		}
		fresh = append(fresh, line)
	}
	return fresh
}

// advance moves the cursor past the given lines.
func (c *logCursor) advance(lines []api.LogLine) {
	for _, line := range lines {
		ts := line.Timestamp.Time
		if ts.After(c.last) {
			c.last = ts
			c.seen = nil
		}
		if ts.Equal(c.last) {
			if c.seen == nil {
				c.seen = map[string]bool{}
			}
			c.seen[logLineKey(line)] = true
		}
	}
}

// logLineKey identifies a line within a single timestamp.
func logLineKey(line api.LogLine) string {
	return line.Level + "\x00" + line.Message
}

// logPrinter renders log lines in the configured output format.
type logPrinter struct {
	w        io.Writer
	json     bool
	showMeta bool
	// stream is set while following. It switches JSON output to one object per
	// line, because a tail has no end at which to close an array.
	stream bool
	// headerWritten keeps the table header to the first batch, so a followed
	// tail does not repeat it on every poll.
	headerWritten bool
}

func newLogPrinter(cmd *cobra.Command, cfg *config.Config) *logPrinter {
	showMeta, _ := cmd.Flags().GetBool("meta")
	return &logPrinter{
		w:        cmd.OutOrStdout(),
		json:     cfg.Output == config.OutputJSON,
		showMeta: showMeta,
	}
}

// print writes the lines, flushing as it goes so a tail appears live.
//
// JSON output is one object per line rather than an array: a followed stream
// has no end, so there is no closing bracket to write.
func (p *logPrinter) print(lines []api.LogLine) error {
	if len(lines) == 0 {
		return nil
	}

	if p.json {
		// One-shot output stays a single indented array, like every other
		// command. Only a stream is emitted line by line.
		if !p.stream {
			return printJSON(p.w, lines)
		}
		enc := json.NewEncoder(p.w)
		for _, line := range lines {
			if err := enc.Encode(line); err != nil {
				return err
			}
		}
		return nil
	}

	tw := newTableWriter(p.w)
	if !p.headerWritten {
		fmt.Fprintln(tw, "TIMESTAMP\tLEVEL\tMESSAGE")
		p.headerWritten = true
	}
	for _, line := range lines {
		message := line.Message
		if p.showMeta {
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

	// --follow decides its own ordering and has no upper bound, so combining it
	// with either is a contradiction rather than something to resolve silently.
	follow, _ := cmd.Flags().GetBool("follow")
	if follow {
		if cmd.Flags().Changed("order") {
			return filter, errors.New("--order cannot be used with --follow, which always prints oldest first")
		}
		if cmd.Flags().Changed("before") {
			return filter, errors.New("--before cannot be used with --follow, which prints lines as they arrive")
		}
	} else if cmd.Flags().Changed("interval") {
		return filter, errors.New("--interval only applies with --follow")
	}

	levels, _ := cmd.Flags().GetStringSlice("level")
	filter.Levels = levels
	filter.Search = mustString(cmd, "search")

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
