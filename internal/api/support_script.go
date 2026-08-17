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
	"strconv"
)

// SupportScript is a product support script, as returned by the scripts
// endpoints. List responses populate only ID, Name, and Tags; the show response
// adds Text, timestamps, and CreatedBy.
type SupportScript struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Tags       string             `json:"tags"`
	Text       string             `json:"text,omitempty"`
	InsertedAt *Timestamp         `json:"inserted_at,omitempty"`
	UpdatedAt  *Timestamp         `json:"updated_at,omitempty"`
	CreatedBy  *SupportScriptUser `json:"created_by,omitempty"`
}

// SupportScriptUser is the user that created a support script.
type SupportScriptUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SupportScriptInput is the create/update body. Empty fields are omitted, so an
// update only changes the fields provided.
type SupportScriptInput struct {
	Name string `json:"name,omitempty"`
	Tags string `json:"tags,omitempty"`
	Text string `json:"text,omitempty"`
}

// SupportScriptList is a page of support scripts plus pagination metadata.
type SupportScriptList struct {
	Scripts    []SupportScript `json:"data"`
	Pagination *Pagination     `json:"pagination,omitempty"`
}

// ListSupportScriptsOptions controls pagination of the script list.
type ListSupportScriptsOptions struct {
	Page     int
	PageSize int
}

// ListSupportScripts returns a page of support scripts for a product via
// GET /orgs/{org}/products/{product}/scripts.
func (c *Client) ListSupportScripts(ctx context.Context, org, product string, opts ListSupportScriptsOptions) (*SupportScriptList, error) {
	if err := requireOrgProduct(org, product); err != nil {
		return nil, err
	}

	query := url.Values{}
	if opts.Page > 0 {
		query.Set("pagination[page]", strconv.Itoa(opts.Page))
	}
	if opts.PageSize > 0 {
		query.Set("pagination[page_size]", strconv.Itoa(opts.PageSize))
	}

	var result SupportScriptList
	if err := c.Get(ctx, scriptsPath(org, product), query, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSupportScript returns a single support script by id via
// GET /orgs/{org}/products/{product}/scripts/{id}.
func (c *Client) GetSupportScript(ctx context.Context, org, product, id string) (*SupportScript, error) {
	if err := requireScriptID(org, product, id); err != nil {
		return nil, err
	}
	var resp struct {
		Data SupportScript `json:"data"`
	}
	if err := c.Get(ctx, scriptPath(org, product, id), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CreateSupportScript creates a support script via
// POST /orgs/{org}/products/{product}/scripts.
func (c *Client) CreateSupportScript(ctx context.Context, org, product string, in SupportScriptInput) (*SupportScript, error) {
	if err := requireOrgProduct(org, product); err != nil {
		return nil, err
	}
	var resp struct {
		Data SupportScript `json:"data"`
	}
	if err := c.Post(ctx, scriptsPath(org, product), in, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UpdateSupportScript updates a support script via
// PUT /orgs/{org}/products/{product}/scripts/{id}.
func (c *Client) UpdateSupportScript(ctx context.Context, org, product, id string, in SupportScriptInput) (*SupportScript, error) {
	if err := requireScriptID(org, product, id); err != nil {
		return nil, err
	}
	var resp struct {
		Data SupportScript `json:"data"`
	}
	if err := c.Put(ctx, scriptPath(org, product, id), in, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteSupportScript deletes a support script via
// DELETE /orgs/{org}/products/{product}/scripts/{id}.
func (c *Client) DeleteSupportScript(ctx context.Context, org, product, id string) error {
	if err := requireScriptID(org, product, id); err != nil {
		return err
	}
	return c.Delete(ctx, scriptPath(org, product, id))
}

func requireOrgProduct(org, product string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if product == "" {
		return errors.New("api: product is required")
	}
	return nil
}

func requireScriptID(org, product, id string) error {
	if err := requireOrgProduct(org, product); err != nil {
		return err
	}
	if id == "" {
		return errors.New("api: support script id is required")
	}
	return nil
}

func scriptsPath(org, product string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/scripts"
}

func scriptPath(org, product, id string) string {
	return scriptsPath(org, product) + "/" + url.PathEscape(id)
}
