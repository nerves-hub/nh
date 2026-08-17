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
	"os"
	"strings"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
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
		savedToken, err := config.LoadToken(dataDir)
		if err != nil {
			return err
		}
		settings, err := config.LoadSettings(dataDir)
		if err != nil {
			return err
		}

		// The built-in default URI applies only when settings don't override it.
		defaultURI := config.DefaultURI
		if settings.URI != "" {
			defaultURI = settings.URI
		}

		cfg := &config.Config{
			URI:            flagValue(cmd, "uri", defaultURI),
			Token:          flagValue(cmd, "token", savedToken),
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

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
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
