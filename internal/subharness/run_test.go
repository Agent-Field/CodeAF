package subharness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// script is a stand-in for the machinery a real run hands its nodes to. It
// records what it was asked to run, which is the only thing these tests are
// about: the walk, not the work.
type script struct {
	seen  []string
	calls map[string]int
	// next is a branch's choice, keyed by node id.
	next map[string]string
	// doneAfter is the round on which a loop.until node's condition holds. A
	// node absent from the map never finishes on its own.
	doneAfter map[string]int
	fail      map[string]error
	pause     time.Duration
}

func (s *script) exec(ctx context.Context, node Node) (Result, error) {
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.seen = append(s.seen, node.Id)
	s.calls[node.Id]++
	if s.pause > 0 {
		time.Sleep(s.pause)
	}
	if err := s.fail[node.Id]; err != nil {
		return Result{}, err
	}
	round, wanted := s.doneAfter[node.Id]
	return Result{
		Out:  node.Id + " ran",
		Next: s.next[node.Id],
		Done: wanted && s.calls[node.Id] >= round,
	}, nil
}

func trailIds(trace Trace) []string {
	ids := make([]string, len(trace.Trail))
	for index, step := range trace.Trail {
		ids[index] = step.Id
	}
	return ids
}

func edgeWords(trace Trace) []string {
	words := make([]string, len(trace.Edges))
	for index, edge := range trace.Edges {
		words[index] = edge.String()
	}
	return words
}

func wantWalk(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("walked [%s], want [%s]", strings.Join(got, " "), strings.Join(want, " "))
	}
}

// branching is one decision and a merge: the shape every branch test needs.
func branching() Harness {
	return Harness{
		Id: Id{Name: "branching", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerIdle}},
				{Id: "pick", Kind: KindBranch, Fields: Fields{"when": "the tests passed"}},
				{Id: "left", Kind: KindAgentLoop, Fields: Fields{"brief": "write the note"}},
				{Id: "right", Kind: KindAgentLoop, Fields: Fields{"brief": "fix the test"}},
				{Id: "merge", Kind: KindVerify, Fields: Fields{"check": "the branch did its work"}},
			},
			Edges: []Edge{
				{"start", "pick"}, {"pick", "left"}, {"pick", "right"},
				{"left", "merge"}, {"right", "merge"},
			},
		},
		Verify: Verify{Ladder: VerifyInvariants},
		Dyn:    Dyn{Ladder: DynBranch, Cap: 3},
	}
}

// A run's first job is to say what happened, in order, condensed.
func TestARunEmitsATrailOfWhatExecuted(t *testing.T) {
	runner := &script{pause: 2 * time.Millisecond}
	trace, err := Run(context.Background(), linear(), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "work", "check")
	wantWalk(t, runner.seen, "start", "work", "check")

	for index, step := range trace.Trail {
		if step.Step != index+1 {
			t.Fatalf("step %d is numbered %d", index, step.Step)
		}
		if step.Err != "" {
			t.Fatalf("step %s carries an error: %s", step.Id, step.Err)
		}
		if step.Elapsed <= 0 {
			t.Fatalf("step %s took no time at all", step.Id)
		}
	}
	if trace.Trail[1].Kind != KindAgentLoop || trace.Trail[1].Out != "work ran" {
		t.Fatalf("the trail lost the node: %+v", trace.Trail[1])
	}
	if trace.Id != linear().Id || trace.Started.IsZero() || trace.Elapsed <= 0 {
		t.Fatalf("the trace does not say who ran or for how long: %+v", trace.Id)
	}
	// Nothing was decided, so nothing was spent, and every edge was taken.
	if trace.Spent != 0 {
		t.Fatalf("a fixed harness spent %d of a budget it does not have", trace.Spent)
	}
	wantWalk(t, edgeWords(trace), "start->work", "work->check")
}

