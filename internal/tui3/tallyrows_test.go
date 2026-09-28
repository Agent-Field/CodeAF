package tui3

// THE COUNT AT THE FOOT OF THE TASKS PLACE, AND THE ROWS ABOVE IT.
//
// The line read `7 done today` over a section drawing four rows, because three
// of the seven were workers folded under a root. A person who reads a count and
// then counts what is under it has found what they will report as a defect.
//
// THE SEVEN AND THE FOUR ANSWER DIFFERENT QUESTIONS. The foot counts what each
// piece of work is, while the page files a conversation and all its work under
// where the conversation stands. The section heading says only what its folds
// hide; it never presents either partition as the other one's total.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
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

// THE HEADING SAYS WHAT THE FOLD IS HOLDING BACK, AND NOTHING ELSE. It used to
// read `2 of 5 shown` — a total for the section in the same words the foot uses
// for a different partition of the same rows, so the two contradicted each other
// on one frame. The foot is untouched here, the shut heading names exactly the
// rows the page is withholding, and opening the fold leaves it nothing to say.
func TestTheTasksSectionHeadNamesOnlyWhatTheFoldHolds(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: false}

	held := len(reading.section(tasksCompleted))
	lines := reading.lay(120)
	drawn := tasksWorkRows(lines)
	frame := strings.Join(reading.rows(120, palette{}), "\n")
	if held == drawn {
		t.Fatalf("the fixture draws every one of its %d pieces of work, so nothing here can see the defect:\n%s", held, frame)
	}
	// The foot is untouched: it is the head's own split and it counts the work.
	if want := itoa(held) + " " + tasksSectionWord(tasksToday); reading.tally() != want {
		t.Fatalf("the foot says %q, want %q — the tally counts the work and never the rows:\n%s", reading.tally(), want, frame)
	}
	// The heading over those rows says only what the fold withholds.
	head := tasksHeadingRow(lines, tasksSectionWord(tasksCompleted))
	if want := tasksSectionWord(tasksCompleted) + railSep + itoa(held-drawn) + " folded away"; head != want {
		t.Fatalf("the foot says %q over a section drawing %d rows of %d, and its heading reads\n  %s\nwant\n  %s\n%s",
			reading.tally(), drawn, held, head, want, frame)
	}

	// Open the fold: the rows catch up with the count and the clause goes,
	// because there is nothing left for it to say.
	reading.open = map[tasksKey]bool{tasksChatKey("room-a"): true, {session: "room-a", id: "1"}: true}
	lines = reading.lay(120)
	frame = strings.Join(reading.rows(120, palette{}), "\n")
	if drawn := tasksWorkRows(lines); drawn != held {
		t.Fatalf("an opened family draws %d rows of %d pieces of work:\n%s", drawn, held, frame)
	}
	if head := tasksHeadingRow(lines, tasksSectionWord(tasksCompleted)); head != tasksSectionWord(tasksCompleted) {
		t.Fatalf("with the fold open the heading still reads %q, and every row is on the page:\n%s", head, frame)
	}
}

// AND THE FOLD CLAUSE COUNTS WHAT THE LAID-OUT PAGE REALLY WITHHOLDS. The clause
// is only worth anything if its number is the work [tasksReading.lay] really left
// off the page, so it is asserted against those rows at both fold states rather
// than against the same arithmetic written out twice.
func TestTheSectionHeadCountsTheRowsItActuallyWithholds(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	for _, open := range []bool{false, true} {
		reading := readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
		reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: false}
		if open {
			reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
		}
		lines := reading.lay(120)
		held := len(reading.section(tasksCompleted))
		withheld := held - tasksWorkRows(lines)
		want := tasksSectionWord(tasksCompleted)
		if withheld > 0 {
			want += railSep + itoa(withheld) + " folded away"
		}
		if got := tasksHeadingRow(lines, tasksSectionWord(tasksCompleted)); got != want {
			t.Fatalf("with the family %s, %d laid-out rows are withheld but the heading is %q, want %q:\n%s",
				map[bool]string{true: "open", false: "shut"}[open], withheld, got, want,
				strings.Join(reading.rows(120, palette{}), "\n"))
		}
	}
}

// A MIXED CONVERSATION DOES NOT TURN A SECTION HEADING INTO A SECOND FOOT. A
// conversation is filed with all of its work under where the CONVERSATION stands,
// so a page of mixed families is exactly where a per-section total on the heading
// contradicted the foot's number for the same word. Every heading here is either
// its word alone or the exact count of work its folds are withholding.
func TestAMixedPageHeadsSectionsOnlyWithTheirFoldedRows(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	lines := reading.lay(120)
	tree := reading.tree()
	frame := strings.Join(reading.rows(120, palette{}), "\n")

	for _, section := range tasksSectionOrder {
		word := tasksSectionWord(section)
		head := tasksHeadingRow(lines, word)
		if head == "" {
			continue
		}
		if strings.Contains(head, " shown") {
			t.Fatalf("the mixed page still presents a section total in %q:\n%s", head, frame)
		}
		drawn := 0
		for _, line := range lines {
			if line.kind == tasksLineTask && tree.filed[tasksKeyOf(line.item.entry)] == section {
				drawn++
			}
		}
		withheld := tree.held(section) - drawn
		want := word
		if withheld > 0 {
			want += railSep + itoa(withheld) + " folded away"
		}
		if head != want {
			t.Fatalf("the %q section draws %d of %d filed rows but is headed %q, want %q:\n%s",
				word, drawn, tree.held(section), head, want, frame)
		}
	}
}

