package tui3

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func c263PlanRows() []session.PlanTaskRow {
	live := livePlanRow()
	live.ID, live.Parent, live.Title = "live", "root", "implement handler"
	return []session.PlanTaskRow{
		{ID: "root", Title: "rewrite the auth", Status: "running", Done: 2, Running: 1, Queued: 1, Total: 4},
		live,
		{ID: "gate", Parent: "root", Title: "schema migration", Status: "running"},
		{ID: "queued", Parent: "root", Title: "integration tests", Status: "pending", Waits: []string{"gate"}},
		{ID: "done-a", Parent: "root", Title: "old fixture", Status: "done"},
		{ID: "done-b", Parent: "root", Title: "old helper", Status: "done"},
	}
}

// A RUN'S ROWS ARE THE OLD TASK ROWS. Every part is its own row in the old
// tree's connectors — the finished ones included, never folded to a count —
// and the part in flight says its call the way a node row says one.
func TestRailPlanDrawsEveryPartAsATaskRow(t *testing.T) {
	a, _ := planAppWith(t, c263PlanRows(), nil)
	a.width, a.height, a.railWide = 120, 30, true
	if !openTaskPlaceWithRows(a) {
		t.Fatal("tasks place did not read the plan")
	}
	got := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	for _, word := range []string{"rewrite the auth", "implement handler", "schema migration",
		"integration tests", "old fixture", "old helper", "bash git grep", "#root", "#live"} {
		if !strings.Contains(got, word) {
			t.Fatalf("the rail lacks %q:\n%s", word, got)
		}
	}
	for _, gone := range []string{"2 done", "2/4", "$ git grep"} {
		if strings.Contains(got, gone) {
			t.Fatalf("the rail still draws the run renderer's %q:\n%s", gone, got)
		}
	}
	for _, row := range a.railRows(a.viewHeight()) {
		if ansi.StringWidth(row) > a.railWidth() {
			t.Fatalf("rail row is %d cells in a %d-cell rail: %q", ansi.StringWidth(row), a.railWidth(), row)
		}
	}
}

// THE RAIL DRAWS THE TREE IN A CONVERSATION NOBODY HAS OPENED THE TASKS PLACE
// IN. The person sits in the chat; the tasks place is a room they may never walk
// into, and a rail that waited for that walk would show no tree in the one
// place the owner asked for it.
func TestRailPlanDrawsWithoutTheTasksPlaceEverOpening(t *testing.T) {
	a, _ := planAppWith(t, c263PlanRows(), nil)
	a.width, a.height, a.railWide = 120, 30, true
	// The paint clock's own beat, which is what moves the stamp the reading's
	// freshness hangs on ([app.refreshElsewhere], [tasksPlace.regroup]).
	a.refreshElsewhere()
	got := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	for _, word := range []string{"rewrite the auth", "implement handler", "schema migration"} {
		if !strings.Contains(got, word) {
			t.Fatalf("the rail of a conversation that never opened the tasks place lacks %q:\n%s", word, got)
		}
	}
}

// railShape is one drawn rail line with the WORDS taken out: every letter and
// digit is one `x`, and everything else — the seam, the tree's connectors, the
// state marks, the separators and the spacing — is kept byte for byte. Two rows
// with the same shape are the same row with different words in it.
func railShape(line string) string {
	var b strings.Builder
	for _, r := range plain(line) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune('x')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}

