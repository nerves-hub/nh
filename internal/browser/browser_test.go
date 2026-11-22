package browser

import (
	"reflect"
	"testing"
)

func TestCanOpen(t *testing.T) {
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want bool
	}{
		{"mac", "darwin", nil, true},
		{"mac over ssh", "darwin", map[string]string{"SSH_CONNECTION": "1.2.3.4 1 5.6.7.8 22"}, false},
		{"mac ssh tty", "darwin", map[string]string{"SSH_TTY": "/dev/ttys001"}, false},
		{"linux x11", "linux", map[string]string{"DISPLAY": ":0"}, true},
		{"linux wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"linux headless", "linux", nil, false},
		{"freebsd headless", "freebsd", nil, false},
		{"windows", "windows", nil, true},
	}
	for _, tc := range cases {
		getenv := func(k string) string { return tc.env[k] }
		if got := canOpen(tc.goos, getenv); got != tc.want {
			t.Errorf("%s: canOpen = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCommand(t *testing.T) {
	const url = "https://example.test/auth/cli?a=1&b=2"
	cases := map[string][]string{
		"darwin":  {"open", url},
		"linux":   {"xdg-open", url},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", url},
	}
	for goos, want := range cases {
		name, args := command(goos, url)
		if got := append([]string{name}, args...); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", goos, got, want)
		}
	}
}

func TestIsWebURL(t *testing.T) {
	cases := map[string]bool{
		"https://manage.nervescloud.com/auth/cli": true,
		"http://localhost:4000/auth/cli":          true,
		"HTTPS://example.test/x":                  true,
		"file:///Applications/Calculator.app":     false,
		"/System/Applications/Calculator.app":     false,
		"-a Calculator":                           false,
		"smb://evil.test/share":                   false,
		"javascript:alert(1)":                     false,
		"https:///no-host":                        false,
		"https://example.test/\x1b[2Kx":           false,
		"https://example.test/a\nb":               false,
		"":                                        false,
	}
	for in, want := range cases {
		if got := IsWebURL(in); got != want {
			t.Errorf("IsWebURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOpenRefusesNonWebURL(t *testing.T) {
	if err := Open("file:///etc/passwd"); err != ErrNotWebURL {
		t.Errorf("want ErrNotWebURL, got %v", err)
	}
}
