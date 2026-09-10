package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// foldLab is a machine with nothing on it but quiet conversations, which is the
// only shape that puts a fold on home at rest. The ages are a day apart so the
// clause the fold ends up with names a row the test can point at.
func foldLab(now time.Time, n int) session.World {
	project := session.Project{Bucket: "alpha", Dir: "/state/alpha", Path: "/work/alpha", Name: "alpha"}
	for i := 0; i < n; i++ {
		project.Sessions = append(project.Sessions, session.SessionRow{
			ID:    fmt.Sprintf("quiet-%02d", i),
			Dir:   fmt.Sprintf("/state/alpha/q%02d", i),
			Title: fmt.Sprintf("Quiet %02d", i),
			At:    now.Add(-time.Duration(i+1) * 24 * time.Hour),
		})
	}
	return session.World{Projects: []session.Project{project}, Read: now}
}

// foldRowWord is what the one fold in a reading says, mark and all.
func foldRowWord(t *testing.T, r switcherReading) string {
	t.Helper()
	for _, line := range r.lines {
		if line.row != nil && line.row.fold {
			return line.row.foldWord
		}
	}
	t.Fatalf("the reading has no fold in it:\n%s", switcherText(r, 120))
	return ""
}

// HOME HAS TWO FOLDS AND THEY SAY ONE SENTENCE.
//
// The list's fold ([switcherReading.addFold]) and the one the phone and the
// project tails draw ([homeQuietWord]) are the same idea — how many rows are
// hidden, and how long they have been quiet — and for a wave they were two
// writers with two spellings: `▸ 4 more, quiet since sep 1` against
// `▸ …7 more, quiet since 3h`, a leading ellipsis on one and not the other and a
// calendar date against an elapsed span. [foldWords] and [quietFoldClause] are
// the one source of truth, and this test is the two callers asking it.
func TestBothOfHomesFoldsSpellTheirCountAndTheirQuietOneWay(t *testing.T) {
	now := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	// Thirteen quiet rows into a reading with no room told to it: the floor
	// draws eight and the fold stands over the other five, and the clause is
	// about the ninth row, which is nine days old.
	world := foldLab(now, 13)
	r := readSwitcher(world, nil, nil, switcherHere{}, nil, time.Time{}, now, switcherView{}, switcherLedgerInput{})

	list := foldRowWord(t, r)
	tail := homeQuietWord(homeLine{kind: homeQuiet, quiet: r.hidden, since: now.Add(-9 * 24 * time.Hour), folded: true}, now)

	const want = "5 more, quiet since 9d"
	if list != tokens.GlyphCollapsed+" "+want {
		t.Fatalf("the list's fold drew %q, want %q", list, tokens.GlyphCollapsed+" "+want)
	}
	if tail != want {
		t.Fatalf("the project tail's fold drew %q, want %q", tail, want)
	}
	if list != tokens.GlyphCollapsed+" "+tail {
		t.Fatalf("home's two folds disagree:\n  list %q\n  tail %q (drawn under a %q)", list, tail, tokens.GlyphCollapsed)
	}
	// THE MARK IS THE CALLER'S AND THE ELLIPSIS IS NOBODY'S. A fold that says
	// `▸ …5 more` has drawn two marks for one idea.
	for what, word := range map[string]string{"the list's fold": list, "the project tail's fold": tail} {
		if strings.Contains(word, "…") {
			t.Fatalf("%s drew %q, which wears an ellipsis under a fold mark", what, word)
		}
	}
	// AND THE AGE IS THE LADDER THE ROWS ABOVE IT WEAR. A month name inside
	// thirty days is the calendar spelling coming back.
	for _, month := range []string{"aug", "sep", "Aug", "Sep"} {
		if strings.Contains(list, month) {
			t.Fatalf("the list's fold drew %q, want the elapsed age %q the rows themselves wear", list, "9d")
		}
	}
}

// AN OPENED FOLD SAYS THE WAY BACK, NOT WHAT IT IS NO LONGER HIDING.
//
// `▾ 5 more` over five rows a person can see leaves the glyph as the only thing
// telling "five are hidden" from "five of these are the ones you asked for".
func TestAnOpenedFoldOnHomeSaysHowManyItWouldTakeAway(t *testing.T) {
	now := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	world := foldLab(now, 13)
	r := readSwitcher(world, nil, nil, switcherHere{}, nil, time.Time{}, now, switcherView{all: true}, switcherLedgerInput{})

	list := foldRowWord(t, r)
	tail := homeQuietWord(homeLine{kind: homeQuiet, quiet: r.hidden, since: now.Add(-9 * 24 * time.Hour)}, now)

	const want = "5 fewer"
	if list != tokens.GlyphExpanded+" "+want {
		t.Fatalf("the opened list fold drew %q, want %q", list, tokens.GlyphExpanded+" "+want)
	}
	if tail != want {
		t.Fatalf("the opened project tail drew %q, want %q", tail, want)
	}
	if strings.Contains(list, "more") || strings.Contains(tail, "more") {
		t.Fatalf("an opened fold still says it is hiding rows:\n  list %q\n  tail %q", list, tail)
	}
}
