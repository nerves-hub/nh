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

// Package interrupt ends nh cleanly on Ctrl-C (SIGINT) or SIGTERM.
//
// Run hands the work a context that the first signal cancels, so in-flight
// HTTP requests and subprocesses started with exec.CommandContext stop and the
// command's deferred cleanup runs: a decrypted signing key written to a
// temporary file is removed rather than left behind.
//
// Some work never sees the context: a prompt blocked reading the terminal, for
// one. So if the work has not returned within a grace period, or a second
// signal arrives, Run gives up on it: it removes every file registered with
// RemoveOnExit and calls Options.Abandon, which is expected to exit.
package interrupt

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// signals are the ones that end a command.
var signals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// Error is the cancellation cause of a context ended by a signal.
type Error struct {
	Signal os.Signal
}

func (e *Error) Error() string { return "interrupted by " + e.Signal.String() }

// ExitCode is the shell's status for a process ended by sig: 128 plus the
// signal number, so 130 for SIGINT and 143 for SIGTERM.
func ExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}

// Options configures Run.
type Options struct {
	// Grace is how long the work has, after the first signal, to return on
	// its own before Run gives up on it.
	Grace time.Duration
	// Abandon is called, after the files registered with RemoveOnExit are
	// removed, when Run gives up on the work. It should end the process.
	Abandon func(sig os.Signal)
}

// Run calls work with a context that the first SIGINT or SIGTERM cancels, and
// returns work's error along with the signal that interrupted it, if any.
func Run(parent context.Context, opts Options, work func(context.Context) error) (os.Signal, error) {
	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, signals...)
	defer signal.Stop(sigc)

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	done := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		var sig os.Signal
		select {
		case sig = <-sigc:
		case <-done:
			return
		}
		cancel(&Error{Signal: sig})

		grace := time.NewTimer(opts.Grace)
		defer grace.Stop()
		select {
		case <-done:
			return
		case <-sigc:
		case <-grace.C:
		}
		removeRegistered()
		if opts.Abandon != nil {
			opts.Abandon(sig)
		}
	}()

	err := work(ctx)
	close(done)
	<-watched

	var ie *Error
	if errors.As(context.Cause(ctx), &ie) {
		return ie.Signal, err
	}
	return nil, err
}

var (
	registryMu sync.Mutex
	registry   = map[string]int{}
)

// RemoveOnExit registers path for removal should Run give up on the work, as
// a backstop for the caller's own deferred os.Remove, which then never runs.
// The returned func unregisters it; call it once the file is gone.
func RemoveOnExit(path string) (forget func()) {
	registryMu.Lock()
	registry[path]++
	registryMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			registryMu.Lock()
			defer registryMu.Unlock()
			if registry[path]--; registry[path] <= 0 {
				delete(registry, path)
			}
		})
	}
}

// removeRegistered removes every registered file, ignoring errors: the files
// may already be gone.
func removeRegistered() {
	registryMu.Lock()
	defer registryMu.Unlock()
	for path := range registry {
		_ = os.Remove(path)
	}
}
