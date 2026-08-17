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
	"strings"
)

// Device is a device within a product, as returned by
// GET /api/orgs/{org}/products/{product}/devices.
type Device struct {
	Identifier          string            `json:"identifier"`
	ConnectionStatus    string            `json:"connection_status"`
	Online              string            `json:"online"`
	Description         string            `json:"description"`
	Tags                []string          `json:"tags"`
	Version             string            `json:"version"`
	OrgName             string            `json:"org_name"`
	ProductName         string            `json:"product_name"`
	UpdatesEnabled      bool              `json:"updates_enabled"`
	UpdatesBlockedUntil *Timestamp        `json:"updates_blocked_until,omitempty"`
	LastCommunication   *Timestamp        `json:"last_communication,omitempty"`
	FirmwareMetadata    *FirmwareMetadata `json:"firmware_metadata,omitempty"`
	DeploymentGroup     *DeploymentGroup  `json:"deployment_group,omitempty"`
}

// FirmwareMetadata describes the firmware currently running on a device.
type FirmwareMetadata struct {
	UUID          string `json:"uuid"`
	Version       string `json:"version"`
	Platform      string `json:"platform"`
	Architecture  string `json:"architecture"`
	Author        string `json:"author"`
	Description   string `json:"description"`
	FwupVersion   string `json:"fwup_version"`
	ID            string `json:"id"`
	Misc          string `json:"misc"`
	Product       string `json:"product"`
	VCSIdentifier string `json:"vcs_identifier"`
}

// DeploymentGroup is the deployment group a device belongs to, if any.
type DeploymentGroup struct {
	Name            string `json:"name"`
	FirmwareUUID    string `json:"firmware_uuid"`
	FirmwareVersion string `json:"firmware_version"`
	IsActive        bool   `json:"is_active"`
}