// The trace is a dag, not a list: the edges it carries are the edges the run
// actually took, so a branch that went right leaves no left edge behind.
func TestABranchTakesOneSuccessorAndTheOtherSideNeverRuns(t *testing.T) {
	runner := &script{next: map[string]string{"pick": "right"}}
	trace, err := Run(context.Background(), branching(), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "right", "merge")
	wantWalk(t, edgeWords(trace), "start->pick", "pick->right", "right->merge")
}

// A branch that says nothing takes the first successor in program order, which
// is what makes a run reproducible when the executor has no opinion.
func TestABranchWithNoChoiceTakesTheFirstSuccessor(t *testing.T) {
	runner := &script{}
	trace, err := Run(context.Background(), branching(), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "left", "merge")
}

func TestABranchToANodeItDoesNotLeadToEndsTheRun(t *testing.T) {
	runner := &script{next: map[string]string{"pick": "merge"}}
	trace, err := Run(context.Background(), branching(), runner.exec)
	if err == nil || !strings.Contains(err.Error(), "does not lead to") {
		t.Fatalf("a branch to a node it does not lead to was allowed: %v", err)
	}
	if trace.Err == "" || trace.Trail[len(trace.Trail)-1].Id != "pick" {
		t.Fatalf("the trace does not record where the run died: %+v", trace)
	}
}

// join is the fan-in fixture: two lanes, and a join whose mode decides whether
// a missing lane is a wait or a shrug.
func joining(mode string) Harness {
	return Harness{
		Id: Id{Name: "joining", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerIdle}},
				{Id: "pick", Kind: KindBranch, Fields: Fields{"when": "which lane"}},
				{Id: "lane-a", Kind: KindAgentLoop, Fields: Fields{"brief": "one half"}},
				{Id: "lane-b", Kind: KindAgentLoop, Fields: Fields{"brief": "the other half"}},
				{Id: "join", Kind: KindParallelJoin, Fields: Fields{"mode": mode}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"check": "both halves are here"}},
			},
			Edges: []Edge{
				{"start", "pick"}, {"pick", "lane-a"}, {"pick", "lane-b"},
				{"lane-a", "join"}, {"lane-b", "join"}, {"join", "check"},
			},
		},
		Verify: Verify{Ladder: VerifyInvariants},
		Dyn:    Dyn{Ladder: DynWidth, Cap: 4},
	}
}

// `all` is a barrier by definition. A branch upstream means one lane will
// never arrive, and everything downstream of the barrier stays unrun rather
// than running on half its inputs.
func TestAJoinInAllModeWaitsForEveryLaneAndIsSkippedIfOneNeverComes(t *testing.T) {
	runner := &script{next: map[string]string{"pick": "lane-a"}}
	trace, err := Run(context.Background(), joining(JoinAll), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "lane-a")

	// The same shape in `any` mode fires on the lane that did arrive.
	runner = &script{next: map[string]string{"pick": "lane-a"}}
	trace, err = Run(context.Background(), joining(JoinAny), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "lane-a", "join", "check")

	// And an `all` join whose lanes both ran is not a barrier against
	// anything: it is only a join.
	whole := joining(JoinAll)
	whole.Program.Nodes[1] = Node{Id: "pick", Kind: KindAgentLoop, Fields: Fields{"brief": "do both"}}
	runner = &script{}
	trace, err = Run(context.Background(), whole, runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "lane-a", "lane-b", "join", "check")
}

// A join with no mode is a barrier, because a join that fires on a partial set
// is a choice somebody has to make out loud.
func TestAJoinWithNoModeIsABarrier(t *testing.T) {
	h := joining(JoinAll)
	delete(h.Program.Nodes[4].Fields, "mode")
	runner := &script{next: map[string]string{"pick": "lane-a"}}
	trace, err := Run(context.Background(), h, runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "pick", "lane-a")
}

func looping(budget, maxRounds int) Harness {
	return Harness{
		Id: Id{Name: "looping", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerIdle}},
				{Id: "tries", Kind: KindLoopUntil, Fields: Fields{
					"until": "the suite is green", "max_rounds": strconv.Itoa(maxRounds),
				}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"check": "it is green"}},
			},
			Edges: []Edge{{"start", "tries"}, {"tries", "check"}},
		},
		Verify: Verify{Ladder: VerifyLoop},
		Dyn:    Dyn{Ladder: DynBranch, Cap: budget},
	}
}

