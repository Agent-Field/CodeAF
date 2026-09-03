package tui3

// WORK NOTHING IS DOING IS NOT `running`, AND THE TWO SURFACES SAY ONE WORD.
//
// The tasks place carries one liveness answer per row ([tasksItem.runs]) and it
// is true of a node that is merely ADMITTED as much as of one a worker is inside.
// So the two nodes waiting behind the piece that needs a person were drawn under
// `running`, dated `now`, and counted `2 running` in the foot — on a machine
// where nothing at all was executing — while the column one keypress away called
// the same two nodes `2 parked`.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// tasksParkedFixture is the shape the demo home seeds and the audit captured:
// one piece of work waiting on a person, and two admitted behind it that nothing
// has started.
func tasksParkedFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.September, 2, 23, 52, 0, 0, loc)
	row := session.SessionRow{ID: "room-a", Title: "The Task Surface", Project: "aforge", Open: true, Live: false}
	entry := func(id, label, status string) session.TaskIndexEntry {
		return session.TaskIndexEntry{ID: id, Label: label, Title: label, SessionID: "room-a", Status: status}
	}
	row.Tasks.Rows = []session.TaskIndexEntry{
		entry("1", "Measure the frame the task surface draws in", string(session.TaskUnverified)),
		entry("2", "Write the change entry and open the pull request against dev", string(session.TaskQueued)),
		entry("3", "Fold the settled work on the task page", string(session.TaskQueued)),
		entry("4", "Cut every list over to the shared row fitter", string(session.TaskRunning)),
	}
	world := session.World{Projects: []session.Project{
		{Name: "aforge", Sessions: []session.SessionRow{row}},
	}, Read: now}
	return world, session.LastDays(now, taskSheetDays), now
}

// TestWorkParkedOnAPersonIsNotFiledUnderRunning is the row itself: the section,
// the count in the foot, and the age beside it.
func TestWorkParkedOnAPersonIsNotFiledUnderRunning(t *testing.T) {
	world, win, now := tasksParkedFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)

	sections := map[string]tasksSection{}
	items := map[string]tasksItem{}
	for _, item := range reading.items {
		sections[item.entry.ID] = item.section
		items[item.entry.ID] = item
	}
	for _, id := range []string{"2", "3"} {
		if got := sections[id]; got != tasksParked {
			t.Fatalf("a node nothing has started is filed under %q, and nothing is executing on this machine — it belongs under %q",
				tasksSectionWord(got), tasksSectionWord(tasksParked))
		}
		// AND IT IS NOT DATED `now`. A row that has not started has no age at
		// either end of it, and `now` said the work was happening this second.
		if age := tasksAgeField(items[id], now).full; age != "" {
			t.Fatalf("a node nothing has started is aged %q on the page, want nothing at all", age)
		}
	}
	// The node a worker IS inside keeps `running`, which is the half of the old
	// answer that was true.
	if got := sections["4"]; got != tasksRunning {
		t.Fatalf("work a worker is actually in is filed under %q, want %q",
			tasksSectionWord(got), tasksSectionWord(tasksRunning))
	}

	// THE FOOT COUNTS WHAT THE HEADINGS SAY. `2 running` over two parked nodes is
	// the sentence a developer read as two workers burning tokens somewhere.
	tally := reading.tally()
	for _, want := range []string{"1 " + tasksSectionWord(tasksNeeds), "1 " + taskSheetNowHead, "2 " + tasksSectionWord(tasksParked)} {
		if !strings.Contains(tally, want) {
			t.Fatalf("the foot reads %q and should count %q", tally, want)
		}
	}
	if strings.Contains(tally, "3 "+taskSheetNowHead) {
		t.Fatalf("the foot reads %q — it is counting parked work as running", tally)
	}
}

// TestTheTasksPlaceAndTheColumnCallParkedWorkOneWord is the law under the fix:
// one source of truth for a word two surfaces draw. The column has said `parked`
// since it grew its five headings; the place reads that word rather than
// spelling a second one, so the two can never drift apart again.
func TestTheTasksPlaceAndTheColumnCallParkedWorkOneWord(t *testing.T) {
	place := tasksSectionWord(tasksParked)
	column := railGroupWords[railParked]
	if place != column {
		t.Fatalf("the tasks place calls this work %q and the column calls it %q — one fact, two surfaces, two words",
			place, column)
	}
	// AND THE WORD IS NOT THE MACHINERY'S. The place's own headings are the
	// vocabulary a person reads, and every one of them is a plain word about
	// work rather than about the scheduler moving it.
	for _, section := range tasksSectionOrder {
		word := tasksSectionWord(section)
		for _, banned := range []string{"auditor", "verdict", "verified", "refuted", "spawned", "enqueued"} {
			if strings.Contains(strings.ToLower(word), banned) {
				t.Fatalf("the %q heading is machinery vocabulary (%q)", word, banned)
			}
		}
	}
}
