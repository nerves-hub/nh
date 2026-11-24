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
	"os"
	"strings"
	"time"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/interrupt"
	"github.com/nerves-hub/nh/internal/mix"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "nh",
	Short: "Manage NervesCloud / NervesHub from the command line",
	Long: `nh is the command-line interface for NervesCloud and NervesHub.

It manages organizations, products, devices, firmware, deployments, and
firmware signing keys, and generates the X.509 certificates devices use to
authenticate.`,
	SilenceUsage: true,
	// PersistentPreRunE resolves global configuration before any subcommand
	// runs and stashes it on the command context for internal/* to read.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		output, err := config.ParseOutput(flagValue(cmd, "output", string(config.OutputTable)))
		if err != nil {
			return err
		}

		// Resolve the data dir first so persisted state — the token saved by
		// `user auth` and the defaults set by `config set` — can serve as the
		// lowest-precedence sources (flag > env > saved file).
		dataDir := flagValue(cmd, "data-dir", config.DefaultDataDir())
		settings, err := config.LoadSettings(dataDir)
		if err != nil {
			return err
		}

		// The built-in default URI applies only when settings don't override it.
		uri := flagValue(cmd, "uri", settings.EffectiveURI())

		// A token given by flag or environment is used as is: the user chose
		// to pair it with this URI. The saved token is only used with the
		// server that issued it, so it never leaks to another host.
		var token string
		var tokenErr error
		if cmd.Flags().Changed("token") || config.Env("TOKEN") != "" {
			token = flagValue(cmd, "token", "")
		} else {
			token, tokenErr = settings.TokenFor(uri)
		}

		cfg := &config.Config{
			URI:            uri,
			Token:          token,
			TokenErr:       tokenErr,
			Org:            flagValue(cmd, "org", settings.Org),
			Product:        flagValue(cmd, "product", settings.Product),
			DataDir:        dataDir,
			NonInteractive: flagBool(cmd, "non-interactive", config.EnvBool("NON_INTERACTIVE")),
			Output:         output,
		}

		cmd.SetContext(config.NewContext(cmd.Context(), cfg))
		return nil
	},
}

// interruptGrace is how long a command has, after Ctrl-C or SIGTERM, to stop
// on its own and run its deferred cleanup before nh exits regardless. Killing
// a subprocess and removing temporary files takes milliseconds; the wait is
// felt only at a prompt, which does not see the cancellation.
const interruptGrace = time.Second

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
//
// The first Ctrl-C or SIGTERM cancels the command's context rather than
// killing the process, so subprocesses (fwup, espsecure, mix) are killed and
// deferred cleanup, such as removing a decrypted signing key, runs. A command
// that has not stopped within interruptGrace, or a second Ctrl-C, ends the
// process, after removing the files registered with interrupt.RemoveOnExit
// and restoring the terminal a password prompt may have left without echo.
func Execute() {
	restoreTerminal := saveTerminalState(os.Stdin)
	sig, err := interrupt.Run(context.Background(), interrupt.Options{
		Grace: interruptGrace,
		Abandon: func(sig os.Signal) {
			restoreTerminal()
			// Exiting with a status rather than dying of the signal, nh gets
			// no newline from the shell after the ^C, so a prompt it was
			// showing would run into the shell's own.
			if term.IsTerminal(int(os.Stderr.Fd())) {
				_, _ = os.Stderr.WriteString("\n")
			}
			os.Exit(interrupt.ExitCode(sig))
		},
	}, func(ctx context.Context) error {
		mix.SetContext(ctx)
		return rootCmd.ExecuteContext(ctx)
	})
	if code := exitCode(sig, err); code != 0 {
		os.Exit(code)
	}
}

// exitCode is nh's exit status: 0 on success, the shell's code for the signal
// when an interrupted command failed (130 for Ctrl-C), and 1 otherwise. A
// command that handles the interrupt itself and returns nil, as `device logs
// --follow` does, still exits 0.
func exitCode(sig os.Signal, err error) int {
	switch {
	case err == nil:
		return 0
	case sig != nil:
		return interrupt.ExitCode(sig)
	default:
		return 1
	}
}

// saveTerminalState records f's terminal state, when it is a terminal, and
// returns a func that puts it back.
func saveTerminalState(f *os.File) (restore func()) {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() { _ = term.Restore(fd, state) }
}

func init() {
	// Global flags. Defaults intentionally stay empty so that an unset flag
	// falls through to the environment in PersistentPreRunE; this also keeps
	// secrets like --token out of generated --help text.
	pf := rootCmd.PersistentFlags()
	pf.String("uri", "", "NervesCloud API base URI (env NERVES_HUB_URI)")
	pf.String("token", "", "personal access token (env NERVES_HUB_TOKEN)")
	pf.String("org", "", "organization scope (env NERVES_HUB_ORG)")
	pf.String("product", "", "product scope (env NERVES_HUB_PRODUCT)")
	pf.String("data-dir", "", "directory for local state (env NERVES_HUB_DATA_DIR)")
	pf.Bool("non-interactive", false, "never prompt; treat missing input as an error (env NERVES_HUB_NON_INTERACTIVE)")
	pf.StringP("output", "o", string(config.OutputTable), "output format: table or json")
}

// flagValue returns the string flag's value when the user explicitly set it,
// otherwise the matching NERVES_HUB_/NERVES_CLOUD_ environment variable, and
// finally def. The env key is the flag name upper-cased with dashes mapped to
// underscores (e.g. "data-dir" -> DATA_DIR).
func flagValue(cmd *cobra.Command, name, def string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	return config.EnvOr(envKey(name), def)
}

// flagBool returns the bool flag's value when explicitly set, otherwise def
// (which callers derive from the environment).
func flagBool(cmd *cobra.Command, name string, def bool) bool {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
	return def
}

// envKey maps a flag name to its environment-variable suffix.
func envKey(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
