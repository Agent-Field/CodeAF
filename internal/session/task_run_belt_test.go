package session

// The chat's task door on the run engine (task_run_belt.go): a `/task` under the
// bash belt seeds the conversation's store, starts the engine, and answers the
// run's id at once; a second `/task` joins the live run; the ending lands. The
// engine here is a double that stands in for internal/run — which this package
// cannot import without a cycle — and it drives the store through the same
// plandb door the real one does, on the completer the door handed it.

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// beltRunCompleter is the provider every worker of the run is seated on — the
// shape bashbelt_plandb_test.go's lane completer takes: one completer, one
// scripted answer, and nothing else.
type beltRunCompleter struct{ text string }

func (c beltRunCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return textResponse(c.text), nil
}

// beltRunDouble is the run engine as the test drives it. Start records that it
// began, holds until the test releases it — so "a run is live" is a state the
// test owns rather than one it races — asks the completer it was handed, and
// completes the store's root with the answer. Land answers a fixed landing.
type beltRunDouble struct {
	mu       sync.Mutex
	summary  RunSummary
	landing  RunLanding
	spec     RunSpec
	entered  chan struct{}
	release  chan struct{}
	finished chan struct{}
	ran      bool
	// ctx is the context the engine was started under, kept so a test can ask
	// whether the run outlived the turn that launched it.
	ctx       context.Context
	landCalls int
	// work, when set, is what the run's workers did: it is called with the run's
	// own working copy before the run ends. real makes the landing the engine's
	// real one (a commit of the copy's own status) instead of a scripted answer,
	// for the tests that follow the work all the way home.
	work func(workspace string)
	real bool
	// honoursStop makes the double end the way the real engine ends when its
	// context is cut: early is what its workers had written by then, it returns
	// without completing the store's root, and it answers the engine's own word
	// for a run that did not finish.
	honoursStop bool
	early       func(workspace string)
	// leaveOpen makes the double end the way the real engine ends a run a
	// limit or a program's own ending took down: its store's root left open.
	leaveOpen bool
}

func newBeltRunDouble(result string) *beltRunDouble {
	return &beltRunDouble{
		summary:  RunSummary{Outcome: beltRunOutcomeDone, Result: result, Nodes: 1, Steps: 2},
		landing:  RunLanding{Branch: "task/fix-the-nil-map-crash", Changed: []string{"internal/session/agent.go"}},
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (d *beltRunDouble) Start(ctx context.Context, spec RunSpec) RunSummary {
	d.mu.Lock()
	d.ran = true
	d.ctx = ctx
	d.spec = spec
	d.mu.Unlock()
	if spec.CompleterFor != nil {
		if completer := spec.CompleterFor("test/model"); completer != nil {
			if response, err := completer.CompleteWithMessages(ctx, []ai.Message{textMessage("user", "run the brief")}); err == nil && response != nil {
				d.mu.Lock()
				d.summary.Result = response.Text()
				d.mu.Unlock()
			}
		}
	}
	if d.early != nil {
		d.early(spec.Workspace)
	}
	// The real run engine reports its reconciled cumulative spend while work is
	// live. Drive the same observer before either the normal or stopped ending.
	if spec.OnSpend != nil {
		d.mu.Lock()
		usd := d.summary.USD
		d.mu.Unlock()
		spec.OnSpend(usd)
	}
	close(d.entered)
	if d.honoursStop {
		select {
		case <-d.release:
		case <-ctx.Done():
			close(d.finished)
			return RunSummary{Outcome: "ran and did not finish", Nodes: 1, Steps: 1}
		}
	} else {
		<-d.release
	}
	if d.work != nil {
		d.work(spec.Workspace)
	}
	if spec.Store != nil && !d.leaveOpen {
		_ = spec.Store.CompleteRoot(d.summary.Result)
	}
	close(d.finished)
	return d.summary
}

func (d *beltRunDouble) Land(_ context.Context, _ *plandb.Store, workspace, _ string) (RunLanding, error) {
	d.mu.Lock()
	d.landCalls++
	d.mu.Unlock()
	if d.real {
		branch, changed, refusal, err := LandRunTree(workspace, "the run", false)
		return RunLanding{Branch: branch, Changed: changed, Refused: refusal}, err
	}
	return d.landing, nil
}

func (d *beltRunDouble) lands() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.landCalls
}

func (d *beltRunDouble) didRun() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ran
}

// registerBeltRunEngine installs the engine double for one test and restores
// whatever was registered before it, so the package's own seam is left as the
// next test found it.
func registerBeltRunEngine(t *testing.T, engine RunEngine) {
	t.Helper()
	previous := chatRunEngine
	RegisterRunEngine(engine)
	t.Cleanup(func() { RegisterRunEngine(previous) })
}

// endBeltRun lets the double's run finish and WAITS FOR THE RUN TO BE OVER: its
// landing committed, its copy given back, its store closed. A test that released
// the run and returned used to race that ending against its own temporary
// directory's removal, and lost it under load as "directory not empty".
func endBeltRun(t *testing.T, agent *Agent, double *beltRunDouble) {
	t.Helper()
	close(double.release)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
}

