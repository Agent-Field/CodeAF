package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The name rule: whitespace folded, a short message kept whole, a long one cut
// at the last word inside the limit and marked, one long word cut at the limit,
// and the ellipsis COUNTS toward the forty characters.
func TestFirstMessageName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  fix   the\nparser ", "fix the parser"},
		{"Please fix the flaky parser test in the lexer package", "Please fix the flaky parser test in the…"},
		{"Please fix the flaky parser tests in lexer packages", "Please fix the flaky parser tests in…"},
		{"0123456789012345678901234567890123456789", "0123456789012345678901234567890123456789"},
		{"01234567890123456789012345678901234567890123", "012345678901234567890123456789012345678…"},
		{"", ""},
		// A character is a code point: forty CJK characters fit, a forty-first is cut.
		{strings.Repeat("字", 40), strings.Repeat("字", 40)},
		{strings.Repeat("字", 41), strings.Repeat("字", 39) + "…"},
	} {
		got := FirstMessageName(c.in)
		if got != c.want {
			t.Errorf("FirstMessageName(%q) = %q, want %q", c.in, got, c.want)
		}
		if n := utf8.RuneCountInString(got); n > OpeningNameLimit {
			t.Errorf("FirstMessageName(%q) is %d characters, the limit is %d", c.in, n, OpeningNameLimit)
		}
	}
}
