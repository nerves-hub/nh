package cmd

import (
	"testing"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/spf13/cobra"
)

func runRoot(t *testing.T, args ...string) *config.Config {
	t.Helper()
	resetState(t)
	var got *config.Config
	probe := &cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, _ []string) error {
			got = config.From(cmd.Context())
			return nil
		},
	}
	rootCmd.AddCommand(probe)
	defer rootCmd.RemoveCommand(probe)
	rootCmd.SetArgs(append([]string{"probe"}, args...))
	if err := rootCmd.Execute(); err != nil {
		return nil
	}
	return got
}

func TestPrecedence(t *testing.T) {
	// flag beats env beats default
	t.Setenv("NERVES_HUB_URI", "https://legacy.example.com")
	t.Setenv("NERVES_CLOUD_ORG", "cloud-org")

	c := runRoot(t, "--uri", "https://flag.example.com")
	if c == nil {
		t.Fatal("config not resolved")
	}
	if c.URI != "https://flag.example.com" {
		t.Errorf("URI: flag should win, got %q", c.URI)
	}
	if c.Org != "cloud-org" {
		t.Errorf("Org: env should resolve, got %q", c.Org)
	}
	if c.Output != config.OutputTable {
		t.Errorf("Output default: got %q", c.Output)
	}
}

func TestHubVsCloudPrefix(t *testing.T) {
	t.Setenv("NERVES_HUB_TOKEN", "hub-tok")
	t.Setenv("NERVES_CLOUD_TOKEN", "cloud-tok")
	c := runRoot(t)
	if c.Token != "hub-tok" {
		t.Errorf("NERVES_HUB_ prefix should win over NERVES_CLOUD_, got %q", c.Token)
	}
}

func TestInvalidOutput(t *testing.T) {
	if c := runRoot(t, "--output", "yaml"); c != nil {
		t.Errorf("invalid --output should error, got %+v", c)
	}
}
