package plan

import (
	"strings"
	"testing"
)

// The burden of proof is a prompt before it is code, so the prompt is what is
// asserted first. These are content tests and not wording tests: each phrase
// below is a clause of the directive that would change the judgment if it went
// missing, and none of them is a turn of phrase that a rewrite would be wrong
// to touch.
func TestSizingPromptCarriesTheBurdenOfProof(t *testing.T) {
	prompt := sizePromptWith(Anchors())
	for _, clause := range []struct {
		name string
		want string
	}{
		{"the null hypothesis", "stands whole until a split proves what it buys"},
		{"it is not a last resort", "not a last resort"},
		{"the joint objective", "the time the person waits, what the work costs to run, and how good the\nanswer comes back"},
		{"nodes made for their own sake", "made for its own sake"},
		{"exactly two things discharge it", "Two things and only two things discharge that burden"},
		{"concurrency", "genuinely run at the same time"},
		{"non-convergence", "cannot\nbe brought to an end inside what one worker can hold"},
		{"a sequence discharges neither", "A sequence discharges neither"},
		{"information gain", "sayable more precisely than the node itself"},
		{"its own done-condition", "own condition for being finished"},
		{"narrower wording is not a part", "narrower wording"},
		{"the same answer twice", "bought twice"},
		{"the real price", "re-pays whatever it must\nbe told before it can start"},
		{"scheduling is part of the price", "one more thing to schedule and wait on"},
		{"named width or named non-convergence", "width that can be named"},
		{"thoroughness never pays", "Thoroughness never\nclears it"},
		{"distinguishable done-conditions per piece", "own way of telling that it is finished"},
	} {
		if !strings.Contains(prompt, clause.want) {
			t.Errorf("the sizing prompt does not carry %s: %q is missing", clause.name, clause.want)
		}
	}
}

// The expansion path asks the same question a second time, of one node, and has
// to carry the same burden — that is where nodes get made for their own sake,
// because something was asked to decompose and something comes back decomposed.
func TestExpansionPathCarriesTheBurdenOfProof(t *testing.T) {
	scope := expandScope{Goal: "the whole of it"}
	rendered := scope.render(&Node{Title: "a piece", Summary: "of the whole"})
	for _, want := range []string{
		"one job unless dividing it proves what it buys",
		"the time the person waits, what the work costs to run, and how good the",
		"Only two things buy it",
		"genuinely run at the same time",
		"cannot be carried to an end inside what one worker",
		"Steps that\nfollow one another buy neither",
		"sayable more precisely than this piece is",
		"own condition for being finished",
		"one part with the\nanswer bought twice",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the expansion scope does not carry %q", want)
		}
	}
}

// No examples, and no domain nouns. The rule the whole wave is written under:
// a prompt that every job passes through must state the judgment in the language
// of work and never in the language of one kind of work, because a domain named
// in it is a shape the model will find again whether or not it is there.
func TestBurdenLanguageNamesNoDomain(t *testing.T) {
	subjects := map[string]string{
		"the expansion burden": expandBurden,
		"the sizing burden":    burdenSectionOf(t, sizePromptWith(Anchors())),
	}
	// Nouns from the domains a planner is most often pointed at, plus the two
	// words that introduce an example. Any of them appearing in a passage that
	// states a general judgment is the failure.
	for _, banned := range []string{
		"for example", "e.g.", "such as", "for instance",
		"report", "document", "file", "code", "test", "benchmark", "website",
		"vendor", "region", "dataset", "repository", "paper", "section",
		"pricing", "market", "competitor", "essay", "article", "component",
	} {
		for name, passage := range subjects {
			if strings.Contains(strings.ToLower(passage), banned) {
				t.Errorf("%s names %q — the judgment must be stated in the language of work, not of one kind of work", name, banned)
			}
		}
	}
}

// burdenSectionOf cuts the sizing prompt down to the passage that states the
// judgment, so the no-domain rule is asserted against the new law and not
// against the anchors or the older sections beside it, which carry examples on
// purpose and always have.
func burdenSectionOf(t *testing.T, prompt string) string {
	t.Helper()
	const start = "The node stands whole until"
	const end = "Also return split_into"
	from := strings.Index(prompt, start)
	to := strings.Index(prompt, end)
	if from < 0 || to < 0 || to < from {
		t.Fatalf("the burden section is not where the prompt says it is (%d, %d)", from, to)
	}
	return prompt[from:to]
}

