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
	"fmt"
	"os"
	"path/filepath"
)

// dataDirReadmeName is the explainer file nh drops into the data directory so
// anyone who stumbles across ~/.nh knows what it holds.
const dataDirReadmeName = "README.md"

// dataDirReadme documents the contents of the data directory. It is written
// once, when the directory is first created, and never overwritten afterwards.
const dataDirReadme = `# nh data directory

This directory holds local state for ` + "`nh`" + `, the NervesCloud / NervesHub
command-line tool. You normally don't need to touch anything in here — ` + "`nh`" + `
manages it for you.

## Contents

- ` + "`settings.json`" + ` — your saved defaults (API base URI, org, product), your
  API token, and any named configuration profiles. Managed by ` + "`nh config`" + `,
  ` + "`nh user auth`" + ` / ` + "`nh user login`" + `, and ` + "`nh user logout`" + `.
- ` + "`keys/<org>/<name>.priv`" + ` — a firmware signing key, encrypted at rest.
- ` + "`keys/<org>/<name>.pub`" + ` — the matching public key.

## Please note

- ` + "`settings.json`" + ` contains an API token and ` + "`.priv`" + ` files contain signing
  keys. Treat this directory as secret: do not commit it to version control or
  share it.
- To point ` + "`nh`" + ` at a different location, set ` + "`NERVES_HUB_DATA_DIR`" + ` or pass
  ` + "`--data-dir`" + `.
`

// EnsureDataDir creates dataDir (0700) if needed and, the first time it does,
// writes a README explaining what the directory is for. An existing README is
// never overwritten. Callers that persist state into the data directory should
// route their directory creation through this helper.
func EnsureDataDir(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("config: creating data dir: %w", err)
	}
	readme := filepath.Join(dataDir, dataDirReadmeName)
	if _, err := os.Stat(readme); err == nil {
		return nil // already present; leave any user edits intact
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("config: checking data dir readme: %w", err)
	}
	if err := os.WriteFile(readme, []byte(dataDirReadme), 0o644); err != nil {
		return fmt.Errorf("config: writing data dir readme: %w", err)
	}
	return nil
}