// Every round is its own step in the trail, because three attempts at one node
// is three things that happened, not one.
func TestALoopStopsWhenItsConditionHoldsAndEachRoundIsAStep(t *testing.T) {
	runner := &script{doneAfter: map[string]int{"tries": 2}}
	trace, err := Run(context.Background(), looping(4, 5), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "tries", "tries", "check")
	if trace.Spent != 1 {
		t.Fatalf("a second round spent %d of the budget, want 1", trace.Spent)
	}
	if trace.Trail[1].Step != 2 || trace.Trail[2].Step != 3 {
		t.Fatalf("rounds are not numbered apart: %+v", trace.Trail)
	}
}

// The budget is the whole point of writing the dynamism rung down. A run that
// spends its last unit stops deciding and finishes on the shape it has — it
// does not fail, and it does not quietly keep going.
func TestALoopIsBoundedByTheDynamismBudgetBeforeItsOwnRounds(t *testing.T) {
	runner := &script{}
	trace, err := Run(context.Background(), looping(2, MaxRounds), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "tries", "tries", "tries", "check")
	if trace.Spent != 2 {
		t.Fatalf("the run spent %d of a cap of 2", trace.Spent)
	}

	// And with room to spare, the node's own max_rounds is what binds.
	runner = &script{}
	trace, err = Run(context.Background(), looping(MaxDynCap, 3), runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "tries", "tries", "tries", "check")
	if trace.Spent != 2 {
		t.Fatalf("three rounds spent %d, want 2", trace.Spent)
	}
}

// A loop that says nothing gets DefaultRounds, not one round and not forever.
func TestALoopWithNoRoundsDeclaredGetsTheDefault(t *testing.T) {
	h := looping(MaxDynCap, 0)
	delete(h.Program.Nodes[1].Fields, "max_rounds")
	runner := &script{}
	trace, err := Run(context.Background(), h, runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls["tries"] != DefaultRounds {
		t.Fatalf("a loop with no rounds ran %d times, want %d", runner.calls["tries"], DefaultRounds)
	}
	if trace.Spent != DefaultRounds-1 {
		t.Fatalf("the default rounds spent %d", trace.Spent)
	}
}

// A run that died halfway is exactly the run worth reading, so the trace comes
// back either way, with the failure in the step that failed.
func TestAFailingNodeEndsTheRunAndIsRecordedInIt(t *testing.T) {
	boom := errors.New("the model refused")
	runner := &script{fail: map[string]error{"work": boom}}
	trace, err := Run(context.Background(), linear(), runner.exec)
	if !errors.Is(err, boom) {
		t.Fatalf("the failure did not come back: %v", err)
	}
	wantWalk(t, trailIds(trace), "start", "work")
	last := trace.Trail[len(trace.Trail)-1]
	if !strings.Contains(last.Err, "the model refused") || last.Out != "" {
		t.Fatalf("the failing step reads as %+v", last)
	}
	if !strings.Contains(trace.Err, "the model refused") {
		t.Fatalf("the trace does not carry the failure: %q", trace.Err)
	}
	if trace.Elapsed <= 0 {
		t.Fatal("a failed run took no time at all")
	}
	// The edges taken up to the failure are still the run's shape.
	wantWalk(t, edgeWords(trace), "start->work")
}

func TestACancelledContextStopsTheWalk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &script{}
	trace, err := Run(ctx, linear(), runner.exec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	if len(runner.seen) != 0 {
		t.Fatalf("a cancelled run still executed %v", runner.seen)
	}
	if trace.Err == "" || len(trace.Trail) != 1 {
		t.Fatalf("the cancellation was not recorded: %+v", trace)
	}
}

func TestRunRefusesAHarnessItCannotTrust(t *testing.T) {
	runner := &script{}
	cyclic := linear()
	cyclic.Program.Edges = append(cyclic.Program.Edges, Edge{"check", "work"})
	if _, err := Run(context.Background(), cyclic, runner.exec); err == nil {
		t.Fatal("a cyclic program ran")
	}
	if _, err := Run(context.Background(), linear(), nil); err == nil {
		t.Fatal("a run with no executor was allowed")
	}
	if len(runner.seen) != 0 {
		t.Fatalf("a refused run executed %v", runner.seen)
	}
}

// The store's run is the whole loop: a version pointer in, a trace on disk,
// and the path to it back.
func TestRunningThroughTheStoreSavesTheTraceBesideTheHarness(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	runner := &script{}
	trace, path, err := store.Run(context.Background(), "linear", 0, runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join("linear", RunDir)) {
		t.Fatalf("the trace landed at %s", path)
	}
	back, err := store.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(back), "start", "work", "check")
	if back.Id.Version != 1 || back.Id != trace.Id {
		t.Fatalf("the saved trace does not name the version it ran: %+v", back.Id)
	}

	// A failed run is evidence too, and it is kept.
	failing := &script{fail: map[string]error{"work": errors.New("no")}}
	_, failedPath, err := store.Run(context.Background(), "linear", 1, failing.exec)
	if err == nil {
		t.Fatal("a failing run came back clean")
	}
	if failedPath == "" {
		t.Fatal("a failed run left no trace behind")
	}
	if kept, err := store.LoadRun(failedPath); err != nil || kept.Err == "" {
		t.Fatalf("the failed trace read back as %+v (%v)", kept, err)
	}
	if runs, err := store.Runs("linear"); err != nil || len(runs) != 2 {
		t.Fatalf("runs are %v (%v)", runs, err)
	}
}