// Grain and burden are complementary and both must be in the prompt at once.
// Alone, each fails in the direction the other guards: follow-the-grain with no
// burden invents seams, and burden with no grain refuses enumerations that were
// already in the material. This is the test that stops a later wave from
// "simplifying" one of them away.
func TestGrainAndBurdenCoexist(t *testing.T) {
	prompt := sizePromptWith(Anchors())
	grain := []string{
		"already\n  enumerate units that stand apart",
		"one unit\n  each, or an even batch of them each when the units are many",
		"Where the material enumerates nothing, there is no split to",
	}
	burden := []string{
		"stands whole until a split proves what it buys",
		"Two things and only two things discharge that burden",
		"Thoroughness never\nclears it",
	}
	for _, want := range append(append([]string{}, grain...), burden...) {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the sizing prompt lost %q — grain and burden are one rule read from two sides", want)
		}
	}
	// And the order is load-bearing: the burden is the standing position, the
	// grain says where to cut once it has been discharged.
	if strings.Index(prompt, burden[0]) > strings.Index(prompt, grain[0]) {
		t.Error("the grain rule precedes the burden it is subject to")
	}
}

// JudgeSplit is the burden as a predicate, and every refusal it can reach must
// come back with words on it. Atomic-and-run is what it answers whenever
// nothing argues for dividing.
func TestJudgeSplitIsAtomicUntilProven(t *testing.T) {
	options := Options{MaxDepth: 3, NodeBudget: 40}
	for _, test := range []struct {
		name   string
		node   Node
		divide bool
		reason string
	}{
		{
			name:   "nothing named, nothing to weigh",
			node:   Node{Kind: KindWork, Size: SizeOversized},
			reason: RefusalUnnamed,
		},
		{
			name:   "one piece named is no split",
			node:   Node{Kind: KindWork, Size: SizeOversized, Parts: []string{"one"}},
			reason: RefusalUnnamed,
		},
		{
			name:   "already within one worker's reach",
			node:   Node{Kind: KindWork, Size: SizeAtomic, Parts: []string{"one", "two"}},
			reason: RefusalWithinReach,
		},
		{
			name:   "unsized is not evidence of anything",
			node:   Node{Kind: KindWork, Parts: []string{"one", "two"}},
			reason: RefusalWithinReach,
		},
		{
			name:   "the ceiling, not a reading of the work",
			node:   Node{Kind: KindWork, Depth: 3, Size: SizeOversized, Parts: []string{"one", "two"}},
			reason: RefusalDepth,
		},
		{
			name:   "named width, and it does not fit one worker",
			node:   Node{Kind: KindWork, Size: SizeOversized, Parts: []string{"one", "two"}},
			divide: true,
		},
		{
			name:   "borderline with named width still qualifies",
			node:   Node{Kind: KindWork, Size: SizeBorderline, Parts: []string{"one", "two"}},
			divide: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := test.node
			verdict := JudgeSplit(&node, options)
			if verdict.Divide != test.divide {
				t.Fatalf("Divide = %v, want %v (reason %q)", verdict.Divide, test.divide, verdict.Reason)
			}
			if verdict.Reason != test.reason {
				t.Fatalf("Reason = %q, want %q", verdict.Reason, test.reason)
			}
			if !verdict.Divide && verdict.Reason == "" {
				t.Fatal("a refusal with no reason is a leaf nobody can explain")
			}
		})
	}
}

func TestJudgeSplitUsesMeasuredCapacityOnlyAtTheAtomicBoundary(t *testing.T) {
	node := Node{Kind: KindWork, Size: SizeAtomic, Parts: []string{"one", "two"}}
	if got := JudgeSplit(&node, Options{MaxDepth: 3}); got.Divide || got.Reason != RefusalWithinReach {
		t.Fatalf("zero capacity changed the atomic verdict: %+v", got)
	}
	if got := JudgeSplit(&node, Options{MaxDepth: 3, CapacitySamples: 7, CapacityOverrunRate: .9}); got.Divide {
		t.Fatalf("thin capacity divided an atomic node: %+v", got)
	}
	if got := JudgeSplit(&node, Options{MaxDepth: 3, CapacitySamples: 8, CapacityOverrunRate: .26}); !got.Divide {
		t.Fatalf("sufficient high overrun history did not divide: %+v", got)
	}
}

