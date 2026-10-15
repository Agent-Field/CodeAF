package session

import "testing"

// The app's naming rule: whitespace folded, a short message kept whole, a long
// one cut at the last word inside the limit and marked, one long word cut at
// the limit.
func TestFirstMessageName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  fix   the\nparser ", "fix the parser"},
		{"Please fix the flaky parser test in the lexer package", "Please fix the flaky parser test in the…"},
		{"0123456789012345678901234567890123456789", "0123456789012345678901234567890123456789"},
		{"01234567890123456789012345678901234567890123", "0123456789012345678901234567890123456789…"},
		{"", ""},
	} {
		if got := FirstMessageName(c.in); got != c.want {
			t.Errorf("FirstMessageName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
