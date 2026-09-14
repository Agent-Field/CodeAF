package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
)

func TestGateDecisions(t *testing.T) {
	files, _ := filepath.Glob("../../bench/swarm/tasks/*.txt")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		t.Logf("%-30s enumerated=%d divide=%v", filepath.Base(f),
			enumeratedItems(string(b)), divisionWorthIt(string(b)))
	}
}

// THE PLAN DOOR IS WHERE MOST DIVISIONS ARE BORN, so it is where the mode has
// to be readable. These rows walk one graph — three sittings that owe each
// other nothing, under a brief with no digit anywhere in it, which is #418's
// replication — through every value the pin takes.
//
// THE UNPINNED ROW MOVED, AND IT MOVED ON A MEASUREMENT. It used to fold, and
// the fold was the fault #418 reports. A designed experiment then ran four
// planner arms against four readings of this gate over 273 judged plan draws
// and put the arm with the gate off on the front
// (docs/design/plan-gate-doe/REPORT.md), so an unpinned door now keeps what the
// planner drew and the count is something a run asks for.
func TestThePlanDoorFoldsOrKeepsAccordingToTheModePinned(t *testing.T) {
	const narrow = "HANDBOOK.md is one file. Deliver lanes that share no lines: rewrite the headings, link the cross-references, insert a contents section."
	for _, probe := range []struct {
		pin    string
		folded int
		why    string
	}{
		{"", 0, "the gate is off unless somebody pinned it on, so the division stands as drawn"},
		{"0", 0, "the rollback switch has always taken the gate away entirely"},
		{"lanes", 0, "the experiment's counting arm lost and its word now reads as off like any other"},
		{"1", 3, "the shipped gate reads no digit beside a plural and folds, which is the bug #418 reports"},
		{"judgment", 0, "three sittings that owe each other nothing are a real division whatever the brief counted"},
	} {
		t.Run(probe.pin, func(t *testing.T) {
			t.Setenv("AFORGE_SPLITGATE", probe.pin)
			graph := threeIndependentSittings(narrow)
			if got := gatePlanDivision(graph, narrow); got != probe.folded {
				t.Fatalf("AFORGE_SPLITGATE=%q folded %d leaves, want %d — %s", probe.pin, got, probe.folded, probe.why)
			}
			want := 3
			if probe.folded > 0 {
				want = 1
			}
			if got := len(graph.Leaves()); got != want {
				t.Fatalf("AFORGE_SPLITGATE=%q left %d work nodes, want %d", probe.pin, got, want)
			}
		})
	}
}

// AND THE BRIEF #418 OPENS WITH IS KEPT UNLESS THE COUNT IS PINNED ON. Three
// labelled lanes over one file are three people's work and the count reads them
// as nothing; that disagreement is the whole of the issue, and the door now
// only reaches it where a run asked for a floor.
func TestThePlanDoorKeepsTheThreeLaneBriefUnlessTheCountIsPinnedOn(t *testing.T) {
	const labelled = "HANDBOOK.md is one file. Deliver three lanes that share no lines. L1: rewrite every heading. L2: link every cross-reference. L3: insert a contents section."
	for _, probe := range []struct {
		pin    string
		folded int
	}{
		{"", 0},
		{"0", 0},
		{"judgment", 0},
		{"1", 3},
	} {
		t.Run(probe.pin, func(t *testing.T) {
			t.Setenv("AFORGE_SPLITGATE", probe.pin)
			graph := threeIndependentSittings(labelled)
			if got := gatePlanDivision(graph, labelled); got != probe.folded {
				t.Fatalf("AFORGE_SPLITGATE=%q folded %d leaves of the three-lane brief, want %d", probe.pin, got, probe.folded)
			}
		})
	}
}

// AND THE SENTENCE A FOLDED NODE CARRIES DOES NOT MOVE. It is what a person
// reads afterwards to learn why their plan is one node, and #418 quotes it
// verbatim; a change to the wording would make every report of this fault,
// before and after, unsearchable against each other. The pin is on here
// because a fold is now something a run asks for — the sentence is not.
func TestTheFoldedNodeStillSaysWhatItAlwaysSaid(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "1")
	const narrow = "rewrite the handbook in lanes that share no lines"
	graph := threeIndependentSittings(narrow)
	if folded := gatePlanDivision(graph, narrow); folded != 3 {
		t.Fatalf("the pinned gate folded %d leaves, want 3", folded)
	}
	const want = "split gate: goal enumerates 0 items, under the 6-item floor — one sitting"
	if got := graph.Nodes[0].Undivided; got != want {
		t.Errorf("the folded node says %q, want %q", got, want)
	}
}

// threeIndependentSittings is a two-stage plan whose three work nodes are each
// sized a sitting and wait on nothing but the stage before them.
func threeIndependentSittings(goal string) *plan.Graph {
	graph := &plan.Graph{Goal: goal, Stages: []plan.Stage{{Title: "the lanes"}}}
	for _, title := range []string{"headings", "cross-references", "contents"} {
		graph.Add(plan.Node{
			Kind:  plan.KindWork,
			Stage: 1,
			Title: title,
			Size:  plan.SizeAtomic,
			Brief: title,
		})
	}
	return graph
}