// beltRunStoreAt is a fresh handle on the run's store, adopted by path — the
// same road [planState.open] takes. Every assertion reads the file rather than
// a copy, because the run's own writers have been at it since any earlier read.
func beltRunStoreAt(t *testing.T, dir string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "", "", "", "")
	if err != nil {
		t.Fatalf("open the run store at %s: %v", dir, err)
	}
	return store
}

func beltRunTaskAt(t *testing.T, dir, id string) *plandb.Task {
	t.Helper()
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	return store.Task(id)
}

func beltRunTaskCount(t *testing.T, dir string) int {
	t.Helper()
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	return len(store.Tasks())
}

// beltRunNotes carries every note left on the run's root, read through a fresh
// handle.
func beltRunNotes(t *testing.T, dir, id string) []string {
	t.Helper()
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	var out []string
	for _, note := range store.Notes(id, 0) {
		out = append(out, note.Body)
	}
	return out
}

// conversationNotes counts the notes this conversation received carrying a
// phrase — a landing wakes the person, so the note is on the steering queue
// until the woken turn reads it, and in the transcript afterwards. Both are
// searched, because which one holds it depends on whether the turn has run.
func conversationNotes(agent *Agent, phrase string) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	count := 0
	for _, note := range agent.steering {
		if strings.Contains(note.text(), phrase) {
			count++
		}
	}
	for _, message := range agent.messages {
		if strings.Contains(messageText(message), phrase) {
			count++
		}
	}
	return count
}

// conversationJournalLines counts display entries in the durable conversation record.
func conversationJournalLines(agent *Agent, phrase string) int {
	count := 0
	for _, entry := range agent.Transcript() {
		if strings.Contains(entry.Text, phrase) {
			count++
		}
	}
	return count
}

// waitFor polls a condition to a bounded deadline, failing with what it was
// waiting on rather than hanging.
func beltRunWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ok() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// planRowFor finds a row in the conversation's plan by its store id, or nil.
func planRowFor(rows []PlanTaskRow, id string) *PlanTaskRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// TestStartTaskBashBeltStartsARunOnTheStore is the door's second road whole: a
// `/task` under the belt answers the store's root id, the plan shows the run
// running, and when the engine comes home the root reads done, the root carries
// the landing, and the conversation has been handed one landing note.
func TestStartTaskBashBeltStartsARunOnTheStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run fixed the nil map")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	completer := &scriptedCompleter{steps: []step{finalText("the run fixed the nil map")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
	})

	clock := &fakeClock{at: time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)}
	agent.taskNow = clock.now

	id, title, _, err := agent.StartTask(context.Background(), "fix the nil map crash", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if id == 0 {
		t.Fatal("the run road answered no run id")
	}
	if title == "" {
		t.Fatal("the run road answered no title")
	}

	// THE STORE'S ROOT IS THE ID THE DOOR ANSWERED, and the plan reads it as
	// this chat's work, running.
	rootID := strconv.FormatUint(id, 10)
	root := beltRunTaskAt(t, dir, rootID)
	if root == nil {
		t.Fatalf("the store holds no root %s", rootID)
	}
	if root.Status != plandb.StatusRunning {
		t.Fatalf("the run's root reads %s, want running", root.Status)
	}
	if row := planRowFor(agent.PlanTasks(), planStoreID(rootID)); row == nil || row.Status != string(plandb.StatusRunning) {
		t.Fatalf("PlanTasks does not show the run running: %+v", agent.PlanTasks())
	}

	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	callsBeforeLanding := completer.requests()
	close(double.release)
	beltRunWaitFor(t, "the run's landing", func() bool {
		task := beltRunTaskAt(t, dir, rootID)
		agent.beltMu.Lock()
		landed := agent.beltRun == nil
		agent.beltMu.Unlock()
		return task != nil && task.Status == plandb.StatusDone && landed
	})
	// THE CONVERSATION TAKES NO TURN AT A LANDING THAT OWES NO ANSWER. The one
	// call a landing does make is the run's own summary, a small errand on the
	// worker model that is not a turn, so it is counted out by the page it reads.
	if got := completer.requests() - callsBeforeLanding - runSummaryRequestCount(completer); got != 0 {
		t.Fatalf("landing made %d completer calls beside the run summary, want zero", got)
	}
	select {
	case <-wakes:
		t.Fatal("done landing published a wake")
	default:
	}

	if task := beltRunTaskAt(t, dir, rootID); task == nil || task.Status != plandb.StatusDone {
		t.Fatalf("the run's root did not read done")
	}
	// THE BRANCH THE PERSON IS TOLD IS THE ONE THEIR FOLDER IS ON, because that is
	// where the work is once it has come home; the copy's own branch is gone.
	home := double.landing
	home.Branch = currentBranch(agent.config.Workspace)
	if !anyNoteCarries(beltRunNotes(t, dir, rootID), "landed on "+home.Branch) {
		t.Fatalf("no note on the root carries the branch: %v", beltRunNotes(t, dir, rootID))
	}
	wantDigest := beltRunOutcomeNote(nil, "", double.summary, home, 0)
	if !strings.Contains(wantDigest, "done") || !strings.Contains(wantDigest, "the run fixed the nil map") ||
		!strings.Contains(wantDigest, "landed on "+home.Branch) {
		t.Fatalf("digest = %q, want outcome, root result, and work destination", wantDigest)
	}
	if got := conversationJournalLines(agent, wantDigest); got != 1 {
		t.Fatalf("the conversation journal carries digest %d times, want one", got)
	}
	journal := agent.file.journalPath()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := newAgent(Config{Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if got := conversationJournalLines(reopened, wantDigest); got != 1 {
		t.Fatalf("reopened conversation carries digest %d times, want one", got)
	}
	// AND THE REOPENED CONVERSATION KNOWS THE RUN ENDED. The ending used to reach
	// the surface and never the checkpoint, so a finished run came back with a
	// spinner and was counted as moving for ever.
	kept := reopened.graph().runRows(id)
	if len(kept) != 1 || kept[0].State != TaskDone || kept[0].StartedAt.IsZero() || kept[0].EndedAt.IsZero() {
		t.Fatalf("the reopened conversation's row for the run = %+v, want one done row with its start and its end", kept)
	}
	// the run's row settled too, on the surface's own lane
	if row := planRowFor(agent.PlanTasks(), planStoreID(rootID)); row == nil || row.Status != string(plandb.StatusDone) {
		t.Fatalf("the plan does not show the run done: %+v", agent.PlanTasks())
	}
}

func TestStartTaskBashBeltPassesTheConversationWallLeftToTheRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.Budget = Budget{Wall: 2 * time.Hour}
	})
	agent.startedAt = time.Now().Add(-90 * time.Minute)

	if _, _, _, err := agent.StartTask(context.Background(), "finish within the time left", false); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	got := double.spec.Elapsed
	double.mu.Unlock()
	if got < 29*time.Minute || got > 31*time.Minute {
		t.Fatalf("run elapsed limit = %v, want the roughly 30 minutes left, not the original 2 hours", got)
	}
	endBeltRun(t, agent, double)
}

