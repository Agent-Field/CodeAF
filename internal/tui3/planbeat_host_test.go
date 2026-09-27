package tui3

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// countedPlanAgent is the real remote client with its plan reads counted. Every
// call still crosses the real wire to the real engine; the count is how many did.
type countedPlanAgent struct {
	*remote.Agent
	rows  atomic.Int64
	pages atomic.Int64
}

func (c *countedPlanAgent) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	c.pages.Add(1)
	return c.Agent.PlanTaskPage(id)
}

func (c *countedPlanAgent) PlanTasks() []session.PlanTaskRow {
	c.rows.Add(1)
	return c.Agent.PlanTasks()
}

// hostedPlanApp is a surface over a REAL engine reached through a REAL client:
// a plan store on disk, a session agent that reads it, the remote server in
// front of that agent, and the client the surface is handed in a hosted
// conversation. `ended` completes the run's root before anything reads it.
func hostedPlanApp(t *testing.T, ended bool) (*app, *countedPlanAgent, string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	workspace := t.TempDir()
	store, err := session.OpenRunPlan(workspace, "the run", "the hand-off's own words")
	if err != nil {
		t.Fatalf("seed the run's store: %v", err)
	}
	if ended {
		if err := store.CompleteRoot("the run is over"); err != nil {
			t.Fatalf("end the run: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	engine, err := session.New(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "fixture",
		BaseURL: "http://127.0.0.1:1/v1", System: "Test only.",
	})
	if err != nil {
		t.Fatalf("build the real session agent: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: workspace}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: engine, Workspace: workspace}, nil
		},
	})
	if err != nil {
		t.Fatalf("serve the engine: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	counted := &countedPlanAgent{Agent: loop.Client.Agent()}
	a := newTestApp(counted)
	a.width, a.height = 200, 40
	a.homeRoot = t.TempDir()
	// THE RUN'S SUMMARY IS KEPT OUT OF EVERY TEST ON THIS FIXTURE. The engine is
	// real and its model's address refuses, so a summary asked after a message
	// would spend its whole budget finding that out; marking one as already out
	// is the surface's own way of not asking for another.
	a.runSummaryRefreshing = true
	return a, counted, session.PlanStorePath(workspace)
}

// A HOSTED CONVERSATION AT REST ASKS ITS ENGINE FOR NOTHING. The reader is always
// there in a hosted conversation, so a beat that ran whenever a reader existed
// would cross the wire every three seconds for ever, in a window nobody is
// touching. With no run that can still move, twenty beats cost no read beyond
// the first one that found the run ended.
func TestAHostedConversationAtRestReadsNoPlanOverTheWire(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, true)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	readPlanRows(t, a)
	first := counted.rows.Load()
	if first != 1 {
		t.Fatalf("the first reading crossed the wire %d times, want once", first)
	}
	if len(a.taskSheet.mine.plan) == 0 {
		t.Fatal("the first reading did not hold the ended run's row, so the test proves nothing")
	}
	for beat := 0; beat < 20; beat++ {
		now = now.Add(elsewhereEvery)
		readPlanRows(t, a)
	}
	if got := counted.rows.Load(); got != first {
		t.Fatalf("twenty beats at rest crossed the wire %d more times, want none", got-first)
	}
}

// AND WITH A RUN THAT CAN STILL MOVE, THE RAIL LEARNS OF A PART WITHIN ONE BEAT.
// The part is added to the real store by another handle, the way a run's worker
// adds one, and nothing is published to this window.
func TestAHostedRailLearnsOfAnAddedPartWithinOneBeat(t *testing.T) {
	a, counted, path := hostedPlanApp(t, false)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	readPlanRows(t, a)
	held := len(a.taskSheet.mine.plan)
	if held == 0 {
		t.Fatal("the first reading did not hold the live run's row")
	}
	worker, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.AddMany([]plandb.TaskSpec{{ID: "part", Title: "a part the run added", Description: "its own words"}}); err != nil {
		t.Fatal(err)
	}
	_ = worker.Close()
	now = now.Add(elsewhereEvery - time.Millisecond)
	readPlanRows(t, a)
	if got := len(a.taskSheet.mine.plan); got != held {
		t.Fatalf("the plan was read again inside its beat: %d rows, was %d", got, held)
	}
	now = now.Add(time.Millisecond)
	readPlanRows(t, a)
	if got := len(a.taskSheet.mine.plan); got != held+1 {
		t.Fatalf("one beat later the rail holds %d rows, want the added part as well as the %d it had", got, held)
	}
	if got := counted.rows.Load(); got != 2 {
		t.Fatalf("learning of one part cost %d reads over the wire, want two", got)
	}
}

type heldHostedPlanAgent struct {
	*countedPlanAgent
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldHostedPlanAgent) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	h.once.Do(func() {
		close(h.started)
		<-h.release
	})
	return h.Agent.PlanTaskPage(id)
}

