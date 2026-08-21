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

// Package iroh is nh's iroh integration: a persisted local endpoint identity
// and the transport used to reach a device's IrohConsole over its ticket. It is
// the only package that depends on the iroh implementation (github.com/tmc/
// go-iroh), keeping that choice isolated behind an io.ReadWriteCloser.
package iroh

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/tmc/go-iroh/key"
)

// identityFile is the endpoint key, stored under the data directory as a hex
// seed. It is the CLI's stable name to the hosted relay, so it must persist:
// a key that changes each run would need re-registering every time.
const identityRelPath = "iroh/identity"

// LoadOrCreateIdentity returns the persisted endpoint secret key under dataDir,
// generating and saving one on first use. The file is written 0600.
func LoadOrCreateIdentity(dataDir string) (key.SecretKey, error) {
	path := filepath.Join(dataDir, identityRelPath)

	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		sk, err := parseIdentity(b)
		if err != nil {
			return key.SecretKey{}, fmt.Errorf("iroh: reading identity %s: %w", path, err)
		}
		return sk, nil
	case errors.Is(err, os.ErrNotExist):
		return createIdentity(dataDir, path)
	default:
		return key.SecretKey{}, fmt.Errorf("iroh: reading identity %s: %w", path, err)
	}
}

// EndpointIDHex returns the endpoint id as 64 hex characters — the form the
// NervesHub iroh-endpoints API registers and stores.
func EndpointIDHex(pk key.PublicKey) string {
	b := pk.Bytes()
	return hex.EncodeToString(b[:])
}

func parseIdentity(b []byte) (key.SecretKey, error) {
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return key.SecretKey{}, fmt.Errorf("identity is not valid hex: %w", err)
	}
	sk, err := key.SecretKeyFromSlice(seed)
	if err != nil {
		return key.SecretKey{}, err
	}
	return sk, nil
}

func createIdentity(dataDir, path string) (key.SecretKey, error) {
	sk, err := key.GenerateSecretKey()
	if err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: generating identity: %w", err)
	}
	// Ensure the top-level data dir (and its README) exists, then the iroh dir.
	if err := config.EnsureDataDir(dataDir); err != nil {
		return key.SecretKey{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: creating identity dir: %w", err)
	}
	seed := sk.Bytes()
	encoded := hex.EncodeToString(seed[:]) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: writing identity: %w", err)
	}
	return sk, nil
}
