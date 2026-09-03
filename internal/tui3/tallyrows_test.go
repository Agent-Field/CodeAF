package tui3

// THE COUNT AT THE FOOT OF THE TASKS PLACE, AND THE ROWS ABOVE IT.
//
// The line read `7 done today` over a section drawing four rows, because three
// of the seven were workers folded under a root. A person who reads a count and
// then counts what is under it has found what they will report as a defect, and
// the only thing on the frame reconciling the two was a clause in the middle of
// one row's tail.
//
// THE SEVEN IS THE TRUE NUMBER. The foot is what the PLACE is holding, it is the
// per-section split of the head's own `N pieces of work`, and a tally that
// counted drawn rows instead would disagree with the head by exactly as much as
// it stopped disagreeing with the rows — and would change under somebody opening
// a fold, which is a fact about the screen and not about the work. So the foot
// keeps its number and the SECTION HEADING, standing between the foot and the
// rows, says how much of it is on the page.

import (
	"strings"
	"testing"
	"time"
)

// tasksWorkRows is how many rows of WORK a laid-out page actually draws — the
// thing a person counts.
func tasksWorkRows(lines []tasksLine) int {
	n := 0
	for _, line := range lines {
		if line.kind == tasksLineTask {
			n++
		}
	}
	return n
}

// tasksHeadingRow is the drawn heading over a named section, or "" when the page
// drew none.
func tasksHeadingRow(lines []tasksLine, word string) string {
	for _, line := range lines {
		if line.kind == tasksLineWord && strings.HasPrefix(line.text, word) {
			return line.text
		}
	}
	return ""
}

// THE COUNT AND THE ROWS UNDER IT AGREE ON ONE FRAME, and they agree because the
// heading between them says how much of the count has a row.
func TestTheTasksCountAndTheRowsUnderItAgreeOnOneFrame(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)

	held := len(reading.section(tasksToday))
	lines := reading.lay(120)
	drawn := tasksWorkRows(lines)
	if held == drawn {
		t.Fatalf("the fixture draws every one of its %d pieces of work, so nothing here can see the defect", held)
	}
	// The foot is untouched: it is the head's own split and it counts the work.
	if want := itoa(held) + " " + tasksSectionWord(tasksToday); reading.tally() != want {
		t.Fatalf("the foot says %q, want %q — the tally counts the work and never the rows", reading.tally(), want)
	}
	// And the heading over those rows reconciles it.
	head := tasksHeadingRow(lines, tasksSectionWord(tasksToday))
	if want := tasksSectionWord(tasksToday) + railSep + itoa(drawn) + " of " + itoa(held) + " shown"; head != want {
		t.Fatalf("the foot says %q over a section drawing %d rows of %d, and its heading reads\n  %s\nwant\n  %s",
			reading.tally(), drawn, held, head, want)
	}

	// Open the fold: the rows catch up with the count and the clause goes,
	// because there is nothing left for it to say.
	reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
	lines = reading.lay(120)
	if drawn := tasksWorkRows(lines); drawn != held {
		t.Fatalf("an opened family draws %d rows of %d pieces of work", drawn, held)
	}
	if head := tasksHeadingRow(lines, tasksSectionWord(tasksToday)); head != tasksSectionWord(tasksToday) {
		t.Fatalf("with the fold open the heading still reads %q, and every row is on the page", head)
	}
}

// AND THE `N of M shown` IS COUNTED THE WAY THE PAGE IS BUILT. The clause is only
// worth anything if its number is the number of rows [tasksReading.lay] really
// produces, so it is asserted against those rows and never against the same
// arithmetic written out twice.
func TestTheSectionHeadCountsTheRowsItActuallyDraws(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	for _, open := range []bool{false, true} {
		reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
		if open {
			reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
		}
		items := reading.section(tasksToday)
		if got, want := reading.shown(items), tasksWorkRows(reading.lay(120)); got != want {
			t.Fatalf("with the family %s the heading counts %d rows and the page draws %d",
				map[bool]string{true: "open", false: "shut"}[open], got, want)
		}
	}
}

// A PAGE THAT FOLDS NOTHING SAYS NOTHING EXTRA, on the heading or on the foot.
func TestASectionWithNothingFoldedAwaySaysNothingExtra(t *testing.T) {
	world, win, now := tasksPolishFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	lines := reading.lay(120)
	for _, line := range lines {
		if line.kind == tasksLineWord && strings.Contains(line.text, " shown") {
			t.Fatalf("a page with no fold on it heads a section %q, and nothing is being held back", line.text)
		}
	}
	if strings.Contains(reading.tally(), " shown") {
		t.Fatalf("the foot says %q, and the foot never counts rows", reading.tally())
	}
	if drawn, held := tasksWorkRows(lines), len(reading.items); drawn != held {
		t.Fatalf("the fixture draws %d rows of %d pieces of work, so this case is not the one it says it is", drawn, held)
	}
}

// AND THE FOOT STILL FITS THE PHONE. The clause went onto the heading rather than
// onto this line because this line is FITTED and already carries every section at
// once: at sixty columns a foot with the clause on it came out as
// `… · 5 done today, 2 s…`, a figure with its end cut off.
func TestTheTasksFootFitsTheNarrowestFrameWholeWithAFoldOnThePage(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	said := reading.tally()
	for _, width := range []int{60, 80, 120, 160} {
		if got := fit(said, width-2); got != said {
			t.Fatalf("at %d columns the foot is cut to\n  %s\nfrom\n  %s", width, got, said)
		}
	}
}
