package plan

import (
	"strings"
	"testing"
)

// THE RULE IS THE FIRST THING A WORKER READS, AND IT IS THE PERSON'S OWN
// SENTENCE. A rule below the assignment is a rule the assignment has already
// argued with, which is the whole of what #427 measured: "Change no files"
// reached the run inside a paragraph of goal, and the work that closed a
// reviewer's finding wrote two files over the top of it.
func TestTheRulesThePersonSetAreTheFirstThingASpecRenders(t *testing.T) {
	spec := Spec{
		Instruction: "run the named command and report its final line",
		Method:      "read what exists before writing anything",
		Constraints: []Constraint{{Text: "Change no files.", Kind: ConstraintNoWrites}},
	}
	rendered := spec.Render(0)
	if !strings.HasPrefix(rendered, ConstraintsHeading+":\n- Change no files.") {
		t.Fatalf("the rules are not the first thing in the render:\n%s", rendered)
	}
	if strings.Index(rendered, "Change no files.") > strings.Index(rendered, "How this kind of work") {
		t.Fatalf("the method was read before the rule:\n%s", rendered)
	}
	// Verbatim, not paraphrased: the gate quotes this text back at the person
	// and may only ever quote what they wrote.
	if !strings.Contains(rendered, "Change no files.") {
		t.Fatalf("the rule was not carried word for word:\n%s", rendered)
	}
	// And a spec that carries nothing but a rule is not an empty spec, or the
	// rule would render to nothing at all.
	rule := Spec{Constraints: spec.Constraints}
	if rule.Empty() || rule.Render(0) == "" {
		t.Fatalf("a spec carrying only a rule reads as empty: %q", rule.Render(0))
	}
}

