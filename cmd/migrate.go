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
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/nerves-hub/nh/internal/legacy"
	"github.com/spf13/cobra"
)

// migrateCmd implements `nh migrate`.
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Import settings and signing keys from the old Elixir CLI",
	Long: `Import configuration and signing keys from the old Elixir nerves_hub_cli.

nh reads the old CLI's data directory (default ~/.nerves-hub, or
$NERVES_HUB_HOME) and copies your saved defaults, API token, and signing keys
into nh's own data directory. Signing keys are re-encoded, not decrypted, so no
key password is required and password-protected keys keep their password.

The old directory is only read, never modified. Existing nh settings and keys
are left untouched unless you pass --force, so it is safe to run more than
once.`,
	Args: cobra.NoArgs,
	RunE: runMigrate,
}

func init() {
	migrateCmd.Flags().String("from", "", "old CLI data directory (default ~/.nerves-hub or $NERVES_HUB_HOME)")
	migrateCmd.Flags().Bool("dry-run", false, "show what would be imported without writing anything")
	migrateCmd.Flags().Bool("force", false, "overwrite settings and keys that already exist")
	rootCmd.AddCommand(migrateCmd)
}

// settingChange records the planned import of a single settings field.
type settingChange struct {
	Name   string `json:"name"`
	Action string `json:"action"` // "import" or "skip"
	Reason string `json:"reason,omitempty"`
}

// keyChange records the planned import of a single signing key.
type keyChange struct {
	Org    string `json:"org"`
	Name   string `json:"name"`
	Action string `json:"action"` // "import" or "skip"
	Reason string `json:"reason,omitempty"`
}

// migrateResult is the JSON-serialisable summary of a migration.
type migrateResult struct {
	From     string          `json:"from"`
	DryRun   bool            `json:"dry_run"`
	Settings []settingChange `json:"settings"`
	Keys     []keyChange     `json:"keys"`
	Warnings []string        `json:"warnings,omitempty"`
}

func runMigrate(cmd *cobra.Command, _ []string) error {
	cfg := config.From(cmd.Context())
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	from, _ := cmd.Flags().GetString("from")
	if from == "" {
		from = legacy.DefaultDir()
	}
	if from == "" {
		return fmt.Errorf("could not determine the old CLI directory; pass --from")
	}
	if info, err := os.Stat(from); err != nil || !info.IsDir() {
		return fmt.Errorf("no old CLI directory at %s (pass --from to point elsewhere)", from)
	}

	legacyCfg, _, err := legacy.ReadConfig(from)
	if err != nil {
		return err
	}
	keys, warnings, err := legacy.ListKeys(from)
	if err != nil {
		return err
	}

	current, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	result := migrateResult{
		From:     from,
		DryRun:   dryRun,
		Settings: planSettings(legacyCfg, current, force),
		Keys:     planKeys(keys, cfg.DataDir, force),
		Warnings: warnings,
	}

	if !dryRun {
		if err := applyMigration(cfg.DataDir, legacyCfg, current, keys, result); err != nil {
			return err
		}
	}

	if cfg.Output == config.OutputJSON {
		return printJSON(cmd.OutOrStdout(), result)
	}
	printMigrateReport(cmd.OutOrStdout(), result)
	return nil
}

// planSettings decides, per field, whether the legacy value would be imported.
func planSettings(l legacy.Config, cur *config.Settings, force bool) []settingChange {
	fields := []struct {
		name     string
		newVal   string
		curVal   string
		redacted bool
	}{
		{"uri", l.URI, cur.URI, false},
		{"org", l.Org, cur.Org, false},
		{"product", l.Product, cur.Product, false},
		{"token", l.Token, cur.Token, true},
	}
	var changes []settingChange
	for _, f := range fields {
		if f.newVal == "" {
			continue // nothing in the old config for this field
		}
		c := settingChange{Name: f.name, Action: "import"}
		if f.curVal != "" && !force {
			c.Action = "skip"
			c.Reason = "already set (use --force to overwrite)"
		}
		if f.redacted && c.Action == "import" {
			c.Reason = "imported (value hidden)"
		}
		changes = append(changes, c)
	}
	return changes
}