// EVERY TASK LOOKS THE SAME, AND THIS IS THE PROOF OF IT. One rail holds a run
// from the plan store — its own row and three parts — and a family of this
// window's own nodes built the same way: a running head, a finished child, a
// running child and a queued one, with the same clock, the same price and ids
// of the same width. The two blocks are drawn by one renderer, so with the
// words taken out they are the same bytes, line for line: the same spinner at
// the same size, the same `#id` slot, the same `time · cost` line, the same
// connectors. A run renderer of its own — a still half-circle, no handle, a
// `✓ N done` fold — fails this on its first line.
func TestARunAndANodeFamilyDrawTheSameShapeOnTheRail(t *testing.T) {
	now := taskFixtureNow
	rows := []session.PlanTaskRow{
		{ID: "t-9", Title: "Bravo work", Status: "claimed", Started: now.Add(-4 * time.Minute), USD: 0.02},
		{ID: "t-a", Parent: "t-9", Title: "Kid one B", Status: "done", Started: now.Add(-3 * time.Minute), Ended: now.Add(-time.Minute)},
		{ID: "t-b", Parent: "t-9", Title: "Kid two B", Status: "claimed", Started: now.Add(-4 * time.Minute), USD: 0.02},
		{ID: "t-c", Parent: "t-9", Title: "Kid six B", Status: "pending"},
	}
	a, _ := planAppWith(t, rows, nil)
	a.width, a.height = 160, 30
	running := session.TaskNotice{Elapsed: 4 * time.Minute, CostUSD: 0.02}
	a.taskUpdate(update(1, "Alpha work", session.TaskRunning, running))
	a.taskUpdate(update(2, "Kid one A", session.TaskDone, session.TaskNotice{}))
	a.taskUpdate(update(3, "Kid two A", session.TaskRunning, running))
	a.taskUpdate(update(4, "Kid six A", session.TaskQueued, session.TaskNotice{}))
	railKinship(a, 1, 2, 3, 4)
	a.paints = 0
	readPlanRows(t, a)

	lines := railText(a, a.viewHeight())
	at := func(title string) int {
		for i, line := range lines {
			if strings.Contains(line, title) {
				return i
			}
		}
		t.Fatalf("the rail has no row for %q:\n%s", title, strings.Join(lines, "\n"))
		return -1
	}
	run, family := at("Bravo work"), at("Alpha work")
	if run > family {
		t.Fatalf("the run is drawn at %d and the family at %d; the run's rows stand ahead of the node rows:\n%s",
			run, family, strings.Join(lines, "\n"))
	}
	runBlock, familyBlock := lines[run:family], lines[family:family+(family-run)]
	for i := range runBlock {
		if got, want := railShape(runBlock[i]), railShape(familyBlock[i]); got != want {
			t.Fatalf("line %d of the run is shaped\n%q\nand the same line of the node family is\n%q\n\nrun:\n%s\n\nfamily:\n%s",
				i, got, want, strings.Join(runBlock, "\n"), strings.Join(familyBlock, "\n"))
		}
	}
	// AND THE SHAPE IS THE OLD ONE: the braille spinner on the rows that are
	// working, a handle at the end of every row, the clock and the price under
	// the running head, and every part its own row.
	spinner := tokens.Spinner(0)
	if !strings.HasPrefix(strings.TrimPrefix(runBlock[0], railSeam), spinner+" Bravo work") {
		t.Fatalf("the run's row does not lead with the working spinner %q:\n%s", spinner, runBlock[0])
	}
	if !strings.HasSuffix(strings.TrimRight(runBlock[0], " "), "#9") {
		t.Fatalf("the run's row does not end in its handle:\n%s", runBlock[0])
	}
	if !strings.Contains(runBlock[1], "4m · $0.02") {
		t.Fatalf("the run's under-row is not the clock and the price:\n%s", strings.Join(runBlock, "\n"))
	}
	if strings.Contains(strings.Join(runBlock, "\n"), "done") && !strings.Contains(strings.Join(runBlock, "\n"), "Kid one B") {
		t.Fatalf("the run's finished part was folded into a count:\n%s", strings.Join(runBlock, "\n"))
	}
}

// A NODE ROW THAT CARRIES A RUN IS THE RUN'S ROW, and the run's parts hang under
// it in the tree's own connectors. The row keeps its node — its handle, its
// telemetry and its door — and is drawn as the head of a family.
func TestARunsPartsHangUnderTheNodeRowThatCarriesIt(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-6", Title: "Sweep the issues", Status: "claimed"}
	kid := session.PlanTaskRow{ID: "t-k3x9qa", Parent: "t-6", Title: "Check the fix", Status: "claimed",
		Started: taskFixtureNow.Add(-time.Minute), USD: 0.01}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, kid}, nil)
	a.width, a.height = 160, 30
	a.taskUpdate(update(6, root.Title, session.TaskRunning, session.TaskNotice{PlanTask: "t-6", Elapsed: time.Minute}))
	a.paints = 0
	readPlanRows(t, a)

	lines := railText(a, a.viewHeight())
	text := strings.Join(lines, "\n")
	if strings.Count(text, "Sweep the issues") != 1 {
		t.Fatalf("the run is drawn %d times, want once:\n%s", strings.Count(text, "Sweep the issues"), text)
	}
	head, part := -1, -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "Sweep the issues"):
			head = i
		case strings.Contains(line, "Check the fix"):
			part = i
		}
	}
	if head < 0 || part <= head {
		t.Fatalf("the part is at %d and its run at %d, want it under the run:\n%s", part, head, text)
	}
	if !strings.HasSuffix(strings.TrimRight(lines[head], " "), "#6") || !strings.HasSuffix(strings.TrimRight(lines[part], " "), "#k3x9qa") {
		t.Fatalf("the run and its part do not wear their handles:\n%s", text)
	}
	if !strings.Contains(lines[part], treeLast) {
		t.Fatalf("the part does not hang from the tree's connector:\n%s", text)
	}
}

