package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// crowdedJob is a job already standing at the node ceiling, which is the state
// every growth path has to be refused from.
func crowdedJob(t *testing.T, sessionID string, size int) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "grow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	specs := []store.NodeSpec{{ID: "job", Brief: "the whole job", Title: "The job"}}
	for index := 1; index < size; index++ {
		specs = append(specs, store.NodeSpec{
			ID: fmt.Sprintf("job-n%d", index), Parent: "job", Brief: "a piece", Title: "A piece",
		})
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: specs},
		store.Provenance{Origin: store.OriginUser, SessionID: sessionID, Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

func jobNode(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	node, ok, err := graph.Node(id)
	if err != nil || !ok {
		t.Fatalf("node %s: ok=%t err=%v", id, ok, err)
	}
	return node
}

// Every path that can grow a job meets the same ceiling. Before this there was
// one ceiling enforced inside one function, and a caller that did not go
// through that function — the revision sentinel — met nothing at all.
func TestGrowJobRefusesEveryCallerPastTheCeiling(t *testing.T) {
	for _, reason := range []string{GrowOverrun, GrowGap, GrowRevision, GrowJIT, GrowCoverage} {
		t.Run(reason, func(t *testing.T) {
			graph := crowdedJob(t, "s1", maxJobNodes)
			verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
				JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: reason, Adding: 2,
			})
			if err != nil {
				t.Fatal(err)
			}
			if verdict.Allow || verdict.Cause != CauseCeiling {
				t.Fatalf("%s grew past the ceiling: %+v", reason, verdict)
			}
			assertRecordOnly(t, graph, "s1", "job", "grown as large")

			growths, err := graph.JobGrowths("job")
			if err != nil {
				t.Fatal(err)
			}
			if len(growths) != 1 || growths[0].Reason != reason || growths[0].Allowed {
				t.Fatalf("the journal does not name what was refused: %+v", growths)
			}
		})
	}
}

// The hole this wave closed: ApplyRevision spliced into the job root with no
// ceiling, no round counter and no rail check whatsoever.
func TestApplyRevisionIsRefusedPastTheJobCeiling(t *testing.T) {
	graph := crowdedJob(t, "s2", maxJobNodes)
	planGraph := &plan.Graph{Goal: "the goal", Nodes: []plan.Node{
		{ID: 500, Title: "One more piece", Summary: "the result asked for it"},
	}}
	operations := []plan.Operation{{Op: "add", Node: 500, Reason: "the landed result contradicts the plan", Applied: true}}

	applied, notes := ApplyRevision(graph, planGraph, "job", "job", operations)
	if applied != 0 {
		t.Fatalf("a sentinel add landed past the ceiling: applied=%d", applied)
	}
	if _, ok, _ := graph.Node("job-n500"); ok {
		t.Fatal("the node the governor refused exists anyway")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "grown as large") {
		t.Fatalf("the refusal did not come back as a note: %v", notes)
	}
	// And the person reading the job's own record is told a governor stopped it.
	assertRecordOnly(t, graph, "s2", "job", "grown as large")

	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 || growths[0].Reason != GrowRevision || growths[0].Allowed || growths[0].Adding != 1 {
		t.Fatalf("the sentinel's refused add is not in the journal: %+v", growths)
	}
}

// A revision that fits is applied and journaled, which is the other half of the
// same fix: the governor bounds growth, it does not forbid it.
func TestApplyRevisionAdmitsAndJournalsGrowthThatFits(t *testing.T) {
	graph := crowdedJob(t, "s3", 4)
	planGraph := &plan.Graph{Goal: "the goal", Nodes: []plan.Node{
		{ID: 7, Title: "One more piece", Summary: "the result asked for it"},
	}}
	applied, notes := ApplyRevision(graph, planGraph, "job", "job",
		[]plan.Operation{{Op: "add", Node: 7, Reason: "the result contradicts the plan", Applied: true}})
	if applied != 1 || len(notes) != 0 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}
	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 || !growths[0].Allowed || growths[0].Round != 1 || growths[0].Adding != 1 {
		t.Fatalf("the admitted round is not journaled: %+v", growths)
	}
}

