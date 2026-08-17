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

package api

import (
	"context"
	"errors"
	"net/url"
)

// SigningKey is a firmware/archive signing key belonging to an organization,
// as returned by GET /api/orgs/{org}/keys[/{name}]. Key is the public key.
type SigningKey struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ListSigningKeys returns an organization's signing keys via
// GET /orgs/{org}/keys.
func (c *Client) ListSigningKeys(ctx context.Context, org string) ([]SigningKey, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list signing keys")
	}

	var resp struct {
		Data []SigningKey `json:"data"`
	}
	if err := c.Get(ctx, keysPath(org), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// signingKeyCreateRequest is the body sent to POST /orgs/{org}/keys.
type signingKeyCreateRequest struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// CreateSigningKey registers a signing key (name + base64 public key) via
// POST /orgs/{org}/keys and returns the created key.
func (c *Client) CreateSigningKey(ctx context.Context, org, name, publicKey string) (*SigningKey, error) {
	if org == "" {
		return nil, errors.New("api: org is required to create a signing key")
	}
	if name == "" {
		return nil, errors.New("api: signing key name is required")
	}
	if publicKey == "" {
		return nil, errors.New("api: public key is required")
	}

	var resp struct {
		Data SigningKey `json:"data"`
	}
	body := signingKeyCreateRequest{Name: name, Key: publicKey}
	if err := c.Post(ctx, keysPath(org), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetSigningKey returns a single signing key by name via
// GET /orgs/{org}/keys/{name}.
func (c *Client) GetSigningKey(ctx context.Context, org, name string) (*SigningKey, error) {
	if org == "" {
		return nil, errors.New("api: org is required to get a signing key")
	}
	if name == "" {
		return nil, errors.New("api: signing key name is required")
	}

	var resp struct {
		Data SigningKey `json:"data"`
	}
	if err := c.Get(ctx, keyPath(org, name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteSigningKey deletes a signing key by name via
// DELETE /orgs/{org}/keys/{name}.
func (c *Client) DeleteSigningKey(ctx context.Context, org, name string) error {
	if org == "" {
		return errors.New("api: org is required to delete a signing key")
	}
	if name == "" {
		return errors.New("api: signing key name is required")
	}
	return c.Delete(ctx, keyPath(org, name))
}

func keysPath(org string) string {
	return "/orgs/" + url.PathEscape(org) + "/keys"
}

func keyPath(org, name string) string {
	return keysPath(org) + "/" + url.PathEscape(name)
}
