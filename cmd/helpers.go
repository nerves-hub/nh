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

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/mix"
	"github.com/spf13/cobra"
)

// tokenNote is the human-readable label recorded with an issued API token: the
// app, its version, and the local hostname (e.g. "nh 1.2.3 (my-host)").
func tokenNote() string {
	return fmt.Sprintf("nh %s (%s)", version, hostname())
}

// exactlyOneArg returns a cobra Args validator requiring exactly one argument,
// with friendly messages: missing when none is given, tooMany when more than
// one is.
func exactlyOneArg(missing, tooMany string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		switch {
		case len(args) == 0:
			return errors.New(missing)
		case len(args) > 1:
			return errors.New(tooMany)
		}
		return nil
	}
}

// exactlyTwoArgs returns a cobra Args validator requiring exactly two
// arguments, with usage shown otherwise.
func exactlyTwoArgs(usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 2 {
			return errors.New(usage)
		}
		return nil
	}
}

// atMostOneArg returns a cobra Args validator allowing zero or one argument,
// with a friendly message when more than one is given.
func atMostOneArg(tooMany string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > 1 {
			return errors.New(tooMany)
		}
		return nil
	}
}

// version is the nh build version. It is overridden at build time via
// -ldflags "-X github.com/nerves-hub/nh/cmd.version=...".
var version = "dev"

// userAgent is the User-Agent string sent with every API request.
func userAgent() string {
	return "nh/" + version
}

// newAuthedClient builds an API client using the resolved token, returning a
// helpful error when no token is available so commands fail clearly rather
// than with a 401. Extra options (e.g. a custom HTTP client) may be supplied.
func newAuthedClient(cfg *config.Config, opts ...api.Option) (*api.Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("not authenticated: run `nh user auth` or set NERVES_HUB_TOKEN")
	}
	opts = append([]api.Option{api.WithUserAgent(userAgent())}, opts...)
	return api.NewClient(cfg.URI, cfg.Token, opts...)
}

// resolveOrg returns the organization scope: the value set via flag,
// environment, or saved settings; falling back to the org auto-detected from a
// Mix project in the working directory.
func resolveOrg(cfg *config.Config) string {
	if cfg.Org != "" {
		return cfg.Org
	}
	return mix.Org()
}

// resolveProduct returns the product scope: the value set via flag,
// environment, or saved settings; falling back to the product auto-detected
// from a Mix project in the working directory.
func resolveProduct(cfg *config.Config) string {
	if cfg.Product != "" {
		return cfg.Product
	}
	return mix.Product()
}

// requireOrg returns the resolved organization scope, or a helpful error when
// none is set via flag, environment, saved settings, or a Mix project.
func requireOrg(cfg *config.Config) (string, error) {
	if org := resolveOrg(cfg); org != "" {
		return org, nil
	}
	return "", errors.New("no organization set: pass --org, set NERVES_HUB_ORG, or run `nh config set org <name>`")
}

// requireProduct returns the resolved product scope, or a helpful error when
// none is set via flag, environment, saved settings, or a Mix project.
func requireProduct(cfg *config.Config) (string, error) {
	if product := resolveProduct(cfg); product != "" {
		return product, nil
	}
	return "", errors.New("no product set: pass --product, set NERVES_HUB_PRODUCT, or run `nh config set product <name>`")
}

// printJSON writes v to w as indented JSON.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// newTableWriter returns a tabwriter configured for nh's tab-separated,
// space-padded list output. Callers write tab-delimited rows and Flush.
func newTableWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
}
