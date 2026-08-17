package subharness

import (
	"strings"
	"testing"
)

// drafted is the page a critic is handed: three jobs in a line, one of them a
// verify, one tool granted and used. It is built rather than parsed so a test can
// say exactly which byte it expected to survive a patch.
func drafted() Harness {
	return Harness{
		Id: Id{Name: "journal", Desc: "decide whether to rewrite the session journal"},
		Program: Program{
			Nodes: []Node{
				{Id: "read", Kind: KindAgentLoop, Fields: Fields{
					"brief":     "read the current journal design — whisper.cpp is not involved, and the em-dash here is the writer's",
					"tools":     "echo",
					"max_turns": "3",
				}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"check": "every failure mode carries an observable"}},
				{Id: "memo", Kind: KindAgentLoop, Fields: Fields{"brief": "write the decision memo"}},
			},
			Edges: []Edge{{"read", "check"}, {"check", "memo"}},
		},
		Whitelist: []string{"echo"},
		Verify:    Verify{Ladder: VerifyInvariants},
		Dyn:       Dyn{Ladder: DynFixed},
	}
}

func brief(t *testing.T, h Harness, id string) string {
	t.Helper()
	node, found := h.Program.Node(id)
	if !found {
		t.Fatalf("no node %q", id)
	}
	return node.Fields.Get("brief")
}

