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

// Package config resolves global nh configuration — API URI, auth token,
// org/product scope, data directory, and output format — from command-line
// flags, environment variables, and built-in defaults.
//
// Environment variables are read under two prefixes: the default NERVES_HUB_
// prefix and the also-supported NERVES_CLOUD_ prefix, in that order, so
// existing nerves_hub_cli setups keep working.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultURI is the NervesCloud API base used when neither --uri nor an
// environment variable is set.
//
// TODO: confirm this matches the default host baked into the old
// nerves_hub_cli before release.
const DefaultURI = "https://manage.nervescloud.com"

// Environment variable prefixes, checked in order (default, then also-supported).
const (
	envPrefixHub   = "NERVES_HUB_"
	envPrefixCloud = "NERVES_CLOUD_"
)

// OutputFormat is the rendering mode for command output.
type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
)

// Config holds the resolved global configuration for a single invocation.
type Config struct {
	// URI is the NervesCloud API base URL.
	URI string
	// Token is the personal access token used to authenticate API requests.
	Token string
	// Org is the default organization scope for commands that accept one.
	Org string
	// Product is the default product scope for commands that accept one.
	Product string
	// DataDir is where nh persists local state (config, cached token).
	DataDir string
	// NonInteractive disables all prompts; missing input becomes an error.
	NonInteractive bool
	// Output selects how command results are rendered.
	Output OutputFormat
}

// Env returns the value of the first set environment variable among the
// NERVES_HUB_<key> and NERVES_CLOUD_<key> variants, in that order. It returns
// "" when neither is set.
func Env(key string) string {
	if v := os.Getenv(envPrefixHub + key); v != "" {
		return v
	}
	return os.Getenv(envPrefixCloud + key)
}

// EnvOr is Env with a fallback returned when neither variable is set.
func EnvOr(key, def string) string {
	if v := Env(key); v != "" {
		return v
	}
	return def
}

// EnvBool reports whether the NERVES_HUB_/NERVES_CLOUD_ variable for key is
// set to a truthy value (1, true, yes; case-insensitive).
func EnvBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(Env(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// DefaultDataDir returns the default location for nh's local state,
// honouring NERVES_HUB_DATA_DIR / NERVES_CLOUD_DATA_DIR when set and otherwise
// falling back to ~/.nh.
func DefaultDataDir() string {
	if dir := Env("DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// UserHomeDir only fails when $HOME is unset; degrade to cwd-relative.
		return ".nh"
	}
	return filepath.Join(home, ".nh")
}

// ParseOutput validates and normalises an output-format string.
func ParseOutput(s string) (OutputFormat, error) {
	switch OutputFormat(strings.ToLower(strings.TrimSpace(s))) {
	case OutputTable:
		return OutputTable, nil
	case OutputJSON:
		return OutputJSON, nil
	default:
		return "", fmt.Errorf("invalid output format %q: must be %q or %q", s, OutputTable, OutputJSON)
	}
}

// ctxKey is the unexported context key under which *Config is stored.
type ctxKey struct{}

// NewContext returns a copy of ctx carrying c.
func NewContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// From returns the *Config carried by ctx, or nil if none is present.
func From(ctx context.Context) *Config {
	c, _ := ctx.Value(ctxKey{}).(*Config)
	return c
}
