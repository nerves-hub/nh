package cmd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptLineReadsConsecutiveLines(t *testing.T) {
	cmd := rootCmd
	resetState(t)
	cmd.SetIn(strings.NewReader("one\r\ntwo\nthree"))
	t.Cleanup(func() { cmd.SetIn(nil) })

	for _, want := range []string{"one", "two", "three", ""} {
		got, err := promptLine(cmd, "", true)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	}
}

// Piping the email and the password to `nh user auth` sends both.
func TestUserAuthPipedEmailAndPassword(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		gotBody = b.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"me","email":"me@x","token":"nhu_x"}}`))
	}))
	defer srv.Close()

	out, err := execCmd(t, "me@x\nsecret\n", "user", "auth", "--uri", srv.URL, "--data-dir", t.TempDir())
	if err != nil {
		t.Fatalf("auth: %v\n%s", err, out)
	}
	if !strings.Contains(gotBody, `"email":"me@x"`) || !strings.Contains(gotBody, `"password":"secret"`) {
		t.Errorf("request body should carry both values, got %s", gotBody)
	}
}

type stuckReader struct{}

func (stuckReader) Read([]byte) (int, error) { return 0, nil }

func TestReadLineGivesUpOnStuckReader(t *testing.T) {
	if _, err := readLine(stuckReader{}); !errors.Is(err, io.ErrNoProgress) {
		t.Errorf("want io.ErrNoProgress, got %v", err)
	}
}