func TestDriveBeltRunLimitUsesTheOrdinaryLandingRoad(t *testing.T) {
	agent, _, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	landCalls := 0
	engine := landingRunDouble{
		summary:   RunSummary{Outcome: "a limit you set stopped it"},
		landing:   RunLanding{Branch: "task/limited", Changed: []string{"kept.txt"}},
		landCalls: &landCalls,
	}

	agent.driveBeltRun(context.Background(), engine, run, RunSpec{})

	if landCalls != 1 {
		t.Fatalf("Land calls = %d, want exactly one ordinary landing", landCalls)
	}
	notice := agent.beltRunNotice(run, engine.summary, engine.landing)
	if notice.State != TaskFailed {
		t.Fatalf("limited run row state = %q, want the same failed state as a cost-limited run", notice.State)
	}
	if !strings.Contains(notice.Report, "a limit you set stopped it") {
		t.Fatalf("limited run row report = %q, want the existing limit ending sentence", notice.Report)
	}
}

// TestStartTaskBashBeltJoinsTheLiveRun: a run is one store, so a second `/task`
// while one is live adds its work to that same store — a child of the run's one
// root — rather than opening another. The second id is in the store, and the
// store still holds one file.
func TestStartTaskBashBeltJoinsTheLiveRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
	})

	first, _, _, err := agent.StartTask(context.Background(), "the first piece of the work", false)
	if err != nil {
		t.Fatalf("StartTask (first): %v", err)
	}
	<-double.entered

	second, _, _, err := agent.StartTask(context.Background(), "a second piece of the work", false)
	if err != nil {
		t.Fatalf("StartTask (second): %v", err)
	}
	if second == 0 || second == first {
		t.Fatalf("the second task answered id %d, want its own", second)
	}
	if got := beltRunTaskCount(t, dir); got != 2 {
		t.Fatalf("the store holds %d tasks, want the run's root and the joined work", got)
	}
	secondID := strconv.FormatUint(second, 10)
	if beltRunTaskAt(t, dir, secondID) == nil {
		t.Fatalf("the store holds no task %s, so the second task opened another store", secondID)
	}
	if task := beltRunTaskAt(t, dir, secondID); task != nil && task.ParentID != strconv.FormatUint(first, 10) {
		t.Fatalf("the second task's parent is %q, want the run's root %d", task.ParentID, first)
	}
	close(double.release)
	beltRunWaitFor(t, "the run's landing", func() bool { return conversationNotes(agent, "landed on ") == 1 })
	// THE JOINED HAND-OFF'S ROW ENDS WITH THE RUN. It was published running when
	// it joined and nothing published its ending, so it span beside a finished
	// run for as long as the window stayed open.
	beltRunWaitFor(t, "the joined row to settle", func() bool {
		rows := agent.graph().runRows(second)
		return len(rows) == 1 && rows[0].State != TaskRunning && !rows[0].EndedAt.IsZero()
	})
}

