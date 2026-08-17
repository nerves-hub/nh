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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

var authEmail string

// authCmd implements `nh user auth`: prompt for email and password, exchange
// them for an API token, and persist the token to the data directory.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Sign in with email and password and save an API token",
	Long: `Authenticate against NervesCloud with your email and password.

You are prompted for your email (unless --email is given) and password; the
password is read without echoing. On success the returned API token is saved
to the settings file in the data directory (default ~/.nh/settings.json) with
0600 permissions, and is used automatically by subsequent commands.`,
	Args: cobra.NoArgs,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&authEmail, "email", "", "account email (skips the email prompt)")
	userCmd.AddCommand(authCmd)
}

func runAuth(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())

	// In non-interactive mode we never display prompts; values must come from
	// --email and piped stdin.
	quiet := cfg.NonInteractive
	if cfg.NonInteractive && stdinIsTerminal(cmd) && authEmail == "" {
		return errors.New("--non-interactive set: provide --email and pipe the password to stdin")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Authenticating to %s\n\n", cfg.URI)

	email := strings.TrimSpace(authEmail)
	if email == "" {
		line, err := promptLine(cmd, "Email: ", quiet)
		if err != nil {
			return err
		}
		email = strings.TrimSpace(line)
	}
	if email == "" {
		return errors.New("email is required")
	}

	password, err := promptPassword(cmd, "Password: ", quiet)
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("password is required")
	}

	// Authenticate with an empty token: this is the call that mints one.
	client, err := api.NewClient(cfg.URI, "", api.WithUserAgent(userAgent()))
	if err != nil {
		return err
	}

	result, err := client.Authenticate(cmd.Context(), email, password, tokenNote())
	if err != nil {
		return err
	}
	if result.Token == "" {
		return errors.New("authentication succeeded but no token was returned")
	}

	if err := config.SaveToken(cfg.DataDir, result.Token); err != nil {
		return err
	}

	who := email
	if result.User.Name != "" {
		who = result.User.Name
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nSuccessfully authenticated as %s\nToken saved to %s\n", who, config.SettingsFilePath(cfg.DataDir))
	return nil
}

// hostname returns the local computer name for labelling the issued token. On
// macOS it prefers the user-facing name from `scutil --get ComputerName`,
// falling back to os.Hostname and finally "unknown".
func hostname() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				return name
			}
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}
