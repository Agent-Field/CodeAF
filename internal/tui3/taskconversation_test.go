package tui3

// A program's task page, drawn from a scripted page the way the store answers
// one ([session.PlanTaskPage.Program]): the pinned line, the conversation in
// place of the steps, the call in flight, and the foot with no box. Nothing
// here seeds a store or runs a program; the page is the fake's, and every
// reading the surface makes of it is the one a real window makes.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// programRunBegan is when the fixture's run started: fourteen minutes and three
// seconds before the frame's own clock, so the pinned age is a figure a test
// can spell.
var programRunBegan = taskFixtureNow.Add(-(14*time.Minute + 3*time.Second))

// programRow is the run's root as the store answers it mid-way: running,
// handed to senior-dev, in its implement stage, with a dollar and a quarter of
// spend rows banked by the model API.
func programRow() session.PlanTaskRow {
	return session.PlanTaskRow{
		ID: "t-7", Title: "rewrite the auth middleware", Status: "running",
		Program: "senior-dev", Stage: "implement", USD: 1.24, Started: programRunBegan,
		Live: plandb.LiveStep{Step: 3, Command: "senior-dev: implement · running", Since: programRunBegan},
	}
}

// programTurns is a conversation three calls long: two answered, each with the
// tools the model asked for, and the third still out, twelve seconds in.
func programTurns() []delegate.Turn {
	model := "deepseek/deepseek-v4-flash"
	return []delegate.Turn{
		{Seq: 1, Started: programRunBegan, Ended: programRunBegan.Add(4 * time.Second), Model: model,
			Sent:    []delegate.Said{{Role: "system", Text: "you are senior-dev"}, {Role: "user", Text: "rewrite the auth middleware to use the new session store"}},
			Reply:   "I'll read the middleware and the store first.",
			Calls:   []delegate.ToolUse{{Name: "read", Args: `{"filePath":"internal/auth/middleware.go"}`}, {Name: "grep", Args: `{"pattern":"SessionStore","path":"internal"}`}},
			CostUSD: 0.4},
		{Seq: 2, Started: programRunBegan.Add(5 * time.Second), Ended: programRunBegan.Add(9 * time.Second), Model: model,
			Sent: []delegate.Said{
				{Role: "assistant", Text: "I'll read the middleware and the store first."},
				{Role: "tool", Tool: "read", Text: "package auth"},
				{Role: "tool", Tool: "grep", Text: "internal/auth/store.go:12: type SessionStore interface {"},
			},
			Reply:   "The store interface is small; I'll change the handler.",
			Calls:   []delegate.ToolUse{{Name: "edit", Args: `{"filePath":"internal/auth/middleware.go","oldString":"func Middleware(`}},
			CostUSD: 0.5},
		{Seq: 3, Started: taskFixtureNow.Add(-12 * time.Second), Model: model,
			Sent: []delegate.Said{{Role: "tool", Tool: "edit", Text: "applied 1 edit"}}},
	}
}

// programPage is the page the fixture's row opens.
func programPage(row session.PlanTaskRow, turns []delegate.Turn) session.PlanTaskPage {
	return session.PlanTaskPage{
		Row:         row,
		Description: "rewrite the auth middleware to use the new session store",
		Live:        row.Live,
		Program: &session.PlanProgram{
			Name: "senior-dev", Stages: []string{"intake", "implement", "verification"},
			Turns: turns, Calls: len(turns),
		},
	}
}

// programPageApp opens the program's page at a width and a height, the two
// keys a person presses, on a surface whose clock is the fixture's.
func programPageApp(t *testing.T, page session.PlanTaskPage, width, height int) (*app, *planFake) {
	t.Helper()
	a, fake := planAppWith(t, []session.PlanTaskRow{page.Row}, map[string]session.PlanTaskPage{page.Row.ID: page})
	a.width, a.height = width, height
	openPlanPage(t, a)
	if !a.taskPlanIsProgram() {
		t.Fatalf("the page opened is not a program's: %+v", a.taskSheet.plan.Program)
	}
	return a, fake
}

