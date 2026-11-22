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

// Archive is a supporting fwup file attached to a product, as returned by
// GET /api/orgs/{org}/products/{product}/archives[/{uuid}]. The fields are
// those NervesHubWeb.API.ArchiveJSON renders.
type Archive struct {
	UUID          string    `json:"uuid"`
	Version       string    `json:"version"`
	Platform      string    `json:"platform"`
	Architecture  string    `json:"architecture"`
	Author        string    `json:"author,omitempty"`
	Description   string    `json:"description,omitempty"`
	Misc          string    `json:"misc,omitempty"`
	VCSIdentifier string    `json:"vcs_identifier,omitempty"`
	Size          int64     `json:"size"`
	InsertedAt    Timestamp `json:"inserted_at"`
}

// archiveUploadField is the multipart field ArchiveController.create pops the
// uploaded file from.
const archiveUploadField = "archive"

// ListArchives returns the archives in a product via
// GET /orgs/{org}/products/{product}/archives.
func (c *Client) ListArchives(ctx context.Context, org, product string) ([]Archive, error) {
	if err := requireArchiveScope(org, product, "list archives"); err != nil {
		return nil, err
	}

	var resp struct {
		Data []Archive `json:"data"`
	}
	if err := c.Get(ctx, archivesPath(org, product), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetArchive returns a single archive by UUID via
// GET /orgs/{org}/products/{product}/archives/{uuid}.
func (c *Client) GetArchive(ctx context.Context, org, product, uuid string) (*Archive, error) {
	if err := requireArchiveScope(org, product, "get an archive"); err != nil {
		return nil, err
	}
	if uuid == "" {
		return nil, errors.New("api: archive uuid is required")
	}

	var resp struct {
		Data Archive `json:"data"`
	}
	path := archivesPath(org, product) + "/" + url.PathEscape(uuid)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UploadArchive uploads an fwup archive to a product via
// POST /orgs/{org}/products/{product}/archives as multipart/form-data, and
// returns the created archive. The file goes in the "archive" field, under
// filename's base name. The body is streamed, so large files are not buffered
// in memory.
func (c *Client) UploadArchive(ctx context.Context, org, product, filename string, r io.Reader) (*Archive, error) {
	if err := requireArchiveScope(org, product, "upload an archive"); err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	go func() {
		part, err := mw.CreateFormFile(archiveUploadField, filepath.Base(filename))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, r); err != nil {
			pw.CloseWithError(err)
			return
		}
		// Closing the multipart writer writes the trailing boundary; its
		// result (nil on success) closes the pipe.
		pw.CloseWithError(mw.Close())
	}()

	var resp struct {
		Data Archive `json:"data"`
	}
	err := c.postReader(ctx, archivesPath(org, product), contentType, pr, &resp)
	// Unblock the writer goroutine if the request ended before reading the
	// whole body (a refused connection, or an early error response).
	pr.Close()
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DownloadArchive streams an archive's file to dst via
// GET /orgs/{org}/products/{product}/archives/{uuid}/download. The server
// answers with a redirect to a signed storage URL, which the HTTP client
// follows. Go's client drops the Authorization header when the redirect goes
// to a different domain (but keeps it for the API host's own subdomains), so
// the token is not sent to a storage bucket. When progress is non-nil it receives the cumulative bytes streamed and
// the total from Content-Length (0 when unknown).
func (c *Client) DownloadArchive(ctx context.Context, org, product, uuid string, dst io.Writer, progress func(read, total int64)) error {
	if err := requireArchiveScope(org, product, "download an archive"); err != nil {
		return err
	}
	if uuid == "" {
		return errors.New("api: archive uuid is required")
	}

	path := archivesPath(org, product) + "/" + url.PathEscape(uuid) + "/download"
	return c.GetRaw(ctx, path, nil, dst, progress)
}

func requireArchiveScope(org, product, action string) error {
	if org == "" {
		return errors.New("api: org is required to " + action)
	}
	if product == "" {
		return errors.New("api: product is required to " + action)
	}
	return nil
}

func archivesPath(org, product string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/archives"
}
