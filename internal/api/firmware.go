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
	"io"
	"mime/multipart"
	"net/url"
	"path/filepath"
)

// Firmware is a firmware image belonging to a product, as returned by
// GET /api/orgs/{org}/products/{product}/firmwares[/{uuid}].
//
// Only uuid/version/platform/architecture are documented; the remaining fields
// are inferred. TODO: confirm against a real response.
type Firmware struct {
	UUID          string    `json:"uuid"`
	Version       string    `json:"version"`
	Platform      string    `json:"platform"`
	Architecture  string    `json:"architecture"`
	Author        string    `json:"author,omitempty"`
	Description   string    `json:"description,omitempty"`
	Product       string    `json:"product,omitempty"`
	VCSIdentifier string    `json:"vcs_identifier,omitempty"`
	Misc          string    `json:"misc,omitempty"`
	FwupVersion   string    `json:"fwup_version,omitempty"`
	InsertedAt    Timestamp `json:"inserted_at"`
	UpdatedAt     Timestamp `json:"updated_at"`
}

// ListFirmwares returns the firmwares in a product via
// GET /orgs/{org}/products/{product}/firmwares.
func (c *Client) ListFirmwares(ctx context.Context, org, product string) ([]Firmware, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list firmwares")
	}
	if product == "" {
		return nil, errors.New("api: product is required to list firmwares")
	}

	var resp struct {
		Data []Firmware `json:"data"`
	}
	path := firmwaresPath(org, product)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetFirmware returns a single firmware by UUID via
// GET /orgs/{org}/products/{product}/firmwares/{uuid}.
func (c *Client) GetFirmware(ctx context.Context, org, product, uuid string) (*Firmware, error) {
	if org == "" {
		return nil, errors.New("api: org is required to get a firmware")
	}
	if product == "" {
		return nil, errors.New("api: product is required to get a firmware")
	}
	if uuid == "" {
		return nil, errors.New("api: firmware uuid is required")
	}

	var resp struct {
		Data Firmware `json:"data"`
	}
	path := firmwaresPath(org, product) + "/" + url.PathEscape(uuid)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UploadFirmware uploads a firmware image to a product via
// POST /orgs/{org}/products/{product}/firmwares as multipart/form-data, and
// returns the created firmware. The multipart body is streamed, so large images
// are not buffered in memory.
//
// TODO: confirm the multipart field name ("firmware") and response envelope
// against a real upload.
func (c *Client) UploadFirmware(ctx context.Context, org, product, filename string, r io.Reader) (*Firmware, error) {
	if org == "" {
		return nil, errors.New("api: org is required to upload a firmware")
	}
	if product == "" {
		return nil, errors.New("api: product is required to upload a firmware")
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	go func() {
		part, err := mw.CreateFormFile("firmware", filepath.Base(filename))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, r); err != nil {
			pw.CloseWithError(err)
			return
		}
		// Closing the multipart writer writes the trailing boundary; propagate
		// its result (nil on success) to close the pipe cleanly.
		pw.CloseWithError(mw.Close())
	}()

	var resp struct {
		Data Firmware `json:"data"`
	}
	if err := c.postReader(ctx, firmwaresPath(org, product), contentType, pr, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DownloadFirmware streams a firmware image to dst via
// GET /orgs/{org}/products/{product}/firmwares/{uuid}/download. When progress
// is non-nil it receives the cumulative bytes streamed and the total from
// Content-Length (0 when unknown).
func (c *Client) DownloadFirmware(ctx context.Context, org, product, uuid string, dst io.Writer, progress func(read, total int64)) error {
	if org == "" {
		return errors.New("api: org is required to download a firmware")
	}
	if product == "" {
		return errors.New("api: product is required to download a firmware")
	}
	if uuid == "" {
		return errors.New("api: firmware uuid is required")
	}

	path := firmwaresPath(org, product) + "/" + url.PathEscape(uuid) + "/download"
	return c.GetRaw(ctx, path, nil, dst, progress)
}

// DeleteFirmware deletes a firmware by UUID via
// DELETE /orgs/{org}/products/{product}/firmwares/{uuid}.
func (c *Client) DeleteFirmware(ctx context.Context, org, product, uuid string) error {
	if org == "" {
		return errors.New("api: org is required to delete a firmware")
	}
	if product == "" {
		return errors.New("api: product is required to delete a firmware")
	}
	if uuid == "" {
		return errors.New("api: firmware uuid is required")
	}

	path := firmwaresPath(org, product) + "/" + url.PathEscape(uuid)
	return c.Delete(ctx, path)
}

func firmwaresPath(org, product string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/firmwares"
}
