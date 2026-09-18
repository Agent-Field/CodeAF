package tui3

// The run's plan as the tasks place draws it: the rows the store answers, the
// page one of those rows opens, and the page a conversation with no plan still
// draws. Every fixture is the store's own reading — the shape
// [session.Agent.PlanTasks] hands over — so nothing here seeds a store or runs a
// worker; the place is driven by the same fake agent its neighbours use.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// planFake is [taskFake] widened by the plan seam this place asserts: the rows a
// conversation's store answers, the page one row opens, and the six steering
// verbs a person's keys turn into. It is the same fake the pane's other tests
// run against — a task session that answers a plan is a widening of one and not
// a different one.
type planFake struct {
	*taskFake
	plan  []session.PlanTaskRow
	pages map[string]session.PlanTaskPage

	// THE SIX VERBS, each recording the call it was asked for so a test can read
	// back the id and the words a key produced. `refuse` is the sentence every one
	// of them answers instead of acting, which is how the store's own refusal is
	// put in front of the pane.
	noted     []planCall
	amended   []planCall
	sized     []planCall
	paused    []string
	resumed   []string
	cancelled []string
	refuse    error
}

// planCall is one verb call as the pane made it: the id, the words when the verb
// carries any, and the number a priority names.
type planCall struct {
	id   string
	text string
	n    int
}

func (f *planFake) PlanTasks() []session.PlanTaskRow { return f.plan }

// PlanRunSummary and RefreshRunSummary keep this general plan fixture on the
// local-store door while answering that it has no stored summary. Summary tests
// widen the fixture and override both methods with their scripted answers.
func (f *planFake) PlanRunSummary(string) (session.RunPlanSummary, bool) {
	return session.RunPlanSummary{}, false
}

func (f *planFake) RefreshRunSummary(context.Context, string, time.Time) (session.RunPlanSummary, bool) {
	return session.RunPlanSummary{}, false
}

func (f *planFake) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	page, ok := f.pages[id]
	return page, ok
}

func (f *planFake) PlanNote(id, text string) error {
	f.noted = append(f.noted, planCall{id: id, text: text})
	if f.refuse == nil {
		f.addNote(id, text)
	}
	return f.refuse
}

// addNote writes a person's note back over the store page, the way the store's
// note verb does: the next read carries it with its author and its moment, which
// is the receipt the page draws and the store cannot draw itself.
func (f *planFake) addNote(id, text string) {
	page, ok := f.pages[id]
	if !ok {
		return
	}
	page.Notes = append(page.Notes, session.PlanTaskNote{Person: true, Body: text, At: taskFixtureNow})
	f.pages[id] = page
}

func (f *planFake) PlanPause(id string) error {
	f.paused = append(f.paused, id)
	if f.refuse == nil {
		f.setStatus(id, "paused")
	}
	return f.refuse
}

func (f *planFake) PlanResume(id string) error {
	f.resumed = append(f.resumed, id)
	if f.refuse == nil {
		f.setStatus(id, "running")
	}
	return f.refuse
}

func (f *planFake) PlanCancel(id string) error {
	f.cancelled = append(f.cancelled, id)
	if f.refuse == nil {
		f.setStatus(id, "cancelled")
	}
	return f.refuse
}

func (f *planFake) PlanAmend(id, text string) error {
	f.amended = append(f.amended, planCall{id: id, text: text})
	return f.refuse
}

func (f *planFake) PlanPriority(id string, n int) error {
	f.sized = append(f.sized, planCall{id: id, n: n})
	return f.refuse
}

// setStatus writes a verb's effect back over the store row, which is what makes a
// second `p` resume what the first paused: the toggle reads the status fresh
// ([app.taskPlanPaused]) and the pane's next reading wears the new word.
func (f *planFake) setStatus(id, status string) {
	for i := range f.plan {
		if f.plan[i].ID == id {
			f.plan[i].Status = status
		}
	}
}

// planAppWith is [taskApp] over an agent that answers a plan: a pinned clock, a
// pinned terminal and a home root of its own, so a plan row dated from the
// fixture and the surface reading it are the same clock (taskFixtureNow).
func planAppWith(t *testing.T, rows []session.PlanTaskRow, pages map[string]session.PlanTaskPage) (*app, *planFake) {
	t.Helper()
	fake := &planFake{
		taskFake: &taskFake{
			fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
			updates:   make(chan session.Event, 8),
		},
		plan:  rows,
		pages: pages,
	}
	a := newTestApp(fake)
	a.width, a.height = 200, 24
	a.homeRoot = t.TempDir()
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	// ONE CONVERSATION, NAMED, so the plan rows hang under this chat the way they
	// do on a real frame rather than standing as orphans.
	a.file, a.title = "/tmp/lab/chat-1/transcript.jsonl", "the run"
	return a, fake
}