// Rounds are counted per lineage under one job. Two siblings that each split
// once are two lineages with one round each — the counter must not let one
// sibling's repair spend another sibling's allowance.
func TestGrowthRoundsAreCountedPerLineage(t *testing.T) {
	graph := crowdedJob(t, "s4", 3)
	// Both lineages are getting somewhere — a file written each round — because
	// that is what "three legitimate rounds" means and it is what the counter is
	// being tested about. A lineage that changes nothing twice running is
	// refused before the counter is ever reached; that rule has its own tests.
	first := GrowRequest{JobRoot: "job", Node: jobNode(t, graph, "job-n1"), Lineage: "job-n1",
		Reason: GrowOverrun, Adding: 1, Measured: true, Produced: 1}
	second := GrowRequest{JobRoot: "job", Node: jobNode(t, graph, "job-n2"), Lineage: "job-n2",
		Reason: GrowOverrun, Adding: 1, Measured: true, Produced: 1}

	for round := 1; round <= MaxOverrunRounds; round++ {
		verdict, err := growJob(context.Background(), graph, nil, first)
		if err != nil || !verdict.Allow || verdict.Round != round {
			t.Fatalf("round %d of the first lineage: %+v err=%v", round, verdict, err)
		}
		admitGrowth(graph, first, verdict, 1)
	}
	spent, err := growJob(context.Background(), graph, nil, first)
	if err != nil || spent.Allow || spent.Cause != CauseRounds {
		t.Fatalf("the first lineage grew past its allowance: %+v err=%v", spent, err)
	}
	// The sibling has spent nothing, and says so.
	fresh, err := growJob(context.Background(), graph, nil, second)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Allow || fresh.Round != 1 {
		t.Fatalf("one lineage's rounds were charged to its sibling: %+v", fresh)
	}
}

// The positive stopping condition: a job whose criterion is already covered is
// refused with rounds and nodes to spare, and the refusal names coverage rather
// than a cap.
func TestGrowJobRefusesWhenTheGoalIsAlreadyCovered(t *testing.T) {
	graph := crowdedJob(t, "s5", 3)
	criterion := plan.Done{
		Produces:   []string{"the comparison table"},
		Conditions: []plan.Check{{Kind: plan.CheckRead, Check: "the table", Expect: "it names both options"}},
	}
	asked := 0
	gate := SatisfierFunc(func(_ context.Context, done plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error) {
		asked++
		if len(done.Conditions) != 1 {
			t.Fatalf("the gate was asked about the wrong criterion: %+v", done)
		}
		return plan.Satisfaction{Complete: true}, nil
	})

	verdict, err := growJob(context.Background(), graph, gate, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowRevision, Adding: 1, Criterion: criterion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("the gate was asked %d times", asked)
	}
	if verdict.Allow || verdict.Cause != CauseCovered {
		t.Fatalf("covered growth was admitted: %+v", verdict)
	}
	if !strings.Contains(verdict.Refused, "already covered") {
		t.Fatalf("the refusal says nothing a person can read: %q", verdict.Refused)
	}
	assertRecordOnly(t, graph, "s5", "job", "already covered")

	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 || growths[0].Cause != CauseCovered || growths[0].Allowed {
		t.Fatalf("the coverage refusal did not record its reason: %+v", growths)
	}
}

// The gate reads the criterion off the job's own root when the caller carries
// none, and it is shown what has landed and what is still committed to.
func TestGrowJobShowsTheGateWhatTheJobHasAndExpects(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "coverage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "compare the options", Title: "The job", Spec: EncodeSpec(plan.Spec{
			Instruction: "compare the options",
			Done:        plan.Done{Produces: []string{"the comparison table"}},
		})},
		{ID: "job-n1", Parent: "job", Brief: "write the first half", Title: "First half"},
		{ID: "job-n2", Parent: "job", Brief: "write the second half", Title: "Second half"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s6", Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("job-n1", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "the first half is written"); err != nil {
		t.Fatal(err)
	}
	var sawLanded, sawInFlight bool
	gate := SatisfierFunc(func(_ context.Context, done plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error) {
		if len(done.Produces) != 1 {
			t.Fatalf("the job's own criterion was not read: %+v", done)
		}
		for _, item := range landed {
			if strings.Contains(item.Result, "first half") {
				sawLanded = true
			}
		}
		sawInFlight = len(inflight) > 0
		return plan.Satisfaction{Uncovered: []plan.Uncovered{{Condition: "the table", Missing: "the second option"}}}, nil
	})
	verdict, err := growJob(context.Background(), graph, gate, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowRevision, Adding: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Allow {
		t.Fatalf("an uncovered condition refused growth: %+v", verdict)
	}
	if !sawLanded || !sawInFlight {
		t.Fatalf("the gate was asked blind: landed=%t inflight=%t", sawLanded, sawInFlight)
	}
}

