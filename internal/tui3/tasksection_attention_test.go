package tui3

// WHAT NEEDS A PERSON IS A FACT ABOUT ONE PIECE OF WORK.
//
// The tasks place used to file a live row under `needs your look` whenever the
// CONVERSATION that ran it was stopped on a question of its own. A conversation
// is stopped on one question — an approval for a command somebody typed in the
// chat, a connect offer, a standing card — and it is almost never about a task,
// so one `can I run: git push` on screen re-filed every running task of that
// conversation under the section a person reads as "the machine has stopped".
//
// These tests pin the fix: the section is read off the row's own state
// ([taskEntryStatus]), and the conversation's question changes nothing about
// where any row is filed.
//
import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// tasksAskingWorld is ONE conversation with four pieces of work out and one the
// machine could not check. `asking` decides the only thing that differs between
// the two readings these tests compare: whether the conversation itself is
// stopped on a question that has nothing to do with any of them.
func tasksAskingWorld(now time.Time, asking bool) (session.World, session.UsageWindow) {
	row := session.SessionRow{
		ID: "room-a", Title: "shipping the gate", Project: "aforge",
		Open: true, Live: true,
	}
	// The presence file is what says a row is HAPPENING ([session.SessionRow]'s
	// Runs asks it), so the three running nodes and the queued one are named here
	// as a live window names its own work.
	row.Presence = session.SessionPresence{
		SessionID: "room-a", State: session.PresenceIdle, UpdatedAt: now,
		RunningTasks: []session.PresenceTask{
			{ID: "1", Title: "read 40 filings", State: string(session.TaskRunning)},
			{ID: "2", Title: "size the corpus", State: string(session.TaskRunning)},
			{ID: "3", Title: "draft the migration", State: string(session.TaskRunning)},
			{ID: "5", Title: "render the fight clip", State: string(session.TaskQueued)},
		},
	}
	if asking {
		row.Presence.State = session.PresenceWaiting
		row.Presence.Reason = "can I run: git push"
	}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-a", ID: "1", Label: "read 40 filings", Status: string(session.TaskRunning)},
		{SessionID: "room-a", ID: "2", Label: "size the corpus", Status: string(session.TaskRunning)},
		{SessionID: "room-a", ID: "3", Label: "draft the migration", Status: string(session.TaskRunning)},
		{SessionID: "room-a", ID: "4", Label: "rotate the certificate",
			Status: string(session.TaskUnverified), EndedAt: now.Add(-2 * time.Hour)},
		{SessionID: "room-a", ID: "5", Label: "render the fight clip", Status: string(session.TaskQueued)},
		{SessionID: "room-a", ID: "6", Label: "toy-scale validation",
			Status: string(session.TaskDone), EndedAt: now.Add(-3 * time.Hour)},
	}
	world := session.World{
		Projects: []session.Project{{Name: "aforge", Sessions: []session.SessionRow{row}}},
		Read:     now,
	}
	return world, session.LastDays(now, 7)
}

// tasksSectionLabels is what one section is holding, by name, in the order the
// page draws it.
func tasksSectionLabels(reading tasksReading, section tasksSection) []string {
	out := make([]string, 0)
	for _, item := range reading.items {
		if item.section != section {
			continue
		}
		out = append(out, tasksLabel(item.entry))
	}
	return out
}

func tasksSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A CONVERSATION'S OWN QUESTION IS NOT FOUR TASKS' QUESTION. Three running nodes
// and one queued one stay where they are while the window they came out of waits
// for somebody to answer something else entirely, and the one row that really
// does need a person — work nobody could check — is the only thing under the
// section that says so.
func TestAConversationsQuestionDoesNotRefileItsRunningTasks(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	world, win := tasksAskingWorld(now, true)
	reading := readTasks(world, tasksMine{}, win, now.Add(-time.Hour), now)

	needs := tasksSectionLabels(reading, tasksNeeds)
	if !tasksSame(needs, []string{"rotate the certificate"}) {
		t.Fatalf("the conversation's own question re-filed work under %q: %v",
			tasksSectionWord(tasksNeeds), needs)
	}
	running := tasksSectionLabels(reading, tasksRunning)
	for _, want := range []string{"read 40 filings", "size the corpus", "draft the migration"} {
		found := false
		for _, got := range running {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q left the %q section while its conversation was asking about something else: %v",
				want, taskSheetNowHead, running)
		}
	}
	if len(running) != 3 {
		t.Fatalf("the %q section holds %d rows, want the three that are working: %v",
			taskSheetNowHead, len(running), running)
	}
	// AND THE QUEUED NODE IS STILL WAITING FOR A SLOT and not for a person: it
	// was swept up by the same clause, because it is admitted and live.
	parked := tasksSectionLabels(reading, tasksParked)
	if !tasksSame(parked, []string{"render the fight clip"}) {
		t.Fatalf("the queued row is not under %q: %v", tasksSectionWord(tasksParked), parked)
	}
}

