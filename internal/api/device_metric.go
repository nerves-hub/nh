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

// MetricReport is one report a device sent over the health or metrics
// extension: the readings it took at a single moment, as returned by
// GET /orgs/{org}/products/{product}/devices/{identifier}/metrics.
type MetricReport struct {
	// Timestamp is when the device took the readings, to microsecond
	// precision. A device sends one report per timestamp, so it identifies the
	// report.
	Timestamp Timestamp `json:"timestamp"`
	// FirmwareUUID is the firmware the device was running when it took the
	// readings, or nil if it had not reported one. A pointer so the server's
	// null survives being printed back out as JSON.
	FirmwareUUID *string `json:"firmware_uuid"`
	// Metrics are the readings, keyed by the device's own metric names. The
	// server drops values that are not numbers before storing them.
	Metrics map[string]float64 `json:"metrics"`
}

// DeviceMetricsFilter narrows a device metrics query. Zero values are omitted,
// letting the server apply its own defaults (100 reports, newest first).
type DeviceMetricsFilter struct {
	// Keys keeps only these metrics. They are sent as the comma-separated
	// list the API expects, and matched as given: a name the device has never
	// reported matches nothing, and a report carrying none of them is left
	// out.
	Keys []string
	// Since is inclusive, Before exclusive, both ISO 8601. Before being
	// exclusive means the oldest report of a page can be passed straight back
	// to fetch the next one without repeating it.
	Since  string
	Before string
	// Limit is 1–1000 and counts reports, not readings; 0 leaves the server
	// default of 100. A page never ends partway through a report.
	Limit int
	// Order is "desc" (default) or "asc".
	Order string
}

// ListDeviceMetrics returns the metric reports a device has sent, via
// GET /orgs/{org}/products/{product}/devices/{identifier}/metrics.
//
// A server with no analytics database stores no history and answers 501,
// which comes back as an *APIError carrying the server's explanation.
func (c *Client) ListDeviceMetrics(ctx context.Context, org, product, identifier string, filter DeviceMetricsFilter) ([]MetricReport, error) {
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
	if keys := trimmedNonEmpty(filter.Keys); len(keys) > 0 {
		query.Set("key", strings.Join(keys, ","))
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
		Data []MetricReport `json:"data"`
	}
	if err := c.Get(ctx, devicePath(org, product, identifier)+"/metrics", query, &resp); err != nil {
		return nil, fmt.Errorf("fetching metrics for device %s: %w", identifier, err)
	}
	return resp.Data, nil
}
