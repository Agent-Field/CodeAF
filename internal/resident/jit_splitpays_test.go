package resident

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// The EV-lookahead gate: with measured evidence a split pays only when the
// node's predicted overrun cost beats the base rate — i.e. it is oversized or
// borderline. Without evidence the gate is inert and the old path is taken
// byte-for-byte.
func TestSplitPaysOnlyAboveTheBaseRate(t *testing.T) {
	oversized := &plan.Node{Kind: plan.KindWork, Size: plan.SizeOversized, Parts: []string{"a", "b"}}
	atomicNode := &plan.Node{Kind: plan.KindWork, Size: plan.SizeAtomic, Parts: []string{"a", "b"}}

	evidence := plan.Options{CapacitySamples: 16, CapacityOverrunRate: 0.3}
	noEvidence := plan.Options{}

	if !splitPays(oversized, evidence) {
		t.Fatal("an oversized node should pay: its predicted overrun is above the base rate")
	}
	if splitPays(atomicNode, evidence) {
		t.Fatal("an atomic node at the base rate should not pay: the second brief re-buys the same wait")
	}
	if !splitPays(atomicNode, noEvidence) {
		t.Fatal("without evidence the gate is inert: an atomic node still pays")
	}
	if !splitPays(nil, evidence) {
		t.Fatal("a nil node is inert, never a refusal")
	}
	if !splitPays(&plan.Node{Kind: plan.KindWork, Parts: []string{"only-one"}}, evidence) {
		t.Fatal("fewer than two named parts is not a split to gate")
	}
}