// TestStartTaskWithoutBeltKeepsTheLegacyRoad: with the switch unset the door is
// the one it always was — a node is admitted and no plan store is seeded, and
// the run engine is never reached — so nothing a conversation sees changes.
func TestStartTaskWithoutBeltKeepsTheLegacyRoad(t *testing.T) {
	// CODEAF_TASK_BELT is deliberately left unset here.
	double := newBeltRunDouble("the run must not go")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
	})

	id, _, _, err := agent.StartTask(context.Background(), "fix the nil map crash", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if agent.graph().node(id) == nil {
		t.Fatal("the legacy road admitted no node")
	}
	if _, err := os.Stat(filepath.Join(dir, planStoreFilename)); err == nil {
		t.Fatal("the belt unset seeded a plan store")
	}
	if double.didRun() {
		t.Fatal("the belt unset reached the run engine")
	}
}

func anyNoteCarries(notes []string, phrase string) bool {
	for _, note := range notes {
		if strings.Contains(note, phrase) {
			return true
		}
	}
	return false
}

func TestLandingDigestCarriesTheStoredNowSentence(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", planRootID, "The run", "person ask")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	payload, err := json.Marshal(storedRunSummary{Summary: RunPlanSummary{
		What: "repair the landing digest",
		Now:  "The focused landing tests pass.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddContext(planRootID, runSummaryContextKind, string(payload)); err != nil {
		t.Fatalf("store summary: %v", err)
	}

	got := beltRunOutcomeNote(store, planRootID, RunSummary{Outcome: beltRunOutcomeDone}, RunLanding{
		Branch: "task/landing-digest", Changed: []string{"internal/session/task_run_belt.go"},
	}, 0)
	want := "done · landed on task/landing-digest: 1 file · The focused landing tests pass."
	if got != want {
		t.Fatalf("landing digest = %q, want %q", got, want)
	}
}

func TestLandingDigestIsUnchangedWithoutAStoredSummary(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", planRootID, "The run", "person ask")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got := beltRunOutcomeNote(store, planRootID, RunSummary{Outcome: beltRunOutcomeDone}, RunLanding{
		Branch: "task/landing-digest", Changed: []string{"internal/session/task_run_belt.go"},
	}, 0)
	want := "done · landed on task/landing-digest: 1 file"
	if got != want {
		t.Fatalf("landing digest = %q, want byte-for-byte legacy digest %q", got, want)
	}
}

// landingRunDouble ends synchronously so the test can observe the exact order:
// the summary refresh must have stored its sentence before the outcome note is
// composed.
type landingRunDouble struct {
	summary   RunSummary
	landing   RunLanding
	landCalls *int
}

func (d landingRunDouble) Start(context.Context, RunSpec) RunSummary { return d.summary }
func (d landingRunDouble) Land(context.Context, *plandb.Store, string, string) (RunLanding, error) {
	if d.landCalls != nil {
		*d.landCalls++
	}
	return d.landing, nil
}

func landingSummaryFixture(t *testing.T, client *scriptedCompleter) (*Agent, *plandb.Store, *beltRun, string) {
	t.Helper()
	// THE BELT IS SET HERE, NEVER INHERITED: the plan store's door only opens
	// under it, and a test that passes because the shell that ran it had the
	// variable is red on every other machine.
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	store, err := plandb.Open(path, "run", planRootID, "The run", "person ask")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 21, 0, 0, 0, time.UTC)
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.Place = Place{Dir: dir}
		config.clock = func() time.Time { return now }
	})
	run := &beltRun{store: store, root: planRootID, row: 1, title: "The run"}
	return agent, store, run, dir
}

func runSummaryRequestCount(client *scriptedCompleter) int {
	count := 0
	for i := 0; i < client.requests(); i++ {
		for _, message := range client.request(i) {
			if strings.Contains(messageText(message), "You write the four lines a person reads") {
				count++
				break
			}
		}
	}
	return count
}

func TestDriveBeltRunRefreshesSummaryOnceBeforeOutcomeNote(t *testing.T) {
	client := &scriptedCompleter{steps: []step{finalText("what: repair landing\nsince: the work landed\nnow: The fresh landing summary is stored.\nnext: Nothing needs you.")}}
	agent, _, run, dir := landingSummaryFixture(t, client)
	engine := landingRunDouble{
		summary: RunSummary{Outcome: beltRunOutcomeDone, Result: "fixed"},
		landing: RunLanding{Branch: "task/landing", Changed: []string{"internal/session/task_run_belt.go"}},
	}

	agent.driveBeltRun(context.Background(), engine, run, RunSpec{})

	if got := runSummaryRequestCount(client); got != 1 {
		t.Fatalf("summary requests = %d, want exactly one", got)
	}
	notes := beltRunNotes(t, dir, planRootID)
	if !anyNoteCarries(notes, "done · fixed · landed on task/landing: 1 file · The fresh landing summary is stored.") {
		t.Fatalf("outcome note was written before the fresh now sentence: %v", notes)
	}
}

