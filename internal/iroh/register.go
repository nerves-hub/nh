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

package iroh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/tmc/go-iroh/key"
)

// EndpointOptions says which organization to register this machine's iroh
// endpoint with, and where its keys are kept.
type EndpointOptions struct {
	// DataDir is nh's data directory, which holds the keys.
	DataDir string
	// Org is the organization the endpoint is for.
	Org string
	// Hostname names this machine in the registration's instance. Empty means
	// os.Hostname.
	Hostname string
	// Notify receives a line when an endpoint is registered. Nil discards it.
	Notify io.Writer
}

// EnsureOrgEndpoint returns this machine's iroh key for opts.Org, registering
// it with the organization, attached to the current user, if it is not
// already. The hosted relay only lets registered endpoints through.
//
// NervesHub holds two rules that shape this (see NervesHub.Devices.NetworkIdentity):
//
//   - a key belongs to one organization, anywhere: registering one that is
//     already held is refused with 409. So nh keeps a key per organization.
//   - a member has one endpoint per (service, instance) in an organization. So
//     each machine registers under its own instance name (see MachineInstance)
//     rather than "default", and a second laptop does not collide with the
//     first.
//
// Before keys were per organization, nh kept one per machine. That key is still
// used for the organization it is registered with, so an existing registration
// keeps working; every other organization gets a new key.
func EnsureOrgEndpoint(ctx context.Context, client *api.Client, opts EndpointOptions) (key.SecretKey, error) {
	if opts.Org == "" {
		return key.SecretKey{}, errors.New("iroh: an organization is required")
	}
	sk, registered, err := orgIdentity(ctx, client, opts)
	if err != nil || registered {
		return sk, err
	}
	if err := register(ctx, client, opts, sk); err != nil {
		return key.SecretKey{}, err
	}
	return sk, nil
}

// orgIdentity returns the key for opts.Org and whether it is known to be
// registered there already.
func orgIdentity(ctx context.Context, client *api.Client, opts EndpointOptions) (key.SecretKey, bool, error) {
	path := OrgIdentityPath(opts.DataDir, opts.Org)
	sk, ok, err := ReadIdentity(path)
	if err != nil {
		return key.SecretKey{}, false, err
	}
	if ok {
		registered, err := endpointRegistered(ctx, client, opts.Org, sk)
		return sk, registered, err
	}

	// No key for this organization yet. If the old per-machine key is the one
	// registered here, keep using it: that is the registration a user of an
	// earlier nh already has.
	//
	// An unreadable old key is skipped rather than reported: it only matters to
	// the one organization it may be registered with, and failing here would
	// stop every new organization over a file it never uses.
	legacy, ok, err := ReadIdentity(LegacyIdentityPath(opts.DataDir))
	if err == nil && ok {
		registered, err := endpointRegistered(ctx, client, opts.Org, legacy)
		if err != nil {
			return key.SecretKey{}, false, err
		}
		if registered {
			saved, err := writeIdentity(opts.DataDir, path, legacy)
			if err != nil {
				return key.SecretKey{}, false, err
			}
			// Another nh run may have saved a different key first; if so,
			// that one still needs checking, which registering does.
			return saved, saved.Bytes() == legacy.Bytes(), nil
		}
		// Otherwise the old key is registered with another organization, or
		// nowhere. Either way it is left alone and this organization gets its
		// own.
	}

	sk, err = LoadOrCreateIdentity(opts.DataDir, path)
	// A new key is not registered. If another nh run made and registered it
	// first, registering finds that out from the 409.
	return sk, false, err
}

// endpointRegistered reports whether sk's endpoint id is registered with org.
func endpointRegistered(ctx context.Context, client *api.Client, org string, sk key.SecretKey) (bool, error) {
	id := EndpointIDHex(sk.Public())
	existing, err := client.ListIrohEndpoints(ctx, org, api.IrohEndpointFilter{Search: id})
	if err != nil {
		return false, fmt.Errorf("checking this machine's iroh endpoint registration with %s: %w", org, err)
	}
	for _, e := range existing {
		if strings.EqualFold(e.Identifier, id) {
			return true, nil
		}
	}
	return false, nil
}

func register(ctx context.Context, client *api.Client, opts EndpointOptions, sk key.SecretKey) error {
	id := EndpointIDHex(sk.Public())

	me, err := client.CurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("resolving your account to register this machine's iroh endpoint: %w", err)
	}

	hostname := opts.Hostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	instance := MachineInstance(hostname, id)

	_, err = client.RegisterIrohEndpoint(ctx, opts.Org, api.IrohEndpointInput{
		Identifier: id,
		Instance:   instance,
		UserEmail:  me.Email,
	})
	if err == nil {
		if opts.Notify != nil {
			fmt.Fprintf(opts.Notify, "Registered this machine's iroh endpoint with %s as %q.\n", opts.Org, instance)
		}
		return nil
	}
	return registrationError(ctx, client, opts, sk, instance, me.Email, err)
}