// Every refusal at selection is written onto the node it refused, and the
// generalist's ordinary leaf — the one that named no parts — is the one that
// most needs the note, since it is otherwise indistinguishable from a leaf
// nobody ever considered.
func TestSelectionJournalsEveryRefusal(t *testing.T) {
	graph := &Graph{Goal: "the whole of it", NextID: 1}
	unnamed := graph.Add(Node{Kind: KindWork, Size: SizeOversized, State: StatePending})
	small := graph.Add(Node{Kind: KindWork, Size: SizeAtomic, State: StatePending,
		Parts: []string{"one", "two"}})
	deep := graph.Add(Node{Kind: KindWork, Size: SizeOversized, State: StatePending,
		Depth: 2, Parts: []string{"one", "two"}})
	wide := graph.Add(Node{Kind: KindWork, Size: SizeOversized, State: StatePending,
		Parts: []string{"one", "two"}})
	running := graph.Add(Node{Kind: KindWork, Size: SizeOversized, State: StateRunning})

	selected := selectForExpansion(graph, Options{MaxDepth: 2, NodeBudget: 40})
	if len(selected) != 1 || selected[0] != wide {
		t.Fatalf("selected %v, want just the node that carried the burden (%d)", selected, wide)
	}
	for _, want := range []struct {
		id     int
		reason string
	}{
		{unnamed, RefusalUnnamed},
		{small, RefusalWithinReach},
		{deep, RefusalDepth},
		{wide, ""},
		{running, ""},
	} {
		if got := graph.Node(want.id).Undivided; got != want.reason {
			t.Errorf("node %d undivided = %q, want %q", want.id, got, want.reason)
		}
	}
}

// The budget is the other refusal, and it is journaled too — with words that
// say it was arithmetic, so a reader does not mistake a ceiling for a reading
// of the material.
func TestBudgetRefusalIsJournaled(t *testing.T) {
	graph := &Graph{Goal: "the whole of it", NextID: 1}
	var ids []int
	for count := 0; count < 3; count++ {
		ids = append(ids, graph.Add(Node{Kind: KindWork, Size: SizeOversized,
			State: StatePending, Parts: []string{"one", "two"}}))
	}
	// Room for one expansion and no more.
	selected := selectForExpansion(graph, Options{MaxDepth: 2, NodeBudget: len(graph.Nodes) + 4})
	if len(selected) != 1 {
		t.Fatalf("selected %d nodes, want 1", len(selected))
	}
	refused := 0
	for _, id := range ids {
		if graph.Node(id).Undivided == RefusalNoRoom {
			refused++
		}
	}
	if refused != len(ids)-1 {
		t.Fatalf("%d nodes journaled the budget refusal, want %d", refused, len(ids)-1)
	}
}

// A node that is divided after all carries no refusal: the field describes the
// shape the graph has, never the shapes it considered.
func TestSplicingClearsTheRefusal(t *testing.T) {
	client := &stubClient{reply: func(system, _ string) string {
		switch {
		case strings.Contains(system, "You list the parts of one stage"):
			return `{"parts":[
				{"title":"First","summary":"Own the first units","sources":["a"]},
				{"title":"Second","summary":"Own the second units","sources":["b"]}]}`
		default:
			return `{"sizes":[
				{"node":1,"size":"atomic","split_into":[]},
				{"node":2,"size":"atomic","split_into":[]},
				{"node":3,"size":"atomic","split_into":[]}]}`
		}
	}}

	graph := &Graph{Goal: "the whole of it", Stages: []Stage{{Title: "Only"}}, NextID: 1}
	parent := graph.Add(Node{Stage: 1, Title: "Wide", Summary: "It enumerates its units",
		Size: SizeOversized, Parts: []string{"first", "second"}})
	graph.Node(parent).Undivided = RefusalUnnamed

	spliced, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40})
	if err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if spliced != 1 {
		t.Fatalf("spliced %d, want 1 — the split carried its burden", spliced)
	}
	if got := graph.Node(parent).Undivided; got != "" {
		t.Errorf("a divided node still carries the refusal %q", got)
	}
}
