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

// Package api is the HTTP client for the NervesCloud / NervesHub REST API.
//
// Client owns transport concerns only — URL resolution, authentication, JSON
// encoding/decoding, and turning non-2xx responses into a structured
// *APIError. Resource-specific request methods (orgs, products, devices, …)
// live in sibling files and build on Client's verb helpers.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent is sent when none is supplied via WithUserAgent.
const DefaultUserAgent = "nh"

// apiPrefix is the path under which the NervesHub REST API is mounted.
const apiPrefix = "/api"

// authScheme is the Authorization header scheme used for personal access
// tokens.
//
// TODO: confirm against the old nerves_hub_cli / NervesHub server whether the
// scheme is "Bearer" or "token"; the migration treats the old CLI's wire
// behavior as the spec.
const authScheme = "Bearer"

// maxErrorBody caps how much of an error response body is read into memory.
const maxErrorBody = 1 << 20 // 1 MiB

// Client is a NervesCloud / NervesHub API client. It is safe for concurrent
// use; construct one per invocation with NewClient.
type Client struct {
	baseURL    *url.URL
	token      string
	userAgent  string
	httpClient *http.Client
}

// Option customises a Client during construction.
type Option func(*Client)

// WithHTTPClient sets the underlying *http.Client. Useful for tests and for
// callers that need custom transport, timeouts, or TLS configuration.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// NewClient builds a Client targeting baseURL (the API host, e.g.
// https://manage.nervescloud.com) and authenticating with token. The token may
// be empty for unauthenticated endpoints.
func NewClient(baseURL, token string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("api: base URL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("api: invalid base URL %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("api: base URL %q must be absolute (scheme and host)", baseURL)
	}

	c := &Client{
		baseURL:    u,
		token:      token,
		userAgent:  DefaultUserAgent,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Get issues a GET to path (relative to the API root) with optional query
// parameters, decoding a JSON response body into out when out is non-nil.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// Post issues a POST with a JSON-encoded body, decoding the response into out.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

// Put issues a PUT with a JSON-encoded body, decoding the response into out.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, body, out)
}

// Patch issues a PATCH with a JSON-encoded body, decoding the response into out.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, nil, body, out)
}

// Delete issues a DELETE to path.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

// postRawText issues a no-body POST and returns the raw response body as a
// string, for endpoints that reply with text/plain rather than JSON.
func (c *Client) postRawText(ctx context.Context, path string) (string, error) {
	endpoint, err := c.resolve(path, nil)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", authScheme+" "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("api: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", parseError(resp)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("api: reading response: %w", err)
	}
	return string(b), nil
}

// GetRaw issues a GET to path and streams a successful (2xx) response body to
// dst without JSON-decoding it, making it suitable for binary downloads.
// Non-2xx responses are returned as *APIError. Redirects are followed by the
// underlying client.
//
// When progress is non-nil it is called with the cumulative bytes streamed and
// the total from Content-Length (0 when the length is unknown).
func (c *Client) GetRaw(ctx context.Context, path string, query url.Values, dst io.Writer, progress func(read, total int64)) error {
	endpoint, err := c.resolve(path, query)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", authScheme+" "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("api: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}

	src := io.Reader(resp.Body)
	if progress != nil {
		src = &countingReader{r: resp.Body, total: max(resp.ContentLength, 0), onChange: progress}
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("api: reading response body: %w", err)
	}
	return nil
}

// countingReader reports the cumulative bytes read to onChange after every
// read.
type countingReader struct {
	r        io.Reader
	total    int64
	read     int64
	onChange func(read, total int64)
}

func (cr *countingReader) Read(b []byte) (int, error) {
	n, err := cr.r.Read(b)
	if n > 0 {
		cr.read += int64(n)
		cr.onChange(cr.read, cr.total)
	}
	return n, err
}

// postReader issues a POST with a streaming request body of the given content
// type, decoding a JSON response into out. It is used for uploads where the
// body is produced incrementally rather than marshaled from a value.
func (c *Client) postReader(ctx context.Context, path, contentType string, body io.Reader, out any) error {
	endpoint, err := c.resolve(path, nil)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", contentType)
	if c.token != "" {
		req.Header.Set("Authorization", authScheme+" "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("api: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("api: decoding response: %w", err)
	}
	return nil
}

// do performs a single request/response cycle: it resolves the URL, encodes
// body as JSON when present, sets headers, and either decodes a 2xx response
// into out or returns an *APIError for non-2xx responses.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	endpoint, err := c.resolve(path, query)
	if err != nil {
		return err
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("api: encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("api: building request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", authScheme+" "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Surface context cancellation/timeout directly for clean handling.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("api: %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		// Drain so the connection can be reused, then discard.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			// Empty body with a 2xx status is not an error.
			return nil
		}
		return fmt.Errorf("api: decoding response: %w", err)
	}
	return nil
}

// resolve joins path (and optional query) onto the client's base URL, ensuring
// the API prefix is present exactly once.
func (c *Client) resolve(path string, query url.Values) (string, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("api: invalid request path %q: %w", path, err)
	}

	full := *c.baseURL
	full.Path = joinPath(c.baseURL.Path, ref.Path)

	// Merge any query encoded in path with the explicit query argument.
	q := full.Query()
	for k, vs := range ref.Query() {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	full.RawQuery = q.Encode()

	return full.String(), nil
}

// joinPath joins the base path and request path, inserting the API prefix when
// the result does not already include it, and collapsing duplicate slashes.
func joinPath(base, path string) string {
	segments := append(splitPath(base), splitPath(apiPrefix)...)
	segments = append(segments, splitPath(path)...)

	out := make([]string, 0, len(segments))
	for _, s := range segments {
		// Skip a second "api" segment if the base path already supplied one.
		if s == "api" && len(out) > 0 && out[len(out)-1] == "api" {
			continue
		}
		out = append(out, s)
	}
	return "/" + strings.Join(out, "/")
}

func splitPath(p string) []string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
