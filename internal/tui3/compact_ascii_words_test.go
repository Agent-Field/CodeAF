package tui3

import (
	"strings"
	"testing"
)

// The linear tier is a screen reader's: a compaction line's marks become ASCII,
// but a reason or a summary's first line in the person's own language is read
// out as written, never as a row of question marks.
func TestCompactLineOnTheLinearTierKeepsWordsInAnyScript(t *testing.T) {
	got := compactASCII("summary skipped: résumé terminé — 要約 · ~7k → ~3k tokens…")
	for _, word := range []string{"résumé", "terminé", "要約"} {
		if !strings.Contains(got, word) {
			t.Fatalf("linear hint lost %q: %q", word, got)
		}
	}
	for _, mark := range []string{"—", "·", "→", "…"} {
		if strings.Contains(got, mark) {
			t.Fatalf("linear hint still carries the mark %q: %q", mark, got)
		}
	}
}
