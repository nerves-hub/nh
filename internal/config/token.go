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
	"errors"
	"strings"
)

// The auth token is persisted inside the settings file (see settings.go) rather
// than a separate file. These helpers manage just the token field, leaving any
// other saved settings untouched.

// LoadToken returns the saved token from the settings file within dataDir. A
// missing file is not an error: it returns "" with a nil error.
func LoadToken(dataDir string) (string, error) {
	s, err := LoadSettings(dataDir)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s.Token), nil
}

// SaveToken records token in the settings file within dataDir, preserving any
// other settings. The file is written with 0600 permissions so the credential
// is not world-readable (see SaveSettings).
func SaveToken(dataDir, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("config: refusing to save an empty token")
	}
	s, err := LoadSettings(dataDir)
	if err != nil {
		return err
	}
	s.Token = token
	return SaveSettings(dataDir, s)
}

// DeleteToken clears the saved token from the settings file within dataDir,
// leaving other settings intact. A missing token (or file) is not an error.
func DeleteToken(dataDir string) error {
	s, err := LoadSettings(dataDir)
	if err != nil {
		return err
	}
	if s.Token == "" {
		return nil
	}
	s.Token = ""
	return SaveSettings(dataDir, s)
}
