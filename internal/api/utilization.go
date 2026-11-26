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

// ErrUtilizationUnsupported is returned when the server is too old to report
// a product's utilization. Such a server answers the route with 404, which
// GetUtilization tells apart from a missing product by asking for the product
// itself.
var ErrUtilizationUnsupported = errors.New("this NervesHub server doesn't report product utilization; upgrade NervesHub to use `nh checklist`")

// Utilization item statuses, groups and device authentication levels, as
// ProductJSON.utilization/1 in nerves_hub_web renders them.
const (
	UtilizationDone        = "done"
	UtilizationAttention   = "attention"
	UtilizationTodo        = "todo"
	UtilizationUnavailable = "unavailable"

	UtilizationGroupAuthentication = "authentication"
	UtilizationGroupRecommended    = "recommended"
	UtilizationGroupOptional       = "optional"

	UtilizationLevelGood    = "good"
	UtilizationLevelWarning = "warning"
)

// Utilization is which NervesHub features a product is using, as judged by
// the server (NervesHub.Products.Utilization in nerves_hub_web).
type Utilization struct {
	// Score counts the authentication and recommended items that are done
	// against those the server can offer.
	Score UtilizationScore `json:"score"`
	// DeviceAuth is how the product's devices authenticate, by method.
	DeviceAuth []UtilizationAuthMethod `json:"device_auth"`
	// Items is every feature checked, in the order the server shows them.
	Items []UtilizationItem `json:"items"`
}

// UtilizationScore is how many scored items are done.
type UtilizationScore struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// UtilizationAuthMethod is one way devices authenticate and how many do.
type UtilizationAuthMethod struct {
	// Key is "shared_secret", "local_cert" or "nerves_key".
	Key   string `json:"key"`
	Label string `json:"label"`
	// Level is "warning" for a method to move away from, otherwise "good". A
	// shared secret is "good" when each device using one has a secret of its own.
	Level   string `json:"level"`
	Devices int    `json:"devices"`
}

// UtilizationItem is one feature and what the server found for the product.
type UtilizationItem struct {
	Key string `json:"key"`
	// Group is "authentication", "recommended" or "optional". Optional items
	// are situational and never scored.
	Group       string `json:"group"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Status is "done", "attention" (a warning: set up, but risky or not
	// showing up yet), "todo" (not in use, never a warning) or "unavailable"
	// (the server can't offer the feature, so it isn't scored).
	Status  string `json:"status"`
	Summary string `json:"summary"`
	DocsURL string `json:"docs_url"`
}

// GetUtilization returns a product's utilization report via
// GET /orgs/{org}/products/{product}/utilization.
func (c *Client) GetUtilization(ctx context.Context, org, product string) (*Utilization, error) {
	if org == "" {
		return nil, errors.New("api: org is required to get a product's utilization")
	}
	if product == "" {
		return nil, errors.New("api: product is required to get a product's utilization")
	}

	var resp struct {
		Data Utilization `json:"data"`
	}
	productPath := "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product)
	err := c.Get(ctx, productPath+"/utilization", nil, &resp)
	if isNotFound(err) {
		// The product answering means it's the route that is missing.
		if probeErr := c.Get(ctx, productPath, nil, nil); probeErr == nil {
			return nil, ErrUtilizationUnsupported
		}
	}
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
