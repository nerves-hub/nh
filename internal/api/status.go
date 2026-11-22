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
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// statusPath is NervesHub's health check, served by NervesHubWeb.Plugs.ImAlive
// at the root of the host rather than under /api.
const statusPath = "/status/alive"

// maxStatusBody caps how much of a health check response is read.
const maxStatusBody = 4 << 10 // 4 KiB

// ServerStatus is the answer to a health check.
type ServerStatus struct {
	// URL is the health check URL that was requested.
	URL string `json:"url"`
	// StatusCode is the HTTP status the server answered with. NervesHub
	// answers 200 when it is up and can reach its database, and 500 when it is
	// up but its database is unreachable.
	StatusCode int `json:"status_code"`
	// Message is the response body, trimmed: "Hello, Friend!" when healthy.
	Message string `json:"message"`
}

// ImAlive's answers. NervesHub before v2.3.0 answered "Hello Friend!", without
// the comma, and had no database check.
var (
	aliveMessages = []string{"Hello, Friend!", "Hello Friend!"}
	deadMessage   = "Sorry, Friend :("
)

// Healthy reports whether NervesHub answered that it is up and can reach its
// database. A 200 with any other body is from something other than NervesHub
// (a web frontend's catch-all route, say), so it isn't healthy.
func (s *ServerStatus) Healthy() bool {
	return s.StatusCode == http.StatusOK && slices.Contains(aliveMessages, s.Message)
}

// DatabaseDown reports whether NervesHub answered that it is up but can't
// reach its database.
func (s *ServerStatus) DatabaseDown() bool {
	return s.StatusCode == http.StatusInternalServerError && s.Message == deadMessage
}

// StatusURL returns the health check URL Status requests.
func (c *Client) StatusURL() string {
	return c.rootURL(statusPath)
}

// Status checks the server's health via GET /status/alive. It needs no
// authentication and sends no token. The error is non-nil only when the server
// couldn't be reached; any HTTP answer, healthy or not, is returned as a
// ServerStatus. The endpoint reports no server version.
func (c *Client) Status(ctx context.Context) (*ServerStatus, error) {
	endpoint := c.StatusURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("api: GET %s: %w", endpoint, ctxErr)
		}
		// A *url.Error already names the method and URL.
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxStatusBody))
	if err != nil {
		return nil, fmt.Errorf("api: reading response: %w", err)
	}
	return &ServerStatus{
		URL:        endpoint,
		StatusCode: resp.StatusCode,
		Message:    strings.TrimSpace(string(body)),
	}, nil
}

// rootURL joins path onto the base URL's host root: the base URL's path with a
// trailing /api segment removed, so a base of https://host/api still reaches
// https://host/status/alive. Query and fragment are dropped.
func (c *Client) rootURL(path string) string {
	u := *c.baseURL
	u.RawQuery, u.Fragment, u.RawPath = "", "", ""
	base := strings.TrimRight(u.Path, "/")
	base = strings.TrimSuffix(base, apiPrefix)
	u.Path = strings.TrimRight(base, "/") + path
	return u.String()
}