// planLine is the drawn line a row's title is on, and whether there is one.
// planLine is the LIST line a title is on — the half of the frame left of the
// seam, because the record pane beside it previews the cursor row and would
// answer with the title twice.
func planLine(text, title string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if at := strings.Index(line, railSeam); at >= 0 {
			line = line[:at]
		}
		if strings.Contains(line, title) {
			return line, true
		}
	}
	return "", false
}

// TWO ROWS, TWO LINES, EACH WITH ITS OWN STATE WORD — and the two figures the
// store carries drawn only where they are something.
//
// A TASK THAT HAS RUN NO STEP AND SPENT NOTHING SAYS SO BY DRAWING NOTHING. The
// emptiness law is the whole of the `0 steps` half of this: a row that wrote
// `0 steps · $0.00` would be telling a person a fact as though it were news.
func TestThePaneDrawsThisChatsPlanRowsWithTheirStateWords(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-alpha", Title: "Alpha", Status: "claimed", Steps: 3, USD: 0.11},
		{ID: "t-beta", Title: "Beta", Status: "done"},
	}
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	text := taskSheetText(a)

	alpha, ok := planLine(text, "Alpha")
	if !ok {
		t.Fatalf("the plan's first row was not drawn:\n%s", text)
	}
	if !strings.Contains(alpha, "running") {
		t.Fatalf("a claimed plan task reads %q, want it to wear `running`", alpha)
	}
	if !strings.Contains(alpha, "3 steps") || !strings.Contains(alpha, "$0.11") {
		t.Fatalf("a plan row's steps and spend were not drawn: %q", alpha)
	}
	beta, ok := planLine(text, "Beta")
	if !ok {
		t.Fatalf("the plan's second row was not drawn:\n%s", text)
	}
	if !strings.Contains(beta, "done") {
		t.Fatalf("a done plan task reads %q, want it to wear `done`", beta)
	}
	if strings.Contains(text, "$0.00") || strings.Contains(text, "0 steps") {
		t.Fatalf("the pane drew a zero as though it were a figure:\n%s", text)
	}
}

// ENTER OPENS THE PAGE THE STORE KEEPS: the description, the notes with their
// author and moment, and the trajectory's steps — each command on its own line.
func TestEnterOnAPlanRowDrawsItsPage(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{
		"t-alpha": {
			Row:         rows[0],
			Description: "the work order",
			Notes:       []session.PlanTaskNote{{Author: "worker-1", Body: "a handoff", At: taskFixtureNow}},
			Steps: []session.PlanStep{
				{Step: 1, Command: "$ echo one", Observation: "one"},
				{Step: 2, Command: "$ echo two", Observation: "two"},
				{Step: 3, Command: "$ echo three", Observation: "three"},
			},
		},
	}
	a, _ := planAppWith(t, rows, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	if item, ok := a.taskSheetCurrent(); !ok || item.plan == nil {
		t.Fatalf("the cursor is not on a plan row: %+v", item.entry)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn {
		t.Fatal("enter over a plan row did not open its page")
	}
	page := taskSheetText(a)
	for _, want := range []string{"the work order", "worker-1", "$ echo one", "$ echo two", "$ echo three"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the plan page is missing %q:\n%s", want, page)
		}
	}
	// THE HEAD OPENS ON THE TASK AS GIVEN TO THE PERSON: title, a folded brief,
	// declared checks, and the folder once. Worker-only addressing and ids never leak.
	a.taskSheet.plan = session.PlanTaskPage{
		Row:         rows[0],
		Description: "first line of the brief\nsecond line\nthird line\nfourth line with t-store-secret and node 47",
		Checks:      []string{"go test ./internal/tui3"},
		Folder:      "/tmp/the-run-copy",
		Steps:       []session.PlanStep{{Step: 1, Command: "cd /tmp/the-run-copy && printf worker-bytes"}},
	}
	page = taskSheetText(a)
	for _, want := range []string{"Alpha", "first line of the brief", "third line", "more lines", "checks", "go test ./internal/tui3", "folder", "/tmp/the-run-copy", "cd /tmp/the-run-copy && printf worker-bytes"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the plan page head is missing %q:\n%s", want, page)
		}
	}
	for _, forbidden := range []string{"t-alpha", "t-store-secret", "node 47", "is your task in the plan"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("the person-facing page leaked %q:\n%s", forbidden, page)
		}
	}
	if strings.Count(page, "/tmp/the-run-copy") != 2 {
		t.Fatalf("folder count = %d, want head plus untouched command:\n%s", strings.Count(page, "/tmp/the-run-copy"), page)
	}

	// AND esc BACKS OUT ONE LAYER to the list, the card's own bargain.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.taskSheet.planOn || !a.at(pageTasks) {
		t.Fatal("esc did not back out of the plan page to the list")
	}
}

