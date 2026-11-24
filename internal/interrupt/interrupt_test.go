//go:build unix

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

package interrupt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// interruptSelf sends SIGINT to the test process, which Run has registered a
// handler for, so the process is not killed.
func interruptSelf(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}
}

func TestRunWithoutSignal(t *testing.T) {
	want := errors.New("boom")
	sig, err := Run(context.Background(), Options{Grace: time.Second}, func(context.Context) error {
		return want
	})
	if sig != nil {
		t.Errorf("signal = %v, want none", sig)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestRunCancelsContextOnSignal(t *testing.T) {
	abandoned := false
	sig, err := Run(context.Background(), Options{
		Grace:   time.Minute,
		Abandon: func(os.Signal) { abandoned = true },
	}, func(ctx context.Context) error {
		interruptSelf(t)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("context was not cancelled")
		}
	})
	if sig != syscall.SIGINT {
		t.Errorf("signal = %v, want SIGINT", sig)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if abandoned {
		t.Error("work that returned within the grace period must not be abandoned")
	}
}

// Work that ignores its context, like a prompt blocked on the terminal, is
// abandoned after the grace period, and the registered files are removed
// before Abandon (which in nh exits) is called.
func TestRunAbandonsStuckWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nh-key.priv")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	forget := RemoveOnExit(path)
	defer forget()

	abandoned := make(chan os.Signal, 1)
	release := make(chan struct{})
	_, _ = Run(context.Background(), Options{
		Grace: 50 * time.Millisecond,
		Abandon: func(sig os.Signal) {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("registered file still present when abandoning: %v", err)
			}
			abandoned <- sig
			close(release)
		},
	}, func(context.Context) error {
		interruptSelf(t)
		<-release
		return nil
	})

	select {
	case sig := <-abandoned:
		if sig != syscall.SIGINT {
			t.Errorf("Abandon got %v, want SIGINT", sig)
		}
	default:
		t.Fatal("stuck work was not abandoned")
	}
}

// A second Ctrl-C abandons the work at once, without waiting out the grace
// period.
func TestRunSecondSignalAbandons(t *testing.T) {
	abandoned := make(chan struct{})
	_, _ = Run(context.Background(), Options{
		Grace:   time.Minute,
		Abandon: func(os.Signal) { close(abandoned) },
	}, func(ctx context.Context) error {
		interruptSelf(t)
		<-ctx.Done()
		interruptSelf(t)
		select {
		case <-abandoned:
		case <-time.After(10 * time.Second):
			t.Error("second signal did not abandon the work")
		}
		return nil
	})
}

func TestRemoveOnExitForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kept")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	forget := RemoveOnExit(path)
	forget()
	forget() // a second call is a no-op

	removeRegistered()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a forgotten file must not be removed: %v", err)
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(syscall.SIGINT); got != 130 {
		t.Errorf("ExitCode(SIGINT) = %d, want 130", got)
	}
	if got := ExitCode(syscall.SIGTERM); got != 143 {
		t.Errorf("ExitCode(SIGTERM) = %d, want 143", got)
	}
}
