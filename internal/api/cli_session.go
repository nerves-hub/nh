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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// CLISessionStatus is returned when polling a CLI session. Status is "waiting"
// until the user enters the code in the browser and confirms, then "ready"
// with UserToken set. Newer servers answer "collected" once the token has
// been handed out.
type CLISessionStatus struct {
	Status    string `json:"status"`
	UserToken string `json:"user_token"`
}

// CLISessionError is the error CheckCLISession returns for a non-2xx
// response. It wraps the *APIError (so errors.As(err, &apiErr) still works,
// and RetryAfter comes from it) and adds what polling needs to decide whether
// to try again.
type CLISessionError struct {
	*APIError
}

// Unwrap returns the underlying *APIError.
func (e *CLISessionError) Unwrap() error { return e.APIError }

// Throttled reports whether the server refused the poll for coming too often.
//
// NervesHub rate limits the poll endpoint per IP address with PlugAttack
// (NervesHubWeb.Plugs.Attack, the general 30-per-minute ip_throttle). PlugAttack
// blocks with a plain-text "403 Forbidden" rather than a 429, so a 403 whose
// body isn't the server's JSON error envelope counts as throttling too. A 429
// is what a proxy in front of the server, or a later server, would send.
func (e *CLISessionError) Throttled() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return !json.Valid(e.Body)
	}
	return false
}

// Expired reports whether the server no longer knows the session: it lasts
// five minutes, and is gone once its token has been collected.
func (e *CLISessionError) Expired() bool {
	return e.StatusCode == http.StatusNotFound
}

// CheckCLISession polls the status of a CLI session via
// GET /auth/cli_session/{token}. It is unauthenticated. A non-2xx response is
// returned as a *CLISessionError.
func (c *Client) CheckCLISession(ctx context.Context, token string) (*CLISessionStatus, error) {
	endpoint, err := c.resolve("/auth/cli_session/"+url.PathEscape(token), nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("api: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr *APIError
		if !errors.As(parseError(resp), &apiErr) {
			return nil, fmt.Errorf("api: GET %s: unexpected status %s", endpoint, resp.Status)
		}
		return nil, &CLISessionError{APIError: apiErr}
	}

	var out struct {
		Data CLISessionStatus `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("api: decoding response: %w", err)
	}
	return &out.Data, nil
}

// CLIPollOptions tunes WaitForCLISession. Zero fields take the defaults below.
type CLIPollOptions struct {
	// Interval is the wait between polls while the session is waiting.
	// Default 3s: the server allows 30 requests a minute from one address
	// across all its API endpoints, so polling every 3s leaves a third of
	// that for anything else running on the same address.
	Interval time.Duration
	// MinBackoff is the first wait after a throttled or failed poll when the
	// server gives no Retry-After. It doubles on each retry. Default 5s.
	MinBackoff time.Duration
	// MaxBackoff caps the wait after a throttled or failed poll, including
	// one the server asks for. Default 60s, the server's throttle period.
	MaxBackoff time.Duration
	// MaxTransientErrors is how many network errors or 5xx responses in a row
	// are retried before giving up; any other answer, throttling included,
	// ends the run. Default 3.
	MaxTransientErrors int
}

func (o CLIPollOptions) withDefaults() CLIPollOptions {
	if o.Interval <= 0 {
		o.Interval = 3 * time.Second
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = 5 * time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 60 * time.Second
	}
	if o.MaxBackoff < o.MinBackoff {
		o.MaxBackoff = o.MinBackoff
	}
	if o.MaxTransientErrors <= 0 {
		o.MaxTransientErrors = 3
	}
	return o
}

// ErrCLISessionExpired is returned by WaitForCLISession when the server no
// longer knows the session, or ctx's deadline passes before it is confirmed.
var ErrCLISessionExpired = errors.New("login expired")

// WaitForCLISession polls the session until the user confirms it, returning
// the issued user token. It stops when ctx is done.
//
// A throttled poll waits (for Retry-After when the server sends one, else an
// increasing backoff) and keeps going: being throttled says nothing about the
// login. Network errors and 5xx responses are retried the same way, up to
// MaxTransientErrors in a row. A 404 means the session expired or was never
// known, and returns an error wrapping ErrCLISessionExpired, as does ctx's
// deadline passing.
func (c *Client) WaitForCLISession(ctx context.Context, token string, opts CLIPollOptions) (string, error) {
	opts = opts.withDefaults()
	backoff := opts.MinBackoff
	transient := 0
	// lastErr is the most recent refused or failed poll, cleared by a good
	// one, so that running out of time after it says why.
	var lastErr error

	for {
		wait := opts.Interval
		status, err := c.CheckCLISession(ctx, token)
		switch {
		case err == nil:
			backoff, transient, lastErr = opts.MinBackoff, 0, nil
			switch status.Status {
			case "ready":
				if status.UserToken == "" {
					return "", errors.New("login confirmed but no token was returned")
				}
				return status.UserToken, nil
			case "collected":
				// Newer servers hand the token out once and answer later polls
				// with "collected" (nerves-hub/nerves_hub_web#3336). The answer
				// that carried it never arrived, so this login can't finish.
				return "", fmt.Errorf("%w: the server already handed out this login's token, but nh never received it", ErrCLISessionExpired)
			}

		case ctx.Err() != nil:
			return "", ctxDoneError(ctx, lastErr)

		default:
			var sessErr *CLISessionError
			isSessErr := errors.As(err, &sessErr)
			switch {
			case isSessErr && sessErr.Expired():
				// The server answers a session's "ready" once; a poll after it
				// lands here too, so this can't say the code wasn't entered.
				return "", fmt.Errorf("%w: the server is no longer waiting for this login (logins last five minutes)", ErrCLISessionExpired)
			case isSessErr && sessErr.Throttled():
				// The server is up and the login may still be confirmed, so
				// this neither counts as nor follows on from a failure.
				transient = 0
			case !isSessErr || sessErr.StatusCode >= 500:
				transient++
				if transient > opts.MaxTransientErrors {
					return "", err
				}
			default:
				return "", err
			}
			lastErr = err
			wait = backoff
			if isSessErr && sessErr.RetryAfter > 0 {
				wait = sessErr.RetryAfter
			}
			wait = min(wait, opts.MaxBackoff)
			backoff = min(backoff*2, opts.MaxBackoff)
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctxDoneError(ctx, lastErr)
		case <-timer.C:
		}
	}
}

// ctxDoneError reports why polling stopped: the deadline passing means the
// login expired, anything else (such as the user cancelling) is passed on.
// When the polls just before the deadline were refused or failed, nh can't
// tell whether the code was entered, so it says what went wrong instead.
func ctxDoneError(ctx context.Context, lastErr error) error {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if lastErr != nil {
		return fmt.Errorf("%w: the server kept refusing to report on the login (last error: %v)", ErrCLISessionExpired, lastErr)
	}
	return fmt.Errorf("%w: the code wasn't entered in time", ErrCLISessionExpired)
}
