package session

// The chat's task door on the run engine (task_run_belt.go): a `/task` under the
// bash belt seeds the conversation's store, starts the engine, and answers the
// run's id at once; a second `/task` joins the live run; the ending lands. The
// engine here is a double that stands in for internal/run — which this package
// cannot import without a cycle — and it drives the store through the same
// plandb door the real one does, on the completer the door handed it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	mu      sync.Mutex
	summary RunSummary
	landing RunLanding
	entered chan struct{}
	release chan struct{}
	ran     bool
	// ctx is the context the engine was started under, kept so a test can ask
	// whether the run outlived the turn that launched it.
	ctx context.Context
}

func newBeltRunDouble(result string) *beltRunDouble {
	return &beltRunDouble{
		summary: RunSummary{Outcome: beltRunOutcomeDone, Result: result, Nodes: 1, Steps: 2},
		landing: RunLanding{Branch: "task/fix-the-nil-map-crash", Changed: []string{"internal/session/agent.go"}},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (d *beltRunDouble) Start(ctx context.Context, spec RunSpec) RunSummary {
	d.mu.Lock()
	d.ran = true
	d.ctx = ctx
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
	close(d.entered)
	<-d.release
	if spec.Store != nil {
		_ = spec.Store.CompleteRoot(d.summary.Result)
	}
	return d.summary
}

func (d *beltRunDouble) Land(context.Context, *plandb.Store, string, string) (RunLanding, error) {
	return d.landing, nil
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
	if !anyNoteCarries(beltRunNotes(t, dir, rootID), "landed on task/fix-the-nil-map-crash") {
		t.Fatalf("no note on the root carries the branch: %v", beltRunNotes(t, dir, rootID))
	}
	wantDigest := beltRunOutcomeNote(nil, "", double.summary, double.landing)
	if !strings.Contains(wantDigest, "done") || !strings.Contains(wantDigest, "the run fixed the nil map") ||
		!strings.Contains(wantDigest, "landed on task/fix-the-nil-map-crash") {
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
	// the run's row settled too, on the surface's own lane
	if row := planRowFor(agent.PlanTasks(), planStoreID(rootID)); row == nil || row.Status != string(plandb.StatusDone) {
		t.Fatalf("the plan does not show the run done: %+v", agent.PlanTasks())
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
	})
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
	})
	want := "done · landed on task/landing-digest: 1 file"
	if got != want {
		t.Fatalf("landing digest = %q, want byte-for-byte legacy digest %q", got, want)
	}
}

// landingRunDouble ends synchronously so the test can observe the exact order:
// the summary refresh must have stored its sentence before the outcome note is
// composed.
type landingRunDouble struct {
	summary RunSummary
	landing RunLanding
}

func (d landingRunDouble) Start(context.Context, RunSpec) RunSummary { return d.summary }
func (d landingRunDouble) Land(context.Context, *plandb.Store, string, string) (RunLanding, error) {
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
