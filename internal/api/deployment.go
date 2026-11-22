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
	"fmt"
	"net/url"
	"strings"
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
	ArchiveUUID    string               `json:"archive_uuid"`
	DeltaUpdatable bool                 `json:"delta_updatable"`
	Notes          string               `json:"notes"`
	Conditions     DeploymentConditions `json:"conditions"`
	CurrentRelease *CurrentRelease      `json:"current_release,omitempty"`
}

// DeploymentConditions are the targeting rules for a deployment group.
// TagOperator is "and" (a device needs every tag) or "or" (any tag will do).
type DeploymentConditions struct {
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	TagOperator string   `json:"tag_operator"`
}

// CurrentRelease is the firmware release a deployment group is currently
// serving. Required releases are installed on the way to any newer release.
type CurrentRelease struct {
	Number     int       `json:"number"`
	Required   bool      `json:"required"`
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

// DeploymentInput holds the writable fields of a deployment group. Zero
// fields are left out of the request, so the same type serves create and
// partial update.
//
// The server splits these fields between two kinds of write. Firmware,
// Archive and Required describe a release: setting any of them adds a new
// release to the group. The rest are settings of the group itself. A single
// PUT that names a firmware or archive only creates the release and ignores
// every group setting, so UpdateDeployment sends the two separately.
type DeploymentInput struct {
	// Release fields (update only).
	Firmware string // firmware UUID
	Archive  string // archive UUID
	Required *bool  // mark the new release required

	// Group settings.
	State          string    // "on" or "off"
	Version        *string   // version condition; "" clears it
	Tags           *[]string // tag conditions; an empty slice clears them
	TagOperator    string    // "and" or "or"
	DeltaUpdatable *bool
	Notes          *string
}

// hasRelease reports whether in asks for a new release.
func (in DeploymentInput) hasRelease() bool {
	return in.Firmware != "" || in.Archive != "" || in.Required != nil
}

// hasGroupSettings reports whether in changes any setting of the group itself.
func (in DeploymentInput) hasGroupSettings() bool {
	return in.State != "" || in.Version != nil || in.Tags != nil ||
		in.TagOperator != "" || in.DeltaUpdatable != nil || in.Notes != nil
}

type conditionsBody struct {
	Tags        *[]string `json:"tags,omitempty"`
	Version     *string   `json:"version,omitempty"`
	TagOperator string    `json:"tag_operator,omitempty"`
}

func (in DeploymentInput) conditions() *conditionsBody {
	if in.Version == nil && in.Tags == nil && in.TagOperator == "" {
		return nil
	}
	return &conditionsBody{Tags: in.Tags, Version: in.Version, TagOperator: in.TagOperator}
}

// createBody is the flat POST body. DeploymentGroup.create_changeset casts
// name, delta_updatable and notes, and requires conditions; firmware is looked
// up by the controller. It does not cast is_active, so state is not sent here.
type createBody struct {
	Name           string          `json:"name"`
	Firmware       string          `json:"firmware"`
	Conditions     *conditionsBody `json:"conditions"`
	DeltaUpdatable *bool           `json:"delta_updatable,omitempty"`
	Notes          *string         `json:"notes,omitempty"`
}

// releaseBody is a PUT that the controller's release clause handles: it
// creates a DeploymentRelease from firmware, archive and required.
type releaseBody struct {
	Firmware string `json:"firmware"`
	Archive  string `json:"archive,omitempty"`
	Required *bool  `json:"required,omitempty"`
}

// groupBody is a PUT that goes through DeploymentGroup.update_changeset. It
// must never carry firmware or archive, or the controller treats it as a
// release and drops these fields.
type groupBody struct {
	State          string          `json:"state,omitempty"`
	DeltaUpdatable *bool           `json:"delta_updatable,omitempty"`
	Notes          *string         `json:"notes,omitempty"`
	Conditions     *conditionsBody `json:"conditions,omitempty"`
}

func (in DeploymentInput) groupBody() groupBody {
	return groupBody{
		State:          in.State,
		DeltaUpdatable: in.DeltaUpdatable,
		Notes:          in.Notes,
		Conditions:     in.conditions(),
	}
}

// CreateDeployment creates a deployment group via
// POST /orgs/{org}/products/{product}/deployments. name and firmware are
// required.
//
// The create endpoint always starts a group off, whatever state the body
// says. When in.State is "on", the group is turned on with a second request
// afterwards. If that request fails, the created group is returned along with
// an error that says it exists but is still off.
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
	if in.Archive != "" || in.Required != nil {
		return nil, errors.New("api: archive and required are set on a release; create the deployment, then update it")
	}

	// create_changeset does cast_embed(:conditions, required: true), so the
	// body always carries a conditions object, even with nothing in it.
	cond := in.conditions()
	if cond == nil {
		cond = &conditionsBody{}
	}
	if cond.Tags == nil {
		cond.Tags = &[]string{}
	}
	if cond.Version == nil {
		empty := ""
		cond.Version = &empty
	}

	body := createBody{
		Name:           name,
		Firmware:       in.Firmware,
		Conditions:     cond,
		DeltaUpdatable: in.DeltaUpdatable,
		Notes:          in.Notes,
	}

	var resp struct {
		Data Deployment `json:"data"`
	}
	if err := c.Post(ctx, deploymentsPath(org, product), body, &resp); err != nil {
		return nil, err
	}
	created := &resp.Data

	if in.State != "on" {
		return created, nil
	}
	updated, err := c.putDeployment(ctx, org, product, name, groupBody{State: "on"})
	if err != nil {
		return created, fmt.Errorf("deployment group %q was created but is still off, because turning it on failed: %w", name, err)
	}
	return updated, nil
}

