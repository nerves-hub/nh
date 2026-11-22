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
	"net/http"
	"net/url"
)

// ErrCertificateNotRecognized is returned by LookupDeviceByCertificate when
// the server finds no device for the certificate. NervesHub gives the same
// answer (403 {"status":"Unauthorized"}) for a certificate it doesn't know,
// one whose device is in another org or was deleted, one whose device the
// caller's role can't see, and a body it can't decode.
var ErrCertificateNotRecognized = errors.New("certificate not recognized")

// LookupDeviceByCertificate asks which device a certificate belongs to via
// POST /orgs/{org}/products/{product}/devices/auth with body
// {"certificate": <base64 of the PEM>}.
//
// The server matches the certificate exactly, by its SHA-1 fingerprint,
// against the certificates registered to devices in the org: it does not
// check a signature or the signer CA, and it needs no private key. The match
// is org-wide, so the device it returns may be in another product than the one
// in the path.
func (c *Client) LookupDeviceByCertificate(ctx context.Context, org, product string, certPEM []byte) (*Device, error) {
	if org == "" {
		return nil, errors.New("api: org is required to look up a certificate")
	}
	if product == "" {
		return nil, errors.New("api: product is required to look up a certificate")
	}
	if len(certPEM) == 0 {
		return nil, errors.New("api: certificate is required")
	}

	body := map[string]string{"certificate": base64.StdEncoding.EncodeToString(certPEM)}
	var resp struct {
		Data Device `json:"data"`
	}
	path := "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/devices/auth"
	if err := c.Post(ctx, path, body, &resp); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && apiErr.Message == "Unauthorized" {
			return nil, ErrCertificateNotRecognized
		}
		return nil, err
	}
	return &resp.Data, nil
}
