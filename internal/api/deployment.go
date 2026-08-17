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

// Deployment is a product's deployment group, as returned by the
// /orgs/{org}/products/{product}/deployments endpoints.
type Deployment struct {
	Name           string               `json:"name"`
	State          string               `json:"state"`
	IsActive       bool                 `json:"is_active"`
	DeviceCount    int                  `json:"device_count"`
	ReleasesCount  int                  `json:"releases_count"`
	FirmwareUUID   string               `json:"firmware_uuid"`
	DeltaUpdatable bool                 `json:"delta_updatable"`
	Conditions     DeploymentConditions `json:"conditions"`
	CurrentRelease *CurrentRelease      `json:"current_release,omitempty"`
}

// DeploymentConditions are the targeting rules for a deployment group.
type DeploymentConditions struct {
	Tags    []string `json:"tags"`
	Version string   `json:"version"`
}

// CurrentRelease is the firmware release a deployment group is currently
// serving.
type CurrentRelease struct {
	Number     int       `json:"number"`
	Firmware   Firmware  `json:"firmware"`
	InsertedAt Timestamp `json:"inserted_at"`
	UpdatedAt  Timestamp `json:"updated_at"`
}

// ListDeployments returns a product's deployment groups via
// GET /orgs/{org}/products/{product}/deployments.
func (c *Client) ListDeployments(ctx context.Context, org, product string) ([]Deployment, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list deployments")
	}
	if product == "" {
		return nil, errors.New("api: product is required to list deployments")
	}

	var resp struct {
		Data []Deployment `json:"data"`
	}
	if err := c.Get(ctx, deploymentsPath(org, product), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetDeployment returns a single deployment group by name via
// GET /orgs/{org}/products/{product}/deployments/{name}.
func (c *Client) GetDeployment(ctx context.Context, org, product, name string) (*Deployment, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if product == "" {
		return nil, errors.New("api: product is required")
	}
	if name == "" {
		return nil, errors.New("api: deployment name is required")
	}

	var resp struct {
		Data Deployment `json:"data"`
	}
	if err := c.Get(ctx, deploymentPath(org, product, name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteDeployment removes a deployment group by name via
// DELETE /orgs/{org}/products/{product}/deployments/{name}.
func (c *Client) DeleteDeployment(ctx context.Context, org, product, name string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if product == "" {
		return errors.New("api: product is required")
	}
	if name == "" {
		return errors.New("api: deployment name is required")
	}
	return c.Delete(ctx, deploymentPath(org, product, name))
}

// DeploymentInput holds the writable fields of a deployment group. Empty/nil
// fields are omitted from the request, so the same type serves create and
// partial update.
type DeploymentInput struct {
	Firmware       string   // firmware UUID
	State          string   // "on" or "off"
	Version        string   // version condition
	Tags           []string // tag conditions
	DeltaUpdatable *bool
}

// deploymentBody is the JSON shape of a deployment write. Name is only used on
// create.
type deploymentBody struct {
	Name           string          `json:"name,omitempty"`
	Firmware       string          `json:"firmware,omitempty"`
	State          string          `json:"state,omitempty"`
	DeltaUpdatable *bool           `json:"delta_updatable,omitempty"`
	Conditions     *conditionsBody `json:"conditions,omitempty"`
}

type conditionsBody struct {
	Tags    []string `json:"tags,omitempty"`
	Version string   `json:"version,omitempty"`
}

func (in DeploymentInput) body(name string) deploymentBody {
	b := deploymentBody{
		Name:           name,
		Firmware:       in.Firmware,
		State:          in.State,
		DeltaUpdatable: in.DeltaUpdatable,
	}
	if in.Version != "" || len(in.Tags) > 0 {
		b.Conditions = &conditionsBody{Tags: in.Tags, Version: in.Version}
	}
	return b
}

// CreateDeployment creates a deployment group via
// POST /orgs/{org}/products/{product}/deployments. name and firmware are
// required.
func (c *Client) CreateDeployment(ctx context.Context, org, product, name string, in DeploymentInput) (*Deployment, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if product == "" {
		return nil, errors.New("api: product is required")
	}
	if name == "" {
		return nil, errors.New("api: deployment name is required")
	}
	if in.Firmware == "" {
		return nil, errors.New("api: firmware is required to create a deployment")
	}

	var resp struct {
		Data Deployment `json:"data"`
	}
	if err := c.Post(ctx, deploymentsPath(org, product), in.body(name), &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UpdateDeployment updates a deployment group via
// PUT /orgs/{org}/products/{product}/deployments/{name}. The body is wrapped in
// a "deployment" key and only the supplied fields are changed.
func (c *Client) UpdateDeployment(ctx context.Context, org, product, name string, in DeploymentInput) (*Deployment, error) {
	if org == "" {
		return nil, errors.New("api: org is required")
	}
	if product == "" {
		return nil, errors.New("api: product is required")
	}
	if name == "" {
		return nil, errors.New("api: deployment name is required")
	}

	body := struct {
		Deployment deploymentBody `json:"deployment"`
	}{Deployment: in.body("")}

	var resp struct {
		Data Deployment `json:"data"`
	}
	if err := c.Put(ctx, deploymentPath(org, product, name), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func deploymentsPath(org, product string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/deployments"
}

func deploymentPath(org, product, name string) string {
	return deploymentsPath(org, product) + "/" + url.PathEscape(name)
}
