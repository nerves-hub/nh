package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const (
	testReleasesPath = testDeploymentPath + "/releases"
	// notFoundBody is what NervesHub answers for a missing route, group or
	// release alike (ErrorJSON "404.json").
	notFoundBody = `{"errors":{"detail":"Resource Not Found or Authorization Insufficient"}}`
	releaseJSON  = `{"number":3,"firmware":{"uuid":"fw-3","version":"1.0.1","platform":"rpi0","architecture":"arm"},
		"archive_uuid":null,"required":true,"description":"Fixes Wi-Fi","notes":null,
		"connecting_code":"Logger.info(:hi)","connecting_code_mode":"first","delta_status":"ready",
		"is_current":true,"created_by":{"type":"user","name":"Jane Doe","email":"jane@example.com"},
		"inserted_at":"2026-11-22T10:00:00Z","updated_at":"2026-11-22T10:05:00Z"}`
	groupJSON = `{"data":{"name":"production","firmware_uuid":"fw-1"}}`
)

// oldServer answers like a NervesHub without the release endpoints: every
// /releases route is a 404, and the rest are answered by respond.
func oldServer(respond func(r deploymentRequest) (int, string)) func(int, deploymentRequest) (int, string) {
	return func(_ int, r deploymentRequest) (int, string) {
		if strings.Contains(r.Path, "/releases") {
			return http.StatusNotFound, notFoundBody
		}
		return respond(r)
	}
}

func TestListDeploymentReleases(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":[` + releaseJSON + `,{"number":2,"firmware":{"uuid":"fw-2"},"created_by":null}]}`
	})

	releases, err := c.ListDeploymentReleases(context.Background(), "acme", "thermostat", "production")
	if err != nil {
		t.Fatalf("ListDeploymentReleases: %v", err)
	}
	if r := (*reqs)[0]; r.Method != http.MethodGet || r.Path != testReleasesPath {
		t.Errorf("request: %s %s", r.Method, r.Path)
	}
	if len(releases) != 2 {
		t.Fatalf("releases: %+v", releases)
	}
	r := releases[0]
	if r.Number != 3 || r.Firmware.UUID != "fw-3" || r.Firmware.Version != "1.0.1" || r.Firmware.Platform != "rpi0" ||
		r.Firmware.Architecture != "arm" || r.ArchiveUUID != "" || !r.Required || r.Description != "Fixes Wi-Fi" ||
		r.ConnectingCode != "Logger.info(:hi)" || r.ConnectingCodeMode != "first" || r.DeltaStatus != "ready" ||
		!r.IsCurrent || r.InsertedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Errorf("release: %+v", r)
	}
	if a := r.CreatedBy; a == nil || a.Type != "user" || a.Name != "Jane Doe" || a.Email != "jane@example.com" {
		t.Errorf("created_by: %+v", a)
	}
	if releases[1].CreatedBy != nil {
		t.Errorf("a null created_by should decode as nil, got %+v", releases[1].CreatedBy)
	}
}

func TestGetDeploymentRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":` + releaseJSON + `}`
	})

	r, err := c.GetDeploymentRelease(context.Background(), "acme", "thermostat", "production", 3)
	if err != nil {
		t.Fatalf("GetDeploymentRelease: %v", err)
	}
	if got := (*reqs)[0]; got.Method != http.MethodGet || got.Path != testReleasesPath+"/3" {
		t.Errorf("request: %s %s", got.Method, got.Path)
	}
	if r.Number != 3 || r.Firmware.UUID != "fw-3" {
		t.Errorf("release: %+v", r)
	}
}

func TestCreateDeploymentRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":` + releaseJSON + `}`
	})

	r, err := c.CreateDeploymentRelease(context.Background(), "acme", "thermostat", "production", ReleaseInput{
		Firmware:           "fw-3",
		Archive:            "ar-1",
		Required:           deploymentBool(false),
		Description:        "Fixes Wi-Fi",
		Notes:              "after the field test",
		ConnectingCode:     "Logger.info(:hi)",
		ConnectingCodeMode: "first",
	})
	if err != nil {
		t.Fatalf("CreateDeploymentRelease: %v", err)
	}
	if r.Number != 3 {
		t.Errorf("release: %+v", r)
	}
	// The body is flat, with the fields DeploymentReleaseController.create takes.
	body := (*reqs)[0].release(t)
	want := map[string]any{
		"firmware": "fw-3", "archive": "ar-1", "required": false, "description": "Fixes Wi-Fi",
		"notes": "after the field test", "connecting_code": "Logger.info(:hi)", "connecting_code_mode": "first",
	}
	if len(body) != len(want) {
		t.Errorf("body: %+v", body)
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestCreateDeploymentReleaseSendsOnlyGivenFields(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusCreated, `{"data":{"number":1}}`
	})

	if _, err := c.CreateDeploymentRelease(context.Background(), "acme", "thermostat", "production",
		ReleaseInput{Firmware: "fw-3"}); err != nil {
		t.Fatalf("CreateDeploymentRelease: %v", err)
	}
	if body := (*reqs)[0].release(t); len(body) != 1 || body["firmware"] != "fw-3" {
		t.Errorf("body: %+v", body)
	}
}

func TestCreateDeploymentReleasePassesOn422(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusUnprocessableEntity, `{"errors":{"firmware":["does not exist"]}}`
	})

	_, err := c.CreateDeploymentRelease(context.Background(), "acme", "thermostat", "production",
		ReleaseInput{Firmware: "nope"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(err.Error(), "firmware: does not exist") {
		t.Errorf("want the 422, got %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("a 422 needs no follow-up request, got %+v", *reqs)
	}
}

func TestUpdateDeploymentRelease(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":` + releaseJSON + `}`
	})

	if _, err := c.UpdateDeploymentRelease(context.Background(), "acme", "thermostat", "production", 3, ReleaseUpdate{
		Required:           deploymentBool(false),
		ConnectingCode:     deploymentStr("IO.puts(1)"),
		ConnectingCodeMode: "override",
	}); err != nil {
		t.Fatalf("UpdateDeploymentRelease: %v", err)
	}
	r := (*reqs)[0]
	if r.Method != http.MethodPatch || r.Path != testReleasesPath+"/3" {
		t.Errorf("request: %s %s", r.Method, r.Path)
	}
	if len(r.Body) != 3 || r.Body["required"] != false || r.Body["connecting_code"] != "IO.puts(1)" ||
		r.Body["connecting_code_mode"] != "override" {
		t.Errorf("body: %+v", r.Body)
	}
}

// An empty connecting code is sent as null, which the server takes as
// removing it; fields left out aren't sent.
func TestUpdateDeploymentReleaseClearsConnectingCode(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"number":3}}`
	})

	if _, err := c.UpdateDeploymentRelease(context.Background(), "acme", "thermostat", "production", 3,
		ReleaseUpdate{ConnectingCode: deploymentStr("")}); err != nil {
		t.Fatalf("UpdateDeploymentRelease: %v", err)
	}
	body := (*reqs)[0].Body
	if v, ok := body["connecting_code"]; !ok || v != nil || len(body) != 1 {
		t.Errorf("body should be {connecting_code: null}, got %+v", body)
	}
}

func TestDeploymentReleaseValidation(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{}}`
	})
	ctx := context.Background()
	checks := map[string]error{}
	_, checks["list without group"] = c.ListDeploymentReleases(ctx, "acme", "thermostat", "")
	_, checks["show number 0"] = c.GetDeploymentRelease(ctx, "acme", "thermostat", "production", 0)
	_, checks["create without firmware"] = c.CreateDeploymentRelease(ctx, "acme", "thermostat", "production", ReleaseInput{})
	_, checks["create bad mode"] = c.CreateDeploymentRelease(ctx, "acme", "thermostat", "production",
		ReleaseInput{Firmware: "fw", ConnectingCodeMode: "middle"})
	_, checks["update nothing"] = c.UpdateDeploymentRelease(ctx, "acme", "thermostat", "production", 1, ReleaseUpdate{})
	_, checks["update bad mode"] = c.UpdateDeploymentRelease(ctx, "acme", "thermostat", "production", 1,
		ReleaseUpdate{ConnectingCodeMode: "middle"})
	_, checks["update no org"] = c.UpdateDeploymentRelease(ctx, "", "thermostat", "production", 1,
		ReleaseUpdate{Required: deploymentBool(true)})
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(*reqs) != 0 {
		t.Errorf("no request should be sent, got %+v", *reqs)
	}
}

