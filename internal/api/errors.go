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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// APIError represents a non-2xx response from the API. Callers can inspect
// StatusCode (or use errors.As) to branch on conditions such as 401/404.
type APIError struct {
	// StatusCode is the HTTP status code (e.g. 404).
	StatusCode int
	// Status is the HTTP status text (e.g. "404 Not Found").
	Status string
	// Message is a human-readable summary derived from the response body.
	Message string
	// FieldErrors holds per-field validation messages when the API returns
	// them, keyed by field path. Errors on nested changesets (embeds and
	// associations) are flattened into dotted paths, with list positions in
	// brackets: "conditions.version", "devices[2].identifier".
	FieldErrors map[string][]string
	// RetryAfter is how long the server asked the caller to wait before
	// trying again, from the response's Retry-After header (either
	// delay-seconds or an HTTP date). It is zero when the header is absent,
	// unparseable or already in the past. NervesHub sends it with the 503 it
	// answers during maintenance; a proxy in front of it may send it with a
	// 429.
	RetryAfter time.Duration
	// Body is the raw (possibly truncated) response body, for diagnostics.
	Body []byte
}

// Error implements the error interface.
func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "api: %d", e.StatusCode)
	if msg != "" {
		fmt.Fprintf(&b, ": %s", msg)
	}
	if e.RetryAfter > 0 {
		fmt.Fprintf(&b, " (retry after %s)", formatRetryAfter(e.RetryAfter))
	}

	// Append field errors in a stable order for readable, testable output.
	if len(e.FieldErrors) > 0 {
		fields := make([]string, 0, len(e.FieldErrors))
		for f := range e.FieldErrors {
			fields = append(fields, f)
		}
		sort.Slice(fields, func(i, j int) bool { return naturalLess(fields[i], fields[j]) })
		for _, f := range fields {
			fmt.Fprintf(&b, "\n  %s: %s", f, strings.Join(e.FieldErrors[f], ", "))
		}
	}
	return b.String()
}

// errorEnvelope models the shapes NervesHub uses for error bodies. Both
// "errors" and "error" appear in the wild, and "errors" may be either a
// detail string map or a field -> messages map.
type errorEnvelope struct {
	Status string          `json:"status"`
	Detail string          `json:"detail"`
	Error  string          `json:"error"`
	Errors json.RawMessage `json:"errors"`
}

// retryAfterNow is the clock Retry-After dates are measured against; tests
// replace it.
var retryAfterNow = time.Now

// parseError reads resp's body and constructs an *APIError, best-effort
// decoding the JSON error envelope.
func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), retryAfterNow()),
		Body:       body,
	}

	var env errorEnvelope
	if len(body) > 0 && json.Unmarshal(body, &env) == nil {
		apiErr.Message = firstNonEmpty(env.Detail, env.Error, env.Status)
		apiErr.FieldErrors, apiErr.Message = decodeErrors(env.Errors, apiErr.Message)
	}

	if apiErr.Message == "" && !isHTML(resp.Header.Get("Content-Type"), body) {
		// Fall back to the trimmed raw body, then the status text. An HTML
		// page, such as a proxy's error page, is left for the status text:
		// printed whole it would bury the one line that matters.
		if s := strings.TrimSpace(string(body)); s != "" && !json.Valid(body) {
			apiErr.Message = s
		}
	}
	return apiErr
}

// decodeErrors interprets the polymorphic "errors" field. It returns any
// per-field messages plus a possibly-updated top-level message (when the
// envelope carried only a detail string under errors).
//
// NervesHub renders errors in two shapes (NervesHubWeb.API.ErrorJSON and
// NervesHubWeb.API.ChangesetJSON):
//
//	{"errors": {"detail": "..."}}
//	{"errors": <Ecto.Changeset.traverse_errors/2 output>}
//
// traverse_errors gives each field a list of messages, but a field holding an
// embed or association gives a nested map (one-to-one) or a list of maps, one
// per entry with "{}" for the valid ones (one-to-many). Those are flattened
// into dotted paths so no message is lost.
func decodeErrors(raw json.RawMessage, message string) (map[string][]string, string) {
	if len(raw) == 0 {
		return nil, message
	}

	fields := make(map[string][]string)
	switch firstByte(raw) {
	case '{':
		var asMap map[string]json.RawMessage
		if json.Unmarshal(raw, &asMap) != nil {
			return nil, message
		}
		for k, v := range asMap {
			if k == "detail" && message == "" {
				if msgs, ok := stringList(v); ok && len(msgs) == 1 {
					message = msgs[0]
					continue
				}
			}
			flattenErrors(k, v, fields)
		}
	case '"':
		// {"errors": "some message"}.
		var s string
		if json.Unmarshal(raw, &s) == nil && message == "" {
			message = s
		}
	case '[':
		// {"errors": ["some message", ...]}: not a NervesHub shape, but
		// cheap to read rather than drop.
		// Strings become the message; anything else nests by position.
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil {
			return nil, message
		}
		var msgs []string
		for i, v := range list {
			if firstByte(v) == '"' {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					msgs = append(msgs, s)
				}
				continue
			}
			flattenErrors("["+strconv.Itoa(i)+"]", v, fields)
		}
		if message == "" {
			message = strings.Join(msgs, "; ")
		}
	}

	if len(fields) == 0 {
		return nil, message
	}
	return fields, message
}

