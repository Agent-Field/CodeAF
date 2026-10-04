package sessionid

import "testing"

func TestValid(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"legacy hex", "0123456789abcdef", true},
		{"cell ulid", "01J8ZQ4W5M3T7N9B2C6D8F0GHK", true},
		{"empty", "", false},
		{"hex too short", "0123456789abcde", false},
		{"hex too long", "0123456789abcdef0", false},
		{"hex upper", "0123456789ABCDEF", false},
		{"hex non-hex digit", "0123456789abcdeg", false},
		{"ulid too short", "01J8ZQ4W5M3T7N9B2C6D8F0GH", false},
		{"ulid too long", "01J8ZQ4W5M3T7N9B2C6D8F0GHKM", false},
		{"ulid lowercase", "01j8zq4w5m3t7n9b2c6d8f0ghk", false},
		{"ulid excluded letter", "01J8ZQ4W5M3T7N9B2C6D8F0GHU", false},
		{"ulid overflow first digit", "81J8ZQ4W5M3T7N9B2C6D8F0GHK", false},
		{"path escape", "../../../../etc/passwd", false},
		{"key id hex32", "0123456789abcdef0123456789abcdef", false},
	}
	for _, c := range cases {
		if got := Valid(c.id); got != c.want {
			t.Errorf("%s: Valid(%q) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
}
