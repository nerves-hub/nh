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

// Package mix auto-detects details of an Elixir/Nerves project in the working
// directory: the NervesCloud organization and product, by reading the Mix
// project configuration via `mix eval`, and the built firmware image, by
// globbing the standard Nerves build layout.
package mix

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Mix expressions that print a single project config value.
const (
	orgExpr  = `IO.puts(Mix.Project.config()[:org])`
	nameExpr = `IO.puts(Mix.Project.config()[:name])`
	appExpr  = `IO.puts(Mix.Project.config()[:app])`
)

// evalTimeout bounds each `mix eval`. Compiling a project can be slow, but a
// hang should not block the CLI indefinitely. It is intentionally not
// configurable.
const evalTimeout = 30 * time.Second

// abandoned is set once a `mix eval` times out, so the remaining lookups in
// this process are skipped rather than each hanging for evalTimeout.
var abandoned bool

// Available reports whether the working directory is a Mix project (it has a
// mix.exs file). It is a variable so tests can override it.
var Available = func() bool {
	info, err := os.Stat("mix.exs")
	return err == nil && !info.IsDir()
}

// execMix runs the `mix eval` command bound to ctx. It is a variable so tests
// can simulate slow or failing runs.
var execMix = func(ctx context.Context, expr string) ([]byte, error) {
	return exec.CommandContext(ctx, "mix", "eval", expr).Output()
}

// Eval runs `mix eval <expr>` in the working directory and returns the last
// non-empty line of standard output, or "" on any error or timeout. Only the
// last line is used because compilation output can precede the evaluated value.
// It is a variable so tests can override it.
var Eval = func(expr string) string {
	return evalWithTimeout(expr, evalTimeout)
}

func evalWithTimeout(expr string, timeout time.Duration) string {
	if abandoned {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := execMix(ctx, expr)
	if ctx.Err() == context.DeadlineExceeded {
		// Report once, then stop trying for the rest of the process.
		if !abandoned {
			abandoned = true
			fmt.Fprintf(os.Stderr,
				"nh: reading the org and product from mix.exs timed out after %s; pass --org/--product (or set NERVES_HUB_ORG/NERVES_HUB_PRODUCT) to skip auto-detection\n",
				timeout)
		}
		return ""
	}
	if err != nil {
		return ""
	}
	return lastLine(out)
}

// Org returns the Mix project's configured :org, or "" when not in a Mix
// project or the value is unset.
func Org() string {
	if !Available() {
		return ""
	}
	return Eval(orgExpr)
}

// Product returns the Mix project's configured :name, falling back to :app, or
// "" when not in a Mix project or neither is set.
func Product() string {
	if !Available() {
		return ""
	}
	if name := Eval(nameExpr); name != "" {
		return name
	}
	return Eval(appExpr)
}

// lastLine returns the last non-empty, trimmed line of b.
func lastLine(b []byte) string {
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