// THE CANCEL KEY A NODE ROW HAS ENDS A PLAN TASK, through the store's own
// cancel verb rather than the engine's (plandb_steer.go). It is the roster's
// `x`, taken over an empty box exactly as a node row takes it, so the same key
// that ends a node ends the plan task and a letter typed into the filter is
// still a letter.
func TestTheCancelKeyOnAPlanRowEndsItThroughTheStore(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	a, fake := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	if item, ok := a.taskSheetCurrent(); !ok || item.plan == nil {
		t.Fatalf("the cursor is not on a plan row: %+v", item.entry)
	}
	drive(t, a, key("x"))
	if len(fake.cancelled) != 1 || fake.cancelled[0] != "t-alpha" {
		t.Fatalf("the cancel key was turned into %v, want one cancel of t-alpha", fake.cancelled)
	}
}

// `p` HOLDS THE TASK AND `p` AGAIN LETS IT GO, and the pane's key line says so:
// the foot names the cancel and the hold beside the door enter takes, because a
// key nobody can find is a key that does not exist.
func TestPOnAPlanRowPausesThenResumes(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	a, fake := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	line := a.taskSheetKeysLine()
	if !strings.Contains(line, tasksPlanCancelWord) || !strings.Contains(line, tasksPlanPauseWord) {
		t.Fatalf("the foot does not name the plan row's keys: %q", line)
	}
	drive(t, a, key("p"))
	if len(fake.paused) != 1 || fake.paused[0] != "t-alpha" {
		t.Fatalf("the first `p` was turned into %v, want one pause of t-alpha", fake.paused)
	}
	// THE SAME KEY AGAIN RELEASES IT, and the foot now says what the next press
	// does rather than what the last one did.
	if line := a.taskSheetKeysLine(); !strings.Contains(line, tasksPlanResumeWord) {
		t.Fatalf("a paused row's foot reads %q, want it to offer the release", line)
	}
	drive(t, a, key("p"))
	if len(fake.resumed) != 1 || fake.resumed[0] != "t-alpha" {
		t.Fatalf("the second `p` was turned into %v, want one resume of t-alpha", fake.resumed)
	}
}

