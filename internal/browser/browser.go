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

// Package browser opens URLs in the user's web browser using the tools the
// operating system already provides.
package browser

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"
)

// ErrNoDisplay is returned by Open when there is no display to open a
// browser on.
var ErrNoDisplay = errors.New("no display available")

// ErrNotWebURL is returned by Open for anything but an absolute http or https
// URL. The openers also open files, apps and other URL schemes, so they are
// never handed anything else.
var ErrNotWebURL = errors.New("not an http or https URL")

// IsWebURL reports whether s is an absolute http or https URL with a host and
// no control characters, which is all Open will open.
func IsWebURL(s string) bool {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// CanOpen reports whether a browser could plausibly be opened here: there is
// a graphical session, and nh isn't running over SSH (where a browser would
// open on the remote machine, if anywhere).
func CanOpen() bool {
	return canOpen(runtime.GOOS, os.Getenv)
}

func canOpen(goos string, getenv func(string) string) bool {
	switch goos {
	case "windows":
		return true
	case "darwin":
		return getenv("SSH_CONNECTION") == "" && getenv("SSH_TTY") == ""
	default:
		// Linux and the BSDs: a browser needs an X11 or Wayland display. An SSH
		// session with X forwarding sets DISPLAY, and a browser opened there
		// does show up locally.
		return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
	}
}

// command returns the program and arguments that open url on goos.
func command(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		// Not "cmd /c start", which would read & and ^ in the URL as shell
		// syntax.
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// Open opens url in the default browser. It returns once the opener has
// started, without waiting for the browser, and discards the opener's output
// so it doesn't interleave with nh's.
func Open(url string) error {
	if !IsWebURL(url) {
		return ErrNotWebURL
	}
	if !CanOpen() {
		return ErrNoDisplay
	}
	name, args := command(runtime.GOOS, url)
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
