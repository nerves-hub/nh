package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// execCmd resets global command state, runs rootCmd with args (feeding in on
// stdin when non-empty), and returns combined stdout+stderr and the error.
func execCmd(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	resetState(t)

	var out bytes.Buffer
	rootCmd.SetArgs(args)
	if in != "" {
		rootCmd.SetIn(strings.NewReader(in))
	}
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	return out.String(), err
}

// resetState restores the package-global cobra command tree between tests.
// Flag values and their Changed status persist across rootCmd.Execute calls,
// so without this earlier tests would leak flags (e.g. --token) into later
// ones. It does not touch environment variables.
func resetState(t *testing.T) {
	t.Helper()

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		resetFlagSet(c.PersistentFlags())
		resetFlagSet(c.Flags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	rootCmd.SetArgs(nil)
	rootCmd.SetIn(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)

	// Isolate every test from the developer's real ~/.nh so persisted
	// tokens and settings never leak in. Tests that need a specific directory
	// still pass --data-dir explicitly, which overrides this.
	t.Setenv("NERVES_CLOUD_DATA_DIR", t.TempDir())
}

func resetFlagSet(fs *pflag.FlagSet) {
	fs.VisitAll(func(f *pflag.Flag) {
		// Slice/array flags append on Set, so resetting them via Set(DefValue)
		// would add a literal default element. Clear them via Replace instead.
		// (All such flags here default to empty.)
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
}
