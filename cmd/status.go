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
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

// statusCmd implements `nh status`: check that the configured server answers
// its health check.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check that the NervesHub server is up",
	Long: `Check that the configured NervesHub server is up, via its unauthenticated
health check (GET /status/alive).

Prints the URI nh is configured to use and whether the server answered. Exits
non-zero when the server can't be reached, or answers but reports a problem
(such as being unable to reach its database). Useful for checking a
self-hosted ` + "`uri`" + ` setting before signing in. No token is needed or sent.`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().Duration("timeout", 10*time.Second, "how long to wait for the server to answer")
	rootCmd.AddCommand(statusCmd)
}

// statusReport is the `nh status -o json` output.
type statusReport struct {
	URI        string `json:"uri"`
	URL        string `json:"url"`
	Reachable  bool   `json:"reachable"`
	Healthy    bool   `json:"healthy"`
	StatusCode int    `json:"status_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

func runStatus(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())

	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		return errors.New("--timeout must be positive")
	}

	client, err := api.NewClient(cfg.URI, "", api.WithUserAgent(userAgent()), api.WithHTTPClient(&http.Client{Timeout: timeout}))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	report := statusReport{URI: cfg.URI, URL: client.StatusURL()}
	status, statusErr := client.Status(ctx)
	if statusErr != nil {
		report.Error = statusErr.Error()
	} else {
		report.Reachable = true
		report.Healthy = status.Healthy()
		report.StatusCode = status.StatusCode
		report.Message = status.Message
	}

	w := cmd.OutOrStdout()
	if cfg.Output == config.OutputJSON {
		if err := printJSON(w, report); err != nil {
			return err
		}
	} else {
		tw := newTableWriter(w)
		fmt.Fprintf(tw, "URI:\t%s\n", cfg.URI)
		fmt.Fprintf(tw, "Status:\t%s\n", statusSummary(report, status))
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	switch {
	case statusErr != nil:
		return fmt.Errorf("server unreachable: %w", statusErr)
	case status.DatabaseDown():
		return fmt.Errorf("server at %s is up but can't reach its database (500 from %s)", cfg.URI, status.URL)
	case !report.Healthy:
		return fmt.Errorf("%s answered %d %s, not NervesHub's health check: check the uri setting", status.URL, status.StatusCode, http.StatusText(status.StatusCode))
	}
	return nil
}

// statusSummary describes a health check result in a few words. status is nil
// when the server was unreachable.
func statusSummary(r statusReport, status *api.ServerStatus) string {
	switch {
	case status == nil:
		return "unreachable"
	case r.Healthy:
		return "up"
	case status.DatabaseDown():
		return "up, but its database is unreachable"
	default:
		return fmt.Sprintf("unexpected answer (%d %s)", r.StatusCode, http.StatusText(r.StatusCode))
	}
}
