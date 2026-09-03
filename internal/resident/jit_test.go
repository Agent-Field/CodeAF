package resident

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// dividerClient is a planning client that answers a fan-out with two parts and
// a sizing pass with whatever the test wants those parts to be. Every call is
// counted, because the cheapest claim on this wave — that a node the plan
// already judged atomic costs nothing extra — is a claim about this number.
type dividerClient struct {
	calls atomic.Int32
	// size is what every child comes back as. Atomic ends the recursion;
	// borderline with named parts continues it forever, which is what the
	// governor has to stop.
	size string
	// round advances the child titles so a second division under the same job
	// does not hand two children the same deliverable.
	round atomic.Int32
}

func (c *dividerClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.calls.Add(1)
	var system string
	for _, message := range messages {
		if message.Role == "system" {
			for _, part := range message.Content {
				system += part.Text
			}
		}
	}
	var body string
	if strings.Contains(system, "You list the parts of one stage") {
		round := c.round.Add(1)
		body = fmt.Sprintf(`{"parts":[
			{"title":"Left %d","summary":"Own the left half of round %d","sources":["l%d"]},
			{"title":"Right %d","summary":"Own the right half of round %d","sources":["r%d"]}]}`,
			round, round, round, round, round, round)
	} else {
		size := c.size
		if size == "" {
			size = "atomic"
		}
		parts := `[]`
		if size != "atomic" {
			parts = `["one","two"]`
		}
		sizes := make([]string, 0, 8)
		for id := 1; id <= 8; id++ {
			sizes = append(sizes, fmt.Sprintf(`{"node":%d,"size":%q,"split_into":%s}`, id, size, parts))
		}
		body = `{"sizes":[` + strings.Join(sizes, ",") + `]}`
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: body}}}, FinishReason: "stop"}},
		Usage: &ai.Usage{PromptTokens: 1, CompletionTokens: 1},
	}, nil
}

// dividableJob is one landed dependency and one claimed node over it, with a
// plan document whose node 2 is the claimed one and is judged divisible.
func dividableJob(t *testing.T, size plan.Size, parts []string) (*store.Store, *plan.Graph) {
	t.Helper()
	graph := residentTestStore(t, "jit.db")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole of it", Title: "The job", Stage: 1},
		{ID: "job-n1", Parent: "job", Brief: "gather the units", Title: "Gather", Stage: 1},
		{ID: "job-n2", Parent: "job", Brief: "write a note per unit", Title: "Write the notes", Stage: 1,
			Needs: []store.Need{{NodeID: "job-n1", Kind: store.FeedsInto}},
			Spec: EncodeSpec(plan.Spec{Instruction: "write a note per unit", Method: "work unit by unit",
				Done: plan.Done{Produces: []string{"a note per unit"}}})},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s", Intent: "note every unit"}); err != nil {
		t.Fatal(err)
	}
	land(t, graph, "job-n1", "Fourteen units, listed by name in units.txt")

	document := &plan.Graph{Goal: "note every unit", Stages: []plan.Stage{{Title: "Only"}}, NextID: 1}
	document.Add(plan.Node{Stage: 1, Title: "Gather", Summary: "Gather the units",
		Kind: plan.KindWork, Size: plan.SizeAtomic, State: plan.StateDone})
	document.Add(plan.Node{Stage: 1, Title: "Write the notes", Summary: "Write a note per unit",
		Kind: plan.KindWork, Size: size, Parts: parts, Needs: []int{1}})
	return graph, document
}

func land(t *testing.T, graph *store.Store, id, summary string) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "test")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}
}

// claim takes a node the way the runner takes it, including the two fields the
// runner fills in by hand and the expander needs to hand the claim back.
func claim(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	held, ok, err := graph.Claim(id, "runner")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(held); err != nil {
		t.Fatal(err)
	}
	node := jobNode(t, graph, id)
	node.Owner, node.ClaimToken = held.Owner, held.Token
	return node
}

