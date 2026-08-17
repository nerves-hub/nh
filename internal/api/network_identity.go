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

// NetworkIdentity is a key a device holds on a network NervesHub does not run
// (an iroh endpoint id, a NetBird / Tailscale / WireGuard public key), as
// returned by the network_identities and iroh_endpoints endpoints.
type NetworkIdentity struct {
	// Identifier is the value possession of which is proven — a public key.
	Identifier string `json:"identifier"`
	// Service is the protocol, e.g. "iroh", "netbird", "tailscale", "wireguard".
	Service string `json:"service"`
	// Instance names which endpoint of the service this is; "default" for
	// anything running a single one.
	Instance string `json:"instance"`
	// Source is whether anything proved the key: "device_reported" means a
	// device did on its own connection; "operator" means it was registered by
	// hand.
	Source string `json:"source"`
	// Details is per-service, non-authoritative metadata (relay URLs, an
	// assigned address). Never a secret.
	Details        map[string]any `json:"details,omitempty"`
	LastReportedAt Timestamp      `json:"last_reported_at"`
	InsertedAt     Timestamp      `json:"inserted_at"`
	UpdatedAt      Timestamp      `json:"updated_at"`
}

// IrohEndpoint is an iroh endpoint id registered to an organization: an
// NetworkIdentity with the owner spelled out.
type IrohEndpoint struct {
	NetworkIdentity
	Owner IrohEndpointOwner `json:"owner"`
}

// IrohEndpointOwner describes what holds an endpoint id. Type is what a caller
// should branch on ("device", "user", or "none"); the other fields are set only
// for the matching type.
type IrohEndpointOwner struct {
	Type             string `json:"type"`
	DeviceIdentifier string `json:"device_identifier,omitempty"`
	UserName         string `json:"user_name,omitempty"`
	UserEmail        string `json:"user_email,omitempty"`
}

// DeviceNetworkIdentityFilter narrows the identities returned for a device.
// Empty fields are omitted; the server rejects an unknown service or a blank
// non-empty filter is treated as no filter.
type DeviceNetworkIdentityFilter struct {
	Service  string
	Instance string
}

// ListDeviceNetworkIdentities returns the network identities a device has
// reported, via GET
// /orgs/{org}/products/{product}/devices/{identifier}/network_identities.
func (c *Client) ListDeviceNetworkIdentities(ctx context.Context, org, product, identifier string, filter DeviceNetworkIdentityFilter) ([]NetworkIdentity, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if product == "" {
		return nil, errors.New("api: product is required")
	}
	if identifier == "" {
		return nil, errors.New("api: device identifier is required")
	}

	query := url.Values{}
	if filter.Service != "" {
		query.Set("service", filter.Service)
	}
	if filter.Instance != "" {
		query.Set("instance", filter.Instance)
	}

	var resp struct {
		Data []NetworkIdentity `json:"data"`
	}
	if err := c.Get(ctx, devicePath(org, product, identifier)+"/network_identities", query, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// IrohEndpointFilter narrows an organization's iroh endpoint listing. Owner, if
// set, is one of "device", "user", or "none". Search matches the start of an
// endpoint id, or part of a device identifier or member name.
type IrohEndpointFilter struct {
	Owner  string
	Search string
}

// ListIrohEndpoints returns an organization's iroh endpoint ids via
// GET /orgs/{org}/iroh_endpoints.
func (c *Client) ListIrohEndpoints(ctx context.Context, org string, filter IrohEndpointFilter) ([]IrohEndpoint, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}

	query := url.Values{}
	if filter.Owner != "" {
		query.Set("owner", filter.Owner)
	}
	if filter.Search != "" {
		query.Set("search", filter.Search)
	}

	var resp struct {
		Data []IrohEndpoint `json:"data"`
	}
	if err := c.Get(ctx, irohEndpointsPath(org), query, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetIrohEndpoint returns a single iroh endpoint id via
// GET /orgs/{org}/iroh_endpoints/{identifier}.
func (c *Client) GetIrohEndpoint(ctx context.Context, org, identifier string) (*IrohEndpoint, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if identifier == "" {
		return nil, errors.New("api: endpoint identifier is required")
	}
	var resp struct {
		Data IrohEndpoint `json:"data"`
	}
	if err := c.Get(ctx, irohEndpointPath(org, identifier), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// IrohEndpointInput is the body for registering an iroh endpoint. Only
// Identifier is required; Instance defaults to "default" server-side, UserEmail
// attaches the endpoint to a member of the organization, and Details records
// non-authoritative metadata.
type IrohEndpointInput struct {
	Identifier string         `json:"identifier"`
	Instance   string         `json:"instance,omitempty"`
	UserEmail  string         `json:"user_email,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// RegisterIrohEndpoint registers an iroh endpoint id with an organization via
// POST /orgs/{org}/iroh_endpoints and returns the stored endpoint.
func (c *Client) RegisterIrohEndpoint(ctx context.Context, org string, in IrohEndpointInput) (*IrohEndpoint, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if in.Identifier == "" {
		return nil, errors.New("api: endpoint identifier is required")
	}
	var resp struct {
		Data IrohEndpoint `json:"data"`
	}
	if err := c.Post(ctx, irohEndpointsPath(org), in, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteIrohEndpoint removes an iroh endpoint id from an organization via
// DELETE /orgs/{org}/iroh_endpoints/{identifier}.
func (c *Client) DeleteIrohEndpoint(ctx context.Context, org, identifier string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if identifier == "" {
		return errors.New("api: endpoint identifier is required")
	}
	return c.Delete(ctx, irohEndpointPath(org, identifier))
}

func irohEndpointsPath(org string) string {
	return "/orgs/" + url.PathEscape(org) + "/iroh_endpoints"
}

func irohEndpointPath(org, identifier string) string {
	return irohEndpointsPath(org) + "/" + url.PathEscape(identifier)
}
