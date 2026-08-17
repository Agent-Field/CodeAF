package subharness

import (
	"strconv"
	"strings"
	"testing"
)

// linear is the plainest harness that is still a real one: something starts
// it, one loop does the work, one verify believes it. Every test that wants a
// valid harness starts here and breaks exactly one thing.
func linear() Harness {
	return Harness{
		Id: Id{Name: "linear", Desc: "one loop, checked", Author: "test", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerIdle}},
				{Id: "work", Kind: KindAgentLoop, Fields: Fields{
					"brief": "fix the failing test", "tools": "read,write", "max_turns": "8",
				}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"ladder": VerifySchema, "check": "the diff applies"}},
			},
			Edges: []Edge{{"start", "work"}, {"work", "check"}},
		},
		Whitelist: []string{"read", "write"},
		Verify:    Verify{Ladder: VerifyInvariants},
		Dyn:       Dyn{Ladder: DynFixed},
	}
}

func mustValidate(t *testing.T, h Harness) {
	t.Helper()
	if err := Validate(h); err != nil {
		t.Fatalf("valid harness refused: %v", err)
	}
}

// refuses asserts that a broken harness is refused, and that the refusal says
// which thing broke — an error a model has to guess at is an error it will
// answer by guessing.
func refuses(t *testing.T, h Harness, want string) {
	t.Helper()
	err := Validate(h)
	if err == nil {
		t.Fatalf("accepted a harness that should have been refused (wanted %q in the error)", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("refused with %q, which does not say %q", err, want)
	}
}

func TestAWholeHarnessRoundTripsThroughItsPage(t *testing.T) {
	h := linear()
	h.Tests = []Test{{Name: "smoke", Input: "a failing test", Expect: "it passes"}}
	mustValidate(t, h)

	data, err := Encode(h)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(again) {
		t.Fatalf("a page did not survive a round trip:\n%s\n---\n%s", data, again)
	}
	if back.Id != h.Id || len(back.Program.Nodes) != 3 || len(back.Program.Edges) != 2 {
		t.Fatalf("decoded a different harness: %+v", back)
	}
	if back.Program.Nodes[1].Fields.Get("brief") != "fix the failing test" {
		t.Fatalf("fields did not survive: %+v", back.Program.Nodes[1])
	}
}

// A field neither the writer nor this package knows is a version skew. Reading
// it as absent would run a harness that is not the one on disk.
func TestAnUnknownFieldOnAPageIsRefused(t *testing.T) {
	_, err := Decode([]byte(`{"id":{"name":"x","version":1},"telepathy":true}`))
	if err == nil || !strings.Contains(err.Error(), "telepathy") {
		t.Fatalf("decoded a page with an unknown field: %v", err)
	}
}

// Silence means the timid rung on both ladders: believe the output, decide
// nothing.
func TestAnOmittedRungMeansTheLeastOfItsLadder(t *testing.T) {
	h := Harness{
		Id:      Id{Name: "bare", Version: 1},
		Program: Program{Nodes: []Node{{Id: "only", Kind: KindToolCall, Fields: Fields{"tool": "read"}}}},
	}
	h.Whitelist = []string{"read"}
	normalized := h.Normalize()
	if normalized.Verify.Ladder != VerifyAccept || normalized.Dyn.Ladder != DynFixed {
		t.Fatalf("silence normalized to %+v %+v", normalized.Verify, normalized.Dyn)
	}
	mustValidate(t, h)
}

func TestTheProgramMustBeADAG(t *testing.T) {
	cyclic := linear()
	cyclic.Program.Edges = append(cyclic.Program.Edges, Edge{"check", "work"})
	refuses(t, cyclic, "not a DAG")
}

// One entry is the whole reachability law: in a DAG every node is reachable
// from some root, so a node nothing leads to is a second entry, and a whole
// disconnected island is a second entry too. Both are either a pasted-in
// harness or a forgotten edge, and both are worth refusing at save time rather
// than discovering in a trace.
func TestEveryNodeMustBeReachableFromTheOneEntry(t *testing.T) {
	orphan := linear()
	orphan.Program.Nodes = append(orphan.Program.Nodes,
		Node{Id: "adrift", Kind: KindToolCall, Fields: Fields{"tool": "read"}})
	refuses(t, orphan, "one entry")

	island := linear()
	island.Program.Nodes = append(island.Program.Nodes,
		Node{Id: "island-a", Kind: KindToolCall, Fields: Fields{"tool": "read"}},
		Node{Id: "island-b", Kind: KindToolCall, Fields: Fields{"tool": "read"}})
	island.Program.Edges = append(island.Program.Edges, Edge{"island-a", "island-b"})
	refuses(t, island, "one entry")
}

func TestEdgesMustNameNodesThatExistExactlyOnce(t *testing.T) {
	dangling := linear()
	dangling.Program.Edges = append(dangling.Program.Edges, Edge{"check", "nowhere"})
	refuses(t, dangling, "does not exist")

	twice := linear()
	twice.Program.Edges = append(twice.Program.Edges, Edge{"work", "check"})
	refuses(t, twice, "declared twice")

	itself := linear()
	itself.Program.Edges = append(itself.Program.Edges, Edge{"check", "check"})
	refuses(t, itself, "edges to itself")
}

func TestNodeIdsObeyTheSlugLawAndAreUnique(t *testing.T) {
	shouty := linear()
	shouty.Program.Nodes[1].Id = "Work"
	shouty.Program.Edges = []Edge{{"start", "Work"}, {"Work", "check"}}
	refuses(t, shouty, "not a slug")

	long := linear()
	long.Program.Nodes[1].Id = strings.Repeat("w", MaxIdBytes+1)
	long.Program.Edges = []Edge{{"start", long.Program.Nodes[1].Id}, {long.Program.Nodes[1].Id, "check"}}
	refuses(t, long, "not a slug")

	twice := linear()
	twice.Program.Nodes[2].Id = "work"
	twice.Program.Edges = []Edge{{"start", "work"}}
	refuses(t, twice, "declared twice")
}

func TestAnUnregisteredKindIsRefusedWithTheRegistryNamed(t *testing.T) {
	h := linear()
	h.Program.Nodes[1].Kind = "agent.think"
	err := Validate(h)
	if err == nil || !strings.Contains(err.Error(), "unregistered kind") {
		t.Fatalf("accepted an unregistered kind: %v", err)
	}
	if !strings.Contains(err.Error(), KindAgentLoop) {
		t.Fatalf("refusal does not list the kinds that do exist: %v", err)
	}
}

// The autonomy spectrum is only worth writing down if it is enforced.
func TestTheDynamismRungGatesTheKindsAProgramMayContain(t *testing.T) {
	fixed := linear()
	fixed.Program.Nodes = append(fixed.Program.Nodes,
		Node{Id: "pick", Kind: KindBranch, Fields: Fields{"when": "the test still fails"}})
	fixed.Program.Edges = append(fixed.Program.Edges, Edge{"check", "pick"})
	refuses(t, fixed, `needs dynamism "branch"`)

	// The same program at the rung that admits it.
	allowed := fixed
	allowed.Dyn = Dyn{Ladder: DynBranch, Cap: 3}
	mustValidate(t, allowed)

	// A branch harness still may not call another sub-harness.
	recursive := allowed
	recursive.Program.Nodes = append(recursive.Program.Nodes,
		Node{Id: "inner", Kind: KindSubharnessCall, Fields: Fields{"name": "other"}})
	recursive.Program.Edges = append(recursive.Program.Edges, Edge{"pick", "inner"})
	refuses(t, recursive, `needs dynamism "recursive"`)
}

func TestTheDynamismBudgetMatchesTheRung(t *testing.T) {
	paid := linear()
	paid.Dyn = Dyn{Ladder: DynFixed, Cap: 4}
	refuses(t, paid, "may not carry a dynamism cap")

	unbounded := linear()
	unbounded.Dyn = Dyn{Ladder: DynBranch}
	refuses(t, unbounded, "outside 1..")

	greedy := linear()
	greedy.Dyn = Dyn{Ladder: DynBranch, Cap: MaxDynCap + 1}
	refuses(t, greedy, "outside 1..")
}

func TestAToolIsRefusedUnlessTheWhitelistNamesIt(t *testing.T) {
	call := linear()
	call.Program.Nodes = append(call.Program.Nodes,
		Node{Id: "publish", Kind: KindToolCall, Fields: Fields{"tool": "shell"}})
	call.Program.Edges = append(call.Program.Edges, Edge{"check", "publish"})
	refuses(t, call, "not on the whitelist")

	handout := linear()
	handout.Program.Nodes[1].Fields["tools"] = "read,shell"
	refuses(t, handout, "not on the whitelist")

	blank := linear()
	blank.Whitelist = []string{"read", "", "write"}
	refuses(t, blank, "blank tool")

	listed := linear()
	listed.Whitelist = []string{"read", "write", "read"}
	refuses(t, listed, "listed twice")
}

func TestANodeMayVerifyLessHardThanTheHarnessButNeverHarder(t *testing.T) {
	below := linear()
	below.Program.Nodes[2].Fields["ladder"] = VerifyAccept
	mustValidate(t, below)

	above := linear()
	above.Program.Nodes[2].Fields["ladder"] = VerifyAdversarial
	refuses(t, above, "above the harness")
}

// A rung is a promise about what a run's output went through. A program with
// nothing that could verify has made the promise with no way to keep it.
func TestAVerificationRungNeedsSomethingInTheProgramToKeepIt(t *testing.T) {
	empty := linear()
	empty.Program.Nodes = empty.Program.Nodes[:2]
	empty.Program.Edges = empty.Program.Edges[:1]
	refuses(t, empty, "no verify node")

	// accept promises nothing, so it needs nothing.
	accepting := empty
	accepting.Verify = Verify{Ladder: VerifyAccept}
	mustValidate(t, accepting)

	human := linear()
	human.Verify = Verify{Ladder: VerifyHuman}
	refuses(t, human, "no human.gate")

	gated := human
	gated.Program.Nodes = append(gated.Program.Nodes,
		Node{Id: "ask", Kind: KindHumanGate, Fields: Fields{"ask": "ship it?"}})
	gated.Program.Edges = append(gated.Program.Edges, Edge{"check", "ask"})
	mustValidate(t, gated)
}

func TestTheHarnessNameAndVersionObeyTheirLaws(t *testing.T) {
	named := linear()
	named.Id.Name = "../escape"
	refuses(t, named, "not a slug")

	empty := linear()
	empty.Id.Name = ""
	refuses(t, empty, "not a slug")

	far := linear()
	far.Id.Version = MaxVersion + 1
	refuses(t, far, "outside 0..")
}

func TestTestsAreNamedAndNamedOnce(t *testing.T) {
	unnamed := linear()
	unnamed.Tests = []Test{{Input: "a", Expect: "b"}}
	refuses(t, unnamed, "no name")

	twice := linear()
	twice.Tests = []Test{{Name: "smoke"}, {Name: "smoke"}}
	refuses(t, twice, "declared twice")
}

func TestAProgramWithNoNodesOrTooManyIsRefused(t *testing.T) {
	empty := linear()
	empty.Program = Program{}
	refuses(t, empty, "no nodes")

	huge := linear()
	huge.Program = Program{Nodes: []Node{{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerIdle}}}}
	for index := 0; index < MaxNodes; index++ {
		id := "n" + strconv.Itoa(index)
		huge.Program.Nodes = append(huge.Program.Nodes,
			Node{Id: id, Kind: KindToolCall, Fields: Fields{"tool": "read"}})
		huge.Program.Edges = append(huge.Program.Edges, Edge{"start", id})
	}
	refuses(t, huge, "past the cap")
}

// The walk order is what makes a run reproducible; ties break on program
// order, so the same page walks the same way twice.
func TestTheTopologicalOrderFollowsProgramOrderOnTies(t *testing.T) {
	h := linear()
	h.Program.Nodes = append(h.Program.Nodes,
		Node{Id: "also", Kind: KindToolCall, Fields: Fields{"tool": "read"}})
	h.Program.Edges = append(h.Program.Edges, Edge{"start", "also"})
	mustValidate(t, h)

	order, err := topo(h.Program)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"start", "work", "check", "also"}
	for index, id := range want {
		if order[index] != id {
			t.Fatalf("walk order %v, want %v", order, want)
		}
	}
}