func expanderOver(graph *store.Store, document *plan.Graph, client plan.Completer) JITExpander {
	return JITExpander{
		Graph: graph,
		Resolve: func(node store.Node) (JITTarget, bool) {
			cut := strings.LastIndex(node.ID, "-n")
			if cut < 0 {
				return JITTarget{}, false
			}
			var planID int
			if _, err := fmt.Sscanf(node.ID[cut+2:], "%d", &planID); err != nil {
				return JITTarget{}, false
			}
			return JITTarget{Plan: document, Prefix: node.ID[:cut], PlanNode: planID,
				Client: client, Options: plan.Options{MaxDepth: 3, NodeBudget: 60}}, true
		},
	}
}

// The wave. A claimed node the plan judged too big for one worker is divided
// where it stands: its parts land under it, the claim goes back, and what the
// node itself is asked to do becomes assembling what they produced.
func TestAClaimedNodeThatIsNotOneJobIsDividedWhereItStands(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	client := &dividerClient{size: "atomic"}
	node := claim(t, graph, "job-n2")

	spliced, expanded := expanderOver(graph, document, client).Expand(context.Background(), node)
	if !expanded || spliced != 2 {
		t.Fatalf("spliced %d expanded %t, want 2 and true", spliced, expanded)
	}
	// Two calls and no more: a fan-out and a sizing pass, exactly what a build
	// level would have spent on the same node.
	if got := client.calls.Load(); got != 2 {
		t.Fatalf("the division cost %d calls, want 2", got)
	}

	parent := jobNode(t, graph, "job-n2")
	if parent.Status != store.Pending {
		t.Fatalf("the divided node is %s, want the claim handed back", parent.Status)
	}
	if !strings.HasPrefix(parent.Brief, gatheringPreamble) {
		t.Fatalf("the divided node still carries its old assignment: %.80q", parent.Brief)
	}
	if !strings.Contains(parent.Brief, "write a note per unit") {
		t.Error("the gathering step forgot what the work was asked for")
	}

	children := childrenOf(t, graph, "job-n2")
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2", len(children))
	}
	for _, child := range children {
		if child.Status != store.Pending {
			t.Errorf("child %s is %s", child.ID, child.Status)
		}
		// The division's entries consume what the node itself consumed: that is
		// the whole reason the question was worth moving to claim time.
		if !feeds(t, graph, "job-n1", child.ID) {
			t.Errorf("child %s does not receive what the node's own input produced", child.ID)
		}
		if !feeds(t, graph, child.ID, "job-n2") {
			t.Errorf("child %s does not feed the step it came out of", child.ID)
		}
		if resolved := DecodeSpec(child.Spec); resolved.Method != "work unit by unit" {
			t.Errorf("child %s lost the working method: %+v", child.ID, resolved)
		}
	}

	// The plan document is the graph's own record of the same fact.
	if divided := document.Node(2); divided.Kind != plan.KindSynthesis {
		t.Errorf("the plan still calls the divided node %q", divided.Kind)
	}
	if depth := document.Node(3).Depth; depth != 1 {
		t.Errorf("the parts sit at depth %d, want 1", depth)
	}

	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 || growths[0].Reason != GrowJIT || !growths[0].Allowed {
		t.Fatalf("the journal cannot say a division grew this job: %+v", growths)
	}
}

// The parts run at the same time, which is the only reason to have made them.
// The node they came out of does not run until they are done.
func TestThePartsOfADivisionAreClaimableTogetherAndTheirParentIsNot(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	node := claim(t, graph, "job-n2")
	if _, expanded := expanderOver(graph, document, &dividerClient{size: "atomic"}).
		Expand(context.Background(), node); !expanded {
		t.Fatal("the node was not divided")
	}

	ready, err := graph.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	eligible := map[string]bool{}
	for _, candidate := range ready {
		eligible[candidate.ID] = true
	}
	for _, child := range childrenOf(t, graph, "job-n2") {
		if !eligible[child.ID] {
			t.Errorf("%s is not eligible, so the parts do not overlap", child.ID)
		}
	}
	if eligible["job-n2"] {
		t.Error("the divided node is claimable while its parts have not run")
	}

	// And once they have, it is — as the gathering step it now is.
	for _, child := range childrenOf(t, graph, "job-n2") {
		land(t, graph, child.ID, "one half, noted")
	}
	ready, err = graph.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	gathering := false
	for _, candidate := range ready {
		if candidate.ID == "job-n2" {
			gathering = true
		}
	}
	if !gathering {
		t.Error("the gathering step never became claimable after its parts landed")
	}
}

