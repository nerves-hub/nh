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

package firmware

import (
	"regexp"
	"strings"
)

// semver matches what Elixir's Version.parse/1 accepts: strict
// major.minor.patch with optional pre-release and build parts.
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

var allDigits = regexp.MustCompile(`^\d+$`)

func stripVersionPrefix(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimLeft(s, "v")
	return strings.TrimLeft(s, "V")
}

// normaliseAtomVMVersion mirrors AtomVM.normalise_version/1: a leading v and a
// two part version are accepted, nothing else is coerced.
func normaliseAtomVMVersion(raw string) (string, bool) {
	s := stripVersionPrefix(raw)
	if semver.MatchString(s) {
		return s, true
	}
	if parts := strings.Split(s, "."); len(parts) == 2 && semver.MatchString(s+".0") {
		return s + ".0", true
	}
	return "", false
}

// normaliseESPIDFVersion mirrors EspIdf.normalise_version/1: as AtomVM, plus a
// one part "1" and ESP's four part "1.2.3.4", read as "1.2.3+4".
func normaliseESPIDFVersion(raw string) (string, bool) {
	s := stripVersionPrefix(raw)
	candidate := s
	if !semver.MatchString(s) {
		parts := strings.Split(s, ".")
		numeric := true
		for _, p := range parts {
			if !allDigits.MatchString(p) {
				numeric = false
			}
		}
		if numeric {
			switch len(parts) {
			case 1:
				candidate = parts[0] + ".0.0"
			case 2:
				candidate = parts[0] + "." + parts[1] + ".0"
			case 4:
				candidate = parts[0] + "." + parts[1] + "." + parts[2] + "+" + parts[3]
			}
		}
	}
	if semver.MatchString(candidate) {
		return candidate, true
	}
	return "", false
}