// TYPING ON THE PAGE IS A NOTE AND NOT A CHAT TURN: the words go to the store's
// note verb and never to the model, the composer says what typing there does,
// and a note is sent with enter.
func TestSendingOnThePlanPageWritesANoteAndStartsNoTurn(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{
		"t-alpha": {Row: rows[0], Description: "the work order"},
	}
	a, fake := planAppWith(t, rows, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn {
		t.Fatal("enter over a plan row did not open its page")
	}
	if !strings.Contains(taskSheetText(a), taskPlanNoteWord) {
		t.Fatalf("the page's composer does not say what typing there does:\n%s", taskSheetText(a))
	}
	for _, r := range "a note" {
		drive(t, a, key(string(r)))
	}
	if got := a.taskSheet.planNote.String(); got != "a note" {
		t.Fatalf("the composer holds %q, want %q", got, "a note")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	want := planCall{id: "t-alpha", text: "a note"}
	if len(fake.noted) != 1 || fake.noted[0] != want {
		t.Fatalf("enter wrote %v, want one note %+v", fake.noted, want)
	}
	if len(fake.sent) != 0 {
		t.Fatalf("sending a note started a chat turn: %v", fake.sent)
	}
}

// A REFUSAL FROM A VERB IS THE PANE'S ONE LINE, the sentence the store
// answered — never a card, which a place cannot draw over itself. It is read on
// the list, where the router's line rides beside the hint.
func TestAPlanVerbRefusalIsSpokenOnThePanesLine(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	a, fake := planAppWith(t, rows, nil)
	fake.refuse = errors.New("the root task is the harness's — it cannot be cancelled")
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, key("x"))
	if !strings.Contains(a.pageMsg, "cannot be cancelled") {
		t.Fatalf("the store's sentence went nowhere: %q", a.pageMsg)
	}
	if !strings.Contains(taskSheetText(a), "cannot be cancelled") {
		t.Fatalf("the refusal is not drawn:\n%s", taskSheetText(a))
	}
}

// AND THE DEFAULT PAGE DRAWS IT TOO, on its closing rule, because the page draws
// its own frame and the router's line has no place on it.
func TestAPlanVerbRefusalIsSpokenOnThePage(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{
		"t-alpha": {Row: rows[0], Description: "the work order"},
	}
	a, fake := planAppWith(t, rows, pages)
	fake.refuse = errors.New("a task that has finished cannot be paused")
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	drive(t, a, key("p"))
	if !strings.Contains(taskSheetText(a), "cannot be paused") {
		t.Fatalf("the page does not draw the store's refusal:\n%s", taskSheetText(a))
	}
}

// A CONVERSATION WITH NO PLAN DRAWS THE PAGE IT HAS ALWAYS DRAWN.
//
// The plan is an authority that is ABSENT rather than empty for a chat that
// never seeded one, and an absent authority must move nothing: the two frames
// below differ in the agent's own interface and in nothing a person can see.
func TestThePaneIsUnchangedWithoutAPlan(t *testing.T) {
	seed := func(a *app) {
		a.file, a.title = "/tmp/lab/chat-1/transcript.jsonl", "the run"
		a.comp.tasks = []session.TaskIndexEntry{
			pastTask("9", "port-the-parser", "Port the parser", time.Hour),
		}
	}
	before, _, _ := taskApp(t)
	seed(before)
	if !openTaskPlaceWithRows(before) {
		t.Fatal("the place refused to open over a record row")
	}
	after, _ := planAppWith(t, nil, nil)
	seed(after)
	if !openTaskPlaceWithRows(after) {
		t.Fatal("the place refused to open over a record row")
	}
	if want, got := taskSheetText(before), taskSheetText(after); want != got {
		t.Fatalf("a conversation with no plan drew a different page:\n--- without a plan ---\n%s\n--- with an empty plan ---\n%s", want, got)
	}
}

// ── THE ROW'S LIVE STEP LINE (c185, SURFACE.md §2A / §4 Cell 2) ─────────────
//
// A plan store row carries the step its worker is running RIGHT NOW on
// PlanTaskRow.Live, and a running row spends its under-block on that step's
// command and the task's own figures. The four tests below are the four facts
// the cell has to hold: the live line and its telemetry appear while a step is
// in flight; a settled row draws neither; a `pending` row says the surface's own
// word `queued`; and a narrow column drops the command's tail without ever
// cutting a glyph or losing the step count.

// livePlanRow is one plan task with a step in flight, so every test below starts
// from the same row and changes one thing about it.
func livePlanRow() session.PlanTaskRow {
	row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "running", Steps: 12, USD: 0.11}
	row.Live.Step = 12
	row.Live.Command = "git grep -n RateLimit internal/api"
	row.Live.Since = taskFixtureNow
	return row
}

// planTextFor opens the tasks place over a plan and answers the whole screen, so
// every test below reads one row without its own app plumbing ([planLine] already
// does this walk for the wide list; this is the same walk named for these tests).
func planTextFor(t *testing.T, rows []session.PlanTaskRow) string {
	t.Helper()
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	return taskSheetText(a)
}

// A RUNNING ROW DRAWS THE COMMAND ITS STEP IS RUNNING AND THE FIGURES UNDER IT.
func TestAPlanRowWithALiveStepDrawsTheCommandAndTheFigures(t *testing.T) {
	text := planTextFor(t, []session.PlanTaskRow{livePlanRow()})
	if !strings.Contains(text, "$ git grep -n RateLimit internal/api") {
		t.Fatalf("the live step's command is not drawn under the running row:\n%s", text)
	}
	if !strings.Contains(text, "12 steps · $0.11") {
		t.Fatalf("the figures are not drawn under the live command:\n%s", text)
	}
}

// A SETTLED ROW DRAWS NEITHER: no live step, so no command line and no block at
// all — the step count stays on the row's own state cell, where every row keeps
// it.
func TestAPlanRowWithNoLiveStepDrawsNoUnderBlock(t *testing.T) {
	row := livePlanRow()
	row.Status, row.Live = "done", session.PlanTaskRow{}.Live
	text := planTextFor(t, []session.PlanTaskRow{row})
	if strings.Contains(text, "$ git grep") {
		t.Fatalf("a settled row drew the live command:\n%s", text)
	}
	if strings.Contains(text, "12 steps · $0.11") {
		t.Fatalf("a settled row drew the under-block telemetry, which is only the live block's line:\n%s", text)
	}
	line, ok := planLine(text, "Alpha")
	if !ok {
		t.Fatalf("the settled row was not drawn:\n%s", text)
	}
	if !strings.Contains(line, "12 steps") || !strings.Contains(line, "$0.11") {
		t.Fatalf("the settled row lost its own figures: %q", line)
	}
}