// The null hypothesis, and the cost of it. A node the plan already judged
// atomic is not asked a second question, is not touched, and costs nothing —
// which is what makes the common path free.
func TestAnAtomicNodeIsNotAskedAgainAndCostsNothing(t *testing.T) {
	for _, test := range []struct {
		name  string
		size  plan.Size
		parts []string
	}{
		{"atomic", plan.SizeAtomic, []string{"first", "second"}},
		// Borderline rather than oversized, and the size is the whole point.
		// An OVERSIZED node nobody could name two pieces for is no longer a
		// free refusal at claim time: the burden's second discharge stands for
		// it, so it is asked the stage question — one call, no fan-out — and
		// divided in time or refused with the answer. The free refusal is what
		// is left, and this is it: a node inside one worker's reach that names
		// no pieces is not asked anything.
		{"no two parts could be named", plan.SizeBorderline, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph, document := dividableJob(t, test.size, test.parts)
			client := &dividerClient{}
			node := claim(t, graph, "job-n2")

			spliced, expanded := expanderOver(graph, document, client).Expand(context.Background(), node)
			if expanded || spliced != 0 {
				t.Fatalf("spliced %d expanded %t, want nothing", spliced, expanded)
			}
			if got := client.calls.Load(); got != 0 {
				t.Fatalf("the common path cost %d model calls, want 0", got)
			}
			if after := jobNode(t, graph, "job-n2"); after.Status != store.Running {
				t.Fatalf("the node was disturbed: %s", after.Status)
			}
			if children := childrenOf(t, graph, "job-n2"); len(children) != 0 {
				t.Fatalf("a refused division left %d children behind", len(children))
			}
			// The refusal is on the record, so a finished graph can say why
			// each of its leaves is one.
			if document.Node(2).Undivided == "" {
				t.Error("nothing says why the node was left whole")
			}
		})
	}
}

// A division whose parts came back no smaller bought nothing, so the node runs
// whole — the same answer the build's own acceptance check gives.
func TestADivisionThatShrinksNothingLeavesTheNodeWhole(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	client := &dividerClient{size: "oversized"}
	node := claim(t, graph, "job-n2")

	spliced, expanded := expanderOver(graph, document, client).Expand(context.Background(), node)
	if expanded || spliced != 0 {
		t.Fatalf("spliced %d expanded %t, want nothing", spliced, expanded)
	}
	if after := jobNode(t, graph, "job-n2"); after.Status != store.Running {
		t.Fatalf("a refused division did not leave the claim alone: %s", after.Status)
	}
	if got := document.Node(2).Undivided; got != plan.RefusalNoSmaller {
		t.Errorf("undivided = %q, want %q", got, plan.RefusalNoSmaller)
	}
}

// Recursion ends where atomicity does. A part that comes back one worker's job
// is dispatched, not divided again — the depth is decided by the reading and
// not by a constant.
func TestRecursionStopsWhenThePartsAreOneJobEach(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	client := &dividerClient{size: "atomic"}
	expander := expanderOver(graph, document, client)
	if _, expanded := expander.Expand(context.Background(), claim(t, graph, "job-n2")); !expanded {
		t.Fatal("the node was not divided")
	}
	before := client.calls.Load()

	for _, child := range childrenOf(t, graph, "job-n2") {
		if _, expanded := expander.Expand(context.Background(), claim(t, graph, child.ID)); expanded {
			t.Fatalf("%s was divided again after being judged one job", child.ID)
		}
	}
	if got := client.calls.Load(); got != before {
		t.Fatalf("re-judging atomic parts cost %d calls", got-before)
	}
}

