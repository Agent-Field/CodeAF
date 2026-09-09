package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestCompactWaitingKeepsFinishedWordsStill(t *testing.T) {
	a := liveStepsApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = liveStepsBase.Add(8 * time.Second)
	captionTimeAt(a, 500*time.Millisecond)
	a.touch()
	before := liveStepRowsOf(t, a, 100)
	captionTimeAt(a, 800*time.Millisecond)
	a.touch()
	after := liveStepRowsOf(t, a, 100)
	if len(before) != len(after) {
		t.Fatal("waiting moved the caption")
	}
	for i, r := range before {
		if plain(r.text) != plain(after[i].text) {
			t.Fatal("waiting changed characters")
		}
		if i == len(before)-1 {
			if strings.Split(r.text, "  ")[0] != strings.Split(after[i].text, "  ")[0] {
				t.Fatal("finished caption or semantic icon shimmered")
			}
			if r.text == after[i].text {
				t.Fatal("inline activity mark did not animate")
			}
		} else if r.text != after[i].text {
			t.Fatal("older caption moved")
		}
	}
}

func TestCompactWaitUsesOnlyKnownResponseAge(t *testing.T) {
	forgetPhases()
	t.Cleanup(forgetPhases)
	a := liveStepsApp(t)
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = liveStepsBase.Add(8 * time.Second)
	a.awaited = liveStepsBase.Add(8 * time.Second)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{9999 * time.Millisecond, ""}, {10 * time.Second, "awaiting response · 10s"}, {61 * time.Second, "awaiting response · 1m 1s"},
	} {
		a.clock = func() time.Time { return a.awaited.Add(tc.age) }
		if got := a.compactWaitWords(a.conversation()); got != tc.want {
			t.Fatalf("age %s: got %q, want %q", tc.age, got, tc.want)
		}
		if got := a.compactWaitWords(deck{lens: overseerLens}); got != "" {
			t.Fatalf("room borrowed parent wait: %q", got)
		}
	}
	a.awaited = time.Time{}
	if got := a.compactWaitWords(a.conversation()); got != "" {
		t.Fatalf("unknown wait invented a clock: %q", got)
	}
}

func TestCompactWaitFallbackPreservesTextAndAccessibleMotion(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256} {
		for _, linear := range []bool{false, true} {
			a := liveStepsApp(t)
			a.linear = linear
			a.pal = newPalette(profile, linear)
			captionTimeAt(a, 500*time.Millisecond)
			before := a.compactWaitMark()
			captionTimeAt(a, 800*time.Millisecond)
			if (linear || profile == tokens.ANSI256) && before != a.compactWaitMark() {
				t.Fatal("accessible activity animated")
			}
			for _, room := range []int{4, 5, 6, 7, 80} {
				tail, inline := a.compactWaitSuffix("text", room, a.conversation())
				if inline != (room >= 7) || (inline && len(plain(tail)) == 0) {
					t.Fatalf("room %d: wrong inline fallback", room)
				}
			}
		}
	}
}
