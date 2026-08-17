package mix

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLastLine(t *testing.T) {
	cases := map[string]string{
		"acme\n":                          "acme",
		"acme":                            "acme",
		"Compiling 3 files (.ex)\nacme\n": "acme", // compile noise before the value
		"  acme  \n":                      "acme",
		"\n\nacme\n\n":                    "acme",
		"line1\nline2\nthermostat\n":      "thermostat",
		"":                                "",
		"\n\n":                            "",
	}
	for in, want := range cases {
		if got := lastLine([]byte(in)); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// withStubs swaps Available/Eval for the duration of a test.
func withStubs(t *testing.T, available bool, eval func(string) string) {
	t.Helper()
	origA, origE := Available, Eval
	t.Cleanup(func() { Available, Eval = origA, origE })
	Available = func() bool { return available }
	Eval = eval
}

func TestOrg(t *testing.T) {
	withStubs(t, true, func(expr string) string {
		if expr == orgExpr {
			return "acme"
		}
		return ""
	})
	if got := Org(); got != "acme" {
		t.Errorf("Org() = %q, want acme", got)
	}
}

func TestProductPrefersName(t *testing.T) {
	withStubs(t, true, func(expr string) string {
		switch expr {
		case nameExpr:
			return "thermostat"
		case appExpr:
			return "thermostat_app"
		}
		return ""
	})
	if got := Product(); got != "thermostat" {
		t.Errorf("Product() = %q, want thermostat", got)
	}
}

func TestProductFallsBackToApp(t *testing.T) {
	var sawName bool
	withStubs(t, true, func(expr string) string {
		switch expr {
		case nameExpr:
			sawName = true
			return "" // :name unset
		case appExpr:
			return "thermostat_app"
		}
		return ""
	})
	if got := Product(); got != "thermostat_app" {
		t.Errorf("Product() = %q, want thermostat_app", got)
	}
	if !sawName {
		t.Error(":name should be tried before :app")
	}
}

func TestNoMixProject(t *testing.T) {
	withStubs(t, false, func(string) string {
		t.Error("Eval should not run when there is no mix.exs")
		return ""
	})
	if got := Org(); got != "" {
		t.Errorf("Org() = %q, want empty", got)
	}
	if got := Product(); got != "" {
		t.Errorf("Product() = %q, want empty", got)
	}
}

// captureStderr redirects os.Stderr for the duration of f and returns what was
// written.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	f()

	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestEvalTimeoutReportsAndYieldsEmpty(t *testing.T) {
	origExec := execMix
	t.Cleanup(func() { execMix = origExec; abandoned = false })

	// A mix that only returns once its context is cancelled (i.e. it hangs).
	execMix = func(ctx context.Context, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	var got string
	msg := captureStderr(t, func() {
		got = evalWithTimeout("anything", 10*time.Millisecond)
	})

	if got != "" {
		t.Errorf("timeout should yield empty result, got %q", got)
	}
	if !strings.Contains(msg, "timed out") || !strings.Contains(msg, "mix.exs") {
		t.Errorf("expected a friendly timeout message, got %q", msg)
	}
}

func TestEvalAbandonsAfterTimeout(t *testing.T) {
	origExec := execMix
	t.Cleanup(func() { execMix = origExec; abandoned = false })

	var calls int
	execMix = func(ctx context.Context, _ string) ([]byte, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	}

	msg := captureStderr(t, func() {
		evalWithTimeout("a", 10*time.Millisecond) // times out, reports
		evalWithTimeout("b", 10*time.Millisecond) // skipped
		evalWithTimeout("c", 10*time.Millisecond) // skipped
	})

	if calls != 1 {
		t.Errorf("after a timeout, further evals should be skipped; got %d calls", calls)
	}
	if strings.Count(msg, "timed out") != 1 {
		t.Errorf("the timeout should be reported exactly once, got:\n%s", msg)
	}
}

func TestEvalSuccess(t *testing.T) {
	origExec := execMix
	t.Cleanup(func() { execMix = origExec; abandoned = false })

	execMix = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("Compiling 2 files (.ex)\nacme\n"), nil
	}
	if got := evalWithTimeout("expr", time.Second); got != "acme" {
		t.Errorf("evalWithTimeout = %q, want acme", got)
	}
}
