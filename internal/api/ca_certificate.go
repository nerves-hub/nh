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
// /orgs/{org}/ca_certificates endpoints (NervesHubWeb.API.CACertificateJSON).
// The server sends no inserted_at or updated_at, although its OpenAPI schema
// lists them.
type CACertificate struct {
	Serial      string    `json:"serial"`
	Description string    `json:"description"`
	NotBefore   Timestamp `json:"not_before"`
	NotAfter    Timestamp `json:"not_after"`
	// JITP is the CA's just-in-time provisioning profile, or nil when the CA
	// has none.
	JITP *CACertificateJITP `json:"jitp"`
}

// CACertificateJITP is a CA certificate's just-in-time provisioning (JITP)
// profile. A device that connects with a certificate signed by the CA, and that
// NervesHub doesn't know yet, is created in ProductName with these tags and
// this description.
type CACertificateJITP struct {
	ProductName string   `json:"product_name"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
}

// CACertificateInput is what CreateCACertificate registers. Cert and
// VerificationCert are PEM; the verification certificate proves ownership of
// the CA's private key.
type CACertificateInput struct {
	Cert             []byte
	VerificationCert []byte
	Description      string
	// JITP, when set, gives the CA a just-in-time provisioning profile. The
	// server needs its ProductName: naming the product (rather than giving its
	// id) needs NervesHub v2.5.0 or later.
	JITP *CACertificateJITP
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

// CreateCACertificate registers a CA certificate with an organization via
// POST /orgs/{org}/ca_certificates with body {"cert": <base64>, "description":
// ..., "verification_cert": <base64>, "jitp": {...}} and returns the stored
// certificate.
func (c *Client) CreateCACertificate(ctx context.Context, org string, in CACertificateInput) (*CACertificate, error) {
	if org == "" {
		return nil, errors.New("api: org is required to register a CA certificate")
	}
	if len(in.Cert) == 0 {
		return nil, errors.New("api: certificate is required")
	}
	if len(in.VerificationCert) == 0 {
		return nil, errors.New("api: verification certificate is required")
	}
	if in.JITP != nil && in.JITP.ProductName == "" {
		return nil, errors.New("api: a JITP profile needs a product")
	}

	type jitpBody struct {
		ProductName string   `json:"product_name"`
		Tags        []string `json:"tags,omitempty"`
		Description string   `json:"description,omitempty"`
	}
	body := struct {
		Cert             string    `json:"cert"`
		Description      string    `json:"description,omitempty"`
		VerificationCert string    `json:"verification_cert"`
		JITP             *jitpBody `json:"jitp,omitempty"`
	}{
		Cert:             base64.StdEncoding.EncodeToString(in.Cert),
		Description:      in.Description,
		VerificationCert: base64.StdEncoding.EncodeToString(in.VerificationCert),
	}
	if in.JITP != nil {
		body.JITP = &jitpBody{
			ProductName: in.JITP.ProductName,
			Tags:        in.JITP.Tags,
			Description: in.JITP.Description,
		}
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
