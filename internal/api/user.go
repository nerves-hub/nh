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
	"net/url"
)

// userAuthRequest is the body sent to POST /users/auth.
type userAuthRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Note is an optional human-readable label for the issued token.
	Note string `json:"note,omitempty"`
}

// User is the authenticated account associated with a token.
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// AuthResult is returned by Authenticate: the issued API token and, when the
// server provides it, the user it belongs to.
type AuthResult struct {
	Token string `json:"token"`
	User  User   `json:"-"`
}

// Authenticate exchanges an email and password for an API token via
// POST /users/auth and returns the token.
//
// TODO: confirm the endpoint path ("/users/auth"), request fields, and
// response envelope against the old nerves_hub_cli / NervesHub server before
// release — the migration treats the old CLI's wire behavior as the spec.
func (c *Client) Authenticate(ctx context.Context, email, password, note string) (*AuthResult, error) {
	var resp struct {
		Data struct {
			Token string `json:"token"`
			User
		} `json:"data"`
	}

	body := userAuthRequest{Email: email, Password: password, Note: note}
	if err := c.Post(ctx, "/users/login", body, &resp); err != nil {
		return nil, err
	}

	return &AuthResult{Token: resp.Data.Token, User: resp.Data.User}, nil
}

// CurrentUser returns the user associated with the client's token via
// GET /users/me.
//
// TODO: confirm the endpoint path and response envelope against the old
// nerves_hub_cli / NervesHub server before release.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var resp struct {
		Data User `json:"data"`
	}
	if err := c.Get(ctx, "/users/me", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CLISession is returned by StartCLISession: a session token to poll with, the
// URL the user must visit to confirm the login, and a confirmation code the
// user can use to visually verify the authenticity of the flow.
type CLISession struct {
	Token            string `json:"token"`
	URL              string `json:"url"`
	ConfirmationCode int    `json:"confirmation_code"`
}

// CLISessionStatus is returned when polling a CLI session. Status is "waiting"
// until the user confirms in the browser, then "ready" with UserToken set.
type CLISessionStatus struct {
	Status    string `json:"status"`
	UserToken string `json:"user_token"`
}

// cliSessionRequest is the body sent to POST /auth/cli_session. The note is a
// human-readable label (e.g. app, version, host) recorded with the token.
type cliSessionRequest struct {
	Note string `json:"note,omitempty"`
}

// StartCLISession begins a browser-confirmation login via
// POST /auth/cli_session. It is unauthenticated. note labels the issued token.
func (c *Client) StartCLISession(ctx context.Context, note string) (*CLISession, error) {
	var resp struct {
		Data CLISession `json:"data"`
	}
	if err := c.Post(ctx, "/auth/cli_session", cliSessionRequest{Note: note}, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CheckCLISession polls the status of a CLI session via
// GET /auth/cli_session/{token}.
func (c *Client) CheckCLISession(ctx context.Context, token string) (*CLISessionStatus, error) {
	var resp struct {
		Data CLISessionStatus `json:"data"`
	}
	if err := c.Get(ctx, "/auth/cli_session/"+url.PathEscape(token), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