func TestDriveBeltRunSummaryFailurePreservesOutcomeNoteAndIsDeadlineBounded(t *testing.T) {
	landing := RunLanding{Branch: "task/landing", Changed: []string{"internal/session/task_run_belt.go"}}
	want := "done · landed on task/landing: 1 file"
	tests := []struct {
		name string
		step step
	}{
		{name: "refused", step: func(context.Context, []ai.Message) (*ai.Response, error) { return nil, context.Canceled }},
		{name: "slow", step: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &scriptedCompleter{steps: []step{tt.step}}
			agent, _, run, dir := landingSummaryFixture(t, client)
			// THE DEADLINE IS SHORTENED, NOT WAITED OUT: what is asserted is that a
			// call that never answers is cut by it, not how long a box under load
			// takes to notice.
			was := beltRunSummaryDeadline
			beltRunSummaryDeadline = 40 * time.Millisecond
			t.Cleanup(func() { beltRunSummaryDeadline = was })
			started := time.Now()
			agent.driveBeltRun(context.Background(), landingRunDouble{
				summary: RunSummary{Outcome: beltRunOutcomeDone}, landing: landing,
			}, run, RunSpec{})
			elapsed := time.Since(started)
			if elapsed > was {
				t.Fatalf("landing delayed %v: the refresh was not cut at its deadline", elapsed)
			}
			notes := beltRunNotes(t, dir, planRootID)
			if len(notes) != 1 || notes[0] != want {
				t.Fatalf("failed refresh changed outcome note: %v, want [%q]", notes, want)
			}
		})
	}
}

func TestDriveBeltRunDoesNotRefreshWhenStoreIsGone(t *testing.T) {
	client := &scriptedCompleter{steps: []step{finalText("what: must not be called\nsince: no\nnow: no\nnext: no")}}
	agent, store, run, _ := landingSummaryFixture(t, client)
	// The conversation no longer has a plan handle, although the engine's
	// already-open handle remains long enough to record its legacy outcome.
	agent.mu.Lock()
	agent.config.Place = Place{Dir: t.TempDir()}
	agent.mu.Unlock()
	agent.driveBeltRun(context.Background(), landingRunDouble{
		summary: RunSummary{Outcome: beltRunOutcomeDone},
	}, run, RunSpec{})
	if got := runSummaryRequestCount(client); got != 0 {
		t.Fatalf("summary requests with no store = %d, want none", got)
	}
	_ = store.Close()
}

