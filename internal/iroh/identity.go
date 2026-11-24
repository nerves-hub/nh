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

// Package iroh is nh's iroh integration: the persisted local endpoint
// identities, their registration with an organization, and the transport used
// to reach a device's IrohConsole. It is the only package that depends on the
// iroh implementation (github.com/tmc/go-iroh), keeping that choice isolated
// behind an io.ReadWriteCloser.
package iroh

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nerves-hub/nh/internal/config"
	"github.com/tmc/go-iroh/key"
)

// legacyIdentityRelPath is the single per-machine key nh kept before keys were
// per organization. NervesHub lets a key belong to only one organization, so it
// is still used for the organization it is already registered with and never
// registered anywhere else.
const legacyIdentityRelPath = "iroh/identity"

// orgIdentityDir holds one key per organization, at orgs/<org>/identity. It is
// a directory of its own so that no organization name can collide with the
// legacy key file beside it.
const orgIdentityDir = "iroh/orgs"

// safeOrgDir matches organization names that can be used as a directory name
// as they are. NervesHub allows A-Z too; those get a hash suffix (see
// orgDirName) so that "Acme" and "acme" stay apart on a case-insensitive
// filesystem.
var safeOrgDir = regexp.MustCompile(`^[a-z0-9_-]+$`)

// LegacyIdentityPath is where nh kept its single per-machine key.
func LegacyIdentityPath(dataDir string) string {
	return filepath.Join(dataDir, legacyIdentityRelPath)
}

// OrgIdentityPath is where nh keeps its key for org.
func OrgIdentityPath(dataDir, org string) string {
	return filepath.Join(dataDir, orgIdentityDir, orgDirName(org), "identity")
}

func orgDirName(org string) string {
	if safeOrgDir.MatchString(org) {
		return org
	}
	sum := sha256.Sum256([]byte(org))
	lower := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, org)
	return lower + "-" + hex.EncodeToString(sum[:4])
}

// LoadOrCreateIdentity returns the key stored at path, generating and saving
// one on first use. The file is written 0600. dataDir is the data directory
// path lives under; it is created, with its README, if it does not exist.
//
// Two nh processes creating the same key at once agree on one: the file is
// created exclusively, and the loser reads the winner's key.
func LoadOrCreateIdentity(dataDir, path string) (key.SecretKey, error) {
	sk, ok, err := ReadIdentity(path)
	if err != nil || ok {
		return sk, err
	}
	sk, err = key.GenerateSecretKey()
	if err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: generating identity: %w", err)
	}
	return writeIdentity(dataDir, path, sk)
}

// ReadIdentity returns the key stored at path. ok is false, with no error, when
// there is no file there.
func ReadIdentity(path string) (sk key.SecretKey, ok bool, err error) {
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		sk, err := parseIdentity(b)
		if err != nil {
			return key.SecretKey{}, false, fmt.Errorf("iroh: reading identity %s: %w", path, err)
		}
		return sk, true, nil
	case errors.Is(err, os.ErrNotExist):
		return key.SecretKey{}, false, nil
	default:
		return key.SecretKey{}, false, fmt.Errorf("iroh: reading identity %s: %w", path, err)
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

// writeIdentity saves sk at path unless a key is already there, in which case
// that key is returned instead: a key that may already be registered is never
// overwritten.
func writeIdentity(dataDir, path string, sk key.SecretKey) (key.SecretKey, error) {
	// Ensure the top-level data dir (and its README) exists, then the key's dir.
	if err := config.EnsureDataDir(dataDir); err != nil {
		return key.SecretKey{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: creating identity dir: %w", err)
	}

	// Write the key to a temporary file (CreateTemp makes it 0600), then link
	// it into place. The link
	// fails if a key is already there, and a reader never sees a key half
	// written.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: writing identity: %w", err)
	}
	defer os.Remove(tmp.Name())

	seed := sk.Bytes()
	_, werr := tmp.WriteString(hex.EncodeToString(seed[:]) + "\n")
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return key.SecretKey{}, fmt.Errorf("iroh: writing identity: %w", werr)
	}

	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another nh run saved a key first; use that one.
			existing, ok, rerr := ReadIdentity(path)
			if rerr != nil {
				return key.SecretKey{}, rerr
			}
			if ok {
				return existing, nil
			}
		}
		return key.SecretKey{}, fmt.Errorf("iroh: writing identity: %w", err)
	}
	return sk, nil
}
