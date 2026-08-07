package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// fakeCraftRepo stands in for the craft repository. Tests construct Workflow
// values directly: what the runtime does with a workflow is this package's
// business, and how a file becomes one is not.
type fakeCraftRepo struct {
	workflow *craft.Workflow
}

func (f *fakeCraftRepo) Load(name string) (*craft.Workflow, error) {
	if f.workflow == nil || f.workflow.Name != name {
		return nil, nil
	}
	return f.workflow, nil
}

func startCraftRun(t *testing.T, graph *store.Store, workflow *craft.Workflow) (*CraftRunner, CraftRun) {
	t.Helper()
	runner := NewCraftRunner(graph, &fakeCraftRepo{workflow: workflow}, "/home/craft")
	run, err := runner.RunCraft(workflow.Name, map[string]string{"topic": "quantum error correction"},
		"craft", "make me a deck on quantum error correction")
	if err != nil {
		t.Fatalf("run craft: %v", err)
	}
	return runner, run
}

// settleCraftNode lands one node the way the runner does: the result is read
// while the node is still open, then the node completes.
func settleCraftNode(t *testing.T, graph *store.Store, runner *CraftRunner, id, result string) CraftAdvance {
	t.Helper()
	return settleCraftNodeSpending(t, graph, runner, id, result, 0)
}

// settleCraftNodeSpending lands one node that cost something the usage table
// does not know about yet — the exact shape of a landing leaf.
func settleCraftNodeSpending(t *testing.T, graph *store.Store, runner *CraftRunner, id, result string, landing float64) CraftAdvance {
	t.Helper()
	node, ok, err := graph.Node(id)
	if err != nil || !ok {
		t.Fatalf("read %s: found=%t err=%v", id, ok, err)
	}
	advance, err := runner.Settle(node, result, landing)
	if err != nil {
		t.Fatalf("settle %s: %v", id, err)
	}
	completeCraftNode(t, graph, id, result)
	if landing > 0 {
		if err := graph.RecordUsage(store.NodeUsage{NodeID: id, Cost: landing}); err != nil {
			t.Fatalf("record usage for %s: %v", id, err)
		}
	}
	return advance
}

