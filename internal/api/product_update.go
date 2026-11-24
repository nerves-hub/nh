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
	"slices"
	"strings"
)

// UpdateToolFwup is the firmware format every product accepts. The web UI
// offers no way to turn it off, so the CLI never sends a list without it.
const UpdateToolFwup = "fwup"

// KnownUpdateTools is every firmware format the server knows about
// (NervesHub.Firmwares.UpdateTool.known/0). Each one except fwup must also be
// enabled on the instance before a product can accept it.
var KnownUpdateTools = []string{UpdateToolFwup, "esp-idf", "atomvm", "rauc"}

// updateToolEnvVars names the setting that enables each optional format on a
// NervesHub instance (NervesHub.Firmwares.UpdateTool.*_enabled?/0).
var updateToolEnvVars = map[string]string{
	"esp-idf": "ESP_IDF_FIRMWARE_ENABLED",
	"atomvm":  "ATOMVM_FIRMWARE_ENABLED",
	"rauc":    "RAUC_FIRMWARE_ENABLED",
}

// ProductUpdate is the body sent to PUT /orgs/{org}/products/{name}. The
// fields sit at the top level, not under a "product" key
// (ProductController.update reads them straight from params).
//
// Every field is optional and only the ones set are sent. The server rejects
// any field it does not recognise, so this must not grow fields it cannot set.
type ProductUpdate struct {
	// AllowedUpdateTools replaces the product's whole list of accepted
	// formats. Use WithFwup to keep fwup in it.
	AllowedUpdateTools           []string `json:"allowed_update_tools,omitempty"`
	RequireUniqueFirmwareVersion *bool    `json:"require_unique_firmware_version,omitempty"`
	AllowUnsignedESPIDFFirmware  *bool    `json:"allow_unsigned_esp_idf_firmware,omitempty"`
	AllowUnsignedAtomVMFirmware  *bool    `json:"allow_unsigned_atomvm_firmware,omitempty"`
	DefaultManagedUpdatesAllowed *bool    `json:"default_managed_updates_allowed,omitempty"`
}

// IsEmpty reports whether u would change nothing.
func (u ProductUpdate) IsEmpty() bool {
	return u.AllowedUpdateTools == nil &&
		u.RequireUniqueFirmwareVersion == nil &&
		u.AllowUnsignedESPIDFFirmware == nil &&
		u.AllowUnsignedAtomVMFirmware == nil &&
		u.DefaultManagedUpdatesAllowed == nil
}

// UpdateProduct changes a product's settings via
// PUT /orgs/{org}/products/{name} and returns the updated product.
//
// A product cannot be renamed. When the server refuses a format because the
// instance has not enabled it, the error says which setting enables it.
func (c *Client) UpdateProduct(ctx context.Context, org, name string, in ProductUpdate) (*Product, error) {
	if org == "" {
		return nil, errors.New("api: org is required to update a product")
	}
	if name == "" {
		return nil, errors.New("api: product name is required")
	}
	if in.IsEmpty() {
		return nil, errors.New("api: no product settings to update")
	}

	var resp struct {
		Data Product `json:"data"`
	}
	if err := c.Put(ctx, productPath(org, name), in, &resp); err != nil {
		return nil, explainUpdateToolError(err)
	}
	return &resp.Data, nil
}

// WithFwup returns tools with surrounding space trimmed, empty and repeated
// entries dropped, and fwup added at the front if it is missing.
func WithFwup(tools []string) []string {
	out := []string{UpdateToolFwup}
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// ChangeUpdateTools applies allow and disallow to the product's current list
// of formats and returns the new list, which always includes fwup. A nil
// current list (a server that does not report it) is read as fwup alone.
// fwup cannot be disallowed, and a tool cannot be both allowed and
// disallowed.
//
// The server never sees a disallowed name, so a typo there would otherwise
// pass as a no-op: a name that is neither in current nor a known tool is an
// error. Disallowing a known tool the product does not allow is a no-op.
func ChangeUpdateTools(current, allow, disallow []string) ([]string, error) {
	if err := CheckUpdateToolChanges(allow, disallow); err != nil {
		return nil, err
	}
	remove := make(map[string]bool)
	for _, t := range disallow {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !slices.Contains(current, t) && !slices.Contains(KnownUpdateTools, t) {
			return nil, fmt.Errorf("cannot disallow unknown update tool %q (known update tools: %s)",
				t, strings.Join(KnownUpdateTools, ", "))
		}
		remove[t] = true
	}

	var kept []string
	for _, t := range WithFwup(append(slices.Clone(current), allow...)) {
		if !remove[t] {
			kept = append(kept, t)
		}
	}
	return kept, nil
}

// CheckUpdateToolChanges reports the mistakes in allow and disallow that need
// no knowledge of the product: disallowing fwup, or naming a tool in both.
// ChangeUpdateTools runs it too; callers use it to fail before any request.
func CheckUpdateToolChanges(allow, disallow []string) error {
	remove := make(map[string]bool)
	for _, t := range disallow {
		t = strings.TrimSpace(t)
		if t == UpdateToolFwup {
			return errors.New("fwup cannot be disallowed: every product accepts fwup firmware")
		}
		if t != "" {
			remove[t] = true
		}
	}
	for _, t := range allow {
		if t = strings.TrimSpace(t); remove[t] {
			return fmt.Errorf("%q is both allowed and disallowed", t)
		}
	}
	return nil
}

// explainUpdateToolError adds a hint to the server's allowed_update_tools
// validation errors (NervesHub.Products.Product.validate_allowed_update_tools/1)
// so the user knows the fix is on the instance, or what the valid names are.
// The returned error still wraps the original *APIError.
func explainUpdateToolError(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	for _, msg := range apiErr.FieldErrors["allowed_update_tools"] {
		if instance, list, ok := notEnabledTools(msg); ok {
			var settings []string
			for _, t := range strings.Split(list, ",") {
				if env, ok := updateToolEnvVars[strings.TrimSpace(t)]; ok {
					settings = append(settings, env+"=true")
				}
			}
			hint := "an administrator must enable the format on " + instance + " first"
			if len(settings) > 0 {
				// all/0 reads these unless the instance pins :update_tools in
				// its config, so they are the usual fix rather than the only one.
				hint += " (usually " + strings.Join(settings, ", ") + ")"
			}
			return fmt.Errorf("%w\n%s", err, hint)
		}
		if strings.HasPrefix(msg, "unknown update tool") {
			return fmt.Errorf("%w\nknown update tools: %s", err, strings.Join(KnownUpdateTools, ", "))
		}
	}
	return err
}

// notEnabledTools splits the server's error for formats the instance has turned
// off, "not enabled on <instance>: <tool>, <tool>", into the instance's name
// and the tool list. The name is the instance's configured
// NervesHub.Branding.friendly_platform_name/0, such as NervesHub or
// NervesCloud, so it may contain anything, ": " included; the tool names never
// do, so the list starts after the last ": ".
func notEnabledTools(msg string) (instance, list string, ok bool) {
	const prefix = "not enabled on "
	rest, found := strings.CutPrefix(msg, prefix)
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, ": ")
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+2:], true
}