// programPageLines is the page as drawn, one plain string per screen row.
func programPageLines(a *app) []string {
	width, height := a.size()
	lines, _, _ := a.taskPlanFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = plain(line)
	}
	return out
}

// saidBy reports whether a row of the page has a speaker's name in the names'
// column and these words beside it, whatever width the column came to.
func saidBy(lines []string, name, words string) bool {
	for _, line := range lines {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), name)
		if ok && strings.HasPrefix(rest, "  ") && strings.HasPrefix(strings.TrimLeft(rest, " "), words) {
			return true
		}
	}
	return false
}

// A PROGRAM'S PAGE IS ITS CONVERSATION WITH CODEAF. The brief opens it under the
// program's name; each call is the program's side and the model's, the model
// named by its short name; every tool the model asked for is a dim row behind
// its family's mark; the call still out is the last line, with its model and its
// clock; and the line under the title is pinned with the stage, the spend, the
// calls and the age.
func TestAProgramsPageDrawsItsConversationWithCodeaf(t *testing.T) {
	a, _ := programPageApp(t, programPage(programRow(), programTurns()), 80, 30)
	lines := programPageLines(a)
	page := strings.Join(lines, "\n")
	t.Logf("a program's page, mid-way:\n%s", page)

	if lines[0] != "rewrite the auth middleware" {
		t.Fatalf("the head's first row is %q, want the task's title", lines[0])
	}
	if lines[1] != "implement · $1.24 · 3 calls · 14m 3s" {
		t.Fatalf("the pinned line is %q, want the stage, the spend, the calls and the age", lines[1])
	}
	for _, said := range []struct{ name, words string }{
		{"senior-dev", "rewrite the auth middleware to use the new session store"},
		{"deepseek-v4-flash", "I'll read the middleware and the store first."},
		{"senior-dev", "read: package auth"},
		{"deepseek-v4-flash", "The store interface is small; I'll change the handler."},
		{"senior-dev", "edit: applied 1 edit"},
	} {
		if !saidBy(lines, said.name, said.words) {
			t.Fatalf("the page does not have %s saying %q:\n%s", said.name, said.words, page)
		}
	}
	read := a.actionMarkFor(session.ActionCategoryForTool("read"))
	grep := a.actionMarkFor(session.ActionCategoryForTool("grep"))
	edit := a.actionMarkFor(session.ActionCategoryForTool("edit"))
	for _, want := range []string{
		read + " read internal/auth/middleware.go",
		grep + " grep SessionStore",
		// A result longer than the room beside the names gives up its tail.
		"grep: internal/auth/store.go:12: type SessionStore interfa" + glyphMore,
		edit + " edit internal/auth/middleware.go",
		a.icon(tokens.GStepRunning) + " deepseek-v4-flash · 12s",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q:\n%s", want, page)
		}
	}
	// THE CALL IN FLIGHT IS THE CONVERSATION'S LAST LINE.
	last := ""
	for _, line := range lines {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, "─") && !strings.Contains(line, taskCardBackWord) {
			last = line
		}
	}
	if !strings.Contains(last, a.icon(tokens.GStepRunning)+" deepseek-v4-flash") {
		t.Fatalf("the last line of the conversation is %q, want the call in flight", last)
	}
	// NEITHER A PROMPT NOR AN ECHO IS A ROW: the program's system prompt and the
	// model's own answer handed back to it are nowhere on the page, and the brief
	// is said once.
	if strings.Contains(page, "you are senior-dev") || strings.Count(page, "I'll read the middleware and the store first.") != 1 {
		t.Fatalf("the page drew a prompt or an echo:\n%s", page)
	}
	if strings.Count(page, "rewrite the auth middleware to use the new session store") != 1 {
		t.Fatalf("the brief is drawn more than once:\n%s", page)
	}
	// EVERY ROW FITS THE FRAME.
	for i, line := range lines {
		if cells := ansi.StringWidth(line); cells > 80 {
			t.Fatalf("row %d is %d cells in an 80-cell frame: %q", i, cells, line)
		}
	}
}

