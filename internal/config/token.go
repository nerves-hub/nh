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
	"fmt"
	"net/url"
	"strings"
)

// The auth token is persisted inside the settings file (see settings.go) rather
// than a separate file, together with the server it was issued by (TokenURI).
// These helpers manage just those fields, leaving any other saved settings
// untouched.
//
// A token is only ever sent to the server it came from. Settings files written
// before nh recorded TokenURI have a token but no server; LoadSettings pins
// those to the URI the token was then being used with: the saved uri setting,
// or DefaultURI when none was set.

// LoadToken returns the saved token from the settings file within dataDir. A
// missing file is not an error: it returns "" with a nil error.
func LoadToken(dataDir string) (string, error) {
	s, err := LoadSettings(dataDir)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s.Token), nil
}

// SaveToken records token, and uri as the server that issued it, in the
// settings file within dataDir, preserving any other settings. When uri isn't
// already the saved default server, it becomes it, so later commands go to the
// server the token works with; uriChanged reports that. The file is written
// with 0600 permissions so the credential is not world-readable (see
// SaveSettings).
func SaveToken(dataDir, uri, token string) (uriChanged bool, err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, errors.New("config: refusing to save an empty token")
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return false, errors.New("config: refusing to save a token without its server URI")
	}
	s, err := LoadSettings(dataDir)
	if err != nil {
		return false, err
	}
	s.Token, s.TokenURI = token, uri
	if !SameURI(s.EffectiveURI(), uri) {
		s.URI = uri
		uriChanged = true
	}
	return uriChanged, SaveSettings(dataDir, s)
}

// DeleteToken clears the saved token from the settings file within dataDir,
// leaving other settings intact. A missing token (or file) is not an error.
func DeleteToken(dataDir string) error {
	s, err := LoadSettings(dataDir)
	if err != nil {
		return err
	}
	if s.Token == "" && s.TokenURI == "" {
		return nil
	}
	s.Token, s.TokenURI = "", ""
	return SaveSettings(dataDir, s)
}

// EffectiveURI is the server nh uses when neither --uri nor an environment
// variable picks one: the saved uri setting, or DefaultURI.
func (s *Settings) EffectiveURI() string {
	if s.URI != "" {
		return s.URI
	}
	return DefaultURI
}

// TokenOwner returns the server the saved token was issued by, or "" when no
// token is saved.
func (s *Settings) TokenOwner() string {
	if strings.TrimSpace(s.Token) == "" {
		return ""
	}
	if s.TokenURI != "" {
		return s.TokenURI
	}
	return s.EffectiveURI()
}

// TokenFor returns the saved token if it was issued by uri. It returns "" and
// a *TokenMismatchError when the token belongs to another server, so the token
// is never sent there, and "" with a nil error when no token is saved.
func (s *Settings) TokenFor(uri string) (string, error) {
	token := strings.TrimSpace(s.Token)
	if token == "" {
		return "", nil
	}
	if owner := s.TokenOwner(); !SameURI(owner, uri) {
		return "", &TokenMismatchError{TokenURI: owner, URI: uri}
	}
	return token, nil
}

// pinTokenURIs records the server for a token saved before nh kept TokenURI,
// in the active settings and in every profile, so that a later change to the
// uri setting can't silently send the token somewhere else.
func (s *Settings) pinTokenURIs() {
	if s.Token != "" && s.TokenURI == "" {
		s.TokenURI = s.EffectiveURI()
	}
	for name, p := range s.Profiles {
		if p.Token != "" && p.TokenURI == "" {
			p.TokenURI = p.URI
			if p.TokenURI == "" {
				p.TokenURI = DefaultURI
			}
			s.Profiles[name] = p
		}
	}
}

// TokenMismatchError reports that the saved token was issued by a different
// server from the one nh is about to talk to.
type TokenMismatchError struct {
	// TokenURI is the server the saved token belongs to.
	TokenURI string
	// URI is the server nh is using.
	URI string
}

func (e *TokenMismatchError) Error() string {
	return fmt.Sprintf("the saved token is for %s, not %s: pass --uri %s to use it, "+
		"or run `nh user login --uri %s` to sign in to %s",
		e.TokenURI, e.URI, e.TokenURI, e.URI, e.URI)
}

// SameURI reports whether a and b name the same server. Scheme and host are
// compared case-insensitively, a default port (443 for https, 80 for http)
// matches no port, and trailing slashes and a trailing /api are ignored, as the
// API client treats those as the same base.
func SameURI(a, b string) bool {
	return normalizeURI(a) == normalizeURI(b)
}

func normalizeURI(s string) string {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(s, "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6 literal
	}
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		host += ":" + port
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	path = strings.TrimRight(strings.TrimSuffix(path, "/api"), "/")
	return scheme + "://" + host + path
}