// A CONSTRAINT IS A PROPERTY OF THE JOB AND THE CHECKLIST IS A PROPERTY OF THE
// DELIVERY, and that is the one way the two stampings differ. The person said
// "change no files" about the run, so every worker the run starts is under it;
// they did not ask a contributing worker for the whole request's behaviours.
func TestSetConstraintsStampsEveryNodeAndTheChecklistDoesNot(t *testing.T) {
	graph := &Graph{Nodes: []Node{
		{ID: 1, Stage: 1, Kind: KindWork, Title: "Gather A", Brief: "gather a"},
		{ID: 2, Stage: 1, Kind: KindWork, Title: "Gather B", Brief: "gather b"},
		{ID: 3, Stage: 2, Kind: KindSynthesis, Title: "Report", Brief: "write it", Needs: []int{1, 2}},
	}}
	rules := []Constraint{{Text: "Change no files.", Kind: ConstraintNoWrites}}
	graph.SetConstraints(rules)
	graph.SetAcceptance([]Point{{Behaviour: "the final line is reported", Quote: "report the final line"}})

	stamped := 0
	for _, node := range graph.Nodes {
		if len(node.Spec.Constraints) == 1 && node.Spec.Constraints[0].Text == "Change no files." {
			stamped++
		}
	}
	if stamped != len(graph.Nodes) {
		t.Fatalf("the rule reached %d of %d nodes", stamped, len(graph.Nodes))
	}
	held := 0
	for _, node := range graph.Nodes {
		if len(node.Spec.Accept) > 0 {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("the checklist reached %d nodes, want the one that delivers", held)
	}
	// An empty list changes nothing, which is the whole compatibility story: a
	// job whose request stated no rule is the job this system already ran.
	clean := &Graph{Nodes: []Node{{ID: 1}}}
	clean.SetConstraints(nil)
	if !clean.Nodes[0].Spec.Empty() {
		t.Fatalf("no rules still wrote something onto the spec: %+v", clean.Nodes[0].Spec)
	}
}

// What normalization is for: a rule that cannot be READ is dropped rather than
// refused, and a reading that cannot be HELD falls back to the weaker one. Both
// directions are fail-safe, because the failure they guard is a delivery failed
// by arithmetic over a rule nobody could settle.
func TestNormalizeConstraintsKeepsOnlyWhatCanBeRead(t *testing.T) {
	clean := NormalizeConstraints([]Constraint{
		{Text: "  Change no files.  ", Kind: "NO_WRITES", Paths: []string{"ignored"}},
		{Text: "", Kind: ConstraintNoWrites},
		{Text: "Only touch ./docs/", Kind: ConstraintPathsOnly, Paths: []string{" ./docs/ ", ""}},
		{Text: "Only these", Kind: ConstraintPathsOnly},
		{Text: "Do not use the network.", Kind: "no-network"},
		{Text: "change no files.", Kind: ConstraintNoWrites},
	})
	if len(clean) != 4 {
		t.Fatalf("normalize kept %d rules: %+v", len(clean), clean)
	}
	if clean[0].Text != "Change no files." || clean[0].Kind != ConstraintNoWrites || clean[0].Paths != nil {
		t.Fatalf("the no-writes rule was not read: %+v", clean[0])
	}
	if clean[1].Kind != ConstraintPathsOnly || len(clean[1].Paths) != 1 || clean[1].Paths[0] != "docs" {
		t.Fatalf("the paths were not cleaned to what the record spells: %+v", clean[1])
	}
	// "only these paths" with no paths reads mechanically as "no paths at all",
	// which would fail every delivery of a job whose model forgot the list.
	if clean[2].Kind != ConstraintOther {
		t.Fatalf("a paths-only rule with no paths is still held mechanically: %+v", clean[2])
	}
	// An unrecognised kind is a rule a reader settles, never one no reader sees.
	if clean[3].Kind != ConstraintOther || clean[3].Text != "Do not use the network." {
		t.Fatalf("an unknown reading did not fall back to other: %+v", clean[3])
	}
	if NormalizeConstraints(nil) != nil {
		t.Fatal("no rules normalized into something")
	}
}

// The two headings are one law said to two audiences, and RulesAbove is where
// the placement is decided for every renderer that is not the spec's own.
func TestRulesAboveKeepsTheRuleInFrontOfTheOrder(t *testing.T) {
	rules := []Constraint{{Text: "Change no files.", Kind: ConstraintNoWrites}}
	above := RulesAbove(rules, "a reviewer found gaps that must be closed")
	if !strings.HasPrefix(above, ConstraintsHeading+":\n- Change no files.\n\na reviewer") {
		t.Fatalf("the rule is not above the order:\n%s", above)
	}
	outranking := RulesOutranking(rules, "Write the check for each, and make it pass.")
	if !strings.HasPrefix(outranking, ConstraintsOutrankHeading+":") {
		t.Fatalf("the repair round was not told which of the two wins:\n%s", outranking)
	}
	// No rules is the ordinary case and must send the bytes it always sent.
	if RulesAbove(nil, "unchanged") != "unchanged" {
		t.Fatal("a job with no rules had its order rewritten")
	}
}

// DIVIDING A NODE IS NOT HOW A JOB WALKS OUT FROM UNDER ITS RULES. An expansion
// mints a fresh sub-graph and inherits the settled points, the terrain and the
// prices from the graph it came out of; the rules the person set come down with
// them, or a job under "change no files" acquires the permission to write the
// moment one of its nodes turns out to be big enough to divide.
func TestAnExpandedSubtreeInheritsTheRulesItsParentWasUnder(t *testing.T) {
	rules := []Constraint{{Text: "Change no files.", Kind: ConstraintNoWrites}}
	graph, parent := claimableGraph()
	graph.SetConstraints(rules)

	sub, _, err := ExpandOne(t.Context(), &capturingClient{reply: splitReply}, graph, parent,
		Options{MaxDepth: 4, NodeBudget: 40}, ClaimContext{})
	if err != nil {
		t.Fatalf("ExpandOne: %v", err)
	}
	if len(sub.Nodes) == 0 {
		t.Fatal("the expansion produced no nodes to be under anything")
	}
	for _, node := range sub.Nodes {
		if len(node.Spec.Constraints) != 1 || node.Spec.Constraints[0].Text != "Change no files." {
			t.Fatalf("%q was minted outside the rule: %+v", node.Title, node.Spec.Constraints)
		}
	}
}
