//go:build unix

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/pki"
)

// executeHelperEnv names the variable that turns TestExecuteHelper into nh:
// it holds the JSON-encoded arguments to run Execute with.
const executeHelperEnv = "NH_TEST_EXECUTE_ARGS"

// TestExecuteHelper is not a test. Run as a subprocess with executeHelperEnv
// set, it runs nh's Execute, which exits the process.
func TestExecuteHelper(t *testing.T) {
	raw := os.Getenv(executeHelperEnv)
	if raw == "" {
		t.Skip("helper process for TestFirmwareUploadInterruptedRemovesKey")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs(args)
	Execute()
	os.Exit(0)
}

// Ctrl-C in the middle of `nh firmware upload` signing must not leave the
// decrypted private key (nh-key-*.priv) or the signed copy (nh-signed-*) in
// $TMPDIR. Before Execute handled signals, SIGINT ended nh at once and its
// deferred removes never ran.
func TestFirmwareUploadInterruptedRemovesKey(t *testing.T) {
	tmp := t.TempDir()
	dir := t.TempDir()
	started := filepath.Join(dir, "started")

	// fwup answers `-m` with metadata, and hangs when asked to sign.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = -m ]; then\n" +
		"  printf 'meta-product=\"thermostat\"\\nmeta-version=\"1.0.0\"\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"touch '" + started + "'\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "fwup"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "image.fw")
	if err := os.WriteFile(image, []byte(fwupTestData), 0o644); err != nil {
		t.Fatal(err)
	}
	_, priv, err := pki.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal([]string{
		"firmware", "upload", image,
		"--org", "acme", "--product", "thermostat",
		"--uri", "http://127.0.0.1:1", "--token", "tok",
		"--data-dir", t.TempDir(),
	})
	helper := exec.Command(os.Args[0], "-test.run=^TestExecuteHelper$")
	helper.Env = append(os.Environ(),
		executeHelperEnv+"="+string(args),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+tmp,
		"NERVES_HUB_PRIVATE_KEY="+pki.PrivateKeyString(priv),
	)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("nh exited before signing started: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			_ = helper.Process.Kill()
			t.Fatal("fwup was never asked to sign")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if keys, _ := filepath.Glob(filepath.Join(tmp, "nh-key-*")); len(keys) == 0 {
		t.Fatal("expected the decrypted key in $TMPDIR while fwup signs")
	}

	if err := helper.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		_ = helper.Process.Kill()
		t.Fatal("nh did not exit after SIGINT")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Errorf("exit = %v, want status 130", err)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "nh-*")); len(left) > 0 {
		t.Errorf("temporary files left behind after Ctrl-C: %v", left)
	}
}

// Ctrl-C cancels the command's context. While fwup is signing, that must kill
// fwup and remove both the decrypted key and the half-written signed copy,
// rather than leave the key in $TMPDIR.
func TestSignFirmwareCancelledRemovesTempFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// A fwup that records it has started, then hangs. The sleep is a child
	// that keeps fwup's stderr pipe open after fwup is killed, so signing
	// returns promptly only because of WaitDelay.
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	script := "#!/bin/sh\ntouch '" + started + "'\nsleep 5\n"
	stub := filepath.Join(dir, "fwup")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := fwupBin
	fwupBin = stub
	t.Cleanup(func() { fwupBin = old })

	in := filepath.Join(dir, "image.fw")
	if err := os.WriteFile(in, []byte(fwupTestData), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range 500 {
			if _, err := os.Stat(started); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	begin := time.Now()
	out, err := signFirmware(ctx, in, "not-a-real-key")
	if err == nil {
		os.Remove(out)
		t.Fatal("signing should fail when cancelled")
	}
	if _, statErr := os.Stat(started); statErr != nil {
		t.Fatalf("fwup never started: %v", statErr)
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Errorf("cancelling took %s; signing waited on fwup's child", d)
	}

	left, _ := filepath.Glob(filepath.Join(tmp, "nh-*"))
	if len(left) > 0 {
		t.Errorf("temporary files left behind after cancelling: %v", left)
	}
}

func TestExitCode(t *testing.T) {
	failed := errors.New("failed")
	cases := []struct {
		name string
		sig  os.Signal
		err  error
		want int
	}{
		{"success", nil, nil, 0},
		{"failure", nil, failed, 1},
		{"interrupted failure", syscall.SIGINT, failed, 130},
		{"terminated failure", syscall.SIGTERM, failed, 143},
		// `device logs --follow` turns Ctrl-C into a clean finish.
		{"interrupted success", syscall.SIGINT, nil, 0},
	}
	for _, c := range cases {
		if got := exitCode(c.sig, c.err); got != c.want {
			t.Errorf("%s: exitCode = %d, want %d", c.name, got, c.want)
		}
	}
}
