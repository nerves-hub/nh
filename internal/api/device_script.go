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
	"time"
)

// DefaultScriptTimeout is how long the server waits for a support script's
// output when no timeout is sent (ScriptController's default of 30_000 ms).
const DefaultScriptTimeout = 30 * time.Second

// MaxScriptTimeout is the longest timeout the server can wait. Its runner
// arms an Erlang timer for the timeout plus a 1s grace, and Erlang timers take
// at most 2^32-1 ms.
const MaxScriptTimeout = (1<<32 - 1 - 1000) * time.Millisecond

// scriptResponseMargin is added to the script timeout to get the HTTP timeout
// for a run, so the server's own timeout (and its 403 reply) is what ends a slow
// script rather than the client hanging up first.
const scriptResponseMargin = 15 * time.Second

// ListDeviceScripts lists the support scripts that can be run on a device: its
// product's scripts (ScriptController.index renders device.product's list).
//
// With no org and product, it calls GET /api/devices/{identifier}/scripts,
// where the server finds the device among the caller's orgs. There is no
// org- and product-scoped GET for a device's scripts.
//
// With both, the device is first checked to be in that product
// (GET /orgs/{org}/products/{product}/devices/{identifier}), so a device
// outside it is reported as not found, as every other device command does.
// The product's scripts are then listed via GET /orgs/{org}/products/{product}/scripts,
// which returns the same list as the device route. The device route is not
// used here because identifiers are only unique within an org: the server's
// lookup across orgs fails when the caller has the same identifier in two.
//
// Setting only one of org and product is an error.
func (c *Client) ListDeviceScripts(ctx context.Context, org, product, identifier string, opts ListSupportScriptsOptions) (*SupportScriptList, error) {
	if identifier == "" {
		return nil, errors.New("api: device identifier is required")
	}
	if (org == "") != (product == "") {
		return nil, errors.New("api: set both org and product, or neither")
	}

	query := url.Values{}
	if opts.Page > 0 {
		query.Set("pagination[page]", strconv.Itoa(opts.Page))
	}
	if opts.PageSize > 0 {
		query.Set("pagination[page_size]", strconv.Itoa(opts.PageSize))
	}

	path := "/devices/" + url.PathEscape(identifier) + "/scripts"
	if org != "" {
		// Only the status matters: the body is discarded rather than decoded.
		if err := c.Get(ctx, devicePath(org, product, identifier), nil, nil); err != nil {
			return nil, err
		}
		path = scriptsPath(org, product)
	}

	var result SupportScriptList
	if err := c.Get(ctx, path, query, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendDeviceScript runs a support script on a device via
// POST /orgs/{org}/products/{product}/devices/{identifier}/scripts/{name_or_id}
// and returns the script's text output.
//
// timeout is how long the server waits for the device to answer, sent as the
// `timeout` query parameter in milliseconds; zero leaves the server's default
// (DefaultScriptTimeout). The HTTP request is allowed that long plus a margin,
// so the client does not give up before the server does.
func (c *Client) SendDeviceScript(ctx context.Context, org, product, identifier, nameOrID string, timeout time.Duration) (string, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return "", err
	}
	if nameOrID == "" {
		return "", errors.New("api: script name or id is required")
	}
	if timeout < 0 {
		return "", errors.New("api: script timeout must not be negative")
	}
	if timeout > MaxScriptTimeout {
		return "", fmt.Errorf("api: script timeout must be at most %v", MaxScriptTimeout)
	}

	var query url.Values
	wait := DefaultScriptTimeout
	if timeout > 0 {
		ms := timeout.Milliseconds()
		if time.Duration(ms)*time.Millisecond < timeout {
			ms++ // round a sub-millisecond remainder up, never down to zero
		}
		query = url.Values{"timeout": {strconv.FormatInt(ms, 10)}}
		wait = time.Duration(ms) * time.Millisecond
	}

	path := devicePath(org, product, identifier) + "/scripts/" + url.PathEscape(nameOrID)
	return c.withMinTimeout(wait+scriptResponseMargin).postRawText(ctx, path, query)
}

// withMinTimeout returns c, or a copy of c whose HTTP client allows at least d
// per request. A client with no timeout (zero) is left alone; one with a
// longer timeout is never shortened. The copy shares c's transport.
func (c *Client) withMinTimeout(d time.Duration) *Client {
	if c.httpClient.Timeout == 0 || c.httpClient.Timeout >= d {
		return c
	}
	hc := *c.httpClient
	hc.Timeout = d
	cc := *c
	cc.httpClient = &hc
	return &cc
}
