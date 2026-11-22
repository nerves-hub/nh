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
// POST /users/auth and returns the token and the user it belongs to. note
// labels the token. (POST /users/login is the deprecated alias of the same
// action; see NervesHubWeb.API.UserController.)
func (c *Client) Authenticate(ctx context.Context, email, password, note string) (*AuthResult, error) {
	var resp struct {
		Data struct {
			Token string `json:"token"`
			User
		} `json:"data"`
	}

	body := userAuthRequest{Email: email, Password: password, Note: note}
	if err := c.Post(ctx, "/users/auth", body, &resp); err != nil {
		return nil, err
	}

	return &AuthResult{Token: resp.Data.Token, User: resp.Data.User}, nil
}

// CurrentUser returns the user associated with the client's token via
// GET /users/me.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var resp struct {
		Data User `json:"data"`
	}
	if err := c.Get(ctx, "/users/me", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// CLISession is returned by StartCLISession: a session token to poll with,
// the web page where the user signs in (the same for every login), and the
// eight-digit confirmation code they type in there to find this login.
type CLISession struct {
	Token            string `json:"token"`
	URL              string `json:"url"`
	ConfirmationCode int    `json:"confirmation_code"`
}

// cliSessionRequest is the body sent to POST /auth/cli_session. The note is a
// human-readable label (e.g. app, version, host) recorded with the token.
type cliSessionRequest struct {
	Note string `json:"note,omitempty"`
}

// StartCLISession begins a CLI login via
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