// A QUEUED ROW SAYS `queued` — the surface's own word for admitted work, never
// the store's `pending` and never `running`.
func TestAQueuedPlanRowSaysQueued(t *testing.T) {
	text := planTextFor(t, []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "pending"}})
	line, ok := planLine(text, "Alpha")
	if !ok {
		t.Fatalf("the queued row was not drawn:\n%s", text)
	}
	if !strings.Contains(line, "queued") {
		t.Fatalf("a pending plan task reads %q, want it to wear `queued`", line)
	}
	if strings.Contains(line, "running") {
		t.Fatalf("a pending plan task still reads `running`: %q", line)
	}
}

// A QUEUED ROW BEHIND NAMED WORK READS `queued · waits: <the work>` ON THE ROW —
// the dependency sentence the rail's own held row draws, joined to the state word
// by the engine's own [session.TaskStatus.RowWord] and never composed twice. AND
// THE WORK IS NAMED, NOT POINTED AT: the store's id stays off the frame, because
// `waits: t-9c1x2` is a pointer a person has to follow ([app.taskWaitTitles]
// states the refusal). Where the sentence will not fit the twenty-cell state
// column the cell says the word and the reason falls to the line the cursor's row
// grows — a dangling `waits:` with nothing after it is the one shape the row must
// not draw.
func TestAQueuedPlanRowNamesTheWorkItWaitsOn(t *testing.T) {
	for _, fixture := range []struct {
		name, parent, want string
		dangling           bool
	}{
		// Twenty cells hold `queued · waits: Root` exactly, so the row says the
		// whole of it.
		{name: "a parent the state column can hold", parent: "Root", want: "queued · waits: Root"},
		// The same sentence will not fit, and the cell gives up the reason whole
		// rather than announcing it and cutting the work off underneath.
		{name: "a parent it cannot", parent: "Add rate limiting to every handler in the API", want: "queued", dangling: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			text := planTextFor(t, []session.PlanTaskRow{
				{ID: "t-root", Title: fixture.parent, Status: "running"},
				{ID: "t-alpha", Title: "Alpha", Status: "pending", Parent: "t-root"},
			})
			line, ok := planLine(text, "Alpha")
			if !ok {
				t.Fatalf("the queued row was not drawn:\n%s", text)
			}
			if !strings.Contains(line, fixture.want) {
				t.Fatalf("the queued row reads %q, want it to say %q", line, fixture.want)
			}
			if strings.Contains(line, "t-root") {
				t.Fatalf("the row named the work it waits on by its store id: %q", line)
			}
			if fixture.dangling && strings.Contains(line, "waits:") {
				t.Fatalf("the row announced a reason it had no room to name: %q", line)
			}
		})
	}
}

// A NARROW COLUMN DROPS THE COMMAND'S TAIL AND KEEPS THE STEP COUNT, and the
// live step's lead is drawn whole — never a half glyph.
func TestANarrowPlanRowDropsTheCommandTailButKeepsTheFigures(t *testing.T) {
	const long = "git grep -n RateLimit internal/api/upload/handler/middleware"
	row := livePlanRow()
	row.Live.Command = long
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, nil)
	a.width = 60
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	text := taskSheetText(a)
	if strings.Contains(text, long) {
		t.Fatalf("the command's tail survived a narrow column:\n%s", text)
	}
	if !strings.Contains(text, "12 steps · $0.11") {
		t.Fatalf("the step count was dropped with the command's tail:\n%s", text)
	}
	// The lead is drawn outside the fitting, so both glyphs are whole.
	if !strings.Contains(text, a.pal.glyph(tokens.GStepRunning)+" "+tokens.GlyphShell+" ") {
		t.Fatalf("the live step's lead is not drawn whole:\n%s", text)
	}
}

// ── the page follows its live edge (SURFACE.md §4, Cell 3) ──────────────────

// planStepCommand is the command the n-th step of a fixture ran. It is unique
// per step so a test can tell whether that one line is on the screen, which is
// the whole question a page that follows a live edge is asked.
func planStepCommand(n int) string { return "cmd-" + itoa(n) }

// planPageWithSteps is a page on one task holding n recorded steps, the shape
// the store answers — one line each, no observation, so a test can count what it
// sees against what it appended.
func planPageWithSteps(row session.PlanTaskRow, n int) session.PlanTaskPage {
	page := session.PlanTaskPage{Row: row, Description: "the work order"}
	for i := 1; i <= n; i++ {
		page.Steps = append(page.Steps, session.PlanStep{Step: i, Command: planStepCommand(i)})
	}
	return page
}

