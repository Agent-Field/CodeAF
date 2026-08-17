package subharness

import (
	"strings"
	"testing"
)

// declared is every kind the package names as a constant. The map registry must
// cover it exactly: a kind a file can write but the validator has never heard of
// is a hole, and a registration for a kind no constant names is dead weight.
var declared = []Kind{
	KindAgentLoop, KindToolCall, KindParallelSplit, KindParallelJoin,
	KindBranch, KindLoopUntil, KindHumanGate, KindVerify,
	KindSubharnessCall, KindTrigger,
}

func TestKindsMapCoversEveryDeclaredKind(t *testing.T) {
	for _, kind := range declared {
		info, ok := KindFor(kind)
		if !ok {
			t.Errorf("kind %q is declared but not registered", kind)
			continue
		}
		if info.Kind != kind {
			t.Errorf("kind %q is registered as %q", kind, info.Kind)
		}
		if strings.TrimSpace(info.Purpose) == "" {
			t.Errorf("kind %q is registered with no purpose", kind)
		}
		if info.Validate == nil {
			t.Errorf("kind %q is registered with no validator", kind)
		}
	}
	if len(Kinds()) != len(declared) {
		t.Errorf("registry holds %d kinds, %d are declared", len(Kinds()), len(declared))
	}
	if _, ok := KindFor("agent.thoughts"); ok {
		t.Error("an unregistered kind resolved")
	}
}

func TestKindsAreListedInAStableOrder(t *testing.T) {
	first, second := Kinds(), Kinds()
	for i := range first {
		if first[i].Kind != second[i].Kind {
			t.Fatalf("Kinds is unstable at %d: %q then %q", i, first[i].Kind, second[i].Kind)
		}
	}
	if i := len(first) - 1; string(first[0].Kind) > string(first[i].Kind) {
		t.Errorf("Kinds is not sorted: %q before %q", first[0].Kind, first[i].Kind)
	}
	menu := KindMenu()
	for _, kind := range declared {
		if !strings.Contains(menu, string(kind)) {
			t.Errorf("KindMenu leaves out %q", kind)
		}
	}
}

func TestNodeWithNoNeedsDependsOnNothing(t *testing.T) {
	e := hostedEntry()
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	roots := e.Roots()
	if len(roots) != 1 || roots[0].ID != "start" {
		t.Fatalf("roots are %v, want the trigger alone", ids(roots))
	}
	if len(roots[0].Needs) != 0 {
		t.Errorf("the root depends on %v", roots[0].Needs)
	}
	if entry, _ := e.Node("read-it"); len(entry.Needs) != 0 {
		t.Errorf("the hosted entry depends on %v; a trigger hands over, it does not precede", entry.Needs)
	}
}

func TestValidateRefusesACycle(t *testing.T) {
	e := Entry{
		Name: "ring", Revision: 1, Tools: []string{"read"},
		Nodes: []Node{
			{ID: "a", Kind: KindAgentLoop, Needs: []string{"b"}},
			{ID: "b", Kind: KindAgentLoop, Needs: []string{"a"}},
		},
	}
	err := e.Validate()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate accepted a cycle: %v", err)
	}
}

func TestValidateBoundsTheToolWhitelist(t *testing.T) {
	e := hostedEntry()
	e.Nodes = append(e.Nodes, Node{
		ID: "shell", Kind: KindToolCall, Needs: []string{"read-it"},
		Call: &ToolCall{Tool: "exec"},
	})
	err := e.Validate()
	if err == nil || !strings.Contains(err.Error(), "whitelist") {
		t.Fatalf("a tool.call reached outside the whitelist: %v", err)
	}

	e.Nodes[2].Call.Tool = "write"
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// A loop may narrow the whitelist and never widen it.
	e.Nodes[1].Loop.Tools = []string{"read", "exec"}
	if err := e.Validate(); err == nil {
		t.Fatal("an agent.loop widened the harness whitelist")
	}
}

func TestValidateRefusesAnUncappedLoop(t *testing.T) {
	e := hostedEntry()
	e.Nodes = append(e.Nodes, Node{
		ID: "again", Kind: KindLoopUntil, Needs: []string{"read-it"},
		Until: &LoopUntil{Body: []string{"read-it"}, Until: "inbox is empty"},
	})
	if err := e.Validate(); err == nil {
		t.Fatal("Validate accepted a loop with no maximum")
	}
	e.Nodes[2].Until.Max = 3
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRefusesTwoPayloadsOnOneNode(t *testing.T) {
	e := hostedEntry()
	e.Nodes[1].Call = &ToolCall{Tool: "read"}

	err := e.Validate()
	if err == nil || !strings.Contains(err.Error(), "tool.call payload") {
		t.Fatalf("a node was an agent.loop and a tool.call at once: %v", err)
	}
}

func TestVerifyRungComesFromTheNodeOrTheEntry(t *testing.T) {
	e := hostedEntry()
	e.Nodes = append(e.Nodes, Node{
		ID: "check", Kind: KindVerify, Needs: []string{"read-it"},
		Brief: "the inbox was read",
	})
	if err := e.Validate(); err == nil {
		t.Fatal("a verify node ran with no rung at all")
	}

	e.Verify = RungInvariants
	if err := e.Validate(); err != nil {
		t.Fatalf("the entry default did not reach the node: %v", err)
	}

	e.Nodes[2].Check = &Check{Rung: "vibes"}
	if err := e.Validate(); err == nil {
		t.Fatal("a node named a rung that is not on the ladder")
	}
	e.Nodes[2].Check.Rung = RungAdversarial
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func ids(nodes []Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}
