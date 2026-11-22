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

// Webhook is an organization webhook: an endpoint the organization's device
// events are sent to, as rendered by NervesHubWeb.API.WebhookJSON.
//
// Secret is only present in the responses to create and regenerate-secret
// (WebhookJSON.with_secret/1); the server never lists or shows it again.
type Webhook struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// Events are the event types delivered, such as "device.connected".
	Events []string `json:"events"`
	// AllProducts is true when the webhook sends every product's events,
	// including products added later. Otherwise Products names them.
	AllProducts bool     `json:"all_products"`
	Products    []string `json:"products"`
	Enabled     bool     `json:"enabled"`
	// DisabledReason says why NervesHub turned the webhook off, if it did.
	DisabledReason string `json:"disabled_reason"`
	// FailingSince is when deliveries started failing; nil while the
	// endpoint is accepting them.
	FailingSince *Timestamp `json:"failing_since"`
	LastError    string     `json:"last_error"`
	LastErrorAt  *Timestamp `json:"last_error_at"`
	InsertedAt   *Timestamp `json:"inserted_at"`
	UpdatedAt    *Timestamp `json:"updated_at"`
	// Secret is the Standard Webhooks signing secret ("whsec_..."), set only
	// by CreateWebhook and RegenerateWebhookSecret.
	Secret string `json:"secret,omitempty"`
}

// WebhookInput is the body of POST /orgs/{org}/webhooks and
// PUT /orgs/{org}/webhooks/{id}, matching the fields Webhook.changeset/3
// casts plus the product names NervesHub.Integrations.ProductScope reads.
// Nil fields are left out, so an update only changes what was set.
//
// Naming Products without AllProducts makes the server take all_products as
// false.
type WebhookInput struct {
	URL         *string  `json:"url,omitempty"`
	Description *string  `json:"description,omitempty"`
	Events      []string `json:"events,omitempty"`
	AllProducts *bool    `json:"all_products,omitempty"`
	Products    []string `json:"products,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// IsEmpty reports whether the input sets no field at all.
func (in WebhookInput) IsEmpty() bool {
	return in.URL == nil && in.Description == nil && in.Events == nil &&
		in.AllProducts == nil && in.Products == nil && in.Enabled == nil
}

// WebhookTestResult is how a webhook's endpoint answered a webhook.test
// event. The server answers 200 whether or not the endpoint accepted it.
type WebhookTestResult struct {
	// Delivered is true when the endpoint answered with a 2xx status.
	Delivered bool `json:"delivered"`
	// Status is the endpoint's response status, or nil when it gave none
	// (a timeout or connection failure).
	Status *int `json:"status"`
	// Error says why the delivery failed, if it did.
	Error string `json:"error"`
}

// ListWebhooks returns an organization's webhooks, oldest first, via
// GET /orgs/{org}/webhooks.
func (c *Client) ListWebhooks(ctx context.Context, org string) ([]Webhook, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list webhooks")
	}

	var resp struct {
		Data []Webhook `json:"data"`
	}
	if err := c.Get(ctx, webhooksPath(org), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetWebhook returns one webhook via GET /orgs/{org}/webhooks/{id}.
func (c *Client) GetWebhook(ctx context.Context, org string, id int) (*Webhook, error) {
	if err := checkWebhookScope(org, id); err != nil {
		return nil, err
	}

	var resp struct {
		Data Webhook `json:"data"`
	}
	if err := c.Get(ctx, webhookPath(org, id), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CreateWebhook adds a webhook via POST /orgs/{org}/webhooks. The returned
// webhook carries its signing secret, which the server returns only this once.
func (c *Client) CreateWebhook(ctx context.Context, org string, in WebhookInput) (*Webhook, error) {
	if org == "" {
		return nil, errors.New("api: org is required to create a webhook")
	}
	if in.URL == nil || *in.URL == "" {
		return nil, errors.New("api: webhook url is required")
	}
	if len(in.Events) == 0 {
		return nil, errors.New("api: at least one webhook event is required")
	}

	var resp struct {
		Data Webhook `json:"data"`
	}
	if err := c.Post(ctx, webhooksPath(org), in, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UpdateWebhook changes a webhook via PUT /orgs/{org}/webhooks/{id}. Only the
// fields set in `in` are sent; the server leaves the rest as they are. The
// signing secret is not changed.
func (c *Client) UpdateWebhook(ctx context.Context, org string, id int, in WebhookInput) (*Webhook, error) {
	if err := checkWebhookScope(org, id); err != nil {
		return nil, err
	}
	if in.IsEmpty() {
		return nil, errors.New("api: nothing to update")
	}

	var resp struct {
		Data Webhook `json:"data"`
	}
	if err := c.Put(ctx, webhookPath(org, id), in, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteWebhook removes a webhook via DELETE /orgs/{org}/webhooks/{id}.
// Deliveries already queued for it are dropped.
func (c *Client) DeleteWebhook(ctx context.Context, org string, id int) error {
	if err := checkWebhookScope(org, id); err != nil {
		return err
	}
	return c.Delete(ctx, webhookPath(org, id))
}

// RegenerateWebhookSecret replaces a webhook's signing secret via
// POST /orgs/{org}/webhooks/{id}/regenerate_secret and returns the webhook
// with the new secret. The old secret stops working at once.
func (c *Client) RegenerateWebhookSecret(ctx context.Context, org string, id int) (*Webhook, error) {
	if err := checkWebhookScope(org, id); err != nil {
		return nil, err
	}

	var resp struct {
		Data Webhook `json:"data"`
	}
	if err := c.Post(ctx, webhookPath(org, id)+"/regenerate_secret", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// TestWebhook sends a webhook.test event to a webhook via
// POST /orgs/{org}/webhooks/{id}/test and reports how its endpoint answered.
// The server waits for the endpoint, up to about 20 seconds.
func (c *Client) TestWebhook(ctx context.Context, org string, id int) (*WebhookTestResult, error) {
	if err := checkWebhookScope(org, id); err != nil {
		return nil, err
	}

	var resp struct {
		Data WebhookTestResult `json:"data"`
	}
	if err := c.Post(ctx, webhookPath(org, id)+"/test", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func checkWebhookScope(org string, id int) error {
	if org == "" {
		return errors.New("api: org is required for webhook requests")
	}
	if id <= 0 {
		return errors.New("api: webhook id must be a positive integer")
	}
	return nil
}

func webhooksPath(org string) string {
	return "/orgs/" + url.PathEscape(org) + "/webhooks"
}

func webhookPath(org string, id int) string {
	return webhooksPath(org) + "/" + strconv.Itoa(id)
}