// A draft is tried before it is saved, which means running a page that the
// registry has never seen.
func TestADraftPageRunsStraightOffDisk(t *testing.T) {
	data, err := Encode(linear())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &script{}
	trace, err := RunFile(context.Background(), path, runner.exec)
	if err != nil {
		t.Fatal(err)
	}
	wantWalk(t, trailIds(trace), "start", "work", "check")

	if _, err := RunFile(context.Background(), filepath.Join(t.TempDir(), "absent.json"), runner.exec); err == nil {
		t.Fatal("running a page that is not there came back clean")
	}
}

// A RUN IS WATCHABLE. Every entry the trace keeps is shown to the watcher as it
// lands, in the same order and with the same contents — a live row and the card
// read back afterwards are two views of one list, never two lists.
func TestRunWatchedShowsEveryStepAsItLands(t *testing.T) {
	runner := &script{}
	var seen []Trail
	trace, err := RunWatched(context.Background(), linear(), runner.exec, func(step Trail) {
		seen = append(seen, step)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || len(seen) != len(trace.Trail) {
		t.Fatalf("watched %d steps, the trail kept %d", len(seen), len(trace.Trail))
	}
	for i, step := range seen {
		if step != trace.Trail[i] {
			t.Fatalf("step %d was watched as %+v and kept as %+v", i, step, trace.Trail[i])
		}
	}
}

// THE STEP A RUN DIES ON IS WATCHED TOO, which is the one worth seeing: the
// report only arrives afterwards, and a failure the live rows never mentioned
// would be a run that went quiet and then said it was broken.
func TestRunWatchedShowsTheFailingStep(t *testing.T) {
	runner := &script{fail: map[string]error{"check": errors.New("the diff did not apply")}}
	var seen []Trail
	trace, err := RunWatched(context.Background(), linear(), runner.exec, func(step Trail) {
		seen = append(seen, step)
	})
	if err == nil {
		t.Fatal("a failing node did not end the run")
	}
	if len(seen) != len(trace.Trail) {
		t.Fatalf("watched %d steps, the trail kept %d", len(seen), len(trace.Trail))
	}
	last := seen[len(seen)-1]
	if last.Id != "check" || last.Err == "" {
		t.Fatalf("the failing step was watched as %+v", last)
	}
}