// Once a rail gesture has chosen a task page, its delayed store read owns every
// following key. The conversation must never receive text intended for the page.
func TestHostedRailPageOwnsKeysWhileItsReadIsInFlight(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, false)
	held := &heldHostedPlanAgent{countedPlanAgent: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	a.input.value = []rune("conversation draft")
	cmd := a.openRailPlan("root", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	for _, r := range "keep the examples short" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := string(a.input.value); got != "conversation draft" {
		t.Fatalf("keys reached the conversation box: %q", got)
	}
	close(held.release)
	drive(t, a, <-answer)
	if a.roomPlan() == nil {
		t.Fatal("the delayed answer did not open the task room")
	}
}

func TestHostedRailPagePendingEscCancelsAndSecondPressSupersedes(t *testing.T) {
	a, _, _ := hostedPlanApp(t, false)
	a.railPlanPending = railPlanPending{id: "first", keys: []tea.KeyPressMsg{key("x")}}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.railPlanPending.id != "" || a.roomOpen() {
		t.Fatal("esc did not cancel the pending page")
	}
	a.railPlanPending = railPlanPending{id: "first", keys: []tea.KeyPressMsg{key("x")}}
	a.beginRailPlan("second")
	if a.railPlanPending.id != "second" || len(a.railPlanPending.keys) != 0 {
		t.Fatalf("second press retained the first target or its keys: %+v", a.railPlanPending)
	}
}

// openHostedPage opens the run's own task room the way a press on its row does,
// over the real wire, and answers the run's id.
func openHostedPage(t *testing.T, a *app) string {
	t.Helper()
	// THE RUN'S SUMMARY IS KEPT OUT OF A TEST THAT COUNTS THE PAGE'S READS. The
	// engine here is real and its model's address refuses, so a summary asked
	// after a message would spend its whole budget finding that out; marking one
	// as already out is the surface's own way of not asking for another.
	a.runSummaryRefreshing = true
	readPlanRows(t, a)
	if len(a.taskSheet.mine.plan) == 0 {
		t.Fatal("the first reading did not hold the run's row")
	}
	id := a.taskSheet.mine.plan[0].ID
	openPlanRoomNow(t, a, id)
	if plan := a.roomPlan(); plan == nil || plan.id != id {
		t.Fatal("the run's task room did not open")
	}
	return id
}

// paintTicks is the paint clock turning `ticks` times inside one beat and then
// the room's own beat once: a frame never reads the page, and the beat reads it
// once, answered and folded before the next, the way a fast local engine
// answers.
func paintTicks(t *testing.T, a *app, ticks int) {
	t.Helper()
	for tick := 0; tick < ticks; tick++ {
		drive(t, a, frameMsg{})
	}
	if a.room != nil {
		drive(t, a, planRoomTickMsg{gen: a.room.gen})
	}
}

// AN OPEN PAGE FOLLOWS ITS TASK ON A BEAT. The follow was offered its read on
// every tick of the paint clock and held back only while one was out, so a fast
// engine was asked again the moment it answered: 509 reads over the wire in
// ninety seconds on a real screen, about six a second, for one open page. A
// step lands every few seconds at best; the rail learns of the same run on a
// three second beat, and the page now does too.
func TestAnOpenPageOnARunningTaskReadsOncePerBeat(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, false)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	openHostedPage(t, a)
	if !a.planRoomRunning() {
		t.Fatalf("the fixture's run is %q, which the room does not follow, so the test proves nothing", a.room.plan.page.Row.Status)
	}
	opened := counted.pages.Load()
	const beats = 5
	for beat := 0; beat < beats; beat++ {
		paintTicks(t, a, 20)
		now = now.Add(elsewhereEvery)
	}
	if got := counted.pages.Load() - opened; got != beats {
		t.Fatalf("an open page on a moving task crossed the wire %d times over %d beats of twenty ticks each, want one read a beat", got, beats)
	}
}