// The governor is the backstop under the judgment, and this is the case it is
// there for: a reading that says "divide" forever. Depth is bounded by the job
// ceiling, not by anyone remembering to stop.
func TestAJudgmentThatAlwaysDividesIsStoppedByTheJobCeiling(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	client := &dividerClient{size: "borderline"}
	expander := expanderOver(graph, document, client)

	// The frontier is whatever was divided last; dividing its parts in turn is
	// exactly the runaway the ceiling has to stop.
	frontier := []string{"job-n2"}
	rounds := 0
	for len(frontier) > 0 && rounds < 500 {
		next := []string{}
		for _, id := range frontier {
			node := claim(t, graph, id)
			spliced, expanded := expander.Expand(context.Background(), node)
			if !expanded {
				// The node runs whole. Hand the claim back so the graph is left
				// the way the runner would leave it.
				if err := graph.Release(store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}); err != nil {
					t.Fatal(err)
				}
				continue
			}
			rounds++
			_ = spliced
			for _, child := range childrenOf(t, graph, id) {
				next = append(next, child.ID)
			}
		}
		frontier = next
	}
	if rounds >= 500 {
		t.Fatal("a judgment that always divides never stopped")
	}

	nodes, err := graph.SubtreeNodes("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) > maxJobNodes {
		t.Fatalf("the job grew to %d nodes, past the ceiling of %d", len(nodes), maxJobNodes)
	}
	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	for _, growth := range growths {
		if !growth.Allowed && growth.Cause == CauseCeiling {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("nothing in the journal says a governor stopped it: %d growths", len(growths))
	}
	// And the refusal was not announced as a handover: the node it refused runs
	// whole, immediately, on the worker already holding it.
	for _, node := range nodes {
		messages, err := graph.NodeMessages(node.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if strings.Contains(message.Body, "as large as jobs are allowed to grow") {
				t.Fatalf("%s was told work was being handed over when it was only run whole", node.ID)
			}
		}
	}
}

// A node nobody is holding is a node this must not touch: dividing it would
// leave whoever is running it writing a result into structure.
func TestAnUnclaimedNodeIsNeverDivided(t *testing.T) {
	graph, document := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	client := &dividerClient{size: "atomic"}
	node := jobNode(t, graph, "job-n2")

	if spliced, expanded := expanderOver(graph, document, client).Expand(context.Background(), node); expanded || spliced != 0 {
		t.Fatalf("an unclaimed node was divided: spliced=%d expanded=%t", spliced, expanded)
	}
	if got := client.calls.Load(); got != 0 {
		t.Fatalf("an unclaimed node cost %d calls", got)
	}
}

// A job with no retained plan runs its nodes whole, which is what every node
// did before this existed.
func TestANodeWithNoPlanRunsWhole(t *testing.T) {
	graph, _ := dividableJob(t, plan.SizeOversized, []string{"first", "second"})
	expander := JITExpander{Graph: graph, Resolve: func(store.Node) (JITTarget, bool) {
		return JITTarget{}, false
	}}
	if spliced, expanded := expander.Expand(context.Background(), claim(t, graph, "job-n2")); expanded || spliced != 0 {
		t.Fatalf("spliced=%d expanded=%t, want nothing", spliced, expanded)
	}
}

// The runner's half of the contract: a node that turned into structure is not
// handed to a worker, and the slot it was holding goes back.
func TestTheRunnerDoesNotWorkANodeThatJustBecameStructure(t *testing.T) {
	graph := openRunnerStore(t)
	spliceChain(t, graph)

	var worked atomic.Int32
	var divided atomic.Int32
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		worked.Add(1)
		return ExecResult{Summary: "done"}, nil
	}, "runner", 2).WithExpand(func(_ context.Context, node store.Node) (int, bool) {
		divided.Add(1)
		// The hook owns the claim once it says yes, exactly as the real one does.
		if err := graph.Release(store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}); err != nil {
			t.Errorf("release %s: %v", node.ID, err)
		}
		return 2, true
	})

	dispatched, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if dispatched != 0 {
		t.Fatalf("dispatched %d nodes that had become structure", dispatched)
	}
	if divided.Load() != 1 {
		t.Fatalf("the division was consulted %d times, want once", divided.Load())
	}
	if worked.Load() != 0 {
		t.Fatal("a node that became structure was still handed to a worker")
	}
	if node := jobNode(t, graph, "first"); node.Status != store.Pending {
		t.Fatalf("the divided node is %s, want the claim handed back", node.Status)
	}
	// The slot came back with it: the next pass can still claim.
	if free := cap(runner.slots) - len(runner.slots); free != cap(runner.slots) {
		t.Fatalf("%d of %d slots are still held", cap(runner.slots)-free, cap(runner.slots))
	}
}

