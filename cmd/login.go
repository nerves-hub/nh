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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// errLoginCancelled is the cancellation cause when the user aborts login.
var errLoginCancelled = errors.New("login cancelled")

// spaceDigits renders a numeric code with a space between each digit, e.g.
// 438231 -> "4 3 8 2 3 1", to make it easier to read aloud and compare.
func spaceDigits(code int) string {
	return strings.Join(strings.Split(strconv.Itoa(code), ""), " ")
}

// CLI login polling cadence and overall deadline. They are package vars so
// tests can shorten them.
var (
	cliPollInterval = 2 * time.Second
	cliPollTimeout  = 5 * time.Minute
)

// userLoginCmd implements `nh user login`: a browser-confirmation login that
// avoids entering a password.
var userLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate by confirming in your browser",
	Long: `Authenticate without entering your password.

nh starts a login session and prints a URL. Open it in your browser and
confirm the login; nh waits for the confirmation and then saves your API
token to the settings file (default ~/.nh/settings.json).`,
	Args: cobra.NoArgs,
	RunE: runUserLogin,
}

func init() {
	userCmd.AddCommand(userLoginCmd)
}

func runUserLogin(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())

	// The CLI-session endpoints are unauthenticated.
	client, err := api.NewClient(cfg.URI, "", api.WithUserAgent(userAgent()))
	if err != nil {
		return err
	}

	session, err := client.StartCLISession(cmd.Context(), tokenNote())
	if err != nil {
		return err
	}
	if session.URL == "" || session.Token == "" {
		return errors.New("login session response was incomplete")
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "To authenticate, open the following URL in your browser and confirm the login:\n\n    %s\n", session.URL)
	if session.ConfirmationCode != 0 {
		fmt.Fprintf(w, "\n    Confirmation code: %s\n", spaceDigits(session.ConfirmationCode))
		fmt.Fprintln(w, "\n    Check that this code matches the one shown in your browser.")
	}
	hint := ""
	if stdinIsTerminal(cmd) {
		hint = " (press Esc to cancel)"
	}
	fmt.Fprintf(w, "\nWaiting for confirmation...%s\n", hint)

	deadlineCtx, cancelTimeout := context.WithTimeout(cmd.Context(), cliPollTimeout)
	defer cancelTimeout()
	ctx, cancel := context.WithCancelCause(deadlineCtx)
	defer cancel(nil)

	// Let the user abort by pressing Esc (or Ctrl-C) while we wait.
	restore := watchForCancel(cmd, cancel)

	userToken, err := pollCLISession(ctx, client, session.Token)
	restore() // restore the terminal before printing anything else

	if err != nil {
		if errors.Is(context.Cause(ctx), errLoginCancelled) {
			fmt.Fprintln(w, "\nLogin cancelled.")
			return nil
		}
		return err
	}

	if err := config.SaveToken(cfg.DataDir, userToken); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nSuccessfully authenticated\nToken saved to %s\n", config.SettingsFilePath(cfg.DataDir))
	return nil
}

// pollCLISession polls the session until it is ready, returning the user token.
func pollCLISession(ctx context.Context, client *api.Client, token string) (string, error) {
	ticker := time.NewTicker(cliPollInterval)
	defer ticker.Stop()

	for {
		status, err := client.CheckCLISession(ctx, token)
		if err != nil {
			// The deadline can elapse mid-request; report it uniformly.
			if ctx.Err() != nil {
				return "", fmt.Errorf("timed out waiting for login confirmation: %w", ctx.Err())
			}
			return "", err
		}
		if status.Status == "ready" {
			if status.UserToken == "" {
				return "", errors.New("login confirmed but no token was returned")
			}
			return status.UserToken, nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for login confirmation: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// watchForCancel puts the terminal into raw mode and, in the background, cancels
// (with errLoginCancelled) when the user presses Esc or Ctrl-C. It returns a
// function that restores the terminal; the returned function is safe to call
// more than once. When stdin is not a terminal it is a no-op.
func watchForCancel(cmd *cobra.Command, cancel context.CancelCauseFunc) func() {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return func() {}
	}
	oldState, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return func() {}
	}

	var once sync.Once
	restore := func() { once.Do(func() { _ = term.Restore(int(f.Fd()), oldState) }) }

	go func() {
		buf := make([]byte, 16)
		for {
			n, err := f.Read(buf)
			if err != nil {
				return
			}
			if n > 0 && isCancelKey(buf[:n]) {
				cancel(errLoginCancelled)
				return
			}
		}
	}()

	return restore
}

// isCancelKey reports whether a chunk of raw terminal input represents a cancel:
// a lone Esc (0x1b), or any Ctrl-C (0x03). A lone Esc is distinguished from an
// escape sequence (e.g. an arrow key) by it being the only byte read.
func isCancelKey(b []byte) bool {
	if len(b) == 1 && b[0] == 0x1b {
		return true
	}
	for _, c := range b {
		if c == 0x03 {
			return true
		}
	}
	return false
}
