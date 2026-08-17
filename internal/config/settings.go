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

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// settingsFileName is the file under the data directory that stores user
// defaults and the saved auth token (e.g. ~/.nh/settings.json).
const settingsFileName = "settings.json"

// SettingKeys lists the keys accepted by `nh config set`/`get`/`unset`. The
// auth token is deliberately not among them: it is managed by `nh user
// auth`/`logout`, not `nh config`.
var SettingKeys = []string{"uri", "org", "product"}

// Settings holds user-configured defaults and the saved auth token, persisted
// to the data directory. The URI, org, and product are the lowest-precedence
// source for those values, below flags and environment variables.
//
// Profiles are named snapshots of the active configuration (URI, org, product,
// token) that can be saved and restored with `nh config save`/`load`.
type Settings struct {
	URI     string `json:"uri,omitempty"`
	Org     string `json:"org,omitempty"`
	Product string `json:"product,omitempty"`
	// Token is the API token saved by `nh user auth`. It is stored here rather
	// than exposed through `nh config`.
	Token    string             `json:"token,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
}

// Profile is a named snapshot of the active configuration.
type Profile struct {
	URI     string `json:"uri,omitempty"`
	Org     string `json:"org,omitempty"`
	Product string `json:"product,omitempty"`
	Token   string `json:"token,omitempty"`
}

// activeProfile captures the current active configuration.
func (s *Settings) activeProfile() Profile {
	return Profile{URI: s.URI, Org: s.Org, Product: s.Product, Token: s.Token}
}

// SaveProfile stores the current active configuration under name, replacing any
// existing profile with that name.
func (s *Settings) SaveProfile(name string) {
	if s.Profiles == nil {
		s.Profiles = make(map[string]Profile)
	}
	s.Profiles[name] = s.activeProfile()
}

// LoadProfile makes the named profile the active configuration. It returns an
// error if no such profile exists.
func (s *Settings) LoadProfile(name string) error {
	p, ok := s.Profiles[name]
	if !ok {
		return fmt.Errorf("no profile named %q", name)
	}
	s.URI, s.Org, s.Product, s.Token = p.URI, p.Org, p.Product, p.Token
	return nil
}

// ProfileNames returns the saved profile names in sorted order.
func (s *Settings) ProfileNames() []string {
	names := make([]string, 0, len(s.Profiles))
	for name := range s.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SettingsFilePath returns the path to the settings file within dataDir.
func SettingsFilePath(dataDir string) string {
	return filepath.Join(dataDir, settingsFileName)
}

// LoadSettings reads persisted settings from dataDir. A missing file is not an
// error: it returns empty Settings.
func LoadSettings(dataDir string) (*Settings, error) {
	b, err := os.ReadFile(SettingsFilePath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("config: parsing settings file %s: %w", SettingsFilePath(dataDir), err)
	}
	return &s, nil
}

// SaveSettings writes s to the settings file within dataDir, creating the
// directory (0700) if needed and writing the file with 0600 permissions.
func SaveSettings(dataDir string, s *Settings) error {
	if err := EnsureDataDir(dataDir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encoding settings: %w", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(SettingsFilePath(dataDir), b, 0o600); err != nil {
		return fmt.Errorf("config: writing settings: %w", err)
	}
	return nil
}

// Get returns the value of the named setting key, or an error for an unknown
// key.
func (s *Settings) Get(key string) (string, error) {
	switch key {
	case "uri":
		return s.URI, nil
	case "org":
		return s.Org, nil
	case "product":
		return s.Product, nil
	default:
		return "", unknownSettingError(key)
	}
}

// Set assigns value to the named setting key, or returns an error for an
// unknown key. An empty value clears the setting.
func (s *Settings) Set(key, value string) error {
	switch key {
	case "uri":
		s.URI = value
	case "org":
		s.Org = value
	case "product":
		s.Product = value
	default:
		return unknownSettingError(key)
	}
	return nil
}

func unknownSettingError(key string) error {
	return fmt.Errorf("unknown setting %q (valid: %s)", key, strings.Join(SettingKeys, ", "))
}