// A PAGE THAT FOLDS NOTHING SAYS NOTHING EXTRA, on the heading or on the foot.
func TestASectionWithNothingFoldedAwaySaysNothingExtra(t *testing.T) {
	world, win, now := tasksPolishFixture()
	// The page after `→`: what this is about is a page with NOTHING held back,
	// and every conversation opens shut ([tasksReading.opens]).
	reading := tasksOpen(readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now))
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
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	said := reading.tally()
	for _, width := range []int{60, 80, 120, 160} {
		if got := fit(said, width-2); got != said {
			t.Fatalf("at %d columns the foot is cut to\n  %s\nfrom\n  %s", width, got, said)
		}
	}
}

// THE FOOT STILL COUNTS WHAT EACH PIECE OF WORK IS. A conversation asking
// its own question cannot turn three working rows, one queued row and one
// finished row into more work for the person.
func TestTheTasksFootStillCountsEveryPieceByItsActualState(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	world, win := tasksAskingWorld(now, true)
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, now.Add(-time.Hour), now)
	want := strings.Join([]string{
		"1 " + tasksSectionWord(tasksNeeds),
		"3 " + tasksSectionWord(tasksRunning),
		"1 " + tasksSectionWord(tasksParked),
		"1 " + tasksSectionWord(tasksToday),
	}, railSep)
	if got := reading.tally(); got != want {
		t.Fatalf("the actual-state foot reads %q, want %q:\n%s", got, want,
			strings.Join(reading.rows(120, palette{}), "\n"))
	}
}

// BOTH OPENING HEADINGS THE CODE BUILDS ARE WORDS THE MANUAL KNOWS. The page has
// two of them and the manual quoted only the second, which is the one a machine
// with any conversation on it never sees. The readings here exercise both
// branches and the corpus is asked for what they produced, so neither heading can
// be restated as a literal that drifts.
func TestTheManualQuotesBothTasksOpeningHeadingsExactly(t *testing.T) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.August, 25, 13, 11, 0, 0, loc)
	win := session.LastDays(now, 15)

	chats := make([]session.SessionRow, 15)
	for i := range chats {
		chats[i] = session.SessionRow{
			ID: itoa(i + 1), Title: "chat " + itoa(i+1), Transcript: "/journals/" + itoa(i+1), At: now,
		}
	}
	for i := 0; i < 13; i++ {
		chats[0].Tasks.Rows = append(chats[0].Tasks.Rows, session.TaskIndexEntry{
			ID: itoa(i + 1), SessionID: chats[0].ID, Label: "work " + itoa(i+1),
			Status: string(session.TaskDone), EndedAt: now,
		})
	}
	chats[0].Tasks.Rows[0].Cost = 2.98
	conversationReading := readTasks(session.World{Projects: []session.Project{{Sessions: chats}}}, tasksMine{}, win, tasksSort{}, time.Time{}, now)

	flat := session.SessionRow{ID: "flat", Title: "flat work", At: now}
	for i := 0; i < 148; i++ {
		flat.Tasks.Rows = append(flat.Tasks.Rows, session.TaskIndexEntry{
			ID: itoa(i + 1), SessionID: flat.ID, Label: "work " + itoa(i+1),
			Status: string(session.TaskDone), EndedAt: now,
		})
	}
	flat.Tasks.Rows[0].Cost = 34.10
	flatReading := readTasks(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{flat}}}}, tasksMine{}, win, tasksSort{}, time.Time{}, now)

	for _, heading := range []string{conversationReading.head(200, false), flatReading.head(200, true)} {
		if !manual.Chat().Mentions(heading) {
			t.Fatalf("the tasks page opens with %q, which the chat manual does not quote", heading)
		}
	}
}

// THE GROUP HEADINGS THE MANUAL QUOTES ARE BUILT FROM THE ROSTER'S OWN WORD
// TABLE. The column's footer of counts is gone and its headings carry the
// counts now (`Running 4`, `Done 6 ▸`); the words moved once before and four
// passages went on teaching the words it had dropped, so the next move of the
// table fails here rather than in front of somebody reading the page.
func TestTheManualQuotesTheRosterGroupsOwnWords(t *testing.T) {
	wants := []string{
		railHeadWords[railRunning] + " " + itoa(4),
		railHeadWords[railIdle] + " " + itoa(3) + " " + glyphShut,
		railHeadWords[railDone] + " " + itoa(6) + " " + glyphShut,
	}
	for _, want := range wants {
		if !manual.Chat().Mentions(want) {
			t.Fatalf("the chat manual does not quote the roster heading %q in the table's own words", want)
		}
	}
}
