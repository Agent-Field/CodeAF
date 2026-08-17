package subharness

import (
	"strings"
	"testing"
)

// chained is the shape the mega-node run should have drawn and didn't: three
// evaluations of the same question, one after the other. Nothing about the
// program is wrong — Validate accepts it — which is the whole reason the claim
// about it has to be checked separately.
func chained() Harness {
	return Harness{
		Id: Id{Name: "compare-chained", Desc: "three approaches, priced", Author: "test", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "read-goal", Kind: KindAgentLoop, Fields: Fields{"brief": "state what the three approaches are"}},
				{Id: "eval-a", Kind: KindAgentLoop, Fields: Fields{"brief": "price approach A"}},
				{Id: "eval-b", Kind: KindAgentLoop, Fields: Fields{"brief": "price approach B"}},
				{Id: "decide", Kind: KindAgentLoop, Fields: Fields{"brief": "recommend one"}},
			},
			Edges: []Edge{{"read-goal", "eval-a"}, {"eval-a", "eval-b"}, {"eval-b", "decide"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
		Dyn:    Dyn{Ladder: DynFixed},
	}
}

// fanned is the same three evaluations drawn the way the derivation says they
// are: opened at a split, gathered at a join, with the decision after it. It is
// the C2 shape the rig was supposed to produce.
func fanned() Harness {
	return Harness{
		Id: Id{Name: "compare-fanned", Desc: "three approaches, priced in lanes", Author: "test", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "read-goal", Kind: KindAgentLoop, Fields: Fields{"brief": "state what the three approaches are"}},
				{Id: "fan", Kind: KindParallelSplit, Fields: Fields{"width": "3", "over": "the three approaches"}},
				{Id: "eval-a", Kind: KindAgentLoop, Fields: Fields{"brief": "price approach A"}},
				{Id: "eval-b", Kind: KindAgentLoop, Fields: Fields{"brief": "price approach B"}},
				{Id: "eval-c", Kind: KindAgentLoop, Fields: Fields{"brief": "price approach C, carrying A and B compactly"}},
				{Id: "gather", Kind: KindParallelJoin, Fields: Fields{"mode": "all"}},
				{Id: "decide", Kind: KindAgentLoop, Fields: Fields{"brief": "recommend one of the three, on the prices in front of you"}},
			},
			Edges: []Edge{
				{"read-goal", "fan"},
				{"fan", "eval-a"}, {"fan", "eval-b"}, {"fan", "eval-c"},
				{"eval-a", "gather"}, {"eval-b", "gather"}, {"eval-c", "gather"},
				{"gather", "decide"},
			},
		},
		Verify: Verify{Ladder: VerifyAccept},
		Dyn:    Dyn{Ladder: DynWidth, Cap: 3},
	}
}

func refusesDerivation(t *testing.T, h Harness, pairs []Derivation, wants ...string) {
	t.Helper()
	err := CheckDerivation(h, pairs)
	if err == nil {
		t.Fatalf("accepted a derivation that should have been refused (wanted %q in the error)", strings.Join(wants, ", "))
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refused with %q, which does not say %q", err, want)
		}
	}
}

func acceptsDerivation(t *testing.T, h Harness, pairs []Derivation) {
	t.Helper()
	if err := CheckDerivation(h, pairs); err != nil {
		t.Fatalf("a sound derivation was refused: %v", err)
	}
}

// The lie the rig could not see before: two jobs called independent and then
// wired one into the other. The refusal has to name the pair AND the path,
// because a designer told only "inconsistent" will guess which pair.
func TestIndependentButChainedIsRefusedByNameAndPath(t *testing.T) {
	h := chained()
	mustValidate(t, h)
	refusesDerivation(t, h, []Derivation{
		{A: "eval-a", B: "eval-b", Rel: RelIndependent, Why: "each prices its own approach; neither reads the other"},
	}, `"eval-a"`, `"eval-b"`, "eval-a->eval-b")
}

// Reachability, not adjacency. Two nodes three steps apart are as dependent as
// two side by side, and a claim of independence across the whole chain is the
// same lie stretched further.
func TestIndependenceIsRefusedAcrossAWholeChain(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "read-goal", B: "decide", Rel: RelIndependent, Why: "one reads, one decides"},
	}, "read-goal->eval-a->eval-b->decide")
}

// Order in the pair is not a way out: independence is a claim about both
// directions, so the check asks both.
func TestIndependenceIsRefusedWhenTheEdgeRunsBackwards(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "decide", B: "eval-a", Rel: RelIndependent, Why: "different jobs"},
	}, "eval-a->eval-b->decide")
}

