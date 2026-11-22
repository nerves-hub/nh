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

// Package tokennote builds the label nh records with the API tokens it is
// issued.
package tokennote

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLength is the most characters (Unicode code points) the server accepts
// in a token note. POST /auth/cli_session refuses a longer note, and
// POST /users/auth stores it in a 255-character column. See
// NervesHub.Accounts.generate_cli_session_token/2.
const MaxLength = 255

// Build returns the note for a token issued to nh: "nh <version> (<host>)".
//
// The server refuses a CLI-login note holding control or format characters
// (Unicode categories Cc and Cf, such as newlines, zero-width joiners and
// direction overrides), since it shows the note to whoever confirms the login.
// A host name can hold any of these (a macOS computer name is free text), so
// they are dropped, as is invalid UTF-8, and runs of whitespace are collapsed.
// The host is shortened as needed to keep the note within MaxLength.
func Build(version, host string) string {
	version = Sanitize(version)
	host = Sanitize(host)
	if host == "" {
		host = "unknown"
	}

	note := fmt.Sprintf("nh %s (%s)", version, host)
	if over := utf8.RuneCountInString(note) - MaxLength; over > 0 {
		hostRunes := []rune(host)
		if over < len(hostRunes) {
			note = fmt.Sprintf("nh %s (%s)", version, strings.TrimSpace(string(hostRunes[:len(hostRunes)-over])))
		}
	}
	return truncate(note, MaxLength)
}

// Sanitize removes characters the server refuses in a note: invalid UTF-8,
// and control and format characters. Whitespace, including newlines and tabs,
// becomes single spaces, and leading and trailing space is trimmed.
func Sanitize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case unicode.IsSpace(r):
			space = true
			continue
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// truncate shortens s to at most n code points.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
