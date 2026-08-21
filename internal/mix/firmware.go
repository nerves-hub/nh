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

package mix

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FirmwareGlob returns the pattern a Nerves project builds its firmware image
// to, derived from MIX_TARGET and MIX_ENV (the same defaults mix itself uses).
func FirmwareGlob() string {
	return filepath.Join("_build", mixTarget()+"_"+mixEnv(), "nerves", "images", "*.fw")
}

// FirmwarePath locates the firmware image built by the Nerves project in the
// working directory, for when no path was given on the command line.
//
// Detection is a glob over the standard Nerves build layout rather than a `mix
// eval`, so it costs nothing and cannot trigger a compile. Every failure names
// what was looked for, because the fix is always to pass the path explicitly.
func FirmwarePath() (string, error) {
	// MIX_TARGET=host builds a host binary, not a .fw, so there is nothing to
	// find and the glob below would be misleading.
	if target := mixTarget(); target == "host" {
		return "", fmt.Errorf("cannot detect firmware: MIX_TARGET is %q, which does not build a firmware image; set MIX_TARGET or pass the path", target)
	}

	pattern := FirmwareGlob()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		// Only returned for a malformed pattern, which would be a bug here.
		return "", fmt.Errorf("cannot detect firmware: %w", err)
	}
	sort.Strings(matches)

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no firmware found at %s; build it first, or pass the path", pattern)
	default:
		return "", fmt.Errorf("found %d firmware images at %s, so the one to upload is ambiguous; pass the path:\n  %s",
			len(matches), pattern, strings.Join(matches, "\n  "))
	}
}

func mixEnv() string    { return envOr("MIX_ENV", "dev") }
func mixTarget() string { return envOr("MIX_TARGET", "host") }

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