// AND A PAGE ON A TASK THAT HAS ENDED READS ONCE, TO OPEN, AND NEVER AGAIN.
func TestAnOpenPageOnAnEndedTaskReadsOnceAndStops(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, true)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	openHostedPage(t, a)
	opened := counted.pages.Load()
	if opened != 1 {
		t.Fatalf("opening the page crossed the wire %d times, want once", opened)
	}
	for beat := 0; beat < 20; beat++ {
		paintTicks(t, a, 20)
		now = now.Add(elsewhereEvery)
	}
	if got := counted.pages.Load() - opened; got != 0 {
		t.Fatalf("a page on an ended task crossed the wire %d more times over twenty beats, want none", got)
	}
}

// A HOSTED PAGE AND ROW CARRY THEIR DISPLAY PARTS OVER THE REAL WIRE, and the
// hosted surface filters those facts rather than exposing run bookkeeping.
func TestHostedPlanPartsCrossTheWireAndFilterThePage(t *testing.T) {
	a, counted, path := hostedPlanApp(t, false)
	workspace := filepath.Dir(filepath.Dir(path))
	seedRows := counted.PlanTasks()
	if len(seedRows) != 1 {
		t.Fatalf("seed rows = %#v, want one", seedRows)
	}
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	liveCommand := "printf live-worker-bytes; plandb task overview"
	if err := store.SetLive("root", 2, liveCommand); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	command := "cd " + workspace + " && printf recorded-worker-bytes; plandb task overview"
	if err := os.MkdirAll(filepath.Dir(seedRows[0].TrajectoryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"kind":"step","step":1,"command":` + hostedJSONQuote(command) + `,"observation":"recorded-output"}` + "\n"
	if err := os.WriteFile(seedRows[0].TrajectoryPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	rows := counted.PlanTasks()
	if len(rows) != 1 || len(rows[0].LiveParts) != 2 {
		t.Fatalf("hosted row live parts = %#v, want two parts", rows)
	}
	page, ok := counted.PlanTaskPage("root")
	if !ok || len(page.Steps) != 1 || len(page.Steps[0].Parts) != 3 {
		t.Fatalf("hosted page step parts = %#v, ok %v, want three parts", page.Steps, ok)
	}

	// THE ENGINE'S FACT ABOUT THE HEAD CROSSES THE WIRE WITH THE PARTS. This row
	// leaves out a part addressed to the run's record, and that part prints: the
	// first line of what came back may be its print and not the work's, so the
	// engine withholds the head and the hosted page must draw none.
	if !page.Steps[0].ObservationHeadWithheld {
		t.Fatalf("hosted page step = %#v, want the head withheld for a row that leaves out a record part", page.Steps[0])
	}

	// THE PAGE CARRIES ITS LIVE STEP, and the room draws it as a call in flight
	// beside the recorded one, the two under one running line. The screen is
	// read on the recorded step alone, so the live step is settled first.
	store, err = plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearLive("root"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	openHostedPage(t, a)
	screen := planRoomText(t, a)
	for _, never := range []string{workspace, "plandb task overview", "recorded-output"} {
		if strings.Contains(screen, never) {
			t.Fatalf("hosted page contains filtered %q:\n%s", never, screen)
		}
	}
	for _, want := range []string{"recorded-worker-bytes"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("hosted page lacks %q:\n%s", want, screen)
		}
	}
}

func hostedJSONQuote(text string) string {
	return strconv.Quote(text)
}

// A REFUSAL THE ENGINE ESTABLISHED AS NOT RUN STAYS IN THE RECORD AND IN THE
// HEAD COUNT, BUT IT IS NOT A STEP A PERSON READS. This fixture writes the real
// trajectory shape, crosses the real remote server and client, and draws the
// page the same way the hosted product does. The older unmarked line proves
// absence still means the old rendering rather than an inferred refusal.
func TestHostedTaskPageOmitsEngineEstablishedNotRunStep(t *testing.T) {
	a, _, path := hostedPlanApp(t, false)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := store.RootID()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	taskDir := plandb.TaskDir(filepath.Dir(path), root)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	trajectory := filepath.Join(taskDir, "trajectory.jsonl")
	const beltSentence = "[not run] no action executed: return exactly one bash tool call per response — this belt has one hand"
	lines := strings.Join([]string{
		`{"kind":"step","step":1,"command":"printf ran-one","observation":"one"}`,
		`{"kind":"step","step":2,"command":"cat first second third","observation":"` + beltSentence + `","not_run":true}`,
		`{"kind":"step","step":3,"command":"printf ran-three","observation":"three"}`,
		`{"kind":"step","step":4,"command":"printf older-record","observation":"legacy answer"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(trajectory, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	openHostedPage(t, a)
	page := roomCallText(t, a)
	if got, want := roomCommands(a), []string{"printf ran-one", "printf ran-three", "printf older-record"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("hosted room's calls are %q, want %q", got, want)
	}
	for _, forbidden := range []string{"cat first second third", "[not run]", "no action executed", "this belt has one hand"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("hosted page drew engine-established refusal text %q:\n%s", forbidden, page)
		}
	}
	if got := roomStepsWord(a); got != "4 steps" {
		t.Fatalf("the record count changed when a row was omitted: %q", got)
	}
}

// AN ACTION THE WORKER ATTEMPTED AND A DOOR REFUSED IS DRAWN, AS ONE LINE AND
// NOT AS A STEP THAT RAN. The record says which kind of not-run call this is
// (`refused` beside `not_run`, written by the run engine from the event), and
// the page reads that fact and nothing else: the line is the permissions page's
// own word and what was tried, it carries no number, the answer written for the
// worker is not drawn under it, and the rows around it keep their recorded
// numbers. A not-run call WITHOUT the fact, in the same record, still has no row.
func TestHostedTaskPageDrawsARefusedActionAsOneLineAndACorrectionAsNone(t *testing.T) {
	a, _, path := hostedPlanApp(t, false)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := store.RootID()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	taskDir := plandb.TaskDir(filepath.Dir(path), root)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const doorSentence = "DOOR-ANSWER-WRITTEN-FOR-THE-WORKER"
	const formSentence = "FORM-ANSWER-WRITTEN-FOR-THE-WORKER"
	lines := strings.Join([]string{
		`{"kind":"step","step":1,"command":"printf ran-one","observation":"one"}`,
		`{"kind":"step","step":2,"command":"touch /outside/the-ground","observation":"` + doorSentence + `","not_run":true,"refused":true}`,
		`{"kind":"step","step":3,"command":"cat first second third","observation":"` + formSentence + `","not_run":true}`,
		`{"kind":"step","step":4,"command":"printf ran-four","observation":"four"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(taskDir, "trajectory.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	openHostedPage(t, a)
	page := roomCallText(t, a)
	refused := taskPlanRefusedWord + railSep + "touch /outside/the-ground"
	var drawn []string
	for _, line := range strings.Split(planRoomText(t, a), "\n") {
		if strings.Contains(line, "touch /outside/the-ground") {
			drawn = append(drawn, strings.TrimSpace(strings.TrimRight(line, " │")))
		}
	}
	if len(drawn) != 1 || !strings.HasSuffix(drawn[0], refused) || strings.ContainsAny(drawn[0], "0123456789") {
		t.Fatalf("a refused action draws as exactly one line, %q, with no number; drew %q:\n%s", refused, drawn, page)
	}
	if got, want := roomCommands(a), []string{"printf ran-one", "printf ran-four"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the room's calls are %q, want %q", got, want)
	}
	if got := roomStepsWord(a); got != "4 steps" {
		t.Fatalf("the room counts %q, want the record's 4 steps", got)
	}
	shell := a.actionLead(session.ActionRun, true)
	for _, forbidden := range []string{doorSentence, formSentence, "cat first second third", shell + "touch"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("the page drew %q, which is the worker's answer, a correction's row, or a shell mark on a call that never ran:\n%s", forbidden, page)
		}
	}
}

// A TASK'S DRAWN STEP NUMBERS CONTINUE ACROSS WAKES. The engine has already
// recorded three steps before the wake and two after it; the hosted page draws
// that record once, in its recorded order.
func TestHostedPageDrawsContinuedTaskStepsOnceInOrder(t *testing.T) {
	a, _, path := hostedPlanApp(t, false)
	id := openHostedPage(t, a)
	taskDir := plandb.TaskDir(filepath.Dir(path), strings.TrimPrefix(id, "t-"))
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatalf("make the continued task record folder: %v", err)
	}
	record := strings.Join([]string{
		`{"kind":"step","step":1,"command":"printf one"}`,
		`{"kind":"step","step":2,"command":"printf two"}`,
		`{"kind":"step","step":3,"command":"printf three"}`,
		`{"kind":"step","step":4,"command":"printf four"}`,
		`{"kind":"step","step":5,"command":"printf five"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(taskDir, "trajectory.jsonl"), []byte(record), 0o600); err != nil {
		t.Fatalf("record the continued task: %v", err)
	}
	a.closeRoom()
	openPlanRoomNow(t, a, id)

	got := roomCommands(a)
	if want := []string{"printf one", "printf two", "printf three", "printf four", "printf five"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the room's step rows = %q, want %q exactly once and in order", got, want)
	}
}
