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
	"net/http"
	"slices"
	"strconv"
)

// DeploymentRelease is one release of a deployment group, as returned by the
// /orgs/{org}/products/{product}/deployments/{name}/releases endpoints. A
// release is named by its Number, which counts up from 1 within the group.
type DeploymentRelease struct {
	Number      int      `json:"number"`
	Firmware    Firmware `json:"firmware"`
	ArchiveUUID string   `json:"archive_uuid"`
	Required    bool     `json:"required"`
	Description string   `json:"description"`
	Notes       string   `json:"notes"`
	// ConnectingCode is Elixir run on a device when it connects, as long as
	// this release is the one it is sent. ConnectingCodeMode says where it
	// runs relative to the group's own connecting code: "first", "last" or
	// "override".
	ConnectingCode     string `json:"connecting_code"`
	ConnectingCodeMode string `json:"connecting_code_mode"`
	// DeltaStatus is "ready", "preparing" or "failed".
	DeltaStatus string `json:"delta_status"`
	IsCurrent   bool   `json:"is_current"`
	// CreatedBy is nil for releases made before the server recorded authors.
	CreatedBy  *ReleaseAuthor `json:"created_by"`
	InsertedAt Timestamp      `json:"inserted_at"`
	UpdatedAt  Timestamp      `json:"updated_at"`
}

// ReleaseAuthor is who created a release: a user (Type "user", with an
// Email) or a product API key (Type "api_key").
type ReleaseAuthor struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// ErrReleasesUnsupported is returned by the release methods when the server
// has no release endpoints. Those servers answer every release route with
// 404, which the methods tell apart from a missing group or release by
// asking for the group itself.
var ErrReleasesUnsupported = errors.New("this NervesHub server doesn't support deployment group releases; upgrade NervesHub to use them")

// ConnectingCodeModes are the values a release's connecting_code_mode takes.
var ConnectingCodeModes = []string{"first", "last", "override"}

// ReleaseInput is a new release. Firmware is required. A release has no
// archive unless Archive is set, whatever the previous release had.
type ReleaseInput struct {
	Firmware           string // firmware UUID
	Archive            string // archive UUID
	Required           *bool
	Description        string
	Notes              string
	ConnectingCode     string
	ConnectingCodeMode string // "first", "last" or "override"; the server defaults to "last"
}

// ReleaseUpdate holds what can still change on a release once it's made. Nil
// and empty fields are left out of the request and keep their value.
type ReleaseUpdate struct {
	Required *bool
	// ConnectingCode replaces the release's connecting code; a pointer to ""
	// removes it.
	ConnectingCode     *string
	ConnectingCodeMode string
}

// releaseCreateBody is the flat POST body. DeploymentReleaseController.create
// looks firmware and archive up by UUID and takes required, description,
// notes, connecting_code and connecting_code_mode.
type releaseCreateBody struct {
	Firmware           string `json:"firmware"`
	Archive            string `json:"archive,omitempty"`
	Required           *bool  `json:"required,omitempty"`
	Description        string `json:"description,omitempty"`
	Notes              string `json:"notes,omitempty"`
	ConnectingCode     string `json:"connecting_code,omitempty"`
	ConnectingCodeMode string `json:"connecting_code_mode,omitempty"`
}

// ListDeploymentReleases returns a deployment group's releases, newest first,
// via GET /orgs/{org}/products/{product}/deployments/{name}/releases.
func (c *Client) ListDeploymentReleases(ctx context.Context, org, product, name string) ([]DeploymentRelease, error) {
	if err := checkReleaseScope(org, product, name); err != nil {
		return nil, err
	}
	releases, err := c.listReleases(ctx, org, product, name)
	if err != nil {
		return nil, c.explainReleaseNotFound(ctx, org, product, name, 0, err)
	}
	return releases, nil
}

// GetDeploymentRelease returns one of a deployment group's releases by number
// via GET /orgs/{org}/products/{product}/deployments/{name}/releases/{number}.
func (c *Client) GetDeploymentRelease(ctx context.Context, org, product, name string, number int) (*DeploymentRelease, error) {
	if err := checkReleaseScope(org, product, name); err != nil {
		return nil, err
	}
	if number < 1 {
		return nil, errors.New("api: release number must be 1 or more")
	}

	var resp struct {
		Data DeploymentRelease `json:"data"`
	}
	if err := c.Get(ctx, releasePath(org, product, name, number), nil, &resp); err != nil {
		return nil, c.explainReleaseNotFound(ctx, org, product, name, number, err)
	}
	return &resp.Data, nil
}

// CreateDeploymentRelease creates a release, which becomes the group's current
// one, via POST /orgs/{org}/products/{product}/deployments/{name}/releases.
// The server refuses one with the same firmware and archive as the current
// release.
func (c *Client) CreateDeploymentRelease(ctx context.Context, org, product, name string, in ReleaseInput) (*DeploymentRelease, error) {
	if err := checkReleaseScope(org, product, name); err != nil {
		return nil, err
	}
	if in.Firmware == "" {
		return nil, errors.New("api: firmware is required to create a release")
	}
	if err := checkConnectingCodeMode(in.ConnectingCodeMode); err != nil {
		return nil, err
	}

	r, err := c.postRelease(ctx, org, product, name, releaseCreateBody{
		Firmware:           in.Firmware,
		Archive:            in.Archive,
		Required:           in.Required,
		Description:        in.Description,
		Notes:              in.Notes,
		ConnectingCode:     in.ConnectingCode,
		ConnectingCodeMode: in.ConnectingCodeMode,
	})
	if err != nil {
		return nil, c.explainReleaseNotFound(ctx, org, product, name, 0, err)
	}
	return r, nil
}

