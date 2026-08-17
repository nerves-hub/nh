package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTransferBarRendersToWriter(t *testing.T) {
	var buf bytes.Buffer
	bar := newTransferBar(&buf, "Uploading image.fw", 1000)

	// Writing to the bar advances it and renders to the writer.
	if _, err := bar.Write(make([]byte, 500)); err != nil {
		t.Fatalf("write: %v", err)
	}
	finishBar(bar)

	out := buf.String()
	if !strings.Contains(out, "Uploading image.fw") {
		t.Errorf("expected the description in the rendered bar, got %q", out)
	}
}

func TestNewTransferBarUnknownTotal(t *testing.T) {
	var buf bytes.Buffer
	// A non-positive total must not panic (it renders a spinner).
	bar := newTransferBar(&buf, "Downloading image.fw", 0)
	if _, err := bar.Write(make([]byte, 128)); err != nil {
		t.Fatalf("write: %v", err)
	}
	finishBar(bar)
}

func TestFinishBarNilSafe(t *testing.T) {
	finishBar(nil) // must not panic
}
