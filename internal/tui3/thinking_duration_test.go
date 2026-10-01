package tui3

import (
	"strings"
	"testing"
	"time"
)

// Timing is absent until a whole second was measured, and an absent timestamp
// cannot become a duration. The disclosure and positive token estimate survive.
func TestThoughtRowsOmitUnknownAndSubsecondDurations(t *testing.T) {
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		began, ended time.Time
	}{
		{"unknown", time.Time{}, time.Time{}},
		{"missing start", time.Time{}, base},
		{"missing end", base, time.Time{}},
		{"zero", base, base},
		{"backwards", base, base.Add(-time.Second)},
		{"short", base, base.Add(400 * time.Millisecond)},
		{"nearly a second", base, base.Add(999 * time.Millisecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, a := wired()
			e := entry{kind: entryThinking, settled: true, began: test.began, ended: test.ended, text: strings.Repeat("a", 40)}
			want := glyphThought + " thought for this turn · 10 tok · ctrl+e"
			if got := plain(a.thoughtRows(&e, 100, false)[0]); got != want {
				t.Errorf("the thought row is %q, want %q", got, want)
			}
			e.open = true
			if got := strings.Join(a.thoughtRows(&e, 100, true), "\n"); !strings.Contains(plain(got), e.text) {
				t.Fatal("the thought disclosure lost its text")
			}
			e.text = "a"
			want = glyphThought + " thought for this turn · ctrl+e"
			if got := plain(a.thoughtRows(&e, 100, false)[0]); got != want {
				t.Errorf("an unknown count left a zero or separator: %q, want %q", got, want)
			}
		})
	}
}

func TestThoughtRowsKeepMeasuredWholeSecondDurations(t *testing.T) {
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	_, a := wired()
	for _, test := range []struct {
		span time.Duration
		word string
	}{
		{time.Second, "1s"},
		{1499 * time.Millisecond, "1s"},
		{1500 * time.Millisecond, "2s"},
		{6 * time.Second, "6s"},
	} {
		e := entry{kind: entryThinking, settled: true, began: base, ended: base.Add(test.span), text: strings.Repeat("a", 40)}
		want := glyphThought + " thought for " + test.word + " · 10 tok · ctrl+e"
		if got := plain(a.thoughtRows(&e, 100, false)[0]); got != want {
			t.Errorf("a measured %s row is %q, want %q", test.span, got, want)
		}
	}
}
