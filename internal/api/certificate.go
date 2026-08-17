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
	"encoding/base64"
	"errors"
	"net/url"
)

// Certificate is an X.509 device certificate, as returned by
// GET /api/orgs/{org}/products/{product}/devices/{identifier}/certificates.
type Certificate struct {
	Serial    string    `json:"serial"`
	NotBefore Timestamp `json:"not_before"`
	NotAfter  Timestamp `json:"not_after"`
}

// ListDeviceCertificates returns the certificates associated with a device via
// GET /orgs/{org}/products/{product}/devices/{identifier}/certificates.
func (c *Client) ListDeviceCertificates(ctx context.Context, org, product, identifier string) ([]Certificate, error) {
	if err := requireCertScope(org, product, identifier); err != nil {
		return nil, err
	}

	var resp struct {
		Data []Certificate `json:"data"`
	}
	if err := c.Get(ctx, deviceCertsPath(org, product, identifier), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetDeviceCertificate returns a single device certificate by serial via
// GET /orgs/{org}/products/{product}/devices/{identifier}/certificates/{serial}.
func (c *Client) GetDeviceCertificate(ctx context.Context, org, product, identifier, serial string) (*Certificate, error) {
	if err := requireCertScope(org, product, identifier); err != nil {
		return nil, err
	}
	if serial == "" {
		return nil, errors.New("api: certificate serial is required")
	}

	var resp struct {
		Data Certificate `json:"data"`
	}
	path := deviceCertsPath(org, product, identifier) + "/" + url.PathEscape(serial)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CreateDeviceCertificate registers a PEM certificate with a device via
// POST /orgs/{org}/products/{product}/devices/{identifier}/certificates with
// body {"cert": <base64 of the PEM>} and returns the stored certificate.
func (c *Client) CreateDeviceCertificate(ctx context.Context, org, product, identifier string, certPEM []byte) (*Certificate, error) {
	if err := requireCertScope(org, product, identifier); err != nil {
		return nil, err
	}
	if len(certPEM) == 0 {
		return nil, errors.New("api: certificate is required")
	}

	body := struct {
		Cert string `json:"cert"`
	}{Cert: base64.StdEncoding.EncodeToString(certPEM)}

	var resp struct {
		Data Certificate `json:"data"`
	}
	if err := c.Post(ctx, deviceCertsPath(org, product, identifier), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteDeviceCertificate removes a certificate from a device via
// DELETE /orgs/{org}/products/{product}/devices/{identifier}/certificates/{serial}.
func (c *Client) DeleteDeviceCertificate(ctx context.Context, org, product, identifier, serial string) error {
	if err := requireCertScope(org, product, identifier); err != nil {
		return err
	}
	if serial == "" {
		return errors.New("api: certificate serial is required")
	}
	return c.Delete(ctx, deviceCertsPath(org, product, identifier)+"/"+url.PathEscape(serial))
}

func requireCertScope(org, product, identifier string) error {
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

func deviceCertsPath(org, product, identifier string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) +
		"/devices/" + url.PathEscape(identifier) + "/certificates"
}