// planKeys decides, per key, whether it would be imported into dataDir.
func planKeys(keys []legacy.Key, dataDir string, force bool) []keyChange {
	changes := make([]keyChange, 0, len(keys))
	for _, k := range keys {
		c := keyChange{Org: k.Org, Name: k.Name, Action: "import"}
		if !force && keyExists(dataDir, k.Org, k.Name) {
			c.Action = "skip"
			c.Reason = "already exists (use --force to overwrite)"
		}
		changes = append(changes, c)
	}
	return changes
}

// applyMigration writes the imported settings and keys. It only touches fields
// and keys the plan marked "import".
func applyMigration(dataDir string, l legacy.Config, cur *config.Settings, keys []legacy.Key, plan migrateResult) error {
	settingsChanged := false
	for _, c := range plan.Settings {
		if c.Action != "import" {
			continue
		}
		switch c.Name {
		case "uri":
			cur.URI = l.URI
		case "org":
			cur.Org = l.Org
		case "product":
			cur.Product = l.Product
		case "token":
			cur.Token = l.Token
		}
		settingsChanged = true
	}
	if settingsChanged {
		if err := config.SaveSettings(dataDir, cur); err != nil {
			return err
		}
	} else if err := config.EnsureDataDir(dataDir); err != nil {
		// Ensure the data dir (and its README) exists even if only keys import.
		return err
	}

	byName := make(map[string]legacy.Key, len(keys))
	for _, k := range keys {
		byName[k.Org+"/"+k.Name] = k
	}
	for _, c := range plan.Keys {
		if c.Action != "import" {
			continue
		}
		if err := writeKey(dataDir, byName[c.Org+"/"+c.Name]); err != nil {
			return err
		}
	}
	return nil
}

// writeKey writes a single imported key into <dataDir>/keys/<org>/, mirroring
// the layout and permissions used by `nh key create`.
func writeKey(dataDir string, k legacy.Key) error {
	dir := filepath.Join(dataDir, "keys", k.Org)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating key directory for %s: %w", k.Org, err)
	}
	if err := os.WriteFile(filepath.Join(dir, k.Name+".priv"), []byte(k.Priv+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing private key %s/%s: %w", k.Org, k.Name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, k.Name+".pub"), []byte(k.Pub+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing public key %s/%s: %w", k.Org, k.Name, err)
	}
	return nil
}

// keyExists reports whether either half of a keypair is already present.
func keyExists(dataDir, org, name string) bool {
	dir := filepath.Join(dataDir, "keys", org)
	for _, ext := range []string{".priv", ".pub"} {
		if _, err := os.Stat(filepath.Join(dir, name+ext)); err == nil {
			return true
		}
	}
	return false
}

func printMigrateReport(w io.Writer, r migrateResult) {
	verb := "Imported from"
	if r.DryRun {
		verb = "Would import from"
	}
	fmt.Fprintf(w, "%s %s\n\n", verb, r.From)

	fmt.Fprintln(w, "Settings:")
	if len(r.Settings) == 0 {
		fmt.Fprintln(w, "  (none found)")
	}
	for _, c := range r.Settings {
		fmt.Fprintf(w, "  %-8s %s%s\n", c.Name, c.Action, parenReason(c.Reason))
	}

	imported, skipped := 0, 0
	for _, c := range r.Keys {
		if c.Action == "import" {
			imported++
		} else {
			skipped++
		}
	}
	fmt.Fprintf(w, "\nSigning keys: %d imported, %d skipped\n", imported, skipped)
	for _, c := range r.Keys {
		fmt.Fprintf(w, "  %s/%s %s%s\n", c.Org, c.Name, c.Action, parenReason(c.Reason))
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\nWarnings (%d):\n", len(r.Warnings))
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  %s\n", warn)
		}
	}

	if r.DryRun {
		fmt.Fprintln(w, "\nThis was a dry run; nothing was written. Re-run without --dry-run to apply.")
	}
}

func parenReason(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}
