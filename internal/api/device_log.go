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

package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// LogLine is a line a device sent over the logging extension, as returned by
// GET /orgs/{org}/products/{product}/devices/{identifier}/logs.
type LogLine struct {
	Timestamp Timestamp `json:"timestamp"`
	// Level is usually an Elixir Logger level (debug, info, notice, warning,
	// error, critical, alert, emergency), but a device may log at any level.
	Level   string `json:"level"`
	Message string `json:"message"`
	// Meta is the Logger metadata the device attached to the line. The server
	// flattens values to strings, but this stays permissive so an unexpected
	// type cannot fail the whole response.
	Meta map[string]any `json:"meta,omitempty"`
}

// DeviceLogsFilter narrows a device log query. Zero values are omitted, letting
// the server apply its own defaults (100 lines, newest first).
type DeviceLogsFilter struct {
	// Levels matches any of the given levels. They are sent as the
	// comma-separated list the API expects, and are matched as given: a level
	// the device never logs at simply matches nothing.
	Levels []string
	// Search matches lines whose message contains this text, ignoring case. It
	// is matched literally, so % and _ are searched for rather than treated as
	// wildcards.
	Search string
	// Since is inclusive, Before exclusive, both ISO 8601. Before being
	// exclusive means the oldest line of a page can be passed straight back to
	// fetch the next one without repeating it.
	Since  string
	Before string
	// Limit is 1–1000; 0 leaves the server default of 100.
	Limit int
	// Order is "desc" (default) or "asc".
	Order string
}

// ListDeviceLogs returns the log lines a device has sent, via
// GET /orgs/{org}/products/{product}/devices/{identifier}/logs.
func (c *Client) ListDeviceLogs(ctx context.Context, org, product, identifier string, filter DeviceLogsFilter) ([]LogLine, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if product == "" {
		return nil, errors.New("api: product is required")
	}
	if identifier == "" {
		return nil, errors.New("api: device identifier is required")
	}

	query := url.Values{}
	if levels := trimmedNonEmpty(filter.Levels); len(levels) > 0 {
		query.Set("level", strings.Join(levels, ","))
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		query.Set("search", search)
	}
	if filter.Since != "" {
		query.Set("since", filter.Since)
	}
	if filter.Before != "" {
		query.Set("before", filter.Before)
	}
	if filter.Limit != 0 {
		query.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Order != "" {
		query.Set("order", filter.Order)
	}

	var resp struct {
		Data []LogLine `json:"data"`
	}
	if err := c.Get(ctx, devicePath(org, product, identifier)+"/logs", query, &resp); err != nil {
		return nil, fmt.Errorf("fetching logs for device %s: %w", identifier, err)
	}
	return resp.Data, nil
}

// trimmedNonEmpty drops blank entries so a stray comma cannot send an empty
// filter value.
func trimmedNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