func beltRunRepoState(t *testing.T, repo string) string {
	t.Helper()
	status, err := git(repo, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		t.Fatalf("read repository state: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(repo, "seed.txt"))
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	return status + "\x00" + string(body)
}

func TestApprovedBeltHandoffCutsRunCopyFromResolvedGround(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(conversation, "seed.txt"), []byte("conversation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(conversation, "add", "seed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(conversation, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "seed"); err != nil {
		t.Fatal(err)
	}
	before := beltRunRepoState(t, conversation)
	sessionDir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
	})
	stand := taskStand{dir: conversation, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 41, "isolated work", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start run: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	want := filepath.Join(canonicalPath(sessionDir), placeTrees, "41")
	if canonicalPath(workspace) != want {
		t.Fatalf("run workspace = %q, want %q", workspace, want)
	}
	if got := beltRunRepoState(t, conversation); got != before {
		t.Fatalf("conversation changed while run worked: %q != %q", got, before)
	}
	close(double.release)
	beltRunWaitFor(t, "run finish", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
}

func TestBeltRunUsesAlternateGroundAndOnlySameGroundMayJoin(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	alternate := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(conversation, "seed.txt"), []byte("conversation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(conversation, "add", "seed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(conversation, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "conversation seed"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alternate, "seed.txt"), []byte("alternate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(alternate, "add", "seed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(alternate, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "alternate seed"); err != nil {
		t.Fatal(err)
	}
	conversationBefore := beltRunRepoState(t, conversation)
	sessionDir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
	})
	alternateStand := taskStand{dir: alternate, mode: TaskModeWorktree, rung: taskGroundSaid}
	if err := agent.startKnownTaskRun(context.Background(), 51, "alternate work", "brief", nil, alternateStand, ""); err != nil {
		t.Fatalf("start alternate run: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	body, err := os.ReadFile(filepath.Join(workspace, "seed.txt"))
	if err != nil || string(body) != "alternate\n" {
		t.Fatalf("run copy was not cut from alternate ground: body=%q err=%v", body, err)
	}
	if err := agent.startKnownTaskRun(context.Background(), 52, "same ground child", "brief", nil, alternateStand, ""); err != nil {
		t.Fatalf("same-ground join: %v", err)
	}
	if task := beltRunTaskAt(t, sessionDir, "52"); task == nil || task.ParentID != "51" {
		t.Fatalf("same-ground hand-off did not join the run: %+v", task)
	}
	conversationStand := taskStand{dir: conversation, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 53, "different ground", "brief", nil, conversationStand, ""); err == nil || !strings.Contains(err.Error(), "share one copy of one folder") {
		t.Fatalf("different-ground hand-off error = %v, want same-ground explanation", err)
	}
	if task := beltRunTaskAt(t, sessionDir, "53"); task != nil {
		t.Fatalf("different-ground hand-off joined the live run: %+v", task)
	}
	if got := beltRunRepoState(t, conversation); got != conversationBefore {
		t.Fatalf("conversation changed during alternate-ground run: %q != %q", got, conversationBefore)
	}
	endBeltRun(t, agent, double)
}

// A RUN'S WORK COMES HOME WHEN THE RUN ENDS, the way a task's always has. The
// workers write in the run's own copy and the person's folder does not move
// while they do; at the end the copy's work is committed, merged into the ground
// it was cut from, and the copy is given back. The run is then OVER: nothing is
// left waiting in memory for a second gesture, so the next hand-off starts a run
// of its own and never joins one whose workers have gone home.
func TestABeltRunsWorkComesHomeWhenItEnds(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("the change is made")
	double.real = true
	var during string
	double.work = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "made.txt"), []byte("made by the run\n"), 0o644); err != nil {
			t.Errorf("the run's own write: %v", err)
		}
		during = beltRunRepoState(t, conversation)
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	before := beltRunRepoState(t, conversation)
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	if workspace == canonicalPath(conversation) {
		t.Fatalf("the run works in the person's own folder %s", workspace)
	}
	close(double.release)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	if during != before {
		t.Fatalf("the person's folder moved while the run worked:\n%s\n--- before ---\n%s", during, before)
	}
	body, err := os.ReadFile(filepath.Join(conversation, "made.txt"))
	if err != nil || string(body) != "made by the run\n" {
		t.Fatalf("the run's work is not in the folder it was cut from: %q, %v", body, err)
	}
	if out, _ := git(conversation, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("the work came home uncommitted:\n%s", out)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("the run's copy %s was not given back: %v", workspace, err)
	}
	if got := double.lands(); got != 1 {
		t.Fatalf("the run's landing ran %d times, want once", got)
	}
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, note := range store.Notes(store.RootID(), 0) {
		said += note.Body + "\n"
	}
	_ = store.Close()
	if !strings.Contains(said, "its work is in "+canonicalPath(conversation)+" on ") {
		t.Fatalf("the run's page does not say where its work is now:\n%s", said)
	}
	// AND THE CARD SAYS MERGED, on the person's own branch. It read `branch kept`
	// over work that was already in their folder.
	rows := agent.graph().runRows(71)
	if len(rows) == 0 || rows[0].Merge != mergeMerged || rows[0].Branch != currentBranch(conversation) {
		t.Fatalf("the run's row says %+v, want merged on %s", rows, currentBranch(conversation))
	}
}

// A HAND-OFF AFTER A RUN HAS ENDED IS A RUN OF ITS OWN, IN A COPY OF ITS OWN.
// It never joins the ended run (whose workers have gone home and whose copy was
// given back), and it is cut from the ground AS THE FIRST RUN LEFT IT, so the
// second run's workers read the first run's work.
func TestAHandoffAfterAnEndedRunStartsAFreshRunInItsOwnCopy(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	first := newBeltRunDouble("the first change is made")
	first.real = true
	first.work = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "first.txt"), []byte("first\n"), 0o644); err != nil {
			t.Errorf("the first run's write: %v", err)
		}
	}
	registerBeltRunEngine(t, first)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	stand := taskStand{dir: conversation, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 81, "the first", "brief", nil, stand, ""); err != nil {
		t.Fatal(err)
	}
	<-first.entered
	endBeltRun(t, agent, first)

	second := newBeltRunDouble("the second change is made")
	second.real = true
	sawFirst := false
	second.work = func(workspace string) {
		_, err := os.Stat(filepath.Join(workspace, "first.txt"))
		sawFirst = err == nil
	}
	registerBeltRunEngine(t, second)
	if err := agent.startKnownTaskRun(context.Background(), 82, "the second", "brief", nil, stand, ""); err != nil {
		t.Fatal(err)
	}
	<-second.entered
	first.mu.Lock()
	firstCopy := first.spec.Workspace
	first.mu.Unlock()
	second.mu.Lock()
	secondCopy, secondRoot := second.spec.Workspace, second.spec.Store.RootID()
	second.mu.Unlock()
	if secondCopy == firstCopy || secondCopy == canonicalPath(conversation) {
		t.Fatalf("the second run works in %s; the first worked in %s and the person's folder is %s", secondCopy, firstCopy, conversation)
	}
	if secondRoot != "82" {
		t.Fatalf("the second hand-off joined the ended run: its store's root is %q, want its own number", secondRoot)
	}
	endBeltRun(t, agent, second)
	if !sawFirst {
		t.Fatal("the second run's copy did not hold the first run's work")
	}
}