// A server without the release API answers each release route with 404, but
// has the group, so the methods say the server needs upgrading.
func TestDeploymentReleasesOnOldServer(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(c *Client) error{
		"list": func(c *Client) error {
			_, err := c.ListDeploymentReleases(ctx, "acme", "thermostat", "production")
			return err
		},
		"show": func(c *Client) error {
			_, err := c.GetDeploymentRelease(ctx, "acme", "thermostat", "production", 2)
			return err
		},
		"create": func(c *Client) error {
			_, err := c.CreateDeploymentRelease(ctx, "acme", "thermostat", "production", ReleaseInput{Firmware: "fw"})
			return err
		},
		"update": func(c *Client) error {
			_, err := c.UpdateDeploymentRelease(ctx, "acme", "thermostat", "production", 2,
				ReleaseUpdate{Required: deploymentBool(true)})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			c, reqs := fakeDeploymentServer(t, oldServer(func(deploymentRequest) (int, string) {
				return http.StatusOK, groupJSON
			}))
			if err := call(c); !errors.Is(err, ErrReleasesUnsupported) {
				t.Errorf("want ErrReleasesUnsupported, got %v", err)
			}
			last := (*reqs)[len(*reqs)-1]
			if last.Method != http.MethodGet || last.Path != testDeploymentPath {
				t.Errorf("should check the group last, got %s %s", last.Method, last.Path)
			}
			for _, r := range *reqs {
				if r.Method == http.MethodPut {
					t.Errorf("release commands must never fall back to a PUT, got %+v", *reqs)
				}
			}
		})
	}
}

// On a server with the release API, a 404 for one release, while the list
// succeeds, means the group has no such release.
func TestGetDeploymentReleaseMissingNumber(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(_ int, r deploymentRequest) (int, string) {
		if r.Path == testReleasesPath {
			return http.StatusOK, `{"data":[]}`
		}
		return http.StatusNotFound, notFoundBody
	})

	_, err := c.GetDeploymentRelease(context.Background(), "acme", "thermostat", "production", 9)
	if err == nil || !strings.Contains(err.Error(), `deployment group "production" has no release 9`) {
		t.Errorf("want a missing-release error, got %v", err)
	}
	if !isNotFound(err) {
		t.Errorf("error should wrap the 404, got %#v", err)
	}
	if len(*reqs) != 2 {
		t.Errorf("want the release, then the list; got %+v", *reqs)
	}
}

// A 404 for the group itself, too, means the group doesn't exist.
func TestDeploymentReleaseMissingGroup(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusNotFound, notFoundBody
	})

	_, err := c.UpdateDeploymentRelease(context.Background(), "acme", "thermostat", "production", 2,
		ReleaseUpdate{Required: deploymentBool(true)})
	if err == nil || errors.Is(err, ErrReleasesUnsupported) ||
		!strings.Contains(err.Error(), `deployment group "production" not found in acme/thermostat`) {
		t.Errorf("want a missing-group error, got %v", err)
	}
	if len(*reqs) != 3 {
		t.Errorf("want the release, the list, then the group; got %+v", *reqs)
	}
}

// Errors other than 404 say nothing about the server's routes, so they pass
// through without more requests.
func TestDeploymentReleasePassesOnOtherErrors(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusUnauthorized, notFoundBody
	})

	_, err := c.ListDeploymentReleases(context.Background(), "acme", "thermostat", "production")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("want the 401, got %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("want one request, got %+v", *reqs)
	}
}

// ── UpdateDeployment's fallback ─────────────────────────────────────────────

