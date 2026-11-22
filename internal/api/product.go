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

// Product is a NervesCloud product within an organization. The server renders
// the same shape for list, show, create and update (ProductJSON.product/1 in
// nerves_hub_web); it sends no id or timestamps.
//
// The settings fields are pointers, or omitted when empty, because servers
// that predate product settings do not send them: an absent setting is
// unknown, not false.
type Product struct {
	Name string `json:"name"`

	// AllowedUpdateTools is the firmware formats the product accepts, such as
	// "fwup", "esp-idf", "atomvm" or "rauc". fwup is always accepted.
	AllowedUpdateTools []string `json:"allowed_update_tools,omitempty"`
	// RequireUniqueFirmwareVersion rejects uploaded firmware whose version
	// already exists for the same platform and architecture.
	RequireUniqueFirmwareVersion *bool `json:"require_unique_firmware_version,omitempty"`
	// AllowUnsignedESPIDFFirmware accepts ESP-IDF images with no Secure Boot
	// v2 signature block.
	AllowUnsignedESPIDFFirmware *bool `json:"allow_unsigned_esp_idf_firmware,omitempty"`
	// AllowUnsignedAtomVMFirmware accepts AtomVM packbeam archives with no
	// signature entry.
	AllowUnsignedAtomVMFirmware *bool `json:"allow_unsigned_atomvm_firmware,omitempty"`
	// DefaultManagedUpdatesAllowed is the managed_updates_allowed grant a new
	// device in this product starts with.
	DefaultManagedUpdatesAllowed *bool `json:"default_managed_updates_allowed,omitempty"`
}

// ListProducts returns the products in org via GET /orgs/{org}/products.
func (c *Client) ListProducts(ctx context.Context, org string) ([]Product, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list products")
	}

	var resp struct {
		Data []Product `json:"data"`
	}
	path := "/orgs/" + url.PathEscape(org) + "/products"
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// productCreateRequest is the body sent to POST /orgs/{org}/products.
//
// The name is a top-level field. The published OpenAPI spec documents a
// {"product":{"name":...}} wrapper, but ProductController.create reads
// Map.take(params, ["name"]), so a wrapped name would never arrive.
type productCreateRequest struct {
	Name string `json:"name"`
}

// CreateProduct creates a product named name in org via
// POST /orgs/{org}/products and returns the created product.
func (c *Client) CreateProduct(ctx context.Context, org, name string) (*Product, error) {
	if org == "" {
		return nil, errors.New("api: org is required to create a product")
	}
	if name == "" {
		return nil, errors.New("api: product name is required")
	}

	var body productCreateRequest
	body.Name = name

	var resp struct {
		Data Product `json:"data"`
	}
	path := "/orgs/" + url.PathEscape(org) + "/products"
	if err := c.Post(ctx, path, body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetProduct returns a single product by name via
// GET /orgs/{org}/products/{name}.
func (c *Client) GetProduct(ctx context.Context, org, name string) (*Product, error) {
	if org == "" {
		return nil, errors.New("api: org is required to get a product")
	}
	if name == "" {
		return nil, errors.New("api: product name is required")
	}

	var resp struct {
		Data Product `json:"data"`
	}
	path := productPath(org, name)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteProduct deletes a product by name via
// DELETE /orgs/{org}/products/{name}.
func (c *Client) DeleteProduct(ctx context.Context, org, name string) error {
	if org == "" {
		return errors.New("api: org is required to delete a product")
	}
	if name == "" {
		return errors.New("api: product name is required")
	}
	return c.Delete(ctx, productPath(org, name))
}

func productPath(org, name string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(name)
}