// THE POINT OF THE WHOLE FILE: a patch touches what it names and nothing else,
// so the text nobody reviewed is byte for byte the text the designer wrote.
func TestApplyTouchesOnlyWhatTheOpNames(t *testing.T) {
	before := drafted()
	was := brief(t, before, "read")

	after, err := Apply(before, []Op{{Op: OpReplaceBrief, Node: "memo", Text: "write the memo, ranked, with the counterargument answered"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := brief(t, after, "memo"); got != "write the memo, ranked, with the counterargument answered" {
		t.Errorf("the brief did not land: %q", got)
	}
	if got := brief(t, after, "read"); got != was {
		t.Errorf("an untouched brief was rewritten:\n was %q\n now %q", was, got)
	}
	if got := brief(t, before, "memo"); got != "write the decision memo" {
		t.Errorf("Apply edited the draft it was handed: %q", got)
	}
}

// Purity is not a preference here: the rig prints the draft beside the revision
// after applying, and a shared Fields map would make the two prints the same page.
func TestApplyDoesNotWriteThroughToTheDraft(t *testing.T) {
	before := drafted()
	after, err := Apply(before, []Op{
		{Op: OpSetField, Node: "read", Field: "max_turns", Text: "2"},
		{Op: OpAddEdge, Node: "read", Text: "memo"},
		{Op: OpSetWhitelist, Text: ""},
	})
	if err == nil {
		t.Fatal("a page granting a tool nothing uses is fine, but this one dropped a granted tool a node still hands out — Validate should refuse it")
	}
	_ = after
	if got := before.Program.Nodes[0].Fields.Get("max_turns"); got != "3" {
		t.Errorf("the draft's field was written through: %q", got)
	}
	if len(before.Program.Edges) != 2 {
		t.Errorf("the draft's edges were written through: %v", before.Program.Edges)
	}
	if len(before.Whitelist) != 1 {
		t.Errorf("the draft's whitelist was written through: %v", before.Whitelist)
	}
}

func TestApplySetFieldRemovesOnEmptyText(t *testing.T) {
	after, err := Apply(drafted(), []Op{{Op: OpSetField, Node: "read", Field: "max_turns"}})
	if err != nil {
		t.Fatal(err)
	}
	node, _ := after.Program.Node("read")
	if _, still := node.Fields["max_turns"]; still {
		t.Errorf("the field survived: %v", node.Fields)
	}
}

func TestApplyAddsANodeAndRelinksIt(t *testing.T) {
	after, err := Apply(drafted(), []Op{
		{Op: OpAddNode, NodeJSON: []byte(`{"id":"counter","kind":"agent.loop","fields":{"brief":"argue the other side"}}`)},
		{Op: OpDropEdge, Node: "check", Text: "memo"},
		{Op: OpAddEdge, Node: "check", Text: "counter"},
		{Op: OpAddEdge, Node: "counter", Text: "memo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Program.Nodes) != 4 {
		t.Fatalf("nodes = %d", len(after.Program.Nodes))
	}
	if got := brief(t, after, "counter"); got != "argue the other side" {
		t.Errorf("brief = %q", got)
	}
	if len(after.Program.Successors("check")) != 1 || after.Program.Successors("check")[0] != "counter" {
		t.Errorf("edges = %v", after.Program.Edges)
	}
}

// A node arriving by patch is read exactly as a node arriving from disk.
func TestApplyRefusesANodeWithAFieldNothingReads(t *testing.T) {
	_, results, err := ApplyReport(drafted(), []Op{
		{Op: OpAddNode, NodeJSON: []byte(`{"id":"x","kind":"agent.loop","brief":"loose"}`)},
	})
	if err != nil {
		t.Fatalf("the page should be unchanged and legal: %v", err)
	}
	if results[0].Applied() || !strings.Contains(results[0].Err.Error(), "node_json") {
		t.Errorf("result = %+v", results[0])
	}
}

// Dropping a node takes its edges with it — anything else leaves an edge into a
// node that does not exist, which is a refusal for the tidying rather than for
// the design.
func TestApplyDropNodeTakesItsEdges(t *testing.T) {
	after, err := Apply(drafted(), []Op{
		{Op: OpDropNode, Node: "check"},
		{Op: OpSetVerify, Text: VerifyAccept},
		{Op: OpAddEdge, Node: "read", Text: "memo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Program.Nodes) != 2 || len(after.Program.Edges) != 1 {
		t.Fatalf("program = %+v", after.Program)
	}
	if after.Verify.Ladder != VerifyAccept {
		t.Errorf("verify = %q", after.Verify.Ladder)
	}
}

// A bad op costs its own line and nothing else. This is the difference between a
// review that lands eleven of twelve findings and a review turn that is thrown away.
func TestApplySkipsWhatItCannotDoAndKeepsTheRest(t *testing.T) {
	after, results, err := ApplyReport(drafted(), []Op{
		{Op: OpReplaceBrief, Node: "nosuch", Text: "..."},
		{Op: OpReplaceBrief, Node: "memo", Text: "write the memo with the counterargument answered"},
		{Op: "rewrite_everything", Node: "memo", Text: "..."},
		{Op: OpAddEdge, Node: "memo", Text: "memo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %d", len(results))
	}
	for at, want := range []bool{false, true, false, false} {
		if results[at].Applied() != want {
			t.Errorf("op %d (%s): applied = %v, want %v (%v)", at, results[at].Op, results[at].Applied(), want, results[at].Err)
		}
	}
	if !strings.Contains(results[0].Err.Error(), "nosuch") {
		t.Errorf("the error does not name the node: %v", results[0].Err)
	}
	if !strings.Contains(results[2].Err.Error(), OpSetField) {
		t.Errorf("the error does not list the ops a critic may use: %v", results[2].Err)
	}
	if got := brief(t, after, "memo"); !strings.Contains(got, "counterargument") {
		t.Errorf("the good op did not land: %q", got)
	}
}

func TestApplyMovesTheDynamismRungAndItsBudget(t *testing.T) {
	for _, sample := range []struct {
		name string
		ops  []Op
	}{
		{"separately", []Op{
			{Op: OpSetDyn, Field: "ladder", Text: DynBranch},
			{Op: OpSetDyn, Field: "cap", Text: "3"},
		}},
		{"together", []Op{{Op: OpSetDyn, Text: "branch 3"}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			after, err := Apply(drafted(), sample.ops)
			if err != nil {
				t.Fatal(err)
			}
			if after.Dyn.Ladder != DynBranch || after.Dyn.Cap != 3 {
				t.Errorf("dyn = %+v", after.Dyn)
			}
		})
	}
}

func TestApplyRefusesARungThatIsNotOnTheLadder(t *testing.T) {
	_, results, err := ApplyReport(drafted(), []Op{
		{Op: OpSetVerify, Text: "very-hard"},
		{Op: OpSetDyn, Field: "cap", Text: "900"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for at, must := range []string{"verify ladder", "0..32"} {
		if results[at].Applied() || !strings.Contains(results[at].Err.Error(), must) {
			t.Errorf("op %d: %+v", at, results[at])
		}
	}
}

// The result passes the law a page from disk passes — a patch cannot mint a
// harness this package would refuse to load.
func TestApplyRevalidatesTheResult(t *testing.T) {
	_, _, err := ApplyReport(drafted(), []Op{{Op: OpDropEdge, Node: "read", Text: "check"}})
	if err == nil {
		t.Fatal("a program with two entries was accepted")
	}
	if !strings.Contains(err.Error(), "one entry") {
		t.Errorf("the error is not the reach law's: %v", err)
	}
}

func TestApplySetsTheDescriptionDetectionMatchesOn(t *testing.T) {
	after, err := Apply(drafted(), []Op{{Op: OpSetDesc, Text: "decide on an append-only event log for the session journal"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(after.Id.Desc, "decide on an append-only") {
		t.Errorf("desc = %q", after.Id.Desc)
	}
}

// An op prints as what it did, because the ops list IS the review's delta.
func TestOpPrintsAsWhatItDid(t *testing.T) {
	for _, sample := range []struct {
		op   Op
		want string
	}{
		{Op{Op: OpReplaceBrief, Node: "memo", Text: "1234567890"}, "replace_brief memo (10 bytes)"},
		{Op{Op: OpAddEdge, Node: "a", Text: "b"}, "add_edge a->b"},
		{Op{Op: OpSetField, Node: "read", Field: "max_turns"}, "set_field read.max_turns removed"},
		{Op{Op: OpSetVerify, Text: "schema"}, "set_verify schema"},
	} {
		if got := sample.op.String(); got != sample.want {
			t.Errorf("got %q, want %q", got, sample.want)
		}
	}
}