// A RUN THAT ONLY READ LANDS NOTHING, SAYS SO, AND GIVES ITS COPY BACK.
func TestAReadOnlyBeltRunLandsNothingAndGivesItsCopyBack(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("read the files")
	double.real = true
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	before := beltRunRepoState(t, conversation)
	if err := agent.startKnownTaskRun(context.Background(), 72, "read only", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	close(double.release)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	if after := beltRunRepoState(t, conversation); after != before {
		t.Fatalf("a run that only read changed the person's folder:\n%s\n--- before ---\n%s", after, before)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("the run's copy %s was not given back: %v", workspace, err)
	}
}

// WORK THAT WILL NOT GO IN KEEPS ITS BRANCH AND SAYS SO. The person committed to
// the same file while the run worked; the run's work is committed on its own
// branch in their repository, their folder is exactly as they left it, and the
// run's page names the kept branch instead of saying the work is in the folder.
func TestABeltRunWhoseWorkConflictsKeepsItsBranchAndSaysSo(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("the change is made")
	double.real = true
	double.work = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "seed.txt"), []byte("the run's line\n"), 0o644); err != nil {
			t.Errorf("the run's own write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(conversation, "seed.txt"), []byte("the person's line\n"), 0o644); err != nil {
			t.Errorf("the person's write: %v", err)
		}
		if _, err := git(conversation, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-am", "the person's own change"); err != nil {
			t.Errorf("the person's commit: %v", err)
		}
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	if err := agent.startKnownTaskRun(context.Background(), 73, "change the seed", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	close(double.release)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	body, _ := os.ReadFile(filepath.Join(conversation, "seed.txt"))
	if string(body) != "the person's line\n" {
		t.Fatalf("the person's folder was written over or holds a conflict:\n%s", body)
	}
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, note := range store.Notes(store.RootID(), 0) {
		said += note.Body + "\n"
	}
	_ = store.Close()
	if strings.Contains(said, "its work is in ") || !strings.Contains(said, "its branch task/") || !strings.Contains(said, " was kept") {
		t.Fatalf("the run's page does not say the branch was kept:\n%s", said)
	}
	if out, _ := git(conversation, "branch", "--list", "task/*"); strings.TrimSpace(out) == "" {
		t.Fatal("the kept branch is not in the person's repository")
	}
	rows := agent.graph().runRows(73)
	if len(rows) == 0 || rows[0].Merge == mergeMerged || !strings.HasPrefix(rows[0].Branch, "task/") {
		t.Fatalf("the run's row says %+v, want the kept branch and never merged", rows)
	}
}

func beltRunCommittedRepo(t *testing.T) string {
	t.Helper()
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "add", "seed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "seed"); err != nil {
		t.Fatal(err)
	}
	return repo
}

// A later run keeps every earlier run readable. This fixture takes the same
// door as production: an ended store is present, then startKnownTaskRun archives
// it and seeds the next run. Rows remain oldest-run-first and an archived part's
// full page survives both the handoff and reopening the conversation.
func TestPlanReadsEveryBeltRunArchive(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "second result"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
	})
	chat := agent.graph().planChat()
	path := filepath.Join(dir, planStoreFilename)
	first, err := plandb.Open(path, "first", "1", "First run", "first brief", chat)
	if err != nil {
		t.Fatalf("open first run: %v", err)
	}
	if _, err := first.AddMany([]plandb.TaskSpec{{ID: "old-part", Title: "Old part", Description: "old work"}}); err != nil {
		t.Fatalf("add old part: %v", err)
	}
	if _, err := first.AddNote("old-part", "worker-old", "kept note"); err != nil {
		t.Fatalf("note old part: %v", err)
	}
	if err := first.AddSpend("old-part", "test/model", "work", 0.42, 1, 1); err != nil {
		t.Fatalf("spend old part: %v", err)
	}
	if _, err := first.Claim("old-part", "worker-old"); err != nil {
		t.Fatalf("claim old part: %v", err)
	}
	if _, err := first.Done("old-part", "worker-old", "kept result", nil, nil); err != nil {
		t.Fatalf("finish old part: %v", err)
	}
	if err := first.CompleteRoot("first result"); err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first run: %v", err)
	}
	writePlanTrajectory(t, dir, "old-part", `{"kind":"step","step":1,"command":"$ echo old","observation":"old"}`)

	double := newBeltRunDouble("second result")
	registerBeltRunEngine(t, double)
	if err := agent.startKnownTaskRun(context.Background(), 2, "Second run", "second brief", nil, taskStand{dir: agent.config.Workspace, mode: TaskModeInPlace}, ""); err != nil {
		t.Fatalf("start second run: %v", err)
	}
	<-double.entered
	close(double.release)
	beltRunWaitFor(t, "second run ending", func() bool { agent.beltMu.Lock(); defer agent.beltMu.Unlock(); return agent.beltRun == nil })

	assertReads := func(label string, got *Agent) {
		t.Helper()
		rows := got.PlanTasks()
		want := []string{"t-1", "t-old-part", "t-2"}
		if ids := rowIDs(rows); !reflect.DeepEqual(ids, want) {
			t.Fatalf("%s rows = %v, want %v", label, ids, want)
		}
		page, ok := got.PlanTaskPage("t-old-part")
		if !ok {
			t.Fatalf("%s cannot open archived part", label)
		}
		if page.Result != "kept result" || len(page.Notes) != 1 || page.Notes[0].Body != "kept note" || len(page.Steps) != 1 || page.Row.USD != 0.42 {
			t.Fatalf("%s archived page = %#v", label, page)
		}
	}
	assertReads("live conversation", agent)

	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = agent.config.Workspace
		config.Place = Place{Dir: dir}
	})
	defer reopened.Close()
	assertReads("reopened conversation", reopened)
}