// appendPlanStep records one more step on the fake's page, as the worker's own
// CLI writes to the store between two frames of somebody reading it.
func appendPlanStep(fake *planFake, id string, n int) {
	page := fake.pages[id]
	page.Steps = append(page.Steps, session.PlanStep{Step: n, Command: planStepCommand(n)})
	fake.pages[id] = page
}

// openPlanPage opens the tasks place over the fixture and enters the plan page
// under the cursor, the two keys a person presses.
func openPlanPage(t *testing.T, a *app) {
	t.Helper()
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn {
		t.Fatal("enter over a plan row did not open its page")
	}
}

// A PAGE OPEN ON A RUNNING TASK FOLLOWS ITS LIVE EDGE: a step the worker takes
// while somebody reads is at the bottom of the page at the next frame, exactly
// as the room follows its own live edge. It is that same reading-on-the-beat,
// spent on the store rather than a journal ([app.taskPlanFollow]).
func TestThePlanPageFollowsAStepAppendedWhileItIsOpen(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{"t-alpha": planPageWithSteps(rows[0], 30)}
	a, fake := planAppWith(t, rows, pages)
	openPlanPage(t, a)

	if !strings.Contains(taskSheetText(a), planStepCommand(30)) {
		t.Fatalf("a page stuck to the live edge did not draw its newest step:\n%s", taskSheetText(a))
	}
	// THE STORE MOVES UNDER IT, the way it does while a worker runs.
	appendPlanStep(fake, "t-alpha", 31)
	drive(t, a, frameMsg{})
	if !strings.Contains(taskSheetText(a), planStepCommand(31)) {
		t.Fatalf("the page did not follow the step appended while it was open:\n%s", taskSheetText(a))
	}
}

// AND A SCROLL UP RELEASES THE PIN, so the newest step no longer walks in from
// under the reader — until they reach the bottom again, which takes the pin
// back. It is the room's own bargain ([app.roomScroll], [app.taskPlanScroll]).
func TestScrollUpOnThePlanPageStopsTheFollow(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{"t-alpha": planPageWithSteps(rows[0], 30)}
	a, fake := planAppWith(t, rows, pages)
	openPlanPage(t, a)

	for i := 0; i < 3; i++ {
		drive(t, a, key("up"))
	}
	if a.taskSheet.planStick {
		t.Fatal("scrolling up left the page pinned to the live edge")
	}
	appendPlanStep(fake, "t-alpha", 31)
	drive(t, a, frameMsg{})
	if strings.Contains(taskSheetText(a), planStepCommand(31)) {
		t.Fatalf("the page followed a step after somebody scrolled up off the edge:\n%s", taskSheetText(a))
	}
	// AND REACHING THE BOTTOM AGAIN RESUMES IT.
	for i := 0; i < 16; i++ {
		drive(t, a, key("down"))
	}
	if !a.taskSheet.planStick {
		t.Fatal("scrolling back to the bottom did not take the pin again")
	}
	appendPlanStep(fake, "t-alpha", 32)
	drive(t, a, frameMsg{})
	if !strings.Contains(taskSheetText(a), planStepCommand(32)) {
		t.Fatalf("the page did not resume following at the bottom:\n%s", taskSheetText(a))
	}
}

// A NOTE LEFT ON THE PAGE IS THE PAGE'S OWN RECEIPT: the words go to the store,
// the page is read again, and the note is drawn under `notes` with its author —
// `you` for the person — and its moment. It starts no chat turn (it is not a
// message to the model), which the note test next door already holds.
func TestANoteLeftOnThePlanPageAppearsWithYouAndItsMoment(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{"t-alpha": {Row: rows[0], Description: "the work order"}}
	a, fake := planAppWith(t, rows, pages)
	openPlanPage(t, a)

	for _, r := range "a longer sleep" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 1 || fake.noted[0] != (planCall{id: "t-alpha", text: "a longer sleep"}) {
		t.Fatalf("enter wrote %v, want one note on t-alpha", fake.noted)
	}
	page := taskSheetText(a)
	for _, want := range []string{"notes", "you", "now", "a longer sleep"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the note's receipt is missing %q:\n%s", want, page)
		}
	}
}

// THE PAGE ALSO SAYS WHEN A NOTE IS READ — the worker is a separate loop, so a
// note waits in the store until the worker asks for its next step. That sentence
// is on the page, not only in the manual, because the page is where the box is.
func TestThePlanPageSaysWhenANoteIsRead(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed"}}
	pages := map[string]session.PlanTaskPage{"t-alpha": {Row: rows[0], Description: "the work order"}}
	a, _ := planAppWith(t, rows, pages)
	openPlanPage(t, a)
	if !strings.Contains(taskSheetText(a), taskPlanPickupWord) {
		t.Fatalf("the page does not say when a note is read:\n%s", taskSheetText(a))
	}
}