// UpdateDeployment updates a deployment group via
// PUT /orgs/{org}/products/{product}/deployments/{name}. The body is wrapped in
// a "deployment" key and only the supplied fields are changed.
//
// A new release (Firmware, Archive, Required) and the group's own settings go
// in separate requests, because the server ignores the settings in a request
// that creates a release. They are sent in the order that never rolls a
// release out to devices it wasn't meant for:
//
//  1. the group's settings, including turning it off, but not turning it on;
//  2. the new release, once conditions and delta updates are in place;
//  3. turning the group on, last.
//
// Only the requests needed are sent. An archive with no firmware keeps the
// current release's firmware, which is read first. If a later request fails,
// the error says which changes have already taken effect.
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

	release, settings := in.hasRelease(), in.hasGroupSettings()
	if !release && !settings {
		return nil, errors.New("api: nothing to update")
	}
	if in.Required != nil && in.Firmware == "" && in.Archive == "" {
		return nil, errors.New("api: required applies to a new release, which needs a firmware or an archive")
	}

	// Without a release there is nothing to order, so one request does it.
	if !release {
		return c.putDeployment(ctx, org, product, name, in.groupBody())
	}

	rb := releaseBody{Firmware: in.Firmware, Archive: in.Archive, Required: in.Required}
	if rb.Firmware == "" {
		// A release always has a firmware (DeploymentRelease.new_changeset
		// requires it), so an archive on its own keeps the current one.
		current, err := c.GetDeployment(ctx, org, product, name)
		if err != nil {
			return nil, fmt.Errorf("looking up the current firmware of deployment group %q: %w", name, err)
		}
		if current.FirmwareUUID == "" {
			return nil, fmt.Errorf("deployment group %q has no current firmware to pair with the archive; pass a firmware too", name)
		}
		rb.Firmware = current.FirmwareUUID
	}

	turnOn := in.State == "on"
	before := in.groupBody()
	if turnOn {
		before.State = ""
	}

	var done []string
	if before != (groupBody{}) {
		if _, err := c.putDeployment(ctx, org, product, name, before); err != nil {
			return nil, err
		}
		done = append(done, "its settings were updated")
	}

	d, err := c.putDeployment(ctx, org, product, name, rb)
	if err != nil {
		return nil, partialUpdateError(name, done, "creating the new release", err)
	}
	done = append(done, "the new release was created")

	if turnOn {
		if d, err = c.putDeployment(ctx, org, product, name, groupBody{State: "on"}); err != nil {
			return nil, partialUpdateError(name, done, "turning it on", err)
		}
	}
	return d, nil
}

// partialUpdateError reports a failed step of UpdateDeployment along with the
// steps that had already taken effect.
func partialUpdateError(name string, done []string, step string, err error) error {
	if len(done) == 0 {
		return err
	}
	return fmt.Errorf("deployment group %q: %s, but %s failed: %w", name, strings.Join(done, " and "), step, err)
}

// putDeployment sends one PUT with body wrapped in a "deployment" key.
func (c *Client) putDeployment(ctx context.Context, org, product, name string, body any) (*Deployment, error) {
	wrapped := struct {
		Deployment any `json:"deployment"`
	}{Deployment: body}

	var resp struct {
		Data Deployment `json:"data"`
	}
	if err := c.Put(ctx, deploymentPath(org, product, name), wrapped, &resp); err != nil {
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
