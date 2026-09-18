package tui3

// The run's plan as the tasks place draws it: the rows the store answers, the
// page one of those rows opens, and the page a conversation with no plan still
// draws. Every fixture is the store's own reading — the shape
// [session.Agent.PlanTasks] hands over — so nothing here seeds a store or runs a
// worker; the place is driven by the same fake agent its neighbours use.

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
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

func (f *planFake) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	page, ok := f.pages[id]
	return page, ok
}

func (f *planFake) PlanNote(id, text string) error {
	f.noted = append(f.noted, planCall{id: id, text: text})
	return f.refuse
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
