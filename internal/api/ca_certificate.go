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

// CACertificate is an organization CA certificate, as returned by the
// /orgs/{org}/ca_certificates endpoints.
type CACertificate struct {
	Serial      string    `json:"serial"`
	Description string    `json:"description"`
	NotBefore   Timestamp `json:"not_before"`
	NotAfter    Timestamp `json:"not_after"`
}

// CACertificateVerificationToken fetches a token used to prove ownership of a
// CA certificate, via GET /orgs/{org}/ca_certificates/verification_token. The
// token is embedded in a verification certificate signed by the CA and sent
// when registering it.
func (c *Client) CACertificateVerificationToken(ctx context.Context, org string) (string, error) {
	if org == "" {
		return "", errors.New("api: org is required")
	}
	var resp struct {
		Data struct {
			Token string `json:"verification_token"`
		} `json:"data"`
	}
	if err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/ca_certificates/verification_token", nil, &resp); err != nil {
		return "", err
	}
	return resp.Data.Token, nil
}

// CreateCACertificate registers a PEM CA certificate with an organization via
// POST /orgs/{org}/ca_certificates with body {"cert": <base64>, "description":
// ..., "verification_cert": <base64>} and returns the stored certificate. The
// verification certificate proves ownership of the CA's private key.
func (c *Client) CreateCACertificate(ctx context.Context, org string, certPEM []byte, description string, verificationCertPEM []byte) (*CACertificate, error) {
	if org == "" {
		return nil, errors.New("api: org is required to register a CA certificate")
	}
	if len(certPEM) == 0 {
		return nil, errors.New("api: certificate is required")
	}
	if len(verificationCertPEM) == 0 {
		return nil, errors.New("api: verification certificate is required")
	}

	body := struct {
		Cert             string `json:"cert"`
		Description      string `json:"description,omitempty"`
		VerificationCert string `json:"verification_cert"`
	}{
		Cert:             base64.StdEncoding.EncodeToString(certPEM),
		Description:      description,
		VerificationCert: base64.StdEncoding.EncodeToString(verificationCertPEM),
	}

	var resp struct {
		Data CACertificate `json:"data"`
	}
	if err := c.Post(ctx, "/orgs/"+url.PathEscape(org)+"/ca_certificates", body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListCACertificates returns an organization's CA certificates via
// GET /orgs/{org}/ca_certificates.
func (c *Client) ListCACertificates(ctx context.Context, org string) ([]CACertificate, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	var resp struct {
		Data []CACertificate `json:"data"`
	}
	if err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/ca_certificates", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetCACertificate returns one of an organization's CA certificates via
// GET /orgs/{org}/ca_certificates/{serial}.
func (c *Client) GetCACertificate(ctx context.Context, org, serial string) (*CACertificate, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if serial == "" {
		return nil, errors.New("api: serial is required")
	}
	var resp struct {
		Data CACertificate `json:"data"`
	}
	if err := c.Get(ctx, caCertPath(org, serial), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteCACertificate removes a CA certificate from an organization via
// DELETE /orgs/{org}/ca_certificates/{serial}.
func (c *Client) DeleteCACertificate(ctx context.Context, org, serial string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if serial == "" {
		return errors.New("api: serial is required")
	}
	return c.Delete(ctx, caCertPath(org, serial))
}

func caCertPath(org, serial string) string {
	return "/orgs/" + url.PathEscape(org) + "/ca_certificates/" + url.PathEscape(serial)
}
