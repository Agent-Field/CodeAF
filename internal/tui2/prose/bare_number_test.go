package prose

import (
	"strings"
	"testing"
)

// TestABareNumberedReplyStillDraws guards #1072: a reply that is only a number
// and a full stop (`32.`, `1024.`) is parsed by Markdown as an ordered list
// with one empty item. The list renderer must draw the marker as the literal
// text the reader sent, never discard it as an empty list and render nothing.
func TestABareNumberedReplyStillDraws(t *testing.T) {
	for _, in := range []string{"32.", "1024."} {
		if got := strings.TrimSpace(strings.Join(Render(in, Options{Width: 80}), "\n")); got != in {
			t.Errorf("Render(%q) = %q, want %q", in, got, in)
		}
	}
	// A genuine ordered list with a body still draws as a list.
	if got := strings.TrimSpace(strings.Join(Render("1. one", Options{Width: 80}), "\n")); got != "1. one" {
		t.Errorf("Render(%q) = %q, want %q", "1. one", got, "1. one")
	}
}
