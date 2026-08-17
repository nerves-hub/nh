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
	"fmt"
	"net/url"
)

// Org is a NervesCloud organization the authenticated user belongs to, as
// returned by GET /api/orgs. Products is populated only when products are
// requested (include=products).
type Org struct {
	Name       string       `json:"name"`
	InsertedAt Timestamp    `json:"inserted_at"`
	UpdatedAt  Timestamp    `json:"updated_at"`
	Products   []OrgProduct `json:"products,omitempty"`
}

// OrgProduct is a product summary embedded in an Org when products are
// included.
type OrgProduct struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ListOrgs returns the organizations the authenticated user belongs to via
// GET /orgs.
func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	return c.listOrgs(ctx, nil)
}

// GetOrg returns a single organization by name. There is no show-by-name
// endpoint, so it filters the user's orgs (requesting their products) and
// returns a not-found error when name is not among them.
func (c *Client) GetOrg(ctx context.Context, name string) (*Org, error) {
	if name == "" {
		return nil, fmt.Errorf("api: org name is required")
	}
	orgs, err := c.listOrgs(ctx, url.Values{"include": {"products"}})
	if err != nil {
		return nil, err
	}
	for i := range orgs {
		if orgs[i].Name == name {
			return &orgs[i], nil
		}
	}
	return nil, fmt.Errorf("organization %q not found (or you are not a member)", name)
}

// OrgMember is a user belonging to an organization, as returned by
// GET /api/orgs/{org}/users.
type OrgMember struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// ListOrgMembers returns the members of an organization via
// GET /orgs/{org}/users.
func (c *Client) ListOrgMembers(ctx context.Context, org string) ([]OrgMember, error) {
	if org == "" {
		return nil, fmt.Errorf("api: org name is required")
	}

	var resp struct {
		Data []OrgMember `json:"data"`
	}
	path := "/orgs/" + url.PathEscape(org) + "/users"
	if err := c.Get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetOrgMember returns a single member of an organization via
// GET /orgs/{org}/users/{email}.
func (c *Client) GetOrgMember(ctx context.Context, org, email string) (*OrgMember, error) {
	if org == "" {
		return nil, fmt.Errorf("api: org name is required")
	}
	if email == "" {
		return nil, fmt.Errorf("api: member email is required")
	}
	var resp struct {
		Data OrgMember `json:"data"`
	}
	if err := c.Get(ctx, orgUserPath(org, email), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// RemoveOrgMember removes a member from an organization via
// DELETE /orgs/{org}/users/{email}.
func (c *Client) RemoveOrgMember(ctx context.Context, org, email string) error {
	if org == "" {
		return fmt.Errorf("api: org name is required")
	}
	if email == "" {
		return fmt.Errorf("api: member email is required")
	}
	return c.Delete(ctx, orgUserPath(org, email))
}

// UpdateOrgMemberRole changes a member's role in an organization via
// PUT /orgs/{org}/users/{email} with body {"role": "..."} and returns the
// updated member.
func (c *Client) UpdateOrgMemberRole(ctx context.Context, org, email, role string) (*OrgMember, error) {
	if org == "" {
		return nil, fmt.Errorf("api: org name is required")
	}
	if email == "" {
		return nil, fmt.Errorf("api: member email is required")
	}
	if role == "" {
		return nil, fmt.Errorf("api: role is required")
	}

	body := struct {
		Role string `json:"role"`
	}{Role: role}

	var resp struct {
		Data OrgMember `json:"data"`
	}
	if err := c.Put(ctx, orgUserPath(org, email), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// InviteOrgMember invites a user to an organization via
// POST /orgs/{org}/users/invite with body {"email": ..., "role": ...}.
func (c *Client) InviteOrgMember(ctx context.Context, org, email, role string) error {
	if org == "" {
		return fmt.Errorf("api: org name is required")
	}
	if email == "" {
		return fmt.Errorf("api: invite email is required")
	}
	if role == "" {
		return fmt.Errorf("api: role is required")
	}

	body := struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}{Email: email, Role: role}

	path := "/orgs/" + url.PathEscape(org) + "/users/invite"
	return c.Post(ctx, path, body, nil)
}

func orgUserPath(org, email string) string {
	return "/orgs/" + url.PathEscape(org) + "/users/" + url.PathEscape(email)
}

func (c *Client) listOrgs(ctx context.Context, query url.Values) ([]Org, error) {
	var resp struct {
		Data []Org `json:"data"`
	}
	if err := c.Get(ctx, "/orgs", query, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}