// Without the release API, a release-only update finds the group, then sends
// the release PUT it always did.
func TestUpdateDeploymentFallsBackToReleasePUT(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, oldServer(func(deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","firmware_uuid":"fw-2"}}`
	}))

	d, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", Required: deploymentBool(true)})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if d.FirmwareUUID != "fw-2" {
		t.Errorf("deployment: %+v", d)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want POST, GET, PUT; got %+v", *reqs)
	}
	(*reqs)[0].release(t)
	if r := (*reqs)[1]; r.Method != http.MethodGet || r.Path != testDeploymentPath {
		t.Errorf("second request should find the group, got %s %s", r.Method, r.Path)
	}
	put := (*reqs)[2]
	if put.Method != http.MethodPut {
		t.Fatalf("third request should be the release PUT, got %s %s", put.Method, put.Path)
	}
	if dep := put.deployment(t); len(dep) != 2 || dep["firmware"] != "fw-2" || dep["required"] != true {
		t.Errorf("release PUT body: %+v", dep)
	}
}

// The fallback keeps the order: settings, release, then turning the group on.
// The settings PUT has already found the group, so it isn't looked up again.
func TestUpdateDeploymentFallbackKeepsOrder(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, oldServer(func(deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","state":"on"}}`
	}))

	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2", State: "on", Notes: deploymentStr("n")}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 4 {
		t.Fatalf("want settings, POST, release PUT, turn-on; got %+v", *reqs)
	}
	if got := (*reqs)[0].deployment(t); got["notes"] != "n" || got["state"] != nil {
		t.Errorf("first request should be the settings, got %+v", got)
	}
	(*reqs)[1].release(t)
	if got := (*reqs)[2].deployment(t); len(got) != 1 || got["firmware"] != "fw-2" {
		t.Errorf("third request should be the release PUT, got %+v", got)
	}
	if got := (*reqs)[3].deployment(t); len(got) != 1 || got["state"] != "on" {
		t.Errorf("last request should turn the group on, got %+v", got)
	}
}

// An archive on its own already reads the group for its firmware, which is
// enough to read a 404 from the POST.
func TestUpdateDeploymentArchiveFallback(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, oldServer(func(deploymentRequest) (int, string) {
		return http.StatusOK, `{"data":{"name":"production","firmware_uuid":"fw-current"}}`
	}))

	if _, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Archive: "ar-1"}); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want GET, POST, PUT; got %+v", *reqs)
	}
	if dep := (*reqs)[2].deployment(t); dep["firmware"] != "fw-current" || dep["archive"] != "ar-1" {
		t.Errorf("release PUT body: %+v", dep)
	}
}

// When the group itself is missing, the 404 is passed on and no PUT is sent.
func TestUpdateDeploymentMissingGroupDoesNotFallBack(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusNotFound, notFoundBody
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2"})
	if !isNotFound(err) {
		t.Errorf("want the 404, got %v", err)
	}
	if len(*reqs) != 2 {
		t.Errorf("want POST then GET, got %+v", *reqs)
	}
}

// A refused release (422) on a server with the API is an answer, not a sign
// of an old server.
func TestUpdateDeploymentReleaseRefusedDoesNotFallBack(t *testing.T) {
	c, reqs := fakeDeploymentServer(t, func(int, deploymentRequest) (int, string) {
		return http.StatusUnprocessableEntity, `{"errors":{"firmware":["The current release already has this firmware and archive"]}}`
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-1"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want the 422, got %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("want one request, got %+v", *reqs)
	}
}

func TestUpdateDeploymentReportsFailedReadBack(t *testing.T) {
	c, _ := fakeDeploymentServer(t, func(_ int, r deploymentRequest) (int, string) {
		if r.Method == http.MethodPost {
			return http.StatusCreated, `{"data":{"number":2}}`
		}
		return http.StatusServiceUnavailable, `{"errors":{"detail":"maintenance"}}`
	})

	_, err := c.UpdateDeployment(context.Background(), "acme", "thermostat", "production",
		DeploymentInput{Firmware: "fw-2"})
	want := "the new release was created, but reading the group back failed"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("want %q, got %v", want, err)
	}
}