// With no numbered sibling, aggregation is exactly the existing single-store
// read rather than a second rendering path.
func TestPlanReadsWithoutArchiveStayIdentical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tasks := store.Tasks(plandb.Filter{Chat: "chat-a"})
	wantRows := make([]PlanTaskRow, 0, len(tasks))
	for _, task := range tasks {
		row := planTaskRow(store, dir, task, planSpendByTask(path), store.LiveSteps())
		// The listing says where the run works, once per row, exactly as the
		// single-store read did before there was an archive to aggregate.
		row.Folder = agent.config.Workspace
		wantRows = append(wantRows, row)
		if task.ID == store.RootID() {
			applyPlanRootProgress(&wantRows[len(wantRows)-1], tasks, store.RootID())
		}
	}
	_ = store.Close()
	want, err := json.Marshal(wantRows)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(agent.PlanTasks())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("single-store rows changed:\ngot  %s\nwant %s", got, want)
	}
}

// A RUN'S REPORTED DOLLARS BELONG TO THE CONVERSATION THAT STARTED IT. These
// cases enter through StartTask under the bash belt and wait on the task lane,
// so they exercise the real door and have no timing sleeps.
func TestStartTaskBashBeltFoldsRunDollarsOnceOnEveryEnding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome string
		stop    bool
	}{
		{name: "finished", outcome: beltRunOutcomeDone},
		{name: "incomplete", outcome: "ran and did not finish"},
		{name: "stopped by the person", outcome: "ran and did not finish", stop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			double := newBeltRunDouble("the run ended")
			double.summary.Outcome = tc.outcome
			double.summary.USD = 0.42
			double.honoursStop = tc.stop
			registerBeltRunEngine(t, double)

			dir := t.TempDir()
			agent, _ := newTestAgent(t, beltRunCompleter{text: "the run ended"}, func(config *Config) {
				config.Workspace = newTestRepo(t)
				config.Place = Place{Dir: dir}
				config.AskConsent = false
			})
			updates, stopUpdates := agent.WatchTaskUpdates()
			defer stopUpdates()

			id, _, _, err := agent.StartTask(context.Background(), "account for this run", false)
			if err != nil {
				t.Fatalf("StartTask: %v", err)
			}
			<-double.entered
			if tc.stop {
				if _, err := agent.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10)); err != nil {
					t.Fatalf("stop run: %v", err)
				}
			} else {
				close(double.release)
			}
			lastTaskUpdate(t, updates)

			if got := agent.Usage().CostUSD; got != 0.42 {
				t.Fatalf("conversation cost after %s run = %v, want the run's $0.42 exactly once", tc.name, got)
			}
		})
	}
}

// THE NEXT TURN READS THE SAME TOTAL. Crossing the conversation limit in a run
// refuses the following turn with the shipped sentence, byte for byte.
func TestStartTaskBashBeltRunSpendRefusesNextTurnWithShippedLimitSentence(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run ended")
	double.summary.USD = 2.05
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run ended"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.SpendRailUSD = 2
	})
	updates, stopUpdates := agent.WatchTaskUpdates()
	defer stopUpdates()

	if _, _, _, err := agent.StartTask(context.Background(), "spend past the conversation limit", false); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered

	// The run is still live, but its reported dollars already belong to this
	// conversation. The next turn must read that total before the run lands.
	refusal := collect(t, mustSubmit(t, agent, "this turn must not start"))
	if len(refusal) != 1 || refusal[0].Err == nil {
		t.Fatalf("the turn after overspending the conversation limit = %v, want one refusal", kinds(refusal))
	}
	const want = "conversation limit reached · $2.05 spent of $2 · /budget changes it"
	if got := refusal[0].Err.Error(); got != want {
		t.Fatalf("next-turn refusal = %q, want unchanged shipped sentence %q", got, want)
	}
	close(double.release)
	lastTaskUpdate(t, updates)
}

func TestRunCostLeft(t *testing.T) {
	tests := []struct {
		name         string
		limit, spent float64
		want         float64
	}{
		{"no limit", 0, 0, 0},
		{"no limit, something spent", 0, 3, 0},
		{"nothing spent", 7, 0, 7},
		{"part spent", 7, 2.5, 4.5},
		{"all spent", 7, 7, math.SmallestNonzeroFloat64},
		{"overspent", 7, 12, math.SmallestNonzeroFloat64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runCostLeft(test.limit, test.spent); got != test.want {
				t.Fatalf("runCostLeft(%v, %v) = %v, want %v", test.limit, test.spent, got, test.want)
			}
		})
	}
}

func TestStartTaskBashBeltPassesTheDollarLimitLeftToTheRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.SpendRailUSD = 9
		config.Interactive = true
		config.Budget = Budget{USD: 7}
	})
	agent.usage.CostUSD = 2.5

	if _, _, _, err := agent.StartTask(context.Background(), "finish within the dollars left", false); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	got := double.spec.CostUSD
	double.mu.Unlock()
	if got != 4.5 {
		t.Fatalf("run cost limit = %v, want the $4.50 left of the smaller $7 launch limit", got)
	}
	endBeltRun(t, agent, double)
}