// registrationError turns a refused registration into something that says what
// happened and what to do about it, rather than a bare status code.
func registrationError(ctx context.Context, client *api.Client, opts EndpointOptions, sk key.SecretKey, instance, email string, err error) error {
	id := EndpointIDHex(sk.Public())
	org := opts.Org

	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("registering this machine's iroh endpoint with %s: %w", org, err)
	}

	switch {
	case apiErr.StatusCode == http.StatusConflict || fieldTaken(apiErr, "service", "identifier"):
		// The key is held somewhere. If another nh run just registered it here
		// that is fine; otherwise it is held outside this organization, which
		// NervesHub will not say more about.
		if registered, lerr := endpointRegistered(ctx, client, org, sk); lerr == nil && registered {
			return nil
		}
		return fmt.Errorf("this machine's iroh key for %s (%s) is already registered outside %s, and NervesHub lets a key belong to only one organization. "+
			"That happens when an nh data directory is copied between machines, or an organization is renamed. "+
			"Remove %s and run this again to make a new key: %w",
			org, shortID(id), org, OrgIdentityPath(opts.DataDir, org), err)

	case apiErr.StatusCode == http.StatusUnprocessableEntity && fieldTaken(apiErr, "org_user_id", "instance"):
		// One endpoint per member per instance. MachineInstance includes the
		// key, so this is an older registration that happens to share it.
		msg := fmt.Sprintf("you already have an iroh endpoint named %q in %s, so this machine's key (%s) cannot be registered under that name", instance, org, shortID(id))
		if clash := instanceHolder(ctx, client, org, email, instance); clash != "" {
			msg += fmt.Sprintf(". If %s is a key this machine no longer uses, remove it with: nh iroh-endpoint delete %s --org %s", shortID(clash), clash, org)
		}
		return fmt.Errorf("%s: %w", msg, err)

	case apiErr.StatusCode == http.StatusUnprocessableEntity:
		return fmt.Errorf("NervesHub would not register this machine's iroh endpoint (%s) with %s for %s: %w", shortID(id), org, email, err)

	case apiErr.StatusCode == http.StatusForbidden:
		return fmt.Errorf("your role in %s does not allow registering iroh endpoints. Ask an organization admin to run: nh iroh-endpoint register %s --instance %s --user-email %s --org %s: %w",
			org, id, instance, email, org, err)

	default:
		return fmt.Errorf("registering this machine's iroh endpoint with %s: %w", org, err)
	}
}

// fieldTaken reports whether apiErr is a unique-constraint failure on any of
// fields. Ecto puts a composite constraint's error on its first field, so for
// (org_user_id, service, instance) that is org_user_id.
func fieldTaken(apiErr *api.APIError, fields ...string) bool {
	for _, f := range fields {
		for _, m := range apiErr.FieldErrors[f] {
			if strings.Contains(m, "already been taken") {
				return true
			}
		}
	}
	return false
}

// instanceHolder returns the endpoint id email holds under instance in org, or
// "" if it cannot be found.
func instanceHolder(ctx context.Context, client *api.Client, org, email, instance string) string {
	endpoints, err := client.ListIrohEndpoints(ctx, org, api.IrohEndpointFilter{Owner: "user"})
	if err != nil {
		return ""
	}
	for _, e := range endpoints {
		if e.Instance == instance && strings.EqualFold(e.Owner.UserEmail, email) {
			return e.Identifier
		}
	}
	return ""
}

// maxHostLabel bounds the hostname part of an instance name. NervesHub allows
// 255 characters; an instance is a label people read in a table.
const maxHostLabel = 40

// MachineInstance names this machine's registration in an organization:
// "nh-<host>-<first 8 hex of the endpoint id>".
//
// NervesHub allows one endpoint per member per instance, so the name must
// differ between a member's machines. The hostname makes it readable; the key
// prefix keeps two machines with the same default hostname apart. With no
// usable hostname it is "nh-<key prefix>".
func MachineInstance(hostname, endpointID string) string {
	short := shortID(endpointID)
	if host := sanitizeHost(hostname); host != "" {
		return "nh-" + host + "-" + short
	}
	return "nh-" + short
}

// sanitizeHost reduces a hostname to its first label, lowercased, with
// anything but letters, digits and single dashes removed.
func sanitizeHost(hostname string) string {
	host, _, _ := strings.Cut(strings.TrimSpace(hostname), ".")
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxHostLabel {
		s = strings.TrimRight(s[:maxHostLabel], "-")
	}
	return s
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