// The honest reading of the same chain: the pairs really are ordered, and saying
// so is accepted without a word about the edges.
func TestDependsOnAChainIsAccepted(t *testing.T) {
	acceptsDerivation(t, chained(), []Derivation{
		{A: "read-goal", B: "eval-a", Rel: RelDepends, Why: "eval-a needs the named approaches"},
		{A: "eval-a", B: "eval-b", Rel: RelDepends, Why: "as drawn, B is handed A's price"},
		{A: "eval-b", B: "decide", Rel: RelDepends, Why: "the decision needs the prices"},
	})
}

// The law is new; a page designed by an older prompt has no table to hold. Both
// spellings of absence pass.
func TestAnAbsentDerivationPasses(t *testing.T) {
	acceptsDerivation(t, chained(), nil)
	acceptsDerivation(t, chained(), []Derivation{})
}

// The C2 shape: three independent evaluations opened at a split. Sibling lanes
// do not reach each other, so the independence claim stands — and the program
// itself is valid, which is the pair of facts the forcing function is for.
func TestTheFannedComparisonValidatesAndItsDerivationHolds(t *testing.T) {
	h := fanned()
	mustValidate(t, h)
	acceptsDerivation(t, h, []Derivation{
		{A: "eval-a", B: "eval-b", Rel: RelIndependent, Why: "each prices one approach from the goal's own statement; no output of one enters the other"},
		{A: "eval-a", B: "eval-c", Rel: RelIndependent, Why: "same: separate approaches, separate prices"},
		{A: "eval-b", B: "eval-c", Rel: RelIndependent, Why: "same: separate approaches, separate prices"},
		{A: "read-goal", B: "eval-a", Rel: RelDepends, Why: "every lane needs the approaches named"},
		{A: "eval-c", B: "decide", Rel: RelDepends, Why: "the join merges nothing, so the last lane carries the three prices to the decision"},
	})
}

// A table that can be switched off by a typo in `rel` is not a machine check.
func TestAnUnknownRelationIsRefused(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "eval-a", B: "eval-b", Rel: "indepedent", Why: "typed in a hurry"},
	}, "indepedent", RelDepends, RelIndependent)
}

// The `why` is the whole difference between an argument and an assertion, and an
// assertion is what the prose mandate already got.
func TestAVerdictWithNoWhyIsRefused(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "eval-a", B: "eval-b", Rel: RelIndependent},
	}, "no `why`", "why no data flows")
	refusesDerivation(t, chained(), []Derivation{
		{A: "read-goal", B: "eval-a", Rel: RelDepends, Why: "   "},
	}, "no `why`", "name the data that flows")
}

// Three jobs derived and one node drawn is the collapse the mega-node run
// performed. The table and the program no longer agree, and the refusal says so.
func TestAPairNamingAJobThatWasNeverDrawnIsRefused(t *testing.T) {
	collapsed := chained()
	collapsed.Program = Program{
		Nodes: []Node{{Id: "evaluate-all", Kind: KindAgentLoop, Fields: Fields{"brief": "price all three approaches and recommend one"}}},
	}
	mustValidate(t, collapsed)
	refusesDerivation(t, collapsed, []Derivation{
		{A: "eval-a", B: "eval-b", Rel: RelIndependent, Why: "separate approaches"},
	}, `"eval-a"`, "not a node in the program")
}

// A pair derived twice — once each way, with two different verdicts — is a table
// contradicting itself, and reading the first one and ignoring the second would
// make which verdict counts depend on line order.
func TestAPairDerivedTwiceIsRefused(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "eval-a", B: "eval-b", Rel: RelDepends, Why: "B is handed A's price"},
		{A: "eval-b", B: "eval-a", Rel: RelIndependent, Why: "on second thought"},
	}, "derived twice")
}

func TestAHalfWrittenPairIsRefused(t *testing.T) {
	refusesDerivation(t, chained(), []Derivation{
		{A: "eval-a", B: "", Rel: RelIndependent, Why: "nothing flows"},
	}, "a pair is two node ids")
	refusesDerivation(t, chained(), []Derivation{
		{A: "eval-a", B: "eval-a", Rel: RelIndependent, Why: "nothing flows"},
	}, "with itself")
}

// pathBetween is what the refusals quote, so its two edge cases are worth
// stating outright: a walk that does not exist, and one that runs the other way.
func TestPathBetweenIsDirected(t *testing.T) {
	p := chained().Program
	if path := pathBetween(p, "read-goal", "decide"); strings.Join(path, "->") != "read-goal->eval-a->eval-b->decide" {
		t.Errorf("the walk forward reads %q", strings.Join(path, "->"))
	}
	if path := pathBetween(p, "decide", "read-goal"); path != nil {
		t.Errorf("a walk backwards along the edges: %q", strings.Join(path, "->"))
	}
	if path := pathBetween(fanned().Program, "eval-a", "eval-b"); path != nil {
		t.Errorf("one lane reaches another: %q", strings.Join(path, "->"))
	}
}
