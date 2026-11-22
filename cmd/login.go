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
	"github.com/nerves-hub/nh/internal/browser"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// errLoginCancelled is the cancellation cause when the user aborts login.
var errLoginCancelled = errors.New("login cancelled")

// formatLoginCode renders the confirmation code the user types into the web
// page in groups of four digits, e.g. 31281247 -> "3128 1247". The page
// ignores spaces and dashes, so the code can be typed as shown.
func formatLoginCode(code int) string {
	digits := strconv.Itoa(code)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CLI login polling. They are package vars so tests can shorten them. The
// timeout matches how long the server keeps a login waiting.
var (
	cliPollOptions = api.CLIPollOptions{}
	cliPollTimeout = 5 * time.Minute
)

// openBrowser opens a URL in the user's browser. Tests replace it.
var openBrowser = browser.Open

// stdoutIsTerminal reports whether the command's stdout is an interactive
// terminal. Tests replace it.
var stdoutIsTerminal = func(cmd *cobra.Command) bool {
	f, ok := cmd.OutOrStdout().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

var loginNoBrowser bool

// userLoginCmd implements `nh user login`: sign in on the web and type in a
// code, which avoids entering a password in the terminal.
var userLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate by signing in on the web and entering a code",
	Long: `Authenticate without entering your password in the terminal.

nh starts a login and shows a URL and an eight-digit code. Open the URL, sign
in to NervesHub, and enter the code there. nh waits for you to confirm, then
saves your API token to the settings file (default ~/.nh/settings.json).

When stdout is a terminal, nh opens the URL in your browser for you; pass
--no-browser to stop it. It never does with --non-interactive, or when there
is no display (such as over SSH). The URL is always printed too.

The code lasts five minutes.`,
	Args: cobra.NoArgs,
	RunE: runUserLogin,
}

func init() {
	userLoginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "don't open the login URL in a browser")
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
	// The URL is printed and opened, so it has to be a plain web address: a
	// control character could rewrite the terminal, and the openers also run
	// files and apps.
	if !browser.IsWebURL(session.URL) {
		return fmt.Errorf("login session response had an invalid URL %q", session.URL)
	}
	if session.ConfirmationCode <= 0 {
		return errors.New("login session response had no confirmation code, which is needed to finish signing in")
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "To sign in, open this URL in your browser and sign in to NervesHub:\n\n    %s\n", session.URL)
	fmt.Fprintf(w, "\nThen enter this code:\n\n    %s\n", formatLoginCode(session.ConfirmationCode))
	fmt.Fprintln(w, "\nThe code expires in five minutes.")

	if !loginNoBrowser && !cfg.NonInteractive && stdoutIsTerminal(cmd) {
		if err := openBrowser(session.URL); err == nil {
			fmt.Fprintln(w, "\nOpened the URL in your browser.")
		} else if !errors.Is(err, browser.ErrNoDisplay) {
			fmt.Fprintf(cmd.ErrOrStderr(), "\nCouldn't open a browser (%v); open the URL yourself.\n", err)
		}
	}

	hint := ""
	if stdinIsTerminal(cmd) {
		hint = " (press Esc to cancel)"
	}
	fmt.Fprintf(w, "\nWaiting for you to enter the code...%s\n", hint)

	deadlineCtx, cancelTimeout := context.WithTimeout(cmd.Context(), cliPollTimeout)
	defer cancelTimeout()
	ctx, cancel := context.WithCancelCause(deadlineCtx)
	defer cancel(nil)

	// Let the user abort by pressing Esc (or Ctrl-C) while we wait.
	restore := watchForCancel(cmd, cancel)

	userToken, err := client.WaitForCLISession(ctx, session.Token, cliPollOptions)
	restore() // restore the terminal before printing anything else

	if err != nil {
		if errors.Is(context.Cause(ctx), errLoginCancelled) {
			fmt.Fprintln(w, "\nLogin cancelled.")
			return nil
		}
		if errors.Is(err, api.ErrCLISessionExpired) {
			return fmt.Errorf("%w; run `nh user login` again", err)
		}
		return err
	}

	// Check the token works, and show whose it is, before saving it. The login
	// has already succeeded, so a failure here only warns.
	who := ""
	if authed, err := api.NewClient(cfg.URI, userToken, api.WithUserAgent(userAgent())); err == nil {
		if user, err := authed.CurrentUser(cmd.Context()); err == nil {
			who = describeUser(user)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "\nWarning: signed in, but couldn't look up the account for the new token: %v\n", err)
		}
	}

	if err := config.SaveToken(cfg.DataDir, userToken); err != nil {
		return err
	}

	if who != "" {
		fmt.Fprintf(w, "\nSuccessfully authenticated as %s\n", who)
	} else {
		fmt.Fprintln(w, "\nSuccessfully authenticated")
	}
	fmt.Fprintf(w, "Token saved to %s\n", config.SettingsFilePath(cfg.DataDir))
	return nil
}

// describeUser names a user for a sign-in message: "Name <email>", or
// whichever of the two is set.
func describeUser(u *api.User) string {
	switch {
	case u.Name != "" && u.Email != "":
		return fmt.Sprintf("%s <%s>", u.Name, u.Email)
	case u.Name != "":
		return u.Name
	default:
		return u.Email
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
