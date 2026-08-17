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
	"fmt"
	"strings"
	"time"
)

// Timestamp wraps time.Time to tolerate the several timestamp formats the
// NervesCloud API emits. Across endpoints these vary in separator ("T" or a
// space, e.g. "2024-05-21T11:06:42" vs "2026-06-03 03:37:01.833917Z"),
// fractional seconds, and whether a timezone is present. Zoneless values are
// interpreted as UTC. It marshals back out as RFC3339 via the embedded
// time.Time.
type Timestamp struct {
	time.Time
}

// timestampLayouts are tried in order when decoding. The ".999999999" makes
// fractional seconds optional; the zoneless layouts parse as UTC.
var timestampLayouts = []string{
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
}

// UnmarshalJSON decodes a JSON string into a Timestamp, accepting the formats
// in timestampLayouts. Null, empty, and the "never" sentinel (used for devices
// that have never communicated) yield the zero time.
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	switch strings.ToLower(s) {
	case "", "null", "never":
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range timestampLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("api: cannot parse timestamp %q", s)
}