// flattenErrors walks one value of a traverse_errors map, adding its
// messages to out under path. Strings are messages; objects nest by key
// ("a.b"); lists of strings are messages, and anything else in a list nests
// by position ("a[0]"). Null and empty values add nothing.
func flattenErrors(path string, raw json.RawMessage, out map[string][]string) {
	switch firstByte(raw) {
	case '{':
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		// Sorted, so two keys that flatten to one path ("a.b" and a
		// nested "a" -> "b") list their messages in a stable order.
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			flattenErrors(joinFieldPath(path, k), m[k], out)
		}
	case '[':
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil {
			return
		}
		for i, v := range list {
			if firstByte(v) == '"' {
				flattenErrors(path, v, out)
				continue
			}
			flattenErrors(path+"["+strconv.Itoa(i)+"]", v, out)
		}
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			out[path] = append(out[path], s)
		}
	case 'n', 0:
		// null, or nothing at all.
	default:
		// A number or boolean: not something NervesHub sends, but its text
		// says more than dropping it would.
		out[path] = append(out[path], string(raw))
	}
}

// joinFieldPath appends key to a dotted field path.
func joinFieldPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// stringList decodes raw as a string or a list of strings.
func stringList(raw json.RawMessage) ([]string, bool) {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}, true
	}
	return nil, false
}

// firstByte returns the first non-space byte of a JSON value, or 0.
func firstByte(raw json.RawMessage) byte {
	for _, c := range raw {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return c
	}
	return 0
}

// naturalLess orders field paths with list positions compared as numbers,
// so "devices[2]" sorts before "devices[10]".
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ca, cb := a[0], b[0]
		if isDigit(ca) && isDigit(cb) {
			na, ra := leadingDigits(a)
			nb, rb := leadingDigits(b)
			// Compare by length first (ignoring leading zeros), then value.
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(ta) != len(tb) {
				return len(ta) < len(tb)
			}
			if ta != tb {
				return ta < tb
			}
			a, b = ra, rb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func leadingDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

// parseRetryAfter reads a Retry-After header value, which RFC 9110 allows
// as either a number of seconds or an HTTP date. It returns zero when the
// value is empty, unparseable, or not in the future.
func parseRetryAfter(v string, at time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	secs, err := strconv.ParseInt(v, 10, 64)
	if err == nil || errors.Is(err, strconv.ErrRange) {
		// Out of range still parses as a number: ParseInt saturates it.
		if secs <= 0 {
			return 0
		}
		if secs > int64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64).Truncate(time.Second)
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(at); d > 0 {
			// HTTP dates have whole seconds; round up so waiting the
			// reported time is always long enough.
			return (d + time.Second - 1).Truncate(time.Second)
		}
	}
	return 0
}

// formatRetryAfter renders d without zero trailing units: "30s", "5m",
// "1h30m".
func formatRetryAfter(d time.Duration) string {
	s := d.Round(time.Second).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// isHTML reports whether a response is an HTML document, by its
// Content-Type or, for proxies that leave that out or get it wrong, by how
// its body starts.
func isHTML(contentType string, body []byte) bool {
	if mt, _, err := mime.ParseMediaType(contentType); err == nil && (mt == "text/html" || mt == "application/xhtml+xml") {
		return true
	}
	start := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	return strings.HasPrefix(start, "<!doctype html") || strings.HasPrefix(start, "<html")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