// A PROGRAM'S PAGE HAS NO BOX. A program reads no note, so the page draws no
// composer, never promises that a worker reads a note at its next step, offers
// no send, hides the caret, and a letter typed at it is nothing — never a note
// sent to the store and never a letter in a box that is not there.
func TestAProgramsPageHasNoNoteBoxAndTakesNoNote(t *testing.T) {
	a, fake := programPageApp(t, programPage(programRow(), programTurns()), 80, 30)
	page := strings.Join(programPageLines(a), "\n")
	for _, never := range []string{taskPlanNoteWord, taskPlanPickupWord, "enter send", "notes", "steps"} {
		if strings.Contains(page, never) {
			t.Fatalf("a program's page says %q:\n%s", never, page)
		}
	}
	if !strings.Contains(page, tasksPlanCancelWord) || !strings.Contains(page, taskCardBackWord) {
		t.Fatalf("a program's page lost its stop or its way back:\n%s", page)
	}
	if a.caret {
		t.Fatal("a program's page shows a caret over no box")
	}
	for _, r := range "pause it" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 0 || len(fake.paused) != 0 || !a.taskSheet.planNote.empty() {
		t.Fatalf("typing on a program's page wrote notes %v, paused %v, box %q", fake.noted, fake.paused, a.taskSheet.planNote.String())
	}
	if !a.taskSheet.planOn {
		t.Fatal("typing on a program's page closed it")
	}
}

// THE PINNED LINE IS PINNED. The page opens stuck to its bottom edge and follows
// the conversation down, and a conversation longer than the frame scrolls — the
// pinned line stays under the title at the bottom, part way up, and back at the
// bottom again, while the newest call is what the bottom shows.
func TestAProgramsPinnedLineSurvivesScrollingToTheBottom(t *testing.T) {
	row := programRow()
	var turns []delegate.Turn
	for i := 1; i <= 40; i++ {
		at := programRunBegan.Add(time.Duration(i) * 10 * time.Second)
		turns = append(turns, delegate.Turn{Seq: i, Started: at, Ended: at.Add(5 * time.Second), Model: "deepseek/deepseek-v4-flash",
			Sent: []delegate.Said{{Role: "tool", Tool: "bash", Text: "result " + itoa(i)}}, Reply: "answer " + itoa(i)})
	}
	a, _ := programPageApp(t, programPage(row, turns), 80, 20)
	pinned := "implement · $1.24 · 40 calls · 14m 3s"
	check := func(when string) []string {
		t.Helper()
		lines := programPageLines(a)
		if lines[1] != pinned {
			t.Fatalf("%s: the row under the title is %q, want the pinned line %q", when, lines[1], pinned)
		}
		return lines
	}
	lines := check("opened")
	if !strings.Contains(strings.Join(lines, "\n"), "answer 40") {
		t.Fatalf("a page opened at its bottom edge does not show the newest call:\n%s", strings.Join(lines, "\n"))
	}
	for i := 0; i < 12; i++ {
		drive(t, a, key("up"))
	}
	if a.taskSheet.planStick {
		t.Fatal("scrolling up left the page stuck to its bottom edge")
	}
	check("scrolled up")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyPgDown})
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if !a.taskSheet.planStick {
		t.Fatal("scrolling back to the bottom did not take the follow up again")
	}
	lines = check("back at the bottom")
	if !strings.Contains(strings.Join(lines, "\n"), "answer 40") {
		t.Fatalf("back at the bottom, the newest call is not on screen:\n%s", strings.Join(lines, "\n"))
	}
}

// THE CALL IN FLIGHT IS GONE WHEN IT RETURNS. The page follows the task on its
// beat; the read after the call's ending reached the log draws the model's
// answer in its place and no running mark anywhere.
func TestTheCallInFlightLeavesWhenItReturns(t *testing.T) {
	row := programRow()
	a, fake := programPageApp(t, programPage(row, programTurns()), 80, 30)
	flying := a.icon(tokens.GStepRunning) + " deepseek-v4-flash"
	if page := strings.Join(programPageLines(a), "\n"); !strings.Contains(page, flying) {
		t.Fatalf("the call in flight is not drawn:\n%s", page)
	}
	back := programTurns()
	back[2].Ended = taskFixtureNow
	back[2].Reply = "The handler now reads the session store."
	fake.pages[row.ID] = programPage(row, back)
	planBeat(t, a)
	page := strings.Join(programPageLines(a), "\n")
	if strings.Contains(page, flying) {
		t.Fatalf("the call that returned is still drawn in flight:\n%s", page)
	}
	if !saidBy(programPageLines(a), "deepseek-v4-flash", "The handler now reads the session store.") {
		t.Fatalf("the call that returned did not draw its answer:\n%s", page)
	}
}

