package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fastPoll = CLIPollOptions{Interval: time.Millisecond, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}

// pollServer answers GET /api/auth/cli_session/sess-tok with handle, passing
// the 1-based poll count.
func pollServer(t *testing.T, handle func(w http.ResponseWriter, n int)) (*Client, *atomic.Int32) {
	t.Helper()
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/auth/cli_session/sess-tok" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("poll should be unauthenticated, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		handle(w, int(polls.Add(1)))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv), &polls
}

func ready(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"data":{"status":"ready","user_token":"nhu_tok"}}`))
}

func TestCheckCLISessionDecodes(t *testing.T) {
	c, _ := pollServer(t, func(w http.ResponseWriter, _ int) { ready(w) })
	got, err := c.CheckCLISession(context.Background(), "sess-tok")
	if err != nil {
		t.Fatalf("CheckCLISession: %v", err)
	}
	if got.Status != "ready" || got.UserToken != "nhu_tok" {
		t.Errorf("got %+v", got)
	}
}

func TestCheckCLISessionError(t *testing.T) {
	c, _ := pollServer(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.CheckCLISession(context.Background(), "sess-tok")

	var sessErr *CLISessionError
	if !errors.As(err, &sessErr) {
		t.Fatalf("want *CLISessionError, got %T %v", err, err)
	}
	if sessErr.RetryAfter != 7*time.Second || !sessErr.Throttled() || sessErr.Expired() {
		t.Errorf("got RetryAfter=%v Throttled=%v Expired=%v", sessErr.RetryAfter, sessErr.Throttled(), sessErr.Expired())
	}
	// Callers branching on *APIError still can.
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("want a wrapped *APIError with 429, got %v", err)
	}
}

func TestCLISessionErrorClassification(t *testing.T) {
	cases := []struct {
		name               string
		status             int
		body               string
		throttled, expired bool
	}{
		{"429", http.StatusTooManyRequests, "", true, false},
		{"plugattack 403", http.StatusForbidden, "Forbidden\n", true, false},
		{"json 403", http.StatusForbidden, `{"errors":{"detail":"Forbidden"}}`, false, false},
		{"404", http.StatusNotFound, `{"errors":{"detail":"Not Found"}}`, false, true},
		{"500", http.StatusInternalServerError, "", false, false},
	}
	for _, tc := range cases {
		e := &CLISessionError{APIError: &APIError{StatusCode: tc.status, Body: []byte(tc.body)}}
		if e.Throttled() != tc.throttled || e.Expired() != tc.expired {
			t.Errorf("%s: Throttled=%v Expired=%v, want %v %v", tc.name, e.Throttled(), e.Expired(), tc.throttled, tc.expired)
		}
	}
}

func TestWaitForCLISessionReady(t *testing.T) {
	c, polls := pollServer(t, func(w http.ResponseWriter, n int) {
		if n < 3 {
			_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
			return
		}
		ready(w)
	})
	tok, err := c.WaitForCLISession(context.Background(), "sess-tok", fastPoll)
	if err != nil || tok != "nhu_tok" {
		t.Fatalf("got %q, %v", tok, err)
	}
	if polls.Load() != 3 {
		t.Errorf("polls: want 3, got %d", polls.Load())
	}
}

func TestWaitForCLISessionHonoursRetryAfter(t *testing.T) {
	c, polls := pollServer(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ready(w)
	})
	opts := fastPoll
	opts.MaxBackoff = 5 * time.Second

	start := time.Now()
	tok, err := c.WaitForCLISession(context.Background(), "sess-tok", opts)
	if err != nil || tok != "nhu_tok" {
		t.Fatalf("got %q, %v", tok, err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("should wait for Retry-After (1s), waited %v", elapsed)
	}
	if polls.Load() != 2 {
		t.Errorf("polls: want 2, got %d", polls.Load())
	}
}

func TestWaitForCLISessionThrottledUntilDeadline(t *testing.T) {
	c, _ := pollServer(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(http.StatusTooManyRequests) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.WaitForCLISession(ctx, "sess-tok", fastPoll)
	if !errors.Is(err, ErrCLISessionExpired) {
		t.Errorf("want ErrCLISessionExpired once the deadline passes, got %v", err)
	}
	// It wasn't the user who ran out of time, so don't say they did.
	if msg := err.Error(); strings.Contains(msg, "wasn't entered") || !strings.Contains(msg, "api: 429") {
		t.Errorf("the error should report the refused polls, got %q", msg)
	}
}

func TestWaitForCLISessionDeadlineWhileWaiting(t *testing.T) {
	c, _ := pollServer(t, func(w http.ResponseWriter, _ int) {
		_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := c.WaitForCLISession(ctx, "sess-tok", fastPoll)
	if !errors.Is(err, ErrCLISessionExpired) || !strings.Contains(err.Error(), "wasn't entered in time") {
		t.Errorf("want an expiry blaming the timeout, got %v", err)
	}
}

func TestWaitForCLISessionThrottleResetsTransientCount(t *testing.T) {
	// Never more than two 502s in a row, so it never gives up.
	c, polls := pollServer(t, func(w http.ResponseWriter, n int) {
		switch n {
		case 1, 2, 4, 5:
			w.WriteHeader(http.StatusBadGateway)
		case 3:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			ready(w)
		}
	})
	opts := fastPoll
	opts.MaxTransientErrors = 2
	tok, err := c.WaitForCLISession(context.Background(), "sess-tok", opts)
	if err != nil || tok != "nhu_tok" {
		t.Fatalf("got %q, %v", tok, err)
	}
	if polls.Load() != 6 {
		t.Errorf("polls: want 6, got %d", polls.Load())
	}
}

func TestWaitForCLISessionExpired(t *testing.T) {
	c, polls := pollServer(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(http.StatusNotFound) })
	_, err := c.WaitForCLISession(context.Background(), "sess-tok", fastPoll)
	if !errors.Is(err, ErrCLISessionExpired) {
		t.Errorf("want ErrCLISessionExpired, got %v", err)
	}
	if polls.Load() != 1 {
		t.Errorf("polls: want 1, got %d", polls.Load())
	}
}

func TestWaitForCLISessionCollected(t *testing.T) {
	c, polls := pollServer(t, func(w http.ResponseWriter, _ int) {
		_, _ = w.Write([]byte(`{"data":{"status":"collected"}}`))
	})
	_, err := c.WaitForCLISession(context.Background(), "sess-tok", fastPoll)
	if !errors.Is(err, ErrCLISessionExpired) || !strings.Contains(err.Error(), "already handed out") {
		t.Errorf("want an expiry saying the token was collected, got %v", err)
	}
	if polls.Load() != 1 {
		t.Errorf("polls: want 1, got %d", polls.Load())
	}
}

func TestWaitForCLISessionStopsOnClientError(t *testing.T) {
	c, polls := pollServer(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"detail":"bad"}}`))
	})
	_, err := c.WaitForCLISession(context.Background(), "sess-tok", fastPoll)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("want the 400 back, got %v", err)
	}
	if polls.Load() != 1 {
		t.Errorf("polls: want 1, got %d", polls.Load())
	}
}

func TestWaitForCLISessionRetriesNetworkErrors(t *testing.T) {
	// A server that has gone away: every poll fails to connect.
	srv := httptest.NewServer(http.NotFoundHandler())
	c := newTestClient(t, srv)
	srv.Close()

	_, err := c.WaitForCLISession(context.Background(), "sess-tok", fastPoll)
	if err == nil || errors.Is(err, ErrCLISessionExpired) {
		t.Errorf("want the connection error after retries, got %v", err)
	}
}

func TestWaitForCLISessionCancelled(t *testing.T) {
	c, _ := pollServer(t, func(w http.ResponseWriter, _ int) {
		_, _ = w.Write([]byte(`{"data":{"status":"waiting"}}`))
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := c.WaitForCLISession(ctx, "sess-tok", fastPoll)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCLISessionExpired) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}