// DeviceList is a page of devices plus the pagination metadata returned by the
// list endpoint. Pagination is nil when the response carries no pagination
// object.
type DeviceList struct {
	Devices    []Device    `json:"data"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

// ListDevicesOptions controls pagination, sorting, and filtering of the device
// list. Zero values are omitted, letting the server apply its defaults.
type ListDevicesOptions struct {
	Page     int
	PageSize int
	// Sort is the column to order by; SortDirection is "asc" or "desc".
	Sort          string
	SortDirection string
	// Filters are passed through verbatim as filters[key]=value, so new
	// server-side filters work without client changes.
	Filters map[string]string
}

// ListDevices returns a page of devices in a product via
// GET /orgs/{org}/products/{product}/devices.
func (c *Client) ListDevices(ctx context.Context, org, product string, opts ListDevicesOptions) (*DeviceList, error) {
	if org == "" {
		return nil, errors.New("api: org is required to list devices")
	}
	if product == "" {
		return nil, errors.New("api: product is required to list devices")
	}

	// Pagination params are nested under a "pagination" map, using Phoenix's
	// bracket notation: pagination[page]=N&pagination[page_size]=M.
	query := url.Values{}
	if opts.Page > 0 {
		query.Set("pagination[page]", strconv.Itoa(opts.Page))
	}
	if opts.PageSize > 0 {
		query.Set("pagination[page_size]", strconv.Itoa(opts.PageSize))
	}

	// TODO: confirm the sort param names against the API codebase (assuming
	// top-level sort/sort_direction).
	if opts.Sort != "" {
		query.Set("sort", opts.Sort)
		if opts.SortDirection != "" {
			query.Set("sort_direction", opts.SortDirection)
		}
	}

	// Filters are nested under a "filters" map, matching the pagination
	// convention: filters[key]=value.
	for key, value := range opts.Filters {
		query.Set("filters["+key+"]", value)
	}

	var result DeviceList
	path := "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/devices"
	if err := c.Get(ctx, path, query, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetDevice returns a single device by identifier via
// GET /orgs/{org}/products/{product}/devices/{identifier}.
func (c *Client) GetDevice(ctx context.Context, org, product, identifier string) (*Device, error) {
	if org == "" {
		return nil, errors.New("api: org is required to get a device")
	}
	if product == "" {
		return nil, errors.New("api: product is required to get a device")
	}
	if identifier == "" {
		return nil, errors.New("api: device identifier is required")
	}

	var resp struct {
		Data Device `json:"data"`
	}
	path := "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) +
		"/devices/" + url.PathEscape(identifier)
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// RebootDevice asks a device to reboot via
// POST /orgs/{org}/products/{product}/devices/{identifier}/reboot.
func (c *Client) RebootDevice(ctx context.Context, org, product, identifier string) error {
	return c.deviceAction(ctx, org, product, identifier, "reboot")
}

// ReconnectDevice asks a device to reconnect via
// POST /orgs/{org}/products/{product}/devices/{identifier}/reconnect.
func (c *Client) ReconnectDevice(ctx context.Context, org, product, identifier string) error {
	return c.deviceAction(ctx, org, product, identifier, "reconnect")
}

// deviceAction issues a no-body POST to a device sub-action endpoint such as
// "reboot" or "reconnect". These return an empty 200 on success.
func (c *Client) deviceAction(ctx context.Context, org, product, identifier, action string) error {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return err
	}
	return c.Post(ctx, devicePath(org, product, identifier)+"/"+action, nil, nil)
}

// devicePath returns the path for a single device.
func devicePath(org, product, identifier string) string {
	return "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) +
		"/devices/" + url.PathEscape(identifier)
}

func requireDeviceScope(org, product, identifier string) error {
	if org == "" {
		return errors.New("api: org is required")
	}
	if product == "" {
		return errors.New("api: product is required")
	}
	if identifier == "" {
		return errors.New("api: device identifier is required")
	}
	return nil
}

// DeviceInput holds the writable fields of a device. Empty/nil fields are
// omitted, so it serves both create and partial update.
type DeviceInput struct {
	Description       string
	Tags              []string
	UpdatesEnabled    *bool
	DeploymentGroupID *int
}

// deviceBody is the JSON shape of a device write. Identifier is only used on
// create. tags are sent as a comma-separated string, as the API expects.
type deviceBody struct {
	Identifier        string `json:"identifier,omitempty"`
	Description       string `json:"description,omitempty"`
	Tags              string `json:"tags,omitempty"`
	UpdatesEnabled    *bool  `json:"updates_enabled,omitempty"`
	DeploymentGroupID *int   `json:"deployment_group_id,omitempty"`
}

func (in DeviceInput) body(identifier string) deviceBody {
	b := deviceBody{
		Identifier:        identifier,
		Description:       in.Description,
		UpdatesEnabled:    in.UpdatesEnabled,
		DeploymentGroupID: in.DeploymentGroupID,
	}
	if len(in.Tags) > 0 {
		b.Tags = strings.Join(in.Tags, ",")
	}
	return b
}

// CreateDevice registers a device via
// POST /orgs/{org}/products/{product}/devices/{identifier}, with the body
// wrapped in a "device" key.
func (c *Client) CreateDevice(ctx context.Context, org, product, identifier string, in DeviceInput) (*Device, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return nil, err
	}
	body := struct {
		Device deviceBody `json:"device"`
	}{Device: in.body(identifier)}

	var resp struct {
		Data Device `json:"data"`
	}
	if err := c.Post(ctx, devicePath(org, product, identifier), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UpdateDevice updates a device via
// PUT /orgs/{org}/products/{product}/devices/{identifier}. The body is wrapped
// in a "device" key and only the supplied fields are changed.
func (c *Client) UpdateDevice(ctx context.Context, org, product, identifier string, in DeviceInput) (*Device, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return nil, err
	}
	body := struct {
		Device deviceBody `json:"device"`
	}{Device: in.body("")}

	var resp struct {
		Data Device `json:"data"`
	}
	if err := c.Put(ctx, devicePath(org, product, identifier), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteDevice deletes a device via
// DELETE /orgs/{org}/products/{product}/devices/{identifier}.
func (c *Client) DeleteDevice(ctx context.Context, org, product, identifier string) error {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return err
	}
	return c.Delete(ctx, devicePath(org, product, identifier))
}

// UpgradeDevice asks a device to upgrade to a specific firmware via
// POST /orgs/{org}/products/{product}/devices/{identifier}/upgrade with body
// {"uuid": <firmware uuid>}.
func (c *Client) UpgradeDevice(ctx context.Context, org, product, identifier, firmwareUUID string) error {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return err
	}
	if firmwareUUID == "" {
		return errors.New("api: firmware uuid is required")
	}
	body := struct {
		UUID string `json:"uuid"`
	}{UUID: firmwareUUID}
	return c.Post(ctx, devicePath(org, product, identifier)+"/upgrade", body, nil)
}

// MoveDevice moves a device to a different product via
// POST /orgs/{org}/products/{product}/devices/{identifier}/move with the
// destination passed as query parameters. It returns the moved device.
func (c *Client) MoveDevice(ctx context.Context, org, product, identifier, newOrg, newProduct string) (*Device, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return nil, err
	}
	if newOrg == "" || newProduct == "" {
		return nil, errors.New("api: destination org and product are required")
	}
	q := url.Values{"new_org_name": {newOrg}, "new_product_name": {newProduct}}
	path := devicePath(org, product, identifier) + "/move?" + q.Encode()

	var resp struct {
		Data Device `json:"data"`
	}
	if err := c.Post(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ClearDevicePenalty clears a device's penalty box via
// DELETE /orgs/{org}/products/{product}/devices/{identifier}/penalty.
func (c *Client) ClearDevicePenalty(ctx context.Context, org, product, identifier string) error {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return err
	}
	return c.Delete(ctx, devicePath(org, product, identifier)+"/penalty")
}

// RunDeviceCode asks a device to run Elixir code in its console connection via
// POST /orgs/{org}/products/{product}/devices/{identifier}/code with body
// {"code": <elixir>}. Output appears in the device's console, not the response.
func (c *Client) RunDeviceCode(ctx context.Context, org, product, identifier, code string) error {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return err
	}
	if code == "" {
		return errors.New("api: code is required")
	}
	body := struct {
		Code string `json:"code"`
	}{Code: code}
	return c.Post(ctx, devicePath(org, product, identifier)+"/code", body, nil)
}

// ListDeviceScripts lists the support scripts available to a device via
// GET /orgs/{org}/products/{product}/devices/{identifier}/scripts.
func (c *Client) ListDeviceScripts(ctx context.Context, org, product, identifier string) ([]SupportScript, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return nil, err
	}
	var resp struct {
		Data []SupportScript `json:"data"`
	}
	if err := c.Get(ctx, devicePath(org, product, identifier)+"/scripts", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// SendDeviceScript runs a support script on a device via
// POST /orgs/{org}/products/{product}/devices/{identifier}/scripts/{name_or_id}
// and returns the script's text output.
func (c *Client) SendDeviceScript(ctx context.Context, org, product, identifier, nameOrID string) (string, error) {
	if err := requireDeviceScope(org, product, identifier); err != nil {
		return "", err
	}
	if nameOrID == "" {
		return "", errors.New("api: script name or id is required")
	}
	path := devicePath(org, product, identifier) + "/scripts/" + url.PathEscape(nameOrID)
	return c.postRawText(ctx, path)
}
