package session

// A run's wall time is one pair of instants (task_run_clock.go): the hand-off,
// and the instant the program it handed its task to was gone — or, for a run
// no program worked, the instant its engine answered. These tests pin the pair
// on every surface this package feeds: the row that settles the run and the
// checkpoint it is kept in, the task page's row, and a run nothing is driving.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// runSpanWord is the page's own spelling of a finished span, restated here
// because the session cannot import the surface; these are the values the page
// draws for the same durations (internal/tui3's countUpWord).
func TestRunSpanWordSpellsASpanTheWayThePageDoes(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{999 * time.Millisecond, ""},
		{5 * time.Second, "5s"},
		{22*time.Minute + 51*time.Second, "22m 51s"},
		{5 * time.Minute, "5m"},
		{67*time.Minute + 34*time.Second, "1h 7m"},
		{2 * time.Hour, "2h"},
	} {
		if got := runSpanWord(tc.d); got != tc.want {
			t.Errorf("runSpanWord(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// A PROGRAM'S RUN IS TIMED FROM THE HAND-OFF TO THE PROGRAM'S EXIT, on its row,
// in the checkpoint a reopened conversation reads, and on its page. The row used
// to settle at the instant it was published — after the landing and a summary
// refresh — and carry no elapsed time at all, and the page counted from the
// store's seeding, before the copy was cut; the same run read three different
// spans on three surfaces.
func TestAProgramsRunIsTimedFromTheHandOffToTheProgramsExit(t *testing.T) {
	double := newBeltRunDouble("submitted and verified")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "submitted and verified"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	handoff := time.Date(2026, time.September, 24, 1, 14, 7, 0, time.UTC)
	clock := &fakeClock{at: handoff}
	agent.taskNow = clock.now

	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "add two files to the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	key := strconv.FormatUint(id, 10)

	// WHILE IT RUNS the page counts from the hand-off, not from the store's
	// seeding, which happened before the copy was cut.
	page, ok := agent.PlanTaskPage(key)
	if !ok || page.Row.Status != string(plandb.StatusRunning) || !page.Row.Started.Equal(handoff) || !page.Row.Ended.IsZero() {
		t.Fatalf("the live page's row = %+v (%v), want running from the hand-off %v", page.Row, ok, handoff)
	}

	// The program exits 22m 51s after the hand-off, and the run's landing and
	// settling happen a good while later on the conversation's clock.
	exited := handoff.Add(22*time.Minute + 51*time.Second)
	record := delegate.ProgramRecord{Name: "fake", StartedAt: handoff.Add(2 * time.Second), EndedAt: exited}
	if err := delegate.WriteProgram(plandb.TaskDir(filepath.Dir(spec.Store.Path()), spec.Store.RootID()), record); err != nil {
		t.Fatal(err)
	}
	clock.advance(30 * time.Minute)
	endBeltRun(t, agent, double)

	kept := agent.graph().runRows(id)
	if len(kept) != 1 || !kept[0].StartedAt.Equal(handoff) || !kept[0].EndedAt.Equal(exited) || kept[0].Elapsed != 22*time.Minute+51*time.Second {
		t.Fatalf("the settled row = %+v, want %v → %v and 22m51s", kept, handoff, exited)
	}
	page, ok = agent.PlanTaskPage(key)
	if !ok || !page.Row.Started.Equal(handoff) || !page.Row.Ended.Equal(exited) {
		t.Fatalf("the ended page's row = %v → %v (%v), want the row's own pair %v → %v", page.Row.Started, page.Row.Ended, ok, handoff, exited)
	}

	// AND A CONVERSATION REOPENED TOMORROW READS THE SAME PAIR AND THE SAME SPAN.
	journal := agent.file.journalPath()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := newAgent(Config{Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	back := reopened.graph().runRows(id)
	if len(back) != 1 || !back[0].EndedAt.Equal(exited) || back[0].Elapsed != 22*time.Minute+51*time.Second {
		t.Fatalf("the reopened row = %+v, want it ended at the program's exit with its span", back)
	}
}

// slowLanding is the run engine whose landing takes a minute of the
// conversation's clock, which is what the squash, the merge and the summary
// refresh of a real landing take in the run's time.
type slowLanding struct {
	*beltRunDouble
	clock *fakeClock
}

func (s slowLanding) Land(ctx context.Context, store *plandb.Store, workspace, root string) (RunLanding, error) {
	s.clock.advance(time.Minute)
	return s.beltRunDouble.Land(ctx, store, workspace, root)
}

// A RUN NO PROGRAM WORKED ENDS WHERE ITS ENGINE ANSWERED, never where its row
// settled: the landing is not the run's work, and a row stamped after it was a
// run that seemed to go on for as long as its homecoming took.
func TestARunWithNoProgramEndsWhereItsEngineAnswered(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run fixed the nil map")
	start := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{at: start}
	registerBeltRunEngine(t, slowLanding{beltRunDouble: double, clock: clock})
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run fixed the nil map"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
	})
	agent.taskNow = clock.now
	id, _, _, err := agent.StartTask(context.Background(), "fix the nil map crash", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	clock.advance(5 * time.Minute)
	endBeltRun(t, agent, double)
	if double.lands() != 1 {
		t.Fatalf("the run landed %d times, want once", double.lands())
	}
	kept := agent.graph().runRows(id)
	if len(kept) != 1 || !kept[0].EndedAt.Equal(start.Add(5*time.Minute)) || kept[0].Elapsed != 5*time.Minute {
		t.Fatalf("the settled row = %+v, want it ended where the engine answered, five minutes in", kept)
	}
}

// A RUN ROW KEEPS HOW IT ENDED AND WHERE ITS WORK IS ACROSS A REOPEN. The saved
// file dropped the ending, the branch and the merge: a program that judged its
// own work unfinished came back reading `a fault: …`, a run a limit ended lost
// which limit, and a kept branch was named nowhere.
func TestARunRowKeepsItsEndingBranchAndSpanAcrossAReopen(t *testing.T) {
	began := time.Date(2026, time.September, 24, 1, 14, 7, 0, time.UTC)
	rows := []TaskNotice{
		{
			ID: 3, Title: "Implement happy-dom teardown", State: TaskFailed, Ending: TaskEndingProgram,
			Report: "senior-dev did not finish: its tests fail\nsubmitted a change the tests do not pass",
			Result: "submitted a change the tests do not pass", Branch: "task/happy-dom-1", Merge: mergeKept,
			Changed: []string{"src/window.ts"}, StartedAt: began, EndedAt: began.Add(29*time.Minute + 8*time.Second),
		},
		{
			ID: 4, Title: "Port the parser", State: TaskFailed, Ending: TaskEndingTimeLimit,
			Report: "a limit you set stopped it", Branch: "task/port-the-parser-2", Merge: mergeKept,
			StartedAt: began, EndedAt: began.Add(time.Hour),
		},
		{
			ID: 5, Title: "Fix the nil map", State: TaskFailed, Ending: TaskEndingCostLimit,
			Report: "a limit you set stopped it", Result: "half the guard", Branch: "task/fix-the-nil-map-3", Merge: mergeKept,
			Changed: []string{"a.go", "a_test.go"}, StartedAt: began, EndedAt: began.Add(4*time.Minute + 23*time.Second),
		},
	}
	dir, workspace := t.TempDir(), t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
	})
	g := agent.graph()
	for i := range rows {
		// Every row takes its number off the graph's one counter, the way a
		// hand-off's row does, so the checkpoint's id counter covers it.
		rows[i].ID = g.reserve()
		agent.publishRunRow(g, rows[i])
	}
	journal := agent.file.journalPath()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := newAgent(Config{Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	for _, row := range rows {
		back := reopened.graph().runRows(row.ID)
		if len(back) != 1 {
			t.Fatalf("row %d came back as %+v", row.ID, back)
		}
		got := back[0]
		if got.Ending != row.Ending || got.Branch != row.Branch || got.Merge != row.Merge || got.Result != row.Result ||
			!reflect.DeepEqual(got.Changed, row.Changed) {
			t.Fatalf("row %d came back as %+v, want its ending, branch, merge, result and files kept", row.ID, got)
		}
		if want := runSpan(row.StartedAt, row.EndedAt); got.Elapsed != want {
			t.Fatalf("row %d came back with span %v, want %v", row.ID, got.Elapsed, want)
		}
		if reason := TaskReasonOf(got.Ending, got.Report); reason != TaskReasonOf(row.Ending, row.Report) || strings.HasPrefix(reason, "a fault") {
			t.Fatalf("row %d reads %q after the reopen, want %q", row.ID, reason, TaskReasonOf(row.Ending, row.Report))
		}
	}
	// The elapsed time is on the file itself, where the record says it is.
	var document taskDocument
	data, err := os.ReadFile(taskCheckpointPath(journal))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, run := range document.Runs {
		if run.ElapsedMS <= 0 {
			t.Fatalf("run %d was saved with no elapsed_ms: %+v", run.ID, run)
		}
	}
}

// A PROGRAM'S RUN A LIMIT ENDED READS ENDED BEFORE ITS WORK LANDS. The engine
// leaves such a store open, and the run's task was ended only after the squash
// and the commit, so the page went on reading `running` over a program that
// had exited — for the two limit endings alone.
func TestALimitEndedProgramRunIsEndedBeforeItsWorkLands(t *testing.T) {
	double := newBeltRunDouble("")
	double.leaveOpen = true
	double.summary = RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost}
	double.work = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "one.txt"), []byte("one\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)

	store := beltRunStoreAt(t, filepath.Dir(spec.Store.Path()))
	defer store.Close()
	root := store.Task(store.RootID())
	notes := store.Notes(store.RootID(), 0)
	if root == nil || root.Status != plandb.StatusFailed || len(notes) == 0 {
		t.Fatalf("the run's task = %+v with notes %+v, want it failed with the landing noted", root, notes)
	}
	if !strings.HasPrefix(notes[0].Body, "landed on ") {
		t.Fatalf("the first note on the run = %q, want the landing's own", notes[0].Body)
	}
	if !root.CompletedAt.Before(notes[0].At) {
		t.Fatalf("the run's task ended at %v and its work landed at %v: it read running while its work landed", root.CompletedAt, notes[0].At)
	}
}

// A PROGRAM'S RUN NOTHING HERE IS DRIVING DOES NOT READ RUNNING. codeaf closed
// while the program ran, nothing wrote the store's ending, and the page read
// `running` with a clock that never stopped and a stage it was no longer in.
// It now reads failed — incomplete on every surface — ended at its last sign of
// life, with nothing live.
func TestAProgramsRunNothingIsDrivingEndsAtItsLastActivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	store, err := plandb.Open(path, "the run", "3", "Implement happy-dom teardown", "the brief", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLive("3", 5, "senior-dev: implement · running"); err != nil {
		t.Fatal(err)
	}
	root := store.Task("3")
	_ = store.Close()
	folder := plandb.TaskDir(dir, "3")
	started := root.UpdatedAt.Add(-29 * time.Minute)
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "senior-dev", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	if err := delegate.AppendTurn(folder, delegate.Turn{Seq: 1, Started: started, Model: "vendor/model"}); err != nil {
		t.Fatal(err)
	}
	last := root.UpdatedAt.Add(90 * time.Second)
	if err := os.Chtimes(filepath.Join(folder, delegate.ConversationFile), last, last); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	page, ok := agent.PlanTaskPage("3")
	if !ok {
		t.Fatal("the run's task answered no page")
	}
	if page.Row.Status != string(plandb.StatusFailed) || !page.Live.Empty() || page.Row.Stage != "" {
		t.Fatalf("the page's row = %+v, want it ended with nothing live", page.Row)
	}
	if !page.Row.Started.Equal(started) || !page.Row.Ended.Equal(last) {
		t.Fatalf("the page's row runs %v → %v, want the program's start %v to its last activity %v", page.Row.Started, page.Row.Ended, started, last)
	}
	if row := planRowFor(agent.PlanTasks(), planStoreID("3")); row == nil || row.Status != string(plandb.StatusFailed) || !row.Ended.Equal(last) {
		t.Fatalf("the listing's row = %+v, want the page's reading", row)
	}
}