func completeCraftNode(t *testing.T, graph *store.Store, id, result string) {
	t.Helper()
	claim, won, err := graph.Claim(id, "craft-test")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", id, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Complete(claim, result); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func craftNodeIDs(t *testing.T, graph *store.Store, prefix string) []string {
	t.Helper()
	nodes, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, node := range nodes {
		if strings.HasPrefix(node.ID, prefix) {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func craftMessages(t *testing.T, graph *store.Store) string {
	t.Helper()
	messages, err := graph.Messages("craft", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return strings.Join(bodies, "\n---\n")
}

const craftResearchResult = "I found four sections worth a slide.\n\nITEMS:\nsurface codes\nmagic states\ndecoders\nfault tolerance overhead\n"

// The fan-out is not a loop: the landed list becomes real sibling leaves, each
// with its own assignment, and everything that was waiting on the fan-out step
// now waits on every one of them.
func TestCraftFanOutUnrollsIntoRealSiblings(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	if !strings.Contains(run.Receipt, "using your presentation craft v abc1234 — 4 steps") {
		t.Fatalf("compile receipt = %q", run.Receipt)
	}

	settleCraftNode(t, graph, runner, run.Prefix+"~research", "the list is above")
	advance := settleCraftNode(t, graph, runner, run.Prefix+"~sections", craftResearchResult)
	if advance.Unrolled != 3 {
		t.Fatalf("unrolled %d items, want the fan cap of 3", advance.Unrolled)
	}

	for index, item := range []string{"surface codes", "magic states", "decoders"} {
		id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, index+1)
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("item %s: found=%t err=%v", id, ok, err)
		}
		if !strings.Contains(node.Brief, "Write the plain slide for "+item+".") {
			t.Fatalf("item %s brief did not fill {{item}}:\n%s", id, node.Brief)
		}
		if !strings.Contains(node.Brief, "The deckwright skill is on PATH") {
			t.Fatalf("item %s lost its skill sentence:\n%s", id, node.Brief)
		}
		if node.Provenance.Craft != CraftRef(workflow) {
			t.Fatalf("item %s craft provenance = %q", id, node.Provenance.Craft)
		}
	}
	if id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, 4); craftNodeExists(t, graph, id) {
		t.Fatalf("the fan cap of 3 admitted a fourth item")
	}

	// Dependents wait on all of them, or the assembly would run against one
	// slide and a list.
	waiting := craftDependencies(t, graph, run.Prefix+"~assemble")
	for index := 1; index <= 3; index++ {
		if !waiting[craftGenerationID(run.Prefix, "sections", craftItemGeneration, index)] {
			t.Fatalf("assemble does not wait on item %d: %+v", index, waiting)
		}
	}
	if thread := craftMessages(t, graph); !strings.Contains(thread, `presentation craft — "sections" fans out over 3 items`) {
		t.Fatalf("no fan-out receipt in the thread:\n%s", thread)
	}
}

// A failed check buys one more bounded round: fresh copies of the steps the
// file named, carrying the verifier's own words, and a fresh check after them.
func TestCraftVerifyFailureBuysOneMoreRoundAndAPassEndsIt(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	landCraftUpToCheck(t, graph, runner, run)

	advance := settleCraftNode(t, graph, runner, run.Prefix+"~check",
		"VERDICT: fail\nslide 3 has no speaker notes\nslide 5 cites nothing")
	if advance.Round != 2 || advance.Spliced != 2 {
		t.Fatalf("round advance = %+v", advance)
	}

	repair := craftGenerationID(run.Prefix, "assemble", craftRoundGeneration, 2)
	node, ok, err := graph.Node(repair)
	if err != nil || !ok {
		t.Fatalf("repair copy %s: found=%t err=%v", repair, ok, err)
	}
	if !strings.Contains(node.Brief, "repair round 2 of 3") ||
		!strings.Contains(node.Brief, "slide 3 has no speaker notes") {
		t.Fatalf("repair copy lost the verifier's feedback:\n%s", node.Brief)
	}
	if node.Provenance.Craft != CraftRef(workflow) {
		t.Fatalf("repair copy craft provenance = %q", node.Provenance.Craft)
	}

	recheck := craftGenerationID(run.Prefix, "check", craftRoundGeneration, 2)
	if deps := craftDependencies(t, graph, recheck); !deps[repair] {
		t.Fatalf("the new check does not wait on the repair: %+v", deps)
	}
	// The deliverable now waits on the new check as well as the old one.
	if deps := craftDependencies(t, graph, run.Prefix); !deps[recheck] {
		t.Fatalf("the run's root was not rewired onto the new check: %+v", deps)
	}
	if thread := craftMessages(t, graph); !strings.Contains(thread, "check failed — one more round (2 of 3)") {
		t.Fatalf("no round receipt in the thread:\n%s", thread)
	}

	before := len(craftNodeIDs(t, graph, run.Prefix))
	completeCraftNode(t, graph, repair, "notes and citations added")
	if advance := settleCraftNode(t, graph, runner, recheck, "VERDICT: pass\nall slides check out"); advance.Spliced != 0 {
		t.Fatalf("a pass opened more work: %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("a pass changed the graph: %d nodes then %d", before, after)
	}
}

// The bound is the bound. When the rounds are spent the run says so plainly
// and delivers what it has, rather than quietly checking one more time.
func TestCraftRoundsExhaustedDeliverWhatLanded(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	workflow.Steps[3].Verify.UntilPass.MaxRounds = 1
	runner, run := startCraftRun(t, graph, workflow)
	landCraftUpToCheck(t, graph, runner, run)

	before := len(craftNodeIDs(t, graph, run.Prefix))
	advance := settleCraftNode(t, graph, runner, run.Prefix+"~check", "VERDICT: fail\nstill no speaker notes")
	if advance.Spliced != 0 || advance.Stopped != "rounds exhausted" {
		t.Fatalf("exhausted advance = %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("an exhausted run spliced anyway: %d nodes then %d", before, after)
	}
	if thread := craftMessages(t, graph); !strings.Contains(thread,
		`the "check" check still failed after 1 round — delivering what landed, unverified`) {
		t.Fatalf("no honest exhaustion receipt:\n%s", thread)
	}
}

// Money past the bound is a decision, not a default. The run stops opening
// work and asks in the same shape the daily rail asks.
func TestCraftCostBoundPausesAndAsksBeforeSplicing(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	settleCraftNode(t, graph, runner, run.Prefix+"~research", "the list is above")
	if err := graph.RecordUsage(store.NodeUsage{NodeID: run.Prefix + "~research", Cost: 1.75}); err != nil {
		t.Fatal(err)
	}

	before := len(craftNodeIDs(t, graph, run.Prefix))
	advance := settleCraftNode(t, graph, runner, run.Prefix+"~sections", craftResearchResult)
	if advance.Spliced != 0 || advance.Stopped != "cost" {
		t.Fatalf("cost-bound advance = %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("work was opened past the cost bound: %d nodes then %d", before, after)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || questions[0].OriginNodeID != run.Prefix ||
		!strings.Contains(questions[0].Text, craftBudgetQuestionPrefix) ||
		!strings.Contains(questions[0].Text, "$1.75 of its $1.50 bound") {
		t.Fatalf("pause-and-ask = %+v", questions)
	}
	if questions[0].Category != store.QuestionCategoryRailRaise ||
		questions[0].Urgency != store.QuestionBlocking {
		t.Fatalf("the craft stop is not shaped like the rail's: %+v", questions[0])
	}
}

// The clock needs nobody's consent: past the bound the run stops opening work
// and says what it delivered, while whatever is already running finishes.
func TestCraftWallClockBoundStopsOpeningWork(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	runner.WithClock(func() time.Time { return time.Now().Add(2 * time.Hour) })
	settleCraftNode(t, graph, runner, run.Prefix+"~research", "the list is above")

	before := len(craftNodeIDs(t, graph, run.Prefix))
	advance := settleCraftNode(t, graph, runner, run.Prefix+"~sections", craftResearchResult)
	if advance.Spliced != 0 || advance.Stopped != "wall clock" {
		t.Fatalf("wall-clock advance = %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("work was opened past the wall clock: %d nodes then %d", before, after)
	}
	const receipt = "presentation craft hit its 30m bound — delivered what landed"
	if thread := craftMessages(t, graph); !strings.Contains(thread, receipt) {
		t.Fatalf("no honest wall-clock receipt:\n%s", thread)
	}
	// The sweep re-derives this same move on every tick, and a bound that has
	// been reached stays reached. Saying it once is the whole point.
	node, _, err := graph.Node(run.Prefix + "~sections")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Settle(node, craftResearchResult, 0); err != nil {
		t.Fatal(err)
	}
	if said := strings.Count(craftMessages(t, graph), receipt); said != 1 {
		t.Fatalf("the wall-clock receipt was said %d times", said)
	}
}

// The run keeps nothing in this process. A store closed mid-run, reopened and
// rebuilt from its journal, resumes at exactly the move it had not made yet.
func TestCraftRunResumesAfterRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "craft.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	workflow := presentationCraft()
	_, run := startCraftRun(t, graph, workflow)
	completeCraftNode(t, graph, run.Prefix+"~research", "the list is above")
	// The fan-out lands, and the process dies before its consequences reach
	// the graph — the exact window the sweep exists for.
	completeCraftNode(t, graph, run.Prefix+"~sections", craftResearchResult)
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	resumed := NewCraftRunner(reopened, &fakeCraftRepo{workflow: workflow}, "/home/craft")
	spliced, err := resumed.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if spliced != 3 {
		t.Fatalf("resumed %d items, want the three the list named", spliced)
	}
	for index := 1; index <= 3; index++ {
		id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, index)
		node, ok, err := reopened.Node(id)
		if err != nil || !ok {
			t.Fatalf("resumed item %s: found=%t err=%v", id, ok, err)
		}
		if node.Provenance.Craft != CraftRef(workflow) {
			t.Fatalf("resumed item %s craft = %q", id, node.Provenance.Craft)
		}
	}
	// A second sweep must be a no-op: the graph, not a flag, is the record of
	// what has already been done.
	again, err := resumed.Sweep(context.Background())
	if err != nil || again != 0 {
		t.Fatalf("second sweep spliced %d (err %v)", again, err)
	}
}

// A check that ignored the format is not a fail — spending a repair round on a
// verdict nobody gave is how a bounded loop becomes an unbounded one.
func TestCraftVerdictWithoutTheMarkerOpensNothing(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	landCraftUpToCheck(t, graph, runner, run)

	before := len(craftNodeIDs(t, graph, run.Prefix))
	advance := settleCraftNode(t, graph, runner, run.Prefix+"~check", "looks broadly fine to me")
	if advance.Spliced != 0 || advance.Stopped != "no verdict" {
		t.Fatalf("verdictless advance = %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("a missing verdict spliced work: %d nodes then %d", before, after)
	}
}

// A craft that moved under a live run is a refusal, not a silent swap: the
// nodes on the graph were compiled from a version that is no longer there.
func TestCraftThatMovedMidRunRefusesToAdvance(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	repo := &fakeCraftRepo{workflow: workflow}
	runner := NewCraftRunner(graph, repo, "/home/craft")
	run, err := runner.RunCraft(workflow.Name, map[string]string{"topic": "quantum"}, "craft", "run it")
	if err != nil {
		t.Fatal(err)
	}
	moved := presentationCraft()
	moved.Commit = "f00ba12"
	repo.workflow = moved

	node, _, err := graph.Node(run.Prefix + "~sections")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Settle(node, craftResearchResult, 0); err == nil ||
		!strings.Contains(err.Error(), "moved to") {
		t.Fatalf("a moved craft advanced anyway: %v", err)
	}
}

func landCraftUpToCheck(t *testing.T, graph *store.Store, runner *CraftRunner, run CraftRun) {
	t.Helper()
	settleCraftNode(t, graph, runner, run.Prefix+"~research", "the list is above")
	settleCraftNode(t, graph, runner, run.Prefix+"~sections", craftResearchResult)
	for index := 1; index <= 3; index++ {
		completeCraftNode(t, graph, craftGenerationID(run.Prefix, "sections", craftItemGeneration, index), "slide written")
	}
	completeCraftNode(t, graph, run.Prefix+"~assemble", "deck assembled at /tmp/deck.md")
}

func craftNodeExists(t *testing.T, graph *store.Store, id string) bool {
	t.Helper()
	_, ok, err := graph.Node(id)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func craftDependencies(t *testing.T, graph *store.Store, id string) map[string]bool {
	t.Helper()
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	needs := make(map[string]bool)
	for _, edge := range edges {
		if edge.To == id {
			needs[edge.From] = true
		}
	}
	return needs
}

// The seam, end to end: a runner with the sentinel installed drives a craft
// run forward as its leaves land, with no craft-specific code in the executor.
func TestRunnerAdvancesACraftRunAsItsLeavesLand(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	craftRunner, run := startCraftRun(t, graph, workflow)

	answers := map[string]string{
		run.Prefix + "~research": "the list is above",
		run.Prefix + "~sections": craftResearchResult,
	}
	runner := NewRunner(graph, func(_ context.Context, node store.Node) (ExecResult, error) {
		answer, known := answers[node.ID]
		if !known {
			answer = "done"
		}
		return ExecResult{Summary: answer, Cost: 0.01}, nil
	}, "craft-runner", 1).WithCraftRunner(craftRunner)

	for pass := 0; pass < 4; pass++ {
		if _, err := runner.Tick(context.Background()); err != nil {
			t.Fatalf("tick %d: %v", pass, err)
		}
		runner.Wait()
	}
	for index := 1; index <= 3; index++ {
		id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, index)
		if !craftNodeExists(t, graph, id) {
			t.Fatalf("the runner did not unroll item %s", id)
		}
	}
}

// The resident's own tick carries the same sweep, so a run advances even when
// nothing is executing in this process at all.
func TestReconcilerTickSweepsCraftRuns(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	craftRunner, run := startCraftRun(t, graph, workflow)
	completeCraftNode(t, graph, run.Prefix+"~research", "the list is above")
	completeCraftNode(t, graph, run.Prefix+"~sections", craftResearchResult)

	if err := New(graph, nil, nil).WithCraftRunner(craftRunner).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, 1); !craftNodeExists(t, graph, id) {
		t.Fatalf("a resident tick did not advance the craft run")
	}
}

// craftBudgetQuestions is one run's own money record: every stop it has posted,
// whatever became of each.
func craftBudgetQuestions(t *testing.T, graph *store.Store, prefix string) []store.AgentQuestion {
	t.Helper()
	questions, err := graph.QuestionsForNode(prefix, 50)
	if err != nil {
		t.Fatal(err)
	}
	var mine []store.AgentQuestion
	for _, question := range questions {
		if strings.Contains(question.Text, craftBudgetQuestionPrefix) {
			mine = append(mine, question)
		}
	}
	return mine
}

// crowdTheBoard fills the question queue with other people's unanswered
// business — more of it than any window over the whole board would show at
// once. A run's own money record has to be findable through a busy day, not
// merely through a quiet one.
func crowdTheBoard(t *testing.T, graph *store.Store, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := graph.AskQuestion(store.AgentQuestion{
			SessionID: "craft", Text: fmt.Sprintf("unrelated question %d", index+1),
			Urgency: store.QuestionWhenever,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// craftAtItsBound lands a run's opening step and takes it to the money bound,
// leaving the fan-out planner's result on the graph and the stop in the thread.
func craftAtItsBound(t *testing.T, graph *store.Store) (*CraftRunner, CraftRun, store.AgentQuestion) {
	t.Helper()
	runner, run := startCraftRun(t, graph, presentationCraft())
	settleCraftNodeSpending(t, graph, runner, run.Prefix+"~research", "the list is above", 1.75)
	if advance := settleCraftNode(t, graph, runner, run.Prefix+"~sections", craftResearchResult); advance.Stopped != "cost" {
		t.Fatalf("the run did not stop at its bound: %+v", advance)
	}
	asked := craftBudgetQuestions(t, graph, run.Prefix)
	if len(asked) != 1 {
		t.Fatalf("the money stop was asked %d times", len(asked))
	}
	return runner, run, asked[0]
}

// Consent has to mean something. The question survives being answered — it is
// the durable record of both halves — so the stop is put once, "keep going"
// buys the run another bound's worth, and the sweep that re-derives the same
// landed node resumes instead of asking again.
func TestCraftBudgetStopIsAskedOnceAndKeepGoingResumesTheRun(t *testing.T) {
	graph := openStore(t)
	// Two hundred other unanswered questions were already on the board when the
	// run reached its bound: the record of having asked is this run's own, and
	// it cannot be something a busy day pages out of view.
	crowdTheBoard(t, graph, 205)
	runner, run, question := craftAtItsBound(t, graph)
	for pass := 0; pass < 2; pass++ {
		if _, err := runner.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if asked := craftBudgetQuestions(t, graph, run.Prefix); len(asked) != 1 {
		t.Fatalf("an unanswered stop was asked %d times", len(asked))
	}

	// Exactly what the head writes down when the user picks the first option.
	if err := graph.ResolveQuestion(question.Seq, store.QuestionAnswered, craftContinueOption+run.Prefix); err != nil {
		t.Fatalf("answer: %v", err)
	}
	spliced, err := runner.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if spliced != 3 {
		t.Fatalf("consent bought %d items, want the three the list named", spliced)
	}
	for index := 1; index <= 3; index++ {
		if id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, index); !craftNodeExists(t, graph, id) {
			t.Fatalf("item %s never opened after the user said keep going", id)
		}
	}
	// And it never asks that question again: the run has headroom, and the
	// record of having asked does not evaporate because it was answered.
	for pass := 0; pass < 3; pass++ {
		if _, err := runner.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if asked := craftBudgetQuestions(t, graph, run.Prefix); len(asked) != 1 {
		t.Fatalf("the money stop was asked %d times across four sweeps", len(asked))
	}
}

// The other answer closes the run: nothing that has not started will start, and
// the root is left open to deliver what did land.
func TestCraftBudgetStopAnsweredWithStopClosesTheRun(t *testing.T) {
	graph := openStore(t)
	runner, run, question := craftAtItsBound(t, graph)
	if err := graph.ResolveQuestion(question.Seq, store.QuestionAnswered, craftStopOption+run.Prefix); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := runner.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, 1); craftNodeExists(t, graph, id) {
		t.Fatalf("a closed run opened %s", id)
	}
	for _, id := range []string{run.Prefix + "~assemble", run.Prefix + "~check"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("read %s: found=%t err=%v", id, ok, err)
		}
		if node.Status != store.Cancelled {
			t.Fatalf("%s is %s, want cancelled so the root can deliver", id, node.Status)
		}
	}
	root, ok, err := graph.Node(run.Prefix)
	if err != nil || !ok {
		t.Fatalf("read the root: found=%t err=%v", ok, err)
	}
	if root.Status != store.Pending {
		t.Fatalf("the run's root is %s — nothing is left to deliver what landed", root.Status)
	}
	if thread := craftMessages(t, graph); !strings.Contains(thread,
		"presentation craft — stopping here as you asked; delivering what already landed") {
		t.Fatalf("no honest stop receipt:\n%s", thread)
	}
	// A closed run stays closed, and says so once.
	if _, err := runner.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked := craftBudgetQuestions(t, graph, run.Prefix); len(asked) != 1 {
		t.Fatalf("a closed run asked again: %d questions", len(asked))
	}
}

// The gate has to see the spend of the leaf whose landing triggered it. That
// leaf is the fan-out planner, and its own cost is the one thing the usage
// table does not have yet at the moment the run decides whether to open a
// couple of dozen more leaves.
func TestCraftMoneyGateSeesTheLandingLeafsOwnSpend(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	runner, run := startCraftRun(t, graph, workflow)
	settleCraftNodeSpending(t, graph, runner, run.Prefix+"~research", "the list is above", 0.80)

	before := len(craftNodeIDs(t, graph, run.Prefix))
	advance := settleCraftNodeSpending(t, graph, runner, run.Prefix+"~sections", craftResearchResult, 0.80)
	if advance.Unrolled != 0 || advance.Stopped != "cost" {
		t.Fatalf("the gate read the table alone: %+v", advance)
	}
	if after := len(craftNodeIDs(t, graph, run.Prefix)); after != before {
		t.Fatalf("work opened past the bound: %d nodes then %d", before, after)
	}
	if asked := craftBudgetQuestions(t, graph, run.Prefix); len(asked) != 1 ||
		!strings.Contains(asked[0].Text, "$1.60 of its $1.50 bound") {
		t.Fatalf("the stop does not quote the spend it decided on: %+v", asked)
	}
}

// A repair copy of a for_each step is still a for_each step. Its items are
// minted from the node that listed them, so a second round's work sits under
// its own repair instead of colliding with the first round's.
func TestARepairedForEachStepUnrollsLikeTheOriginal(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	// A legal file: the check depends on the fan-out step, so it may revise it.
	workflow.Steps[3].Verify.UntilPass.Revise = []string{"sections"}
	runner, run := startCraftRun(t, graph, workflow)
	landCraftUpToCheck(t, graph, runner, run)

	if advance := settleCraftNode(t, graph, runner, run.Prefix+"~check",
		"VERDICT: fail\nthe slides do not cover what the research found"); advance.Round != 2 {
		t.Fatalf("the failed check bought no round: %+v", advance)
	}
	repair := craftGenerationID(run.Prefix, "sections", craftRoundGeneration, 2)
	advance := settleCraftNode(t, graph, runner, repair, craftResearchResult)
	if advance.Unrolled != 3 {
		t.Fatalf("a repaired fan-out landed a list and unrolled %d of it", advance.Unrolled)
	}
	for index := 1; index <= 3; index++ {
		item := craftChildID(repair, craftItemGeneration, index)
		if !craftNodeExists(t, graph, item) {
			t.Fatalf("the repaired round never opened %s", item)
		}
		if first := craftGenerationID(run.Prefix, "sections", craftItemGeneration, index); item == first {
			t.Fatalf("the repair's items collided with round one's: %s", item)
		}
	}
	// The new check waits on the work the repair actually produced.
	recheck := craftGenerationID(run.Prefix, "check", craftRoundGeneration, 2)
	if deps := craftDependencies(t, graph, recheck); !deps[craftChildID(repair, craftItemGeneration, 1)] {
		t.Fatalf("the new check does not wait on the repaired items: %+v", deps)
	}
}

// A worker that ignored the format named no items. Reading its apology as a
// list is how a run spends a real model call on a leaf titled "finished with no
// summary" — so the marker is required, and Settle and the sweep are handed the
// same words so they cannot reach different conclusions about it.
func TestAFanOutWorkerThatNamedNoItemsUnrollsNothing(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	craftRunner, run := startCraftRun(t, graph, workflow)
	runner := NewRunner(graph, func(_ context.Context, node store.Node) (ExecResult, error) {
		if node.ID == run.Prefix+"~sections" {
			// It said nothing at all: the runner substitutes a summary.
			return ExecResult{}, nil
		}
		return ExecResult{Summary: "the list is above"}, nil
	}, "craft-runner", 1).WithCraftRunner(craftRunner)
	for pass := 0; pass < 2; pass++ {
		if _, err := runner.Tick(context.Background()); err != nil {
			t.Fatalf("tick %d: %v", pass, err)
		}
		runner.Wait()
	}

	node, ok, err := graph.Node(run.Prefix + "~sections")
	if err != nil || !ok {
		t.Fatalf("read the fan-out: found=%t err=%v", ok, err)
	}
	if node.Summary != "finished with no summary" {
		t.Fatalf("the fan-out landed with %q", node.Summary)
	}
	// The live reading and the resumed one agree, because they read the same
	// words.
	if _, err := craftRunner.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := craftGenerationID(run.Prefix, "sections", craftItemGeneration, 1); craftNodeExists(t, graph, id) {
		t.Fatalf("a phantom item %s was unrolled from an empty list", id)
	}
	if thread := craftMessages(t, graph); !strings.Contains(thread,
		`the "sections" step named no items to fan out over`) {
		t.Fatalf("no honest reason in the thread:\n%s", thread)
	}
}

// The verdict marker is read case-insensitively over the verifier's own bytes.
// Folding case is not length-preserving in every language a check may print in,
// and an offset taken in a folded copy points somewhere else in the original —
// which reads a failed check as no verdict and ships unverified work.
func TestCraftVerdictSurvivesACaseFoldThatMovesBytes(t *testing.T) {
	for _, probe := range []struct {
		result string
		pass   bool
		known  bool
	}{
		{"VERDICT: pass", true, true},
		{"verdict: fail\nthe deck has no notes", false, true},
		{"Sonuç bulunamadı ısı VERDICT: fail", false, true},
		{"ﬁnal ﬀ VERDICT: pass", true, true},
		{"looks broadly fine to me", false, false},
		{"", false, false},
	} {
		pass, known := craftVerdict(probe.result)
		if pass != probe.pass || known != probe.known {
			t.Errorf("craftVerdict(%q) = (%t, %t), want (%t, %t)",
				probe.result, pass, known, probe.pass, probe.known)
		}
	}
}

// The id parser decides what a landed node MEANT. A generation nobody mints is
// not a craft node this sentinel understands, and routing one into a fan-out or
// a repair round on the strength of a lenient read is how a stranger's node
// spends a run's money.
func TestCraftNodePartsFailsClosed(t *testing.T) {
	for _, probe := range []struct {
		id   string
		want bool
	}{
		{"craft-deck-1~sections", true},
		{"craft-deck-1~sections~i1", true},
		{"craft-deck-1~sections~r12", true},
		{"craft-deck-1~sections~r0", false},
		{"craft-deck-1~sections~r007", false},
		{"craft-deck-1~sections~x3", false},
		{"craft-deck-1~sections~2", false},
		{"craft-deck-1~sections~r", false},
		{"craft-deck-1~sections~r99999999999999999999", false},
		{"~sections~r2", false},
		{"craft-deck-1~~r2", false},
		{"craft-deck-1~sections~r2~i1", false},
		{"task-14-n2", false},
	} {
		if _, _, _, _, ok := craftNodeParts(probe.id); ok != probe.want {
			t.Errorf("craftNodeParts(%q) ok = %t, want %t", probe.id, ok, probe.want)
		}
	}
}

// A round may only promise the new check dependencies it actually plants. A
// step whose node is gone — surgery took it mid-run — cannot be repaired, and
// naming it anyway makes Splice refuse the check for needing an unknown node,
// which the sweep would then re-derive identically forever.
func TestARoundNeverPromisesARepairItCannotPlant(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	provenance := store.Provenance{
		Origin: store.OriginUser, SessionID: "craft", Intent: "run the presentation craft",
		Craft: CraftRef(workflow),
	}
	// The run as surgery left it: the check is still there, the step its round
	// would revise is not.
	const prefix = "craft-presentation-9"
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: prefix, Brief: "deliver the deck", Title: "presentation"},
		{ID: prefix + "~check", Parent: prefix, Brief: "check the deck", Title: "check"},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	runner := NewCraftRunner(graph, &fakeCraftRepo{workflow: workflow}, "/home/craft")
	node, ok, err := graph.Node(prefix + "~check")
	if err != nil || !ok {
		t.Fatalf("read the check: found=%t err=%v", ok, err)
	}
	advance, err := runner.Settle(node, "VERDICT: fail\nnothing to build on", 0)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if advance.Stopped != "nothing to revise" || advance.Spliced != 0 {
		t.Fatalf("advance = %+v", advance)
	}
	if craftNodeExists(t, graph, craftGenerationID(prefix, "check", craftRoundGeneration, 2)) {
		t.Fatalf("a check was spliced whose repair was never planted")
	}
}

// The receipt names the version a run is actually on. A file nobody saved is
// its own version, and saying so is the same honesty the mid-run guard is.
func TestTheReceiptSaysWhenARunIsOnAnUnsavedFile(t *testing.T) {
	graph := openStore(t)
	workflow := presentationCraft()
	workflow.Commit = "abc1234def5678+dirty-1a2b3c4d"
	_, run := startCraftRun(t, graph, workflow)
	if !strings.Contains(run.Receipt, "v abc1234+dirty") {
		t.Fatalf("compile receipt = %q", run.Receipt)
	}
}