// And with no hook installed, nothing about dispatch changes.
func TestWithNoDivisionInstalledDispatchIsUnchanged(t *testing.T) {
	graph := openRunnerStore(t)
	spliceChain(t, graph)

	var worked atomic.Int32
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		worked.Add(1)
		return ExecResult{Summary: "done"}, nil
	}, "runner", 2)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if worked.Load() != 1 {
		t.Fatalf("worked %d nodes, want the one that was ready", worked.Load())
	}
}

func childrenOf(t *testing.T, graph *store.Store, parent string) []store.Node {
	t.Helper()
	nodes, err := graph.SubtreeNodes(parent)
	if err != nil {
		t.Fatal(err)
	}
	children := make([]store.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Parent == parent {
			children = append(children, node)
		}
	}
	return children
}

func feeds(t *testing.T, graph *store.Store, from, to string) bool {
	t.Helper()
	edges, err := graph.Edges()
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == store.FeedsInto {
			return true
		}
	}
	return false
}

// A division's claimable parts are admitted cheapest-predicted first when the
// capacity fold has measured evidence, and left in the expander's own order
// when it does not — the fifo invariant a measurement could only have perturbed.
func TestOrderAdmitsTheCheapestPredictedSiblingFirst(t *testing.T) {
	// Three independent parts, sized small to large in the order the expander
	// handed them. With evidence the cheapest is admitted first; without it the
	// handed order is returned untouched.
	children := []plan.Node{
		{ID: 1, Size: plan.SizeAtomic, Title: "atomic"},
		{ID: 2, Size: plan.SizeOversized, Title: "oversized"},
		{ID: 3, Size: plan.SizeBorderline, Title: "borderline"},
	}
	evidence := plan.Options{CapacitySamples: 8, CapacityOverrunRate: .5}
	got := order(children, evidence)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 3 || got[2].ID != 2 {
		t.Fatalf("evidence order = %v, want atomic(1) borderline(3) oversized(2)", titles(got))
	}
	// No evidence: the order is the one the expander handed.
	if got := order(children, plan.Options{}); len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatalf("no-evidence order = %v, want the handed order 1 2 3", titles(got))
	}
}

func TestOrderRespectsDependenciesBeforeCost(t *testing.T) {
	// The cheapest part depends on the costliest one, so the costliest must be
	// admitted first — cost only orders among siblings already free to run.
	children := []plan.Node{
		{ID: 1, Size: plan.SizeAtomic, Needs: []int{2}, Title: "atomic"},
		{ID: 2, Size: plan.SizeOversized, Title: "oversized"},
	}
	evidence := plan.Options{CapacitySamples: 8, CapacityOverrunRate: .5}
	got := order(children, evidence)
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("order = %v, want oversized(2) before its atomic dependent(1)", titles(got))
	}
}

func TestOrderKeepsFifoAmongEquallySizedSiblings(t *testing.T) {
	// The base rate is the same for every node of one model, so siblings the
	// planner sized equally tie and keep the order the expander handed.
	children := []plan.Node{
		{ID: 1, Size: plan.SizeAtomic, Title: "first"},
		{ID: 2, Size: plan.SizeAtomic, Title: "second"},
		{ID: 3, Size: plan.SizeAtomic, Title: "third"},
	}
	evidence := plan.Options{CapacitySamples: 8, CapacityOverrunRate: .9}
	got := order(children, evidence)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatalf("order = %v, want the handed order among equal costs", titles(got))
	}
}

func titles(nodes []plan.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Title
	}
	return out
}