// NOTHING IS DRAWN FOR NOTHING. A program that has spent nothing, made no call
// and named no stage wears its state word alone on the pinned line — no
// `$0.00`, no `0 calls` — and a call left open in the log of a run that has
// ended is not in flight on a page about work that is over.
func TestAProgramsPageDrawsNothingForZeroOrUnknown(t *testing.T) {
	row := programRow()
	row.USD, row.Stage, row.Started, row.Live = 0, "", time.Time{}, plandb.LiveStep{}
	page := programPage(row, nil)
	page.Program.Calls = 0
	a, _ := programPageApp(t, page, 80, 20)
	lines := programPageLines(a)
	if lines[1] != "running" {
		t.Fatalf("the pinned line of a program that has said nothing is %q, want its state word alone", lines[1])
	}
	text := strings.Join(lines, "\n")
	for _, never := range []string{"$0.00", "0 calls", "0s", "earlier calls"} {
		if strings.Contains(text, never) {
			t.Fatalf("the page drew %q for a figure nobody has:\n%s", never, text)
		}
	}
	if !saidBy(lines, "senior-dev", "rewrite the auth middleware to use the new session store") {
		t.Fatalf("a program that has made no call yet does not open on its brief:\n%s", text)
	}

	// AN ENDED RUN'S OPEN CALL IS NO CALL IN FLIGHT.
	ended := programRow()
	ended.Status, ended.Stage, ended.Live, ended.Ended = "done", "", plandb.LiveStep{}, taskFixtureNow.Add(-time.Minute)
	b, _ := programPageApp(t, programPage(ended, programTurns()), 80, 30)
	done := strings.Join(programPageLines(b), "\n")
	if strings.Contains(done, b.icon(tokens.GStepRunning)) {
		t.Fatalf("an ended run's page draws a call in flight:\n%s", done)
	}
	if lines := programPageLines(b); lines[1] != "done · $1.24 · 3 calls · 13m 3s" {
		t.Fatalf("an ended run's pinned line is %q, want its state, spend, calls and the age it ended at", lines[1])
	}
}

// A REFUSED OR FAILED CALL IS ONE PLAIN LINE. codeaf's own refusal is codeaf's
// line, because no model saw the call; a model's failure is that model's line.
// And a program that summarized its own history says so in one line, rather than
// replaying it.
func TestARefusedFailedOrRestartedCallIsOnePlainLine(t *testing.T) {
	turns := programTurns()[:2]
	at := programRunBegan.Add(time.Minute)
	turns = append(turns,
		delegate.Turn{Seq: 3, Started: at, Ended: at, Model: "deepseek/deepseek-v4-flash", Failed: "upstream 503\nretry later"},
		delegate.Turn{Seq: 4, Started: at, Ended: at.Add(time.Second), Model: "deepseek/deepseek-v4-flash", Restarted: true,
			Sent: []delegate.Said{{Role: "user", Text: "the whole history, summarized"}}, Reply: "Carrying on from the summary."},
		delegate.Turn{Seq: 5, Started: at, Model: "deepseek/deepseek-v4-flash", Refused: "the run's dollar ceiling is reached"},
	)
	a, _ := programPageApp(t, programPage(programRow(), turns), 80, 40)
	lines := programPageLines(a)
	page := strings.Join(lines, "\n")
	for _, said := range []struct{ name, words string }{
		{"deepseek-v4-flash", convFailedWord + " · upstream 503"},
		{"senior-dev", convRestartedWord},
		{"codeaf", taskPlanRefusedWord + " · the run's dollar ceiling is reached"},
	} {
		if !saidBy(lines, said.name, said.words) {
			t.Fatalf("the page does not have %s saying %q:\n%s", said.name, said.words, page)
		}
	}
	if strings.Contains(page, "retry later") || strings.Contains(page, "the whole history, summarized") {
		t.Fatalf("a failure or a summarized history drew more than its one line:\n%s", page)
	}
}

