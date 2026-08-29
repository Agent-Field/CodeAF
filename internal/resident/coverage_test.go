package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// alwaysCovered is the reading that refused three of ink v4-flash s14's rounds:
// a model told the plan's acceptance points are all mapped onto work that has
// landed, over a tree the world had a great deal to say about.
var alwaysCovered = SatisfierFunc(func(context.Context, plan.Done, []plan.Landed, []plan.Spec) (plan.Satisfaction, error) {
	return plan.Satisfaction{Complete: true}, nil
})

// judgedJob is a job with a criterion to be covered against, and a repair round
// spliced beside it — the shape both stores end in.
func judgedJob(t *testing.T) *store.Store {
	t.Helper()
	graph := inkJob(t, t.TempDir())
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2-x1", Brief: "finish the grid"},
		{ID: "task-2-x1-n1", Parent: "task-2-x1", Brief: "a piece"},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "make the grid lay out"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

func coverageVerdict(t *testing.T, graph *store.Store, lineage, reason string, round int) GrowVerdict {
	t.Helper()
	node := jobNode(t, graph, "task-2-x1")
	verdict, err := growJob(context.Background(), graph, alwaysCovered, GrowRequest{
		JobRoot: "task-2-x1", Node: node, Lineage: lineage, Reason: reason, Round: round,
		Criterion: plan.Done{Produces: []string{"a grid that lays out"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return verdict
}

// COVERAGE IS A CLAIM ABOUT THE PLAN; A FINDING IS EVIDENCE FROM THE WORLD.
//
// ink v4-flash s14 refused an overrun round, a revision round and a gap round as
// goal-already-covered while its own readings ran 172 checks with 18 red. No
// round of any reason may be refused that way while the world has something
// standing.
func TestCoverageCannotRefuseWhileAReadingIsRed(t *testing.T) {
	graph := judgedJob(t)

	// With nothing standing, the coverage question is still the one that can
	// say there is nothing left to do.
	if verdict := coverageVerdict(t, graph, "task-2", GrowOverrun, 2); verdict.Allow ||
		verdict.Cause != CauseCovered {
		t.Fatalf("coverage may still refuse a job the world says nothing about: %+v", verdict)
	}

	// And then the world speaks: eighteen of the project's own checks are red.
	if err := graph.RecordVerification("task-2-x1-n1", store.VerificationReading{
		When: "on the finished tree", Command: "npm test", Read: true,
		Named: 172, Red: 18, Sample: []string{"grid lays out box children"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, reason := range []string{GrowOverrun, GrowRevision, GrowGap} {
		for _, round := range []int{1, 2, 3} {
			verdict := coverageVerdict(t, graph, "task-2", reason, round)
			if !verdict.Allow {
				t.Fatalf("%s round %d refused over red checks: %+v", reason, round, verdict)
			}
			if len(verdict.CoveredDespite) == 0 ||
				verdict.CoveredDespite[0] != EvidenceFailing {
				t.Fatalf("the disagreement was not recorded: %+v", verdict.CoveredDespite)
			}
		}
	}

	// And the admission carries it into the journal, where an autopsy reads it.
	node := jobNode(t, graph, "task-2-x1")
	request := GrowRequest{JobRoot: "task-2-x1", Node: node, Lineage: "task-2", Reason: GrowGap, Round: 2}
	admitGrowth(graph, request, GrowVerdict{Allow: true, Round: 2,
		CoveredDespite: []string{EvidenceFailing}}, 1)
	rounds, err := graph.JobGrowthRounds("task-2")
	if err != nil {
		t.Fatal(err)
	}
	last := rounds[len(rounds)-1]
	if len(last.CoveredDespite) != 1 || last.CoveredDespite[0] != EvidenceFailing {
		t.Fatalf("covered_despite is not in the journal: %+v", last)
	}
}

// igel v4-flash s13: a revision round weighed under `task-2-x1`, whose id
// namespace does not contain `task-2-x1-n1` — which is where the eight deleted
// public names had been journaled ninety seconds earlier. The job has one world.
func TestCoverageReadsTheJobsWorldAndNotTheAskingLineages(t *testing.T) {
	graph := judgedJob(t)
	if err := graph.RecordSurface("task-2-x1-n1", store.SurfaceReading{
		Compared: 3, Lost: 8, Names: []string{"Igel.default_model_path", "Igel.results_path"},
	}); err != nil {
		t.Fatal(err)
	}
	verdict := coverageVerdict(t, graph, "task-2-x1", GrowRevision, 1)
	if !verdict.Allow {
		t.Fatalf("a revision round was refused over a world it could not see: %+v", verdict)
	}
	if len(verdict.CoveredDespite) != 1 || verdict.CoveredDespite[0] != EvidenceLost {
		t.Fatalf("covered despite = %v, want the deleted public names", verdict.CoveredDespite)
	}
}

// The other kinds the record holds, each on its own: a behaviour nothing
// exercises, a check that names one and asserts nothing, callers a reshaped
// definition left behind, and a file the plan promised that the disk does not
// hold.
func TestEveryKindOfStandingEvidenceStopsACoverageRefusal(t *testing.T) {
	for _, probe := range []struct {
		kind string
		gate store.DeliveryGate
	}{
		{EvidenceUnexercised, store.DeliveryGate{Gap: "nothing exercises it",
			Unexercised: []string{"the display style property accepts \"grid\""}}},
		{EvidenceUnasserted, store.DeliveryGate{Gap: "nothing weighs it",
			Unasserted: []string{"gridTemplateColumns accepts fr units"}}},
		{EvidenceConsumers, store.DeliveryGate{Gap: "its callers were left behind",
			Consumers: []string{"Igel.configs is now a class and six callers index it"}}},
		{EvidenceMechanical, store.DeliveryGate{Gap: "src/grid.ts is not on disk",
			Mechanical: true, Unclosed: true}},
	} {
		t.Run(probe.kind, func(t *testing.T) {
			graph := judgedJob(t)
			if err := graph.RecordDeliveryGate("task-2-x1", probe.gate); err != nil {
				t.Fatal(err)
			}
			verdict := coverageVerdict(t, graph, "task-2", GrowOverrun, 3)
			if !verdict.Allow {
				t.Fatalf("a round was refused over %s: %+v", probe.kind, verdict)
			}
			if !strings.Contains(strings.Join(verdict.CoveredDespite, " "), probe.kind) {
				t.Fatalf("covered despite = %v, want %s", verdict.CoveredDespite, probe.kind)
			}
		})
	}
}
