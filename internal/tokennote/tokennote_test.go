package tokennote

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestBuild(t *testing.T) {
	cases := map[string]struct{ version, host, want string }{
		"plain":            {"1.2.3", "my-host", "nh 1.2.3 (my-host)"},
		"empty host":       {"dev", "", "nh dev (unknown)"},
		"newline":          {"dev", "Josh's\nMac", "nh dev (Josh's Mac)"},
		"tabs and spaces":  {"dev", "  a \t  b  ", "nh dev (a b)"},
		"direction marker": {"dev", "evil\u202Etxt.exe", "nh dev (eviltxt.exe)"},
		"zero width":       {"dev", "a\u200Bb\uFEFF", "nh dev (ab)"},
		"control chars":    {"dev", "a\x00b\x1bc\x7f", "nh dev (abc)"},
		"invalid utf8":     {"dev", "a\xffb", "nh dev (ab)"},
		"only junk host":   {"dev", "\u200B\n", "nh dev (unknown)"},
		"unicode kept":     {"dev", "Café Ünïcödé 💻", "nh dev (Café Ünïcödé 💻)"},
	}
	for name, tc := range cases {
		if got := Build(tc.version, tc.host); got != tc.want {
			t.Errorf("%s: Build(%q, %q) = %q, want %q", name, tc.version, tc.host, got, tc.want)
		}
	}
}

func TestBuildLength(t *testing.T) {
	for _, host := range []string{strings.Repeat("h", 300), strings.Repeat("é", 300), strings.Repeat("💻", 300)} {
		got := Build("1.2.3", host)
		if n := utf8.RuneCountInString(got); n != MaxLength {
			t.Errorf("want %d characters, got %d", MaxLength, n)
		}
		if !strings.HasPrefix(got, "nh 1.2.3 (") || !strings.HasSuffix(got, ")") {
			t.Errorf("shortening the host should keep the note's shape, got %q", got)
		}
	}

	// A version too long to keep the shape is cut at the limit.
	got := Build(strings.Repeat("v", 400), "host")
	if n := utf8.RuneCountInString(got); n != MaxLength {
		t.Errorf("want %d characters, got %d", MaxLength, n)
	}
}

func TestBuildMeetsServerRules(t *testing.T) {
	got := Build("dev\x01", "x\u2066y\r\n\u00ADz")
	if !utf8.ValidString(got) {
		t.Fatalf("not valid UTF-8: %q", got)
	}
	for _, r := range got {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			t.Errorf("note %q holds %U, which the server refuses", got, r)
		}
	}
}