// The rollback proof: with the gate off, the paid question is never asked and
// the caps are the whole governor — which is what every path did before.
func TestGrowthGateOffAsksNothing(t *testing.T) {
	previous := GrowthGate
	GrowthGate = false
	t.Cleanup(func() { GrowthGate = previous })

	graph := crowdedJob(t, "s7", 3)
	gate := SatisfierFunc(func(context.Context, plan.Done, []plan.Landed, []plan.Spec) (plan.Satisfaction, error) {
		t.Fatal("the gate was asked with the gate switched off")
		return plan.Satisfaction{}, nil
	})
	verdict, err := growJob(context.Background(), graph, gate, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowOverrun, Adding: 1,
		Criterion: plan.Done{Produces: []string{"anything"}},
	})
	if err != nil || !verdict.Allow {
		t.Fatalf("the caps alone refused growth: %+v err=%v", verdict, err)
	}
}

// A gate that cannot answer must not truncate work: the question is the only
// one in the governor whose failure direction is a decision about someone's
// unfinished job.
func TestGrowJobFailsOpenWhenTheGateErrors(t *testing.T) {
	graph := crowdedJob(t, "s8", 3)
	gate := SatisfierFunc(func(context.Context, plan.Done, []plan.Landed, []plan.Spec) (plan.Satisfaction, error) {
		return plan.Satisfaction{}, fmt.Errorf("the provider is down")
	})
	verdict, err := growJob(context.Background(), graph, gate, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowOverrun, Adding: 1,
		Criterion: plan.Done{Produces: []string{"anything"}},
	})
	if err != nil || !verdict.Allow {
		t.Fatalf("an unanswered question stopped a job: %+v err=%v", verdict, err)
	}
}

// A redirect is the person speaking, and a person is not answerable by "the
// goal is already covered" — they have just changed what the goal is. The caps
// still hold.
func TestUngatedGrowthKeepsTheCaps(t *testing.T) {
	graph := crowdedJob(t, "s9", maxJobNodes)
	gate := SatisfierFunc(func(context.Context, plan.Done, []plan.Landed, []plan.Spec) (plan.Satisfaction, error) {
		t.Fatal("a person's redirect was put to the coverage gate")
		return plan.Satisfaction{}, nil
	})
	planGraph := &plan.Graph{Goal: "the goal", Nodes: []plan.Node{{ID: 42, Title: "What they asked for"}}}
	applied, notes := ApplyRevisionGoverned(context.Background(),
		Growth{Reason: GrowRedirect, Ask: gate, Ungated: true},
		graph, planGraph, "job", "job",
		[]plan.Operation{{Op: "add", Node: 42, Reason: "they asked for it", Applied: true}})
	if applied != 0 || len(notes) != 1 {
		t.Fatalf("the ceiling did not hold for a redirect: applied=%d notes=%v", applied, notes)
	}
}

// The loop this closes, in the shape it was measured in: one lineage was
// re-planned three rounds running, each round handed a byte-identical remainder,
// each round leaving nothing at all in the tree, at $0.47 a round. The only
// thing that ever stopped it was the round cap, three rounds and $1.42 later.
// A count cannot tell a round that is finishing the work from a round that is
// repeating it, so the governor now reads what happened instead.
func TestALineageThatIsHandedTheSameRemainderTwiceIsAFixedPointAndStops(t *testing.T) {
	graph := crowdedJob(t, "s1", 3)
	node := jobNode(t, graph, "job-n1")
	gap := "the spacing constants are still not derived; homeCardCap is still 48"

	first, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap, Adding: 1,
		Measured: true, Produced: 0, Remainder: RemainderDigest(gap),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allow {
		t.Fatalf("the first round was refused: %+v", first)
	}
	admitGrowth(graph, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap,
		Measured: true, Produced: 0, Remainder: RemainderDigest(gap),
	}, first, 1)

	// The continuation ran and the reviewer named the same remainder again, in
	// the same words. Asking for it a third time buys what the second round
	// already bought.
	second, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap, Adding: 1,
		Measured: true, Produced: 0, Remainder: RemainderDigest("The  Spacing constants are still not derived; homeCardCap is still 48\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Allow || second.Cause != CauseFixedPoint {
		t.Fatalf("a lineage handed its own remainder back grew again: %+v", second)
	}
	if second.Refused != RefusedFixedPoint {
		t.Fatalf("refusal = %q", second.Refused)
	}
}