// ── the one-shot door's smallness gate ─────────────────────────────────────
//
// Issue #1007's live cell: `aforge do` divided a two-file fix into three task
// nodes, spent its whole token budget on coordination and never settled,
// where one worker did the same job and landed. These pin the gate that cell
// bought: a one-shot errand divides at three genuinely independent parts and
// not below, and it answers to no pin — the gate stands on the `do` door
// because of what that door is, not because somebody asked.

// The cell itself: a two-part ask is one worker doing them in order, however
// the planner draws it. The fold leaves one work node holding the whole goal
// as its brief — the shape `aforge exec` would have given the run.
func TestAOneShotErrandDoesNotDivideATwoPartAsk(t *testing.T) {
	graph := &plan.Graph{
		Goal:   "Fix the nil cursor in intervals.py and the off-by-one in merge.py",
		Stages: []plan.Stage{{Title: "the fixes"}},
	}
	graph.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "nil cursor", Size: plan.SizeAtomic, Brief: "fix intervals.py"})
	graph.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "off-by-one", Size: plan.SizeAtomic, Brief: "fix merge.py"})

	if folded := gateErrandDivision(graph); folded != 2 {
		t.Fatalf("the gate folded %d leaves, want the two it was handed", folded)
	}
	if leaves := graph.Leaves(); len(leaves) != 1 {
		t.Fatalf("the folded plan has %d work nodes, want one worker", len(leaves))
	}
	if got := graph.Nodes[0].Brief; got != graph.Goal {
		t.Fatalf("the one worker's brief is %q, want the whole goal", got)
	}
	if !strings.HasPrefix(graph.Nodes[0].Undivided, "smallness gate:") {
		t.Fatalf("the fold does not say why: %q", graph.Nodes[0].Undivided)
	}
}

// AND THREE INDEPENDENT PARTS FOLD TOO: the floor that called itself
// evidence still passed the measured failure. The errand never divides —
// it is one worker, and the claim-time JIT holds the payer's voice.
func TestAOneShotErrandFoldsEvenThreeIndependentParts(t *testing.T) {
	graph := threeIndependentSittings("caption the twelve image files")
	if folded := gateErrandDivision(graph); folded != 3 {
		t.Fatalf("three independent parts folded %d leaves, want all three", folded)
	}
	if got := len(graph.Leaves()); got != 1 {
		t.Fatalf("the folded plan has %d work nodes, want one worker", got)
	}
}

func TestAOneShotErrandFoldsAChainToo(t *testing.T) {
	graph := &plan.Graph{
		Goal:   "Port the parser, then its tests, then the docs",
		Stages: []plan.Stage{{Title: "the port"}},
	}
	first := graph.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "parser", Size: plan.SizeAtomic, Brief: "port the parser"})
	second := graph.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "tests", Size: plan.SizeAtomic, Brief: "port the tests"})
	third := graph.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "docs", Size: plan.SizeAtomic, Brief: "port the docs"})
	if err := graph.AddNeed(second, first); err != nil {
		t.Fatal(err)
	}
	if err := graph.AddNeed(third, second); err != nil {
		t.Fatal(err)
	}

	if folded := gateErrandDivision(graph); folded != 3 {
		t.Fatalf("a strict chain folded %d leaves, want all three — one independent part is one sitting", folded)
	}
	if got := len(graph.Leaves()); got != 1 {
		t.Fatalf("the folded chain has %d work nodes, want one worker", got)
	}
}

// AND THE GATE STANDS AT THE DOOR THE ERRAND PLANS THROUGH, NOT BESIDE IT.
// The same scripted planner draws the same two independent parts for both
// surfaces; the conversation keeps them, and the one-shot errand runs one
// worker on the whole goal. This is the seam the live cell went through —
// the planner's first reading of the ask, where most divisions are born.
func TestTheErrandDoorFoldsWhatAConversationKeeps(t *testing.T) {
	// The pin-driven gate is stood down for the reason the parts-route tests
	// state: the only gate speaking here must be the one under test.
	t.Setenv("AFORGE_SPLITGATE", "0")
	const goal = "Two things, unrelated: a haiku about the first cold morning, and what the parser vendors charge."
	for _, probe := range []struct {
		name      string
		errand    bool
		wantNodes int
	}{
		{"a conversation keeps the two parts", false, 3},
		{"a one-shot errand runs one worker", true, 1},
	} {
		t.Run(probe.name, func(t *testing.T) {
			graph := openCacheStore(t)
			settings := config.Config{Model: "worker/model", MaxDepth: 1, NodeBudget: 8}
			planner := &partsPlanClient{model: "worker/model", stages: []string{"Answer"}, parts: map[int][]scriptPart{
				1: {{title: "Haiku", summary: "Write the haiku."}, {title: "Vendors", summary: "Price the vendors."}},
			}}
			client := adoptLiveClient(settings, planner.model, planner)
			plans := &jobPlans{graphs: map[string]plannedJob{}}

			subtree, err := planSubtree(settings, client, client, plans, graph, "", probe.errand)(context.Background(), resident.Compiled{
				Goal: goal, Scale: head.ScaleProject,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(subtree.Nodes) != probe.wantNodes {
				t.Fatalf("the plan admitted %d nodes, want %d — %s", len(subtree.Nodes), probe.wantNodes, probe.name)
			}
			if probe.errand && subtree.Nodes[0].Brief != goal {
				t.Fatalf("the one worker's brief is %q, want the whole goal", subtree.Nodes[0].Brief)
			}
		})
	}
}
