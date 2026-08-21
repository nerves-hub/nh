package mix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildFirmware creates the Nerves build layout inside a temp cwd and returns
// the directory holding the images.
func buildFirmware(t *testing.T, target, env string, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	images := filepath.Join(dir, "_build", target+"_"+env, "nerves", "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(images, n), []byte("fw"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return images
}

func TestFirmwarePathDetectsSingleImage(t *testing.T) {
	t.Setenv("MIX_TARGET", "rpi4")
	t.Setenv("MIX_ENV", "dev")
	buildFirmware(t, "rpi4", "dev", "app.fw")

	got, err := FirmwarePath()
	if err != nil {
		t.Fatalf("FirmwarePath: %v", err)
	}
	if filepath.Base(got) != "app.fw" {
		t.Errorf("detected %q, want app.fw", got)
	}
}

func TestFirmwarePathHonoursMixEnv(t *testing.T) {
	t.Setenv("MIX_TARGET", "rpi0")
	t.Setenv("MIX_ENV", "prod")

	// Both a dev and a prod image exist in the same project; only the one
	// matching MIX_ENV may be picked, and its presence must not read as
	// ambiguous.
	dir := t.TempDir()
	t.Chdir(dir)
	for _, env := range []string{"dev", "prod"} {
		images := filepath.Join(dir, "_build", "rpi0_"+env, "nerves", "images")
		if err := os.MkdirAll(images, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(images, "app.fw"), []byte("fw"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := FirmwarePath()
	if err != nil {
		t.Fatalf("FirmwarePath: %v", err)
	}
	if !strings.Contains(got, filepath.Join("rpi0_prod", "nerves", "images")) {
		t.Errorf("detected %q, want the rpi0_prod image", got)
	}
}

func TestFirmwarePathHostTargetIsAnError(t *testing.T) {
	t.Setenv("MIX_TARGET", "host")
	t.Setenv("MIX_ENV", "dev")

	_, err := FirmwarePath()
	if err == nil || !strings.Contains(err.Error(), "MIX_TARGET") {
		t.Fatalf("expected a host-target error, got %v", err)
	}
}

func TestFirmwarePathDefaultsToHost(t *testing.T) {
	// Unset both: mix's own defaults are dev/host, so this is the host error.
	t.Setenv("MIX_TARGET", "")
	t.Setenv("MIX_ENV", "")

	if _, err := FirmwarePath(); err == nil {
		t.Fatal("expected an error when MIX_TARGET is unset (defaults to host)")
	}
}

func TestFirmwarePathNoneFound(t *testing.T) {
	t.Setenv("MIX_TARGET", "rpi4")
	t.Setenv("MIX_ENV", "dev")
	buildFirmware(t, "rpi4", "dev") // layout exists, no images

	_, err := FirmwarePath()
	if err == nil || !strings.Contains(err.Error(), "no firmware found") {
		t.Fatalf("expected a not-found error, got %v", err)
	}
	// The error must name the pattern so the fix is obvious.
	if err != nil && !strings.Contains(err.Error(), "rpi4_dev") {
		t.Errorf("error should name the glob, got %v", err)
	}
}

func TestFirmwarePathAmbiguousListsCandidates(t *testing.T) {
	t.Setenv("MIX_TARGET", "rpi4")
	t.Setenv("MIX_ENV", "dev")
	buildFirmware(t, "rpi4", "dev", "a.fw", "b.fw")

	_, err := FirmwarePath()
	if err == nil {
		t.Fatal("expected an ambiguity error for two images")
	}
	for _, want := range []string{"ambiguous", "a.fw", "b.fw"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got %v", want, err)
		}
	}
}