// A LONG RUN'S PAGE SAYS HOW MANY CALLS IT LEAVES OUT, the ceiling beside the
// spend when the page knows it, and at forty columns every name stands on a line
// of its own with its words hung under it — every row still inside the frame.
func TestAProgramsPageAtNarrowWidthAndWithEarlierCalls(t *testing.T) {
	page := programPage(programRow(), programTurns())
	page.Program.Earlier, page.Program.Calls, page.Program.CeilingUSD = 142, 145, 5
	a, _ := programPageApp(t, page, 40, 40)
	lines := programPageLines(a)
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "142 "+convEarlierWord) {
		t.Fatalf("the page does not say how many earlier calls it leaves out:\n%s", text)
	}
	if !strings.HasPrefix(lines[1], "implement · $1.24 of $5.00") {
		t.Fatalf("the pinned line is %q, want the ceiling beside the spend", lines[1])
	}
	if !strings.Contains(text, "\n senior-dev\n") || !strings.Contains(text, "\n   rewrite the auth") {
		t.Fatalf("at forty columns the names do not stand on lines of their own:\n%s", text)
	}
	for i, line := range lines {
		if cells := ansi.StringWidth(line); cells > 40 {
			t.Fatalf("row %d is %d cells in a 40-cell frame: %q", i, cells, line)
		}
	}
}

// THE RAIL ROW OF A PROGRAM'S RUN SAYS ITS STAGE AND WHAT IT HAS SPENT SO FAR,
// where it used to say only its clock. Both come off the run's plan row the
// surface already holds, never off a read the frame makes.
func TestTheRailRowOfAProgramsRunSaysItsStageAndSpend(t *testing.T) {
	row := programRow()
	row.ID = "7"
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, nil)
	counted := &railPlanCounter{planFake: fake}
	a.agent = counted
	a.width, a.height = 120, 30
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, row.Title, session.TaskRunning, session.TaskNotice{StartedAt: programRunBegan})})
	node := a.tasks[7]
	if node == nil {
		t.Fatal("the run's row never reached the rail")
	}
	reads := counted.rows
	under := plain(strings.Join(a.railUnder(node, 40), "\n"))
	if !strings.Contains(under, "implement") || !strings.Contains(under, "$1.24") {
		t.Fatalf("the rail row of a program's run reads %q, want its stage and its spend", under)
	}
	if counted.rows != reads || counted.pages != 0 {
		t.Fatalf("drawing the rail row read the agent: rows %d→%d, pages %d", reads, counted.rows, counted.pages)
	}
	// A RUN NO PROGRAM WAS HANDED IS UNCHANGED.
	row.Program, row.Stage = "", ""
	fake.plan[0] = row
	a.planRows = fake.plan
	plainUnder := plain(strings.Join(a.railUnder(node, 40), "\n"))
	if strings.Contains(plainUnder, "implement") || strings.Contains(plainUnder, "$1.24") {
		t.Fatalf("an ordinary run's rail row reads %q, which is a program's", plainUnder)
	}
}

// A PROGRAM'S PLAN ROW DRAWS ITS STAGE, NEVER A COMMAND. The worker publishes a
// program's phase on the live row a bash worker publishes its command on, and
// behind the shell's `$` it read as a command somebody typed.
func TestAProgramsPlanRowDrawsItsStageAndNotACommand(t *testing.T) {
	text := planTextFor(t, []session.PlanTaskRow{programRow()})
	if strings.Contains(text, "$ senior-dev") || strings.Contains(text, "running · running") {
		t.Fatalf("a program's plan row drew its stage as a command:\n%s", text)
	}
	if !strings.Contains(text, "implement") {
		t.Fatalf("a program's plan row does not name its stage:\n%s", text)
	}
}

