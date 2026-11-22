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
	"fmt"
	"net/url"
	"strings"
)

// DeviceSharedSecret is one of a device's own shared secrets (an "nhd_" key),
// as returned by
// GET /orgs/{org}/products/{product}/devices/{identifier}/shared_secrets.
//
// Secret is only set in the response to a create: the server never lists it
// and it cannot be fetched again afterwards.
type DeviceSharedSecret struct {
	Key           string     `json:"key"`
	Secret        string     `json:"secret,omitempty"`
	DeactivatedAt *Timestamp `json:"deactivated_at"`
	LastUsed      *Timestamp `json:"last_used"`
	InsertedAt    Timestamp  `json:"inserted_at"`
}

// Active reports whether the shared secret has not been deactivated.
func (s DeviceSharedSecret) Active() bool {
	return s.DeactivatedAt == nil || s.DeactivatedAt.IsZero()
}

// ListDeviceSharedSecrets returns every shared secret key a device has been
// given, deactivated ones included, via
// GET /orgs/{org}/products/{product}/devices/{identifier}/shared_secrets.
// The server returns keys only, never the secrets.
func (c *Client) ListDeviceSharedSecrets(ctx context.Context, org, product, identifier string) ([]DeviceSharedSecret, error) {
	if err := requireSharedSecretScope(org, product, identifier); err != nil {
		return nil, err
	}

	var resp struct {
		Data []DeviceSharedSecret `json:"data"`
	}
	if err := c.Get(ctx, deviceSharedSecretsPath(org, product, identifier), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// CreateDeviceSharedSecret issues a new key and secret for a device via
// POST /orgs/{org}/products/{product}/devices/{identifier}/shared_secrets
// (no request body). The returned secret is the only copy: the server never
// returns it again.
func (c *Client) CreateDeviceSharedSecret(ctx context.Context, org, product, identifier string) (*DeviceSharedSecret, error) {
	if err := requireSharedSecretScope(org, product, identifier); err != nil {
		return nil, err
	}

	var resp struct {
		Data DeviceSharedSecret `json:"data"`
	}
	if err := c.Post(ctx, deviceSharedSecretsPath(org, product, identifier), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteDeviceSharedSecret deactivates one of a device's active shared
// secrets via
// DELETE /orgs/{org}/products/{product}/devices/{identifier}/shared_secrets/{key}.
// The server keeps the key, listed with its deactivation time, and closes any
// connection made with it. An unknown or already deactivated key is a 404.
func (c *Client) DeleteDeviceSharedSecret(ctx context.Context, org, product, identifier, key string) error {
	if err := requireSharedSecretScope(org, product, identifier); err != nil {
		return err
	}
	if key == "" {
		return errors.New("api: shared secret key is required")
	}
	// A key travels as one path segment. "." and ".." would be read as dot
	// segments, and anything normalizing the path (a proxy, for one) would
	// then send the DELETE to the device or product itself. Real keys are
	// "nhd_" plus URL-safe base64, so they never take these forms.
	if key == "." || key == ".." || strings.Contains(key, "/") {
		return fmt.Errorf("api: invalid shared secret key %q", key)
	}
	return c.Delete(ctx, deviceSharedSecretsPath(org, product, identifier)+"/"+url.PathEscape(key))
}

func requireSharedSecretScope(org, product, identifier string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if product == "" {
		return errors.New("api: product is required")
	}
	if identifier == "" {
		return errors.New("api: device identifier is required")
	}
	return nil
}

func deviceSharedSecretsPath(org, product, identifier string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) +
		"/devices/" + url.PathEscape(identifier) + "/shared_secrets"
}