// THE COUNT LINE SAID IT TWICE. The foot is the per-section split of the head's
// own count, so a mis-filed row is a wrong number as well as a wrong heading —
// `4 needs your look` on a machine with three workers in worktrees.
func TestTheRunningCountSurvivesAConversationsQuestion(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	world, win := tasksAskingWorld(now, true)
	reading := readTasks(world, tasksMine{}, win, now.Add(-time.Hour), now)

	want := strings.Join([]string{
		"1 " + tasksSectionWord(tasksNeeds),
		"3 " + tasksSectionWord(tasksRunning),
		"1 " + tasksSectionWord(tasksParked),
		"1 " + tasksSectionWord(tasksToday),
	}, railSep)
	if got := reading.tally(); got != want {
		t.Fatalf("the foot counts\n\t%s\nwant\n\t%s", got, want)
	}
	if reading.whole != 6 {
		t.Fatalf("the head counts %d pieces of work, want 6", reading.whole)
	}
	// The mark on a working row is the working one. A row filed under `needs your
	// look` wears the steer mark ([tasksGlyph]), which is the half of this defect
	// a person actually sees.
	rows := reading.rows(120, newPalette(tokens.NoColor, false))
	line := ""
	for _, drawn := range rows {
		if strings.Contains(drawn, "read 40 filings") {
			line = drawn
		}
	}
	if line == "" {
		t.Fatalf("the running row was not drawn at all:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(line, tokens.GlyphNeedsHuman) {
		t.Fatalf("a working row wears the steer mark:\n\t%s", line)
	}
	if !strings.Contains(line, tokens.GlyphWorking) {
		t.Fatalf("a working row does not wear %q:\n\t%s", tokens.GlyphWorking, line)
	}
}

// THE PAGE GROUPS THE SAME WHATEVER THE WINDOW IS DOING. Two readings of the
// same work, one taken while the conversation is stopped on a question and one
// while it is idle, put every row in the same place — which is the whole claim,
// stated as a comparison rather than as a list of expected sections.
func TestTheGroupingIsTheSameWhetherOrNotTheConversationIsAsking(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	calmWorld, win := tasksAskingWorld(now, false)
	askingWorld, _ := tasksAskingWorld(now, true)
	calm := readTasks(calmWorld, tasksMine{}, win, now.Add(-time.Hour), now)
	asking := readTasks(askingWorld, tasksMine{}, win, now.Add(-time.Hour), now)

	for _, section := range tasksSectionOrder {
		want := tasksSectionLabels(calm, section)
		got := tasksSectionLabels(asking, section)
		if !tasksSame(want, got) {
			t.Fatalf("%q holds %v while the conversation is asking and %v while it is idle",
				tasksSectionWord(section), got, want)
		}
	}
	if calm.tally() != asking.tally() {
		t.Fatalf("the foot reads\n\t%s\nwhile the conversation is asking and\n\t%s\nwhile it is idle",
			asking.tally(), calm.tally())
	}
}

// WORK A PERSON HAS TO DECIDE ABOUT IS STILL UNDER `needs your look`. Nothing
// but a person moves an unverified row ([session.Agent]'s ResolveUnverified), so
// it belongs there whether or not the conversation that ran it is alive at all —
// and it is the case the conversation-level clause was hiding rather than
// finding.
func TestWorkNobodyCouldCheckStillNeedsYourLook(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	quiet := session.SessionRow{ID: "room-b", Title: "thor clips", Project: "media"}
	quiet.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-b", ID: "1", Label: "verify the pro model's pricing",
			Status: string(session.TaskUnverified), EndedAt: now.Add(-time.Hour)},
		{SessionID: "room-b", ID: "2", Label: "summarise loud movers",
			Status: string(session.TaskDone), EndedAt: now.Add(-2 * time.Hour)},
	}
	world := session.World{
		Projects: []session.Project{{Name: "media", Sessions: []session.SessionRow{quiet}}},
		Read:     now,
	}
	reading := readTasks(world, tasksMine{}, session.LastDays(now, 7), now.Add(-time.Hour), now)
	if got := tasksSectionLabels(reading, tasksNeeds); !tasksSame(got, []string{"verify the pro model's pricing"}) {
		t.Fatalf("work nobody could check is not under %q: %v", tasksSectionWord(tasksNeeds), got)
	}
	page := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if !strings.Contains(page, tokens.GlyphNeedsHuman+" verify the pro model's pricing") {
		t.Fatalf("the row needing a decision did not wear %q:\n%s", tokens.GlyphNeedsHuman, page)
	}
}

// AND THE SECTION IS THE READING'S OWN ANSWER, NOT A SECOND TABLE. Every row on
// the page is under `needs your look` exactly when
// [session.TaskStatus.Attention] says the node will not move without a person —
// which is what carries the two deliberate cases this page must keep: work
// nobody could check, and edits left on a branch that never came home
// ([session.TaskStatus.ChangesUnlanded]), wherever the facts behind a row can
// say so.
func TestTheNeedsSectionIsTheWorksOwnReading(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 11, 0, 0, time.UTC)
	for _, asking := range []bool{false, true} {
		world, win := tasksAskingWorld(now, asking)
		// A dead window's live-looking rows are in the sweep too: the record
		// claims running and nothing is behind it.
		gone := session.SessionRow{ID: "room-c", Title: "gone", Project: "aforge"}
		gone.Tasks.Rows = []session.TaskIndexEntry{
			{SessionID: "room-c", ID: "1", Label: "the certificate rotation", Status: string(session.TaskRunning)},
			{SessionID: "room-c", ID: "2", Label: "install the render toolchain",
				Status: string(session.TaskFailed), Ending: session.TaskEndingError, EndedAt: now.Add(-time.Hour)},
		}
		world.Projects = append(world.Projects, session.Project{
			Name: "aforge", Sessions: []session.SessionRow{gone},
		})
		reading := readTasks(world, tasksMine{}, win, now.Add(-time.Hour), now)
		if len(reading.items) == 0 {
			t.Fatalf("the reading drew nothing to judge (asking=%v)", asking)
		}
		for _, item := range reading.items {
			want := taskEntryStatus(item.entry, item.runs).Attention
			got := item.section == tasksNeeds
			if got != want {
				t.Fatalf("%q is under %q (asking=%v) while its own reading says needs-a-person=%v",
					tasksLabel(item.entry), tasksSectionWord(item.section), asking, want)
			}
		}
	}
}