// A PROGRAM'S RUN IS OFFERED NO TAB, SO NO TAB OFFERS IT A BOX. The run's tab
// used to open the stored page with the tab's own keyboard, which had to be
// kept from drawing a box for a program; a program's run has no tab now, and
// its task opens in the conversation's own tab, whose box sends a program
// nothing ([TestAProgramsRoomSendsNothingAndSaysSo]). Its held rows are no work
// tab's rows either, so the tab cannot be opened on them by any door.
func TestAProgramsRunOpensNoWorkTab(t *testing.T) {
	row := programRow()
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: programPage(row, programTurns())})
	a.width, a.height = 120, 28
	a.taskSheet.mine.plan = fake.plan
	if tab, ok := a.workTab(); ok {
		t.Fatalf("the program's run is offered a tab of its own, %q", tab.word)
	}
	if cmd := a.openWorkTab(); cmd != nil || a.workTabOn {
		t.Fatal("the work tab opened on a program's run")
	}
}

// A PROGRAM'S WORDS ARE ONE CLEAN ROW. What a program sends is a tool's raw
// output: the first line that says anything is drawn, with every escape
// sequence a terminal would obey taken out, a tab as a space, and no other
// control character left in it.
func TestAProgramsWordsAreOneCleanRow(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\n\n  \x1b[31mFAIL\x1b[0m\tpkg\x07 0.3s\nmore", "FAIL pkg 0.3s"},
		{"\x1b]0;a title\x07ok", "ok"},
		{"\r\n\r\nplain\r\n", "plain"},
		{"", ""},
	} {
		if got := convHead(tc.in); got != tc.want {
			t.Errorf("convHead(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// THE ARGUMENTS SAY WHAT A CALL WAS ABOUT, read forgivingly: the most telling
// argument, then the first string there is, a line cut inside a string read to
// its cut, and a line that is not an object drawn as written.
func TestACallsArgumentsAreReadForWhatTheCallWasAbout(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"command":"go test ./...","description":"Runs tests"}`, "go test ./..."},
		{`{"description":"Runs tests","command":"go vet ./..."}`, "go vet ./..."},
		{`{"filePath":"internal/auth/middleware.go","oldString":"func M`, "internal/auth/middleware.go"},
		{`{"oldString":"a \"quoted\" line\nand more`, `a "quoted" line and more`},
		{`{"limit":5,"name":"x"}`, "x"},
		{`{"limit":5}`, ""},
		{`go test ./...`, "go test ./..."},
		{`{"url":"https://example.com/a:b"}`, "https://example.com/a:b"},
	} {
		if got := convCallAbout(tc.args); got != tc.want {
			t.Errorf("convCallAbout(%s) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// EVERY DOOR INTO A PROGRAM'S TASK OPENS ITS CONVERSATION, AS A ROOM. The card
// in the conversation, a transcript link, the task strip and the home panel all
// come through [app.openRoomFor], which used to open an ordinary room — a blank
// page, because a program has no worker transcript — and then a full-frame page
// over the conversation with no tab strip. A held row that names its program
// opens the program's room at once, inside the conversation's tab, with the
// program's conversation as its body — whichever way the row's id is spelled:
// the store answers `t-7`, and a comparison against the bare number missed
// every real row.
func TestEveryDoorIntoAProgramsTaskOpensItsConversation(t *testing.T) {
	for _, id := range []string{"7", "t-7"} {
		t.Run(id, func(t *testing.T) {
			row := programRow()
			row.ID = id
			a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"7": programPage(row, programTurns())})
			a.width, a.height = 120, 28
			a.openRoomFor(7, row.Title)
			if a.programOf() == nil || a.railTaskPlanOn || a.taskSheet.planOn {
				t.Fatalf("the door did not open the program's room: room=%v railPage=%v page=%v", a.room != nil, a.railTaskPlanOn, a.taskSheet.planOn)
			}
			cmd := a.takeRoomPump()
			if cmd == nil {
				t.Fatal("nothing asked the store for the program's page")
			}
			drain(t, a, cmd)
			if len(fake.noted) != 0 {
				t.Fatalf("opening the room wrote notes %v", fake.noted)
			}
			if text := roomText(a); !strings.Contains(text, "I'll read the middleware and the store first.") {
				t.Fatalf("the room does not show the program's conversation:\n%s", text)
			}
		})
	}
}

// AND A TASK THAT IS NOT A PROGRAM'S STILL OPENS ITS ROOM, at once and without
// asking the store: the redirect is for a program's run and no other.
func TestADoorIntoAnOrdinaryTaskStillOpensItsRoom(t *testing.T) {
	row := programRow()
	row.ID, row.Program, row.Stage = "7", "", ""
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, nil)
	if a.programTask(7) {
		t.Fatal("an ordinary task was taken for a program's")
	}
	a.openRoomFor(7, row.Title)
	if a.railPlanPending.id != "" {
		t.Fatal("an ordinary task's door asked the store for a page instead of opening its room")
	}
}

// A PROGRAM'S RUNS ARE NO TAB, AND A BELT RUN BESIDE THEM KEEPS ITS OWN. With
// two of senior-dev's runs in one conversation the strip used to offer a tab
// named after one of them; a program's task opens in the conversation's own
// tab now, so neither is a tab — and a run the belt switch drives beside them
// is still the tab, named after itself and never after a program's run.
func TestAProgramsRunsAreNoTabAndABeltRunKeepsItsOwn(t *testing.T) {
	landed := programRow()
	landed.ID, landed.Title, landed.Status, landed.Stage = "t-1", "Implement true-myth", "done", ""
	working := programRow()
	working.ID, working.Title = "t-2", "Implement happy-dom"
	a, fake := planAppWith(t, []session.PlanTaskRow{landed, working}, nil)
	a.width, a.height = 120, 30
	a.taskSheet.mine.plan = fake.plan
	if tab, ok := a.workTab(); ok {
		t.Fatalf("a program's run is offered a tab, %q", tab.word)
	}
	belt := session.PlanTaskRow{ID: "t-3", Title: "Fix the flake", Status: "running"}
	a.taskSheet.mine.plan = append(append([]session.PlanTaskRow(nil), fake.plan...), belt)
	if tab, ok := a.workTab(); !ok || tab.word != belt.Title {
		t.Fatalf("the belt run's tab is %q, want %q", tab.word, belt.Title)
	}
}

// A ROOM OPENED ON A PROGRAM'S TASK BECOMES THE PROGRAM'S ROOM. The sessions
// place brings a conversation forward and reopens the room it was aimed at
// before that conversation's rows are read, so the row check at the door cannot
// see the program; the room asks the store itself, and becomes the program's
// room — still a room in the conversation's tab, never a page drawn over it.
func TestARoomOpenedOnAProgramsTaskBecomesItsRoom(t *testing.T) {
	row := programRow()
	row.ID = "7"
	a, _ := planAppWith(t, nil, map[string]session.PlanTaskPage{row.ID: programPage(row, programTurns())})
	a.width, a.height = 120, 28
	a.room = a.newRoom(7, row.Title)
	cmd := a.roomProgramCheck(7)
	if cmd == nil {
		t.Fatal("the room did not ask whether its task is a program's")
	}
	drive(t, a, cmd())
	if a.programOf() == nil || a.room.id != 7 {
		t.Fatalf("the room did not become the program's room: room=%v", a.room != nil)
	}
	if a.railTaskPlanOn || a.taskSheet.planOn {
		t.Fatal("the program's task was drawn as a page over the conversation")
	}
	if text := roomText(a); !strings.Contains(text, "I'll read the middleware and the store first.") {
		t.Fatalf("the room does not show the program's conversation:\n%s", text)
	}
	// AND AN ORDINARY TASK KEEPS ITS ROOM.
	plain := row
	plain.Program, plain.Stage = "", ""
	b, _ := planAppWith(t, nil, map[string]session.PlanTaskPage{"8": {Row: plain}})
	b.room = b.newRoom(8, "ordinary")
	drive(t, b, b.roomProgramCheck(8)())
	if b.room == nil || b.programOf() != nil {
		t.Fatal("an ordinary task's room was changed")
	}
}