// UpdateDeploymentRelease changes whether a release is required, or its
// connecting code, via
// PATCH /orgs/{org}/products/{product}/deployments/{name}/releases/{number}.
// A release's firmware, archive, description and notes can't change.
func (c *Client) UpdateDeploymentRelease(ctx context.Context, org, product, name string, number int, in ReleaseUpdate) (*DeploymentRelease, error) {
	if err := checkReleaseScope(org, product, name); err != nil {
		return nil, err
	}
	if number < 1 {
		return nil, errors.New("api: release number must be 1 or more")
	}
	if err := checkConnectingCodeMode(in.ConnectingCodeMode); err != nil {
		return nil, err
	}

	body := map[string]any{}
	if in.Required != nil {
		body["required"] = *in.Required
	}
	if in.ConnectingCode != nil {
		if *in.ConnectingCode == "" {
			body["connecting_code"] = nil
		} else {
			body["connecting_code"] = *in.ConnectingCode
		}
	}
	if in.ConnectingCodeMode != "" {
		body["connecting_code_mode"] = in.ConnectingCodeMode
	}
	if len(body) == 0 {
		return nil, errors.New("api: nothing to update on the release")
	}

	var resp struct {
		Data DeploymentRelease `json:"data"`
	}
	if err := c.Patch(ctx, releasePath(org, product, name, number), body, &resp); err != nil {
		return nil, c.explainReleaseNotFound(ctx, org, product, name, number, err)
	}
	return &resp.Data, nil
}

func (c *Client) listReleases(ctx context.Context, org, product, name string) ([]DeploymentRelease, error) {
	var resp struct {
		Data []DeploymentRelease `json:"data"`
	}
	if err := c.Get(ctx, releasesPath(org, product, name), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) postRelease(ctx context.Context, org, product, name string, body releaseCreateBody) (*DeploymentRelease, error) {
	var resp struct {
		Data DeploymentRelease `json:"data"`
	}
	if err := c.Post(ctx, releasesPath(org, product, name), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// explainReleaseNotFound turns the 404 a release endpoint answered into the
// reason for it. The server gives the same 404 body for a group or release it
// doesn't have as it does for a route it doesn't have, so the reason is found
// by asking for things whose answer differs:
//
//   - for one release (number > 0), the group's release list: if that
//     succeeds, the group exists and has no such release;
//   - then the group itself: if that succeeds, the server has no release
//     endpoints, since a server with them answers for any group it has;
//     if it is a 404 too, the group doesn't exist (or isn't visible).
//
// Any other error passes through unchanged, as does a 404 that a check can't
// explain.
func (c *Client) explainReleaseNotFound(ctx context.Context, org, product, name string, number int, err error) error {
	if !isNotFound(err) {
		return err
	}
	if number > 0 {
		_, listErr := c.listReleases(ctx, org, product, name)
		switch {
		case listErr == nil:
			return fmt.Errorf("deployment group %q has no release %d: %w", name, number, err)
		case !isNotFound(listErr):
			return err
		}
	}
	if _, groupErr := c.GetDeployment(ctx, org, product, name); groupErr != nil {
		if isNotFound(groupErr) {
			return fmt.Errorf("deployment group %q not found in %s/%s: %w", name, org, product, err)
		}
		return err
	}
	return ErrReleasesUnsupported
}

// sendRelease creates the release UpdateDeployment asks for. It uses
// POST .../releases where the server has it, and otherwise the release PUT
// that older servers take, which UpdateDeployment always sent before. A 404
// from the POST means no release endpoint only when the group is known to
// exist: groupKnown says an earlier request already found it, and otherwise
// it is looked up. It returns the group when the request that made the
// release answers with it, which only the PUT does.
func (c *Client) sendRelease(ctx context.Context, org, product, name string, rb releaseBody, groupKnown bool) (*Deployment, error) {
	_, err := c.postRelease(ctx, org, product, name, releaseCreateBody{
		Firmware: rb.Firmware,
		Archive:  rb.Archive,
		Required: rb.Required,
	})
	if err == nil || !isNotFound(err) {
		return nil, err
	}
	if !groupKnown {
		if _, groupErr := c.GetDeployment(ctx, org, product, name); groupErr != nil {
			if isNotFound(groupErr) {
				return nil, err
			}
			return nil, groupErr
		}
	}
	return c.putDeployment(ctx, org, product, name, rb)
}

// DeployFirmware gives a deployment group a new release of firmware, with no
// archive, and changes nothing else. Like UpdateDeployment it uses
// POST .../releases where the server has it and the release PUT otherwise, but
// it doesn't read the group back afterwards, so a failed read can't report a
// release that was made as a failure.
func (c *Client) DeployFirmware(ctx context.Context, org, product, name, firmware string) error {
	if err := checkReleaseScope(org, product, name); err != nil {
		return err
	}
	if firmware == "" {
		return errors.New("api: firmware is required to deploy")
	}
	_, err := c.sendRelease(ctx, org, product, name, releaseBody{Firmware: firmware}, false)
	return err
}

func checkReleaseScope(org, product, name string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if product == "" {
		return errors.New("api: product is required")
	}
	if name == "" {
		return errors.New("api: deployment name is required")
	}
	return nil
}

func checkConnectingCodeMode(mode string) error {
	if mode != "" && !slices.Contains(ConnectingCodeModes, mode) {
		return fmt.Errorf("api: invalid connecting code mode %q (use first, last or override)", mode)
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func releasesPath(org, product, name string) string {
	return deploymentPath(org, product, name) + "/releases"
}

func releasePath(org, product, name string, number int) string {
	return releasesPath(org, product, name) + "/" + strconv.Itoa(number)
}