// THE LIVE STEP IS DRAWN ONE STEP EARLY and leaves the page when the task ends:
// the running glyph in place of the number, the command in ink, and the call's
// own clock dim under it — and the next read after the store cleared the live
// row draws none of it (taskPlanBody, internal/plandb's live.go).
func TestTheLiveStepLeavesThePlanPageWhenTheTaskEnds(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Title: "Alpha", Status: "claimed", Steps: 3}}
	page := planPageWithSteps(rows[0], 3)
	page.Live = plandb.LiveStep{Step: 4, Command: "go test ./...", Since: taskFixtureNow.Add(-41 * time.Second)}
	pages := map[string]session.PlanTaskPage{"t-alpha": page}
	a, fake := planAppWith(t, rows, pages)
	openPlanPage(t, a)

	text := taskSheetText(a)
	if !strings.Contains(text, "$ go test ./...") {
		t.Fatalf("the live step's command is not on the page:\n%s", text)
	}
	if !strings.Contains(text, "running 41s") {
		t.Fatalf("the live step's clock is not under it:\n%s", text)
	}
	// THE TASK ENDS: the store clears the live row and the root lands.
	ended := fake.pages["t-alpha"]
	ended.Live = plandb.LiveStep{}
	ended.Row.Status = "done"
	fake.pages["t-alpha"] = ended
	drive(t, a, frameMsg{})
	text = taskSheetText(a)
	if strings.Contains(text, "go test ./...") {
		t.Fatalf("the live step outlived the task that was running it:\n%s", text)
	}
}

// planDrawnKin is the connector cell the place draws each plan row with, in draw
// order and keyed by the row's own id — the layout the place actually paints, so
// a test reads the same string the person does.
func planDrawnKin(a *app) map[string]string {
	out := map[string]string{}
	r := a.tasksFiltered()
	for _, line := range r.lay(a.taskSheetListWidth()) {
		if line.kind == tasksLineTask && line.item.plan != nil {
			out[line.item.entry.ID] = line.kin
		}
	}
	return out
}

// planUnderKins is the connector cell each live-step under-block rides, in draw
// order.
func planUnderKins(a *app) []string {
	out := make([]string, 0, 1)
	r := a.tasksFiltered()
	for _, line := range r.lay(a.taskSheetListWidth()) {
		if line.kind == tasksLinePlanUnder {
			out = append(out, line.kin)
		}
	}
	return out
}

// A TASK HANGS UNDER THE TASK THAT REQUESTED IT. The plan is a graph: the store
// records every child under its parent (PlanTaskRow.Parent), and the place used
// to build every row at depth 0, so a person saw a flat list and the only
// relation drawn was the word `waits:` on a held row.
func TestThePlanTreeDrawsAChildIndentedUnderItsParent(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-root", Title: "Root", Status: "claimed"},
		{ID: "t-alpha", Title: "Alpha", Parent: "t-root", Status: "claimed"},
	}
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	kin := planDrawnKin(a)
	root, child := kin["t-root"], kin["t-alpha"]
	if root == "" || child == "" {
		t.Fatalf("both rows must be drawn (root %q, child %q)", root, child)
	}
	if !strings.HasSuffix(child, tasksKinCont) && !strings.HasSuffix(child, tasksKinLast) {
		t.Fatalf("the child was not drawn under its parent with a connector: %q", child)
	}
	if ansi.StringWidth(child) <= ansi.StringWidth(root) {
		t.Fatalf("the child sits at its parent's own column (parent %q, child %q)", root, child)
	}
	text := taskSheetText(a)
	ri, ci := strings.Index(text, "Root"), strings.Index(text, "Alpha")
	if ri < 0 || ci < 0 || ri > ci {
		t.Fatalf("the child is not drawn below its parent:\n%s", text)
	}
}

// A dependency names the wait but never changes the parent-drawn hierarchy.
func TestAQueuedPlanRowStaysWithItsParent(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-parent", Title: "Parent", Status: "running"},
		{ID: "t-dep", Title: "Dep", Status: "running"},
		{ID: "t-alpha", Title: "Alpha", Parent: "t-parent", Status: "pending", Waits: []string{"t-dep"}},
	}
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	kin := planDrawnKin(a)
	dep, parent, child := kin["t-dep"], kin["t-parent"], kin["t-alpha"]
	if dep == "" || parent == "" || child == "" {
		t.Fatalf("both rows must be drawn (parent %q, child %q)", parent, child)
	}
	if !strings.HasSuffix(child, tasksKinCont) && !strings.HasSuffix(child, tasksKinLast) {
		t.Fatalf("the held row was not drawn under the task it waits on: %q", child)
	}
	if ansi.StringWidth(child) <= ansi.StringWidth(parent) {
		t.Fatalf("the held row sits at its parent's own column (parent %q, child %q)", parent, child)
	}
	line, ok := planLine(taskSheetText(a), "Alpha")
	if !ok {
		t.Fatal("the held row was not drawn")
	}
	if !strings.Contains(line, "queued · waits: Dep") {
		t.Fatalf("the held row reads %q, want `queued · waits: Dep`", line)
	}
}

