package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLongFallbackTitleClipsWordsBeforeRemovingTrailingGlue(t *testing.T) {
	for _, tc := range []struct{ brief, want string }{
		{strings.Repeat("a", 60) + " photograph exceedinglylongword", strings.Repeat("a", 60) + " photograph"},
		{strings.Repeat("b", 68) + " and exceedinglylongword", strings.Repeat("b", 68)},
		{strings.Repeat("界", 21) + " with exceedinglylongword", strings.Repeat("界", 21)},
	} {
		if got := taskPersonTitle(tc.brief); got != tc.want || len(got) > titleLimit || !utf8.ValidString(got) {
			t.Fatalf("title=%q, want %q", got, tc.want)
		}
	}
	if got := taskPersonTitle(strings.Repeat("界", 50)); len(got) > titleLimit || !utf8.ValidString(got) {
		t.Fatalf("single long word=%q", got)
	}
}
