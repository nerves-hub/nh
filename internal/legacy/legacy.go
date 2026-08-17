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

// Package legacy reads the on-disk state written by the old Elixir
// nerves_hub_cli so that `nh migrate` can import it. It locates the old data
// directory, decodes its ETF-encoded config, and lists the signing keys stored
// under it — converting each private key into the base64url form nh expects.
//
// Nothing here writes to the legacy directory; it is treated as read-only.
package legacy

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// configFileName is the ETF-encoded config the old CLI wrote at the root of its
// data directory.
const configFileName = "nerves-hub.config"

const (
	privExt = ".priv"
	pubExt  = ".pub"
)

// DefaultDir returns the old nerves_hub_cli data directory: NERVES_HUB_HOME
// when set, otherwise ~/.nerves-hub. It returns "" only when the home directory
// cannot be determined and no override is set.
func DefaultDir() string {
	if dir := strings.TrimSpace(os.Getenv("NERVES_HUB_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".nerves-hub")
}

// Config holds the settings read from the old CLI's config file. Fields absent
// from the file are left empty.
type Config struct {
	URI     string
	Org     string
	Product string
	Token   string
}

// IsEmpty reports whether no config values were found.
func (c Config) IsEmpty() bool {
	return c.URI == "" && c.Org == "" && c.Product == "" && c.Token == ""
}

// ReadConfig reads and decodes <dir>/nerves-hub.config. A missing file is not
// an error: it returns a zero Config and found=false.
func ReadConfig(dir string) (cfg Config, found bool, err error) {
	path := filepath.Join(dir, configFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("legacy: reading %s: %w", path, err)
	}
	m, err := decodeConfigMap(raw)
	if err != nil {
		return Config{}, false, fmt.Errorf("legacy: parsing %s: %w", path, err)
	}
	return Config{
		URI:     m["uri"],
		Org:     m["org"],
		Product: m["product"],
		Token:   m["token"],
	}, true, nil
}

// Key is a signing key discovered in the legacy directory, with its private key
// already converted to the base64url envelope form nh stores.
type Key struct {
	Org  string
	Name string
	// Priv is the encrypted private-key envelope, base64url-encoded (no
	// padding), ready to write to <data-dir>/keys/<org>/<name>.priv.
	Priv string
	// Pub is the base64 public key, ready to write to the matching .pub file.
	Pub string
}

// ListKeys walks <dir>/keys/<org>/*.priv, pairs each with its .pub, and returns
// the keys with private material converted to nh's base64url form. Keys whose
// .pub is missing or whose files fail to convert are skipped with a collected
// warning rather than aborting the whole scan. A missing keys directory is not
// an error: it returns an empty slice.
func ListKeys(dir string) (keys []Key, warnings []string, err error) {
	keysRoot := filepath.Join(dir, "keys")
	orgs, err := os.ReadDir(keysRoot)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("legacy: reading %s: %w", keysRoot, err)
	}

	for _, orgEntry := range orgs {
		if !orgEntry.IsDir() {
			continue
		}
		org := orgEntry.Name()
		orgDir := filepath.Join(keysRoot, org)
		entries, err := os.ReadDir(orgDir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: reading directory: %v", org, err))
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), privExt) {
				continue
			}
			name := strings.TrimSuffix(e.Name(), privExt)
			key, warn := loadKey(orgDir, org, name)
			if warn != "" {
				warnings = append(warnings, warn)
				continue
			}
			keys = append(keys, key)
		}
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Org != keys[j].Org {
			return keys[i].Org < keys[j].Org
		}
		return keys[i].Name < keys[j].Name
	})
	return keys, warnings, nil
}

// loadKey reads and converts a single keypair. It returns a non-empty warning
// (and a zero Key) when the pair cannot be read or converted.
func loadKey(orgDir, org, name string) (Key, string) {
	privRaw, err := os.ReadFile(filepath.Join(orgDir, name+privExt))
	if err != nil {
		return Key{}, fmt.Sprintf("%s/%s: reading private key: %v", org, name, err)
	}
	pubRaw, err := os.ReadFile(filepath.Join(orgDir, name+pubExt))
	if err != nil {
		return Key{}, fmt.Sprintf("%s/%s: missing or unreadable public key (%v)", org, name, err)
	}
	priv, err := ConvertPriv(string(privRaw))
	if err != nil {
		return Key{}, fmt.Sprintf("%s/%s: %v", org, name, err)
	}
	return Key{
		Org:  org,
		Name: name,
		Priv: priv,
		Pub:  strings.TrimSpace(string(pubRaw)),
	}, ""
}

// ConvertPriv normalises a legacy private key into the base64url (unpadded)
// form nh reads. nerves_hub_cli already stores the encrypted envelope as
// base64url, but this also accepts standard base64 (from older versions) and
// tolerates padding and surrounding whitespace. The encrypted bytes are
// preserved exactly, so no passphrase is required. It validates that the decoded
// bytes are an Erlang term (version byte 131) so a truncated or foreign file is
// caught here rather than at signing time.
func ConvertPriv(encoded string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(encoded), "=")
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		// Fall back to the standard alphabet for keys written by older CLIs.
		raw, err = base64.RawStdEncoding.DecodeString(trimmed)
		if err != nil {
			return "", fmt.Errorf("decoding private key: %w", err)
		}
	}
	if len(raw) == 0 || raw[0] != etfVersion {
		return "", fmt.Errorf("private key is not a recognised nerves_hub_cli key")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