// THE LIVE STEP RIDES THE NODE'S OWN COLUMN. A running row's under-block — the
// `$ <command>` step line and the `N steps · $` figures under it — is drawn at
// the depth the row itself sits at, never at column zero.
func TestThePlanTreeDrawsTheLiveStepAtTheNodesIndentation(t *testing.T) {
	child := livePlanRow()
	child.Parent = "t-root"
	rows := []session.PlanTaskRow{
		{ID: "t-root", Title: "Root", Status: "running"},
		child,
	}
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	kin := planDrawnKin(a)
	root, kid := kin["t-root"], kin["t-alpha"]
	if root == "" || kid == "" {
		t.Fatalf("both rows must be drawn (root %q, kid %q)", root, kid)
	}
	under := planUnderKins(a)
	if len(under) == 0 {
		t.Fatal("the running node's live step was not drawn")
	}
	for _, kin := range under {
		if ansi.StringWidth(kin) != ansi.StringWidth(kid) {
			t.Fatalf("the live step sits at %q, want the node's own column %q", kin, kid)
		}
		if ansi.StringWidth(kin) <= ansi.StringWidth(root) {
			t.Fatalf("the live step is not indented under the node: %q", kin)
		}
	}
	if text := taskSheetText(a); !strings.Contains(text, "$ git grep -n RateLimit internal/api") {
		t.Fatalf("the live step's command is not drawn:\n%s", text)
	}
}

// STEERING IS UNCHANGED BY THE TREE: a note left on a CHILD from its page still
// lands through PlanNote, on the child's own id.
func TestANoteOnAPlanChildStillLandsThroughPlanNote(t *testing.T) {
	alpha := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Parent: "t-root", Status: "claimed"}
	rows := []session.PlanTaskRow{{ID: "t-root", Title: "Root", Status: "claimed"}, alpha}
	pages := map[string]session.PlanTaskPage{"t-alpha": {Row: alpha, Description: "the work order"}}
	a, fake := planAppWith(t, rows, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	var want session.TaskIndexEntry
	for _, item := range a.tasksFiltered().items {
		if item.plan != nil && item.entry.ID == "t-alpha" {
			want = item.entry
		}
	}
	if want.ID == "" {
		t.Fatal("the child row was not on the page to point at")
	}
	a.taskSheetPointAt(want)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn {
		t.Fatal("enter over the child row did not open its page")
	}
	for _, r := range "a longer sleep" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	wantNote := planCall{id: "t-alpha", text: "a longer sleep"}
	if len(fake.noted) != 1 || fake.noted[0] != wantNote {
		t.Fatalf("enter wrote %v, want one note %+v", fake.noted, wantNote)
	}
}

// THE TASK'S PAGE SHOWS ITS CHILDREN UNDER ITS STEPS, each with its live line,
// the way the rail draws a family — and leaves the note composer and its receipt
// exactly where they were.
func TestThePlanPageShowsChildrenUnderItsSteps(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running", Steps: 2}
	child := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Parent: "t-root", Status: "running", Steps: 3}
	child.Live.Step = 3
	child.Live.Command = "go test ./internal/api"
	rows := []session.PlanTaskRow{root, child}
	pages := map[string]session.PlanTaskPage{
		"t-root": {
			Row:      root,
			Steps:    []session.PlanStep{{Step: 1, Command: "git status"}},
			Children: []session.PlanTaskRow{child},
		},
	}
	a, _ := planAppWith(t, rows, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn {
		t.Fatal("enter over the root row did not open its page")
	}
	text := taskSheetText(a)
	if !strings.Contains(text, "Alpha") {
		t.Fatalf("the page does not draw the task's child:\n%s", text)
	}
	if !strings.Contains(text, "$ go test ./internal/api") {
		t.Fatalf("the page does not draw the child's live line:\n%s", text)
	}
	if !strings.Contains(text, taskPlanNoteWord) {
		t.Fatalf("the tree changed the page's note composer:\n%s", text)
	}
}