// A RUN'S TASK OPENS THE TASK ROOM AND WEARS ITS HEAD: the trail with the way
// back at its end, and the facts with the clock, the steps and the money. The
// figures the store has not got are absent, never zero.
func TestARunsTaskWearsTheTaskRoomsHead(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "claimed", Steps: 17, USD: 0.02,
		Started: taskFixtureNow.Add(-4 * time.Minute)}
	pages := map[string]session.PlanTaskPage{"t-alpha": {Row: row, Description: "the work order",
		Steps: []session.PlanStep{{Step: 1, Command: "gh issue list", Observation: "12 issues"}}}}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, pages)
	a.height = 40
	openPlanRoomNow(t, a, "t-alpha")
	head := plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	lines := strings.Split(head, "\n")
	if !strings.Contains(head, "Alpha") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), roomBackWord) {
		t.Fatalf("the room's head is not the trail with the way back over the task:\n%s", head)
	}
	for _, want := range []string{"4m", "$0.02"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the room's head does not say %q:\n%s", want, head)
		}
	}
	if got := roomStepsWord(a); got != "17 steps" {
		t.Fatalf("the room's facts count %q, want the store's 17 steps", got)
	}
	if got := roomCommands(a); len(got) != 1 || got[0] != "gh issue list" {
		t.Fatalf("the task's step is not the room's shell row: %q", got)
	}

	// AND A TASK THAT HAS NOT STARTED OR SPENT SAYS NEITHER.
	bare := session.PlanTaskRow{ID: "t-beta", Title: "Beta", Status: "pending"}
	b, _ := planAppWith(t, []session.PlanTaskRow{bare}, map[string]session.PlanTaskPage{"t-beta": {Row: bare}})
	openPlanRoomNow(t, b, "t-beta")
	facts := plain(strings.Join(b.roomHeadRows(b.width), "\n"))
	for _, zero := range []string{"$0", "0 steps", "0s"} {
		if strings.Contains(facts, zero) {
			t.Fatalf("an unstarted task's facts rule says %q:\n%s", zero, facts)
		}
	}
}

// A PART WITH NO STEP IN FLIGHT DRAWS NO LIVE LINE. The page used to draw the
// running mark and the shell lead under a part that had landed, with nothing
// after them (`◑ $`), because the line was drawn whatever the part's live
// step said. A part in the room is the rail's row, and a row with no call says
// none.
func TestAPartWithNoStepInFlightDrawsNoLiveLine(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "claimed"}
	landed := session.PlanTaskRow{ID: "t-landed", Parent: "t-root", Title: "Landed part", Status: "done"}
	empty := session.PlanTaskRow{ID: "t-empty", Parent: "t-root", Title: "Empty call", Status: "claimed"}
	empty.Live.Step = 3
	pages := map[string]session.PlanTaskPage{"t-root": {Row: root, Children: []session.PlanTaskRow{landed, empty}}}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, landed, empty}, pages)
	a.height = 40
	openPlanRoomNow(t, a, "t-root")
	text := planRoomText(t, a)
	if !strings.Contains(text, "Landed part") || !strings.Contains(text, "Empty call") {
		t.Fatalf("the room lacks its parts:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, " │├└─"))
		if strings.HasSuffix(trimmed, tokens.GlyphShell) || trimmed == tokens.GlyphShell {
			t.Fatalf("a part with no command in flight drew an empty live line %q:\n%s", line, text)
		}
	}
	if got := planLiveRow("", nil, 40, a.pal); got != "" {
		t.Fatalf("an empty command drew a live line %q", got)
	}
}