// The same finding read from the tree instead of from the text, for a remainder
// that came back reworded. One fruitless round is never refused — a leaf can run
// out of budget before it writes its first file, and that is exactly the round a
// repair exists for. Two in a row is a standstill.
func TestALineageThatHasChangedNothingTwiceStopsAndTheFirstRoundNeverDoes(t *testing.T) {
	graph := crowdedJob(t, "s1", 3)
	node := jobNode(t, graph, "job-n1")
	// An empty tree the rounds are read against. It is named rather than
	// asserted because MEASURED IS A FACT ABOUT THE WORLD HAVING BEEN READ: a
	// request that hands neither an artifact list nor a workspace has not read
	// anything, whatever it says about itself, and the governor correctly
	// declines to weigh it. See GrowRequest.weighed.
	tree := t.TempDir()

	first, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowOverrun, Adding: 1,
		Workspace: tree, Remainder: RemainderDigest("nothing has been written yet"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allow {
		t.Fatalf("the floor was refused: a run with nothing on disk must get its round: %+v", first)
	}
	admitGrowth(graph, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowOverrun,
		Workspace: tree, Remainder: RemainderDigest("nothing has been written yet"),
	}, first, 1)

	second, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowOverrun, Adding: 1,
		Workspace: tree, Remainder: RemainderDigest("still nothing on disk, in different words"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Allow || second.Cause != CauseStandstill {
		t.Fatalf("a lineage that has changed nothing twice grew again: %+v", second)
	}

	// And the reason reaches the record on the node, where the delivery and the
	// headless stream both read it.
	messages, err := graph.NodeMessages("job-n1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	said := false
	for _, message := range messages {
		if strings.Contains(message.Body, RefusedStandstill) {
			said = true
			if message.Progress == nil || message.Progress.Phase == "" {
				t.Fatalf("the refusal was recorded with nothing for the stream to show: %+v", message)
			}
		}
	}
	if !said {
		t.Fatal("the refusal was never recorded on the work it stopped")
	}
}

// A lineage that IS getting somewhere keeps its rounds. This is the other half
// of the rule and the one that would be broken by a cruder version of it: a
// round that changed files earns the next one, however little it changed.
func TestALineageThatChangedFilesKeepsGrowing(t *testing.T) {
	graph := crowdedJob(t, "s1", 3)
	node := jobNode(t, graph, "job-n1")

	first, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap, Adding: 1,
		Measured: true, Produced: 0, Remainder: RemainderDigest("the first gap"),
	})
	if err != nil || !first.Allow {
		t.Fatalf("first round: %+v %v", first, err)
	}
	admitGrowth(graph, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap,
		Measured: true, Produced: 0, Remainder: RemainderDigest("the first gap"),
	}, first, 1)

	second, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: node, Lineage: "job-n1", Reason: GrowGap, Adding: 1,
		Measured: true, Produced: 2, Remainder: RemainderDigest("a different gap, with two files now written"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Allow {
		t.Fatalf("a lineage that wrote two files was refused its next round: %+v", second)
	}
}

// ofetch s12, in one test. The delivery gate read the tree it was handing over
// and named src/circuit-breaker.ts; the first repair round that finding bought
// asked to be planned; and the coverage question answered "everything this job
// is judged on is already covered", so nothing ran and the run ended partial
// after 145 calls. Two readings of one job, refusing each other in silence.
//
// The first round after a finding that names a file of the record is not the
// coverage question's to refuse — the finding IS a reading of the world, and a
// narrower one.
func TestTheFirstRoundAFindingBuysIsNotRefusedOnCoverage(t *testing.T) {
	graph := crowdedJob(t, "s12", 3)
	criterion := plan.Done{
		Produces:   []string{"the circuit breaker"},
		Conditions: []plan.Check{{Kind: plan.CheckRun, Check: "pnpm test", Expect: "it exits 0"}},
	}
	asked := 0
	gate := SatisfierFunc(func(context.Context, plan.Done, []plan.Landed, []plan.Spec) (plan.Satisfaction, error) {
		asked++
		return plan.Satisfaction{Complete: true}, nil
	})

	verdict, err := growJob(context.Background(), graph, gate, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowGap, Adding: 1,
		Criterion: criterion, Grounded: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatalf("the coverage question was bought for a grounded first round: asked %d times", asked)
	}
	if !verdict.Allow {
		t.Fatalf("the round a review finding bought was refused: %+v", verdict)
	}
}

// And the caps are untouched by it: a grounded round is exempt from the one
// question that contradicts its own evidence and from nothing else.
func TestAGroundedRoundStillObeysTheCaps(t *testing.T) {
	graph := crowdedJob(t, "s12", maxJobNodes)
	verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "job", Node: jobNode(t, graph, "job"), Reason: GrowGap, Adding: 1, Grounded: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Allow || verdict.Cause != CauseCeiling {
		t.Fatalf("a grounded round walked through the ceiling: %+v", verdict)
	}
}
