package workgraph

import (
	"encoding/json"
	"strings"
	"testing"
)

// A NODE'S SOURCE AND HIDDEN FLAG ARE THE PRODUCER'S FACTS, COPIED SO NO ANSWER
// ALIASES THE SNAPSHOT, WITH THE HIDDEN ONES COUNTED AND NEVER REMOVED.
func TestTheGraphCarriesSourceAndDismissedAsRecordedAndCountsTheHiddenOnes(t *testing.T) {
	src := &TaskSource{ID: "s#1", Kind: SourceMessage, Text: "sweep the calls", At: minute(2)}
	hide := func(f *Fact) { f.Node.Dismissed = true }
	withSource := func(f *Fact) { f.Node.Source = src }
	snap := mustBuild(t, input(agentA,
		task("a", 1, state(StateFailed), withSource, hide),
		task("a", 2, state(StateFailed), withSource),
		task("a", 3, state(StateFailed)), // recorded before sources existed
	))
	res := answer(t, snap, GraphRequest{Scope: agentA})
	if res.Totals.Nodes != 3 || res.Totals.ByState[StateFailed] != 3 || res.Totals.Dismissed != 1 {
		t.Fatalf("totals = %+v, want a hidden failure still counted and the hidden count said", res.Totals)
	}
	by := map[string]GraphNode{}
	for _, n := range res.Nodes {
		by[n.Ref.Task] = n
	}
	if by["1"].Source == nil || by["1"].Source.ID != "s#1" || !by["1"].Dismissed || by["2"].Dismissed || by["3"].Source != nil {
		t.Fatalf("nodes = %+v", by)
	}
	// An answer owns its copy: changing it does not change the next answer.
	by["1"].Source.Text = "tampered"
	if again := answer(t, snap, GraphRequest{Scope: agentA}); again.Nodes[0].Source.Text != "sweep the calls" {
		t.Fatalf("an answer aliased the snapshot: %+v", again.Nodes[0].Source)
	}
	raw, _ := json.Marshal(by["3"])
	if strings.Contains(string(raw), `"source"`) || strings.Contains(string(raw), `"dismissed"`) {
		t.Fatalf("a node with no source or hide says %s", raw)
	}
	one, three := by["1"], by["3"]
	if nodeBytes(&one) <= nodeBytes(&three) {
		t.Fatal("the byte estimate ignores the source")
	}
}
