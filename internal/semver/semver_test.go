package semver

import "testing"

// The cases were checked against Elixir's Version.parse/1, which NervesHub
// uses to validate archive versions.
func TestValid(t *testing.T) {
	cases := map[string]bool{
		"1.0.0":            true,
		"0.0.0":            true,
		"10.20.30":         true,
		"1.0.0-rc.1":       true,
		"1.0.0-rc.1+b":     true,
		"1.0.0+build.01":   true,
		"1.2.3-0a":         true,
		"1.0.0-alpha-beta": true,
		"":                 false,
		"v1.0.0":           false,
		"1.0":              false,
		"1":                false,
		"01.0.0":           false,
		"1.01.0":           false,
		"1.0.0-01":         false,
		"1.0.0-alpha..1":   false,
		"1.0.0-":           false,
		"1.0.0+":           false,
		" 1.0.0":           false,
		"1.0.0\n":          false,
	}
	for v, want := range cases {
		if got := Valid(v); got != want {
			t.Errorf("Valid(%q) = %v, want %v", v, got, want)
		}
	}
}
