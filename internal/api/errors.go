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
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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
	// them, keyed by field name.
	FieldErrors map[string][]string
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

	// Append field errors in a stable order for readable, testable output.
	if len(e.FieldErrors) > 0 {
		fields := make([]string, 0, len(e.FieldErrors))
		for f := range e.FieldErrors {
			fields = append(fields, f)
		}
		sort.Strings(fields)
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

// parseError reads resp's body and constructs an *APIError, best-effort
// decoding the JSON error envelope.
func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       body,
	}

	var env errorEnvelope
	if len(body) > 0 && json.Unmarshal(body, &env) == nil {
		apiErr.Message = firstNonEmpty(env.Detail, env.Error, env.Status)
		apiErr.FieldErrors, apiErr.Message = decodeErrors(env.Errors, apiErr.Message)
	}

	if apiErr.Message == "" {
		// Fall back to the trimmed raw body, then the status text.
		if s := strings.TrimSpace(string(body)); s != "" && !json.Valid(body) {
			apiErr.Message = s
		}
	}
	return apiErr
}

// decodeErrors interprets the polymorphic "errors" field. It returns any
// per-field messages plus a possibly-updated top-level message (when the
// envelope carried only a detail string under errors).
func decodeErrors(raw json.RawMessage, message string) (map[string][]string, string) {
	if len(raw) == 0 {
		return nil, message
	}

	// Shape 1: {"errors": {"detail": "..."}} or {"errors": {"field": [..]}}.
	var asMap map[string]json.RawMessage
	if json.Unmarshal(raw, &asMap) == nil {
		fields := make(map[string][]string)
		for k, v := range asMap {
			msgs := decodeMessages(v)
			if k == "detail" && message == "" && len(msgs) == 1 {
				message = msgs[0]
				continue
			}
			if len(msgs) > 0 {
				fields[k] = msgs
			}
		}
		if len(fields) == 0 {
			return nil, message
		}
		return fields, message
	}

	// Shape 2: {"errors": "some message"}.
	var asString string
	if json.Unmarshal(raw, &asString) == nil && message == "" {
		message = asString
	}
	return nil, message
}

// decodeMessages coerces a value that may be a string or a []string into a
// slice of strings.
func decodeMessages(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return []string{s}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
