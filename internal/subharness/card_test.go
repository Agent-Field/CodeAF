package subharness

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// flake is the shape the card and the runner are both tested against: a trigger
// that is hosted as a command, a worker, a branch with two arms, a bounded loop,
// a check and a gate. It is one harness rather than one per test so that a
// change to the model has one place to be felt.
func flake() Harness {
	return Harness{
		Id: Id{Name: "triage-flake", Desc: "chase a flaky test to a fix", Version: 2},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerHosted, "args": "since, label"}},
				{Id: "name-it", Kind: KindAgentLoop, Fields: Fields{
					"brief": "name the test that failed and why", "tools": "read, grep", "max_turns": "8",
				}},
				{Id: "pick", Kind: KindBranch, Fields: Fields{"when": "contains flaky"}},
				{Id: "rerun", Kind: KindToolCall, Fields: Fields{"tool": "bash", "args": "go test -run TestFoo -count 20"}},
				{Id: "explain", Kind: KindAgentLoop, Fields: Fields{"brief": "say why it is not flaky"}},
				{Id: "tries", Kind: KindLoopUntil, Fields: Fields{"until": "ok", "max_rounds": "3"}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"ladder": VerifyLoop, "check": "go test ./..."}},
				{Id: "land", Kind: KindHumanGate, Fields: Fields{"ask": "land the fix?"}},
			},
			Edges: []Edge{
				{"start", "name-it"}, {"name-it", "pick"},
				{"pick", "rerun"}, {"pick", "explain"},
				{"rerun", "tries"}, {"tries", "check"}, {"check", "land"},
			},
		},
		Whitelist: []string{"read", "grep", "bash"},
		Verify:    Verify{Ladder: VerifyLoop},
		Dyn:       Dyn{Ladder: DynBranch, Cap: 4},
	}
}

// postset is the shape the owner read off a screen and rejected: a fan-out into
// lanes, a gather, a check and a choice. It is the second sample because the
// laws about NESTING — lanes drawn once, a gather drawn not at all — need a
// program that has both, and flake has neither.
func postset() Harness {
	return Harness{
		Id: Id{Name: "social-post-set", Desc: "a set of platform images from one brief"},
		Program: Program{
			Nodes: []Node{
				{Id: "art-direction", Kind: KindAgentLoop, Fields: Fields{
					"brief": "You are the art director for a social-media campaign",
					"tools": "read, bash", "max_turns": "6",
				}},
				{Id: "split-variants", Kind: KindParallelSplit, Fields: Fields{
					"width": "3", "over": "the platform variants named in the spec",
				}},
				{Id: "make-twitter", Kind: KindAgentLoop, Fields: Fields{
					"brief": "You produce exactly ONE finished square image",
					"tools": "generate_image, view_image",
				}},
				{Id: "make-linkedin", Kind: KindAgentLoop, Fields: Fields{
					"brief": "You produce exactly ONE finished feed image",
					"tools": "generate_image, view_image",
				}},
				{Id: "join-variants", Kind: KindParallelJoin, Fields: Fields{"mode": JoinAll}},
				{Id: "verify-deliverables", Kind: KindVerify, Fields: Fields{
					"ladder": VerifyInvariants, "check": "Every variant named in the spec exists on disk",
				}},
				{Id: "branch-missing", Kind: KindBranch, Fields: Fields{"when": "failed"}},
				{Id: "recover-missing", Kind: KindAgentLoop, Fields: Fields{
					"brief": "Draw the variants that are missing", "tools": "generate_image",
				}},
				{Id: "report", Kind: KindAgentLoop, Fields: Fields{"brief": "Name every file that was made"}},
			},
			Edges: []Edge{
				{"art-direction", "split-variants"},
				{"split-variants", "make-twitter"}, {"split-variants", "make-linkedin"},
				{"make-twitter", "join-variants"}, {"make-linkedin", "join-variants"},
				{"join-variants", "verify-deliverables"},
				{"verify-deliverables", "branch-missing"},
				{"branch-missing", "recover-missing"}, {"branch-missing", "report"},
			},
		},
		Whitelist: []string{"generate_image", "view_image", "read", "bash"},
		Verify:    Verify{Ladder: VerifyInvariants},
		Dyn:       Dyn{Ladder: DynWidth, Cap: 3},
		Tests:     []Test{{Name: "both-platforms"}, {Name: "twitter-only"}},
	}
}

func TestTheCardNumbersTheStepsInTheOrderTheyRun(t *testing.T) {
	harness := flake()
	if err := Validate(harness); err != nil {
		t.Fatalf("the sample harness does not validate: %v", err)
	}
	card := Card(harness)
	lines := strings.Split(card, "\n")

	head := lines[0]
	for _, want := range []string{"triage-flake", "v2", "chase a flaky test to a fix"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the head line does not say %q: %q", want, head)
		}
	}

	// THE STEPS ARE THE WALK'S ORDER, numbered, one row each — so a person who
	// read the card top to bottom has read the run (run.go's topological walk).
	// The numbers belong to the MAIN FLOW: `rerun` and everything after it is
	// reachable only through one arm of `pick`, so it is nested rather than
	// numbered, and the run is three steps long.
	for at, id := range []string{"start", "name-it", "pick"} {
		if !hasRow(lines, itoaCard(at+1)+"  "+id) {
			t.Fatalf("step %d (%s) is not a numbered row of its own:\n%s", at+1, id, card)
		}
	}
	if hasRow(lines, "4  ") {
		t.Fatalf("the main flow numbers a step that only one arm reaches:\n%s", card)
	}

	// A CHOICE READS AS A CHOICE, in the words somebody would say it in, and the
	// arm's own steps hang under the arm that reaches them.
	for _, want := range []string{"if contains flaky → rerun", "otherwise → explain"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the branch's arms are not on the card (%q):\n%s", want, card)
		}
	}

	// EVERY KIND SAYS WHAT IT DOES, and never what it is.
	for _, want := range []string{
		"name the test that failed and why  · reads files, searches inside files · up to 8 turns",
		"runs a command — go test -run TestFoo -count 20",
		"repeat until ok (up to 3 times)",
		"check — go test ./...",
		"asks you — land the fix?",
		"starts when you run it  · takes since, label",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}

	// AND THE FOOT IS THE BOUNDS, which is what approval is actually about —
	// one plain sentence per bound, and no sentence at all for a bound that is
	// not in force.
	for _, want := range []string{
		"can use · reads files · searches inside files · runs commands",
		"may pick its own way as it runs, up to 4 times",
		"checks its own work and fixes what it finds",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the foot does not say %q:\n%s", want, card)
		}
	}
}

// THE CARD MAY NOT SPEAK ABOUT ITS OWN MACHINERY. Every word below names
// something inside this binary — a node kind, a field, a rung of a ladder — and
// the person being asked to keep the recipe has never seen inside this binary.
func TestTheCardNamesNoMachineryAtAll(t *testing.T) {
	for _, harness := range []Harness{flake(), postset()} {
		// A NODE'S OWN NAME IS EXEMPT and has to be: the designer wrote it, the
		// person can rename it, and `split-variants` is a word about the work
		// however much it reads like one about the program. What is being
		// checked is everything the CARD adds around those names.
		card := Card(harness)
		ids := make([]string, 0, len(harness.Program.Nodes))
		for _, node := range harness.Program.Nodes {
			ids = append(ids, node.Id)
		}
		sort.Slice(ids, func(i, j int) bool { return len(ids[i]) > len(ids[j]) })
		for _, id := range ids {
			card = strings.ReplaceAll(card, id, "~")
		}
		banned := append(kindNames(),
			"width", "cap ", "dyn", "ladder", "invariants", "rederive", "adversarial",
			"schema", "join", "max_turns", "max_rounds", "whitelist", "verified", "verdict",
		)
		for _, word := range banned {
			if strings.Contains(card, word) {
				t.Fatalf("%s's card says %q, which is a word about this program and not about the work:\n%s",
					harness.Id.Name, word, card)
			}
		}
	}
}

// A LANE IS DRAWN ONCE. The card used to preview a fan-out's lanes under the
// fan-out AND then number them again as steps of the main flow, and there is no
// reading of that page which is not "it happens twice".
func TestTheCardDrawsAFanOutsLanesExactlyOnce(t *testing.T) {
	harness := postset()
	if err := Validate(harness); err != nil {
		t.Fatalf("the sample harness does not validate: %v", err)
	}
	card := Card(harness)
	if !strings.Contains(card, "side by side, one lane each:") {
		t.Fatalf("the fan-out has no plain heading:\n%s", card)
	}
	for _, lane := range []string{"make-twitter", "make-linkedin"} {
		if got := strings.Count(card, lane); got != 1 {
			t.Fatalf("lane %q is on the card %d times:\n%s", lane, got, card)
		}
	}
	// A GATHER DRAWS NOTHING AT ALL: coming back to one thread is said by the
	// indent ending, and a row for it would be the card describing its layout.
	if strings.Contains(card, "join-variants") {
		t.Fatalf("the gather took a row of its own:\n%s", card)
	}
	// The lanes are indented under the heading and the main flow is not.
	for _, want := range []string{"\n2  split-variants", "\n     ·  make-twitter"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not nest (%q):\n%s", want, card)
		}
	}
	for _, want := range []string{
		"across the platform variants named in the spec  · up to 3 at once",
		"check — Every variant named in the spec exists on disk",
		"can use · makes images · looks at images · reads files · runs commands",
		"may open up to 3 extra lanes while it runs",
		"checks its own work against the rules it was given",
		"tried on · both-platforms · twitter-only",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
}

// A choice pointing at work that happens either way is a POINTER and stays one:
// nesting the whole rest of the program under one arm would say the rest only
// happens if the choice goes that way.
func TestTheCardKeepsSharedWorkOnTheMainFlow(t *testing.T) {
	harness := postset()
	harness.Program.Nodes = append(harness.Program.Nodes,
		Node{Id: "land", Kind: KindHumanGate, Fields: Fields{"ask": "ship them?"}})
	harness.Program.Edges = append(harness.Program.Edges,
		Edge{"recover-missing", "land"}, Edge{"report", "land"})
	card := Card(harness)
	if !strings.Contains(card, "\n5  land") {
		t.Fatalf("work both arms reach is not a step of the run:\n%s", card)
	}
	if got := strings.Count(card, "ship them?"); got != 1 {
		t.Fatalf("the shared step is on the card %d times:\n%s", got, card)
	}
}

// THE EMPTINESS LAW ON THE FOOT: a recipe that decides nothing and believes
// whatever it is handed says nothing about either, rather than printing the
// bottom rung of two ladders as though they were features.
func TestTheCardSaysNothingAboutBoundsThatAreNotInForce(t *testing.T) {
	harness := Harness{
		Id:        Id{Name: "quiet", Version: 1},
		Program:   Program{Nodes: []Node{{Id: "think", Kind: KindAgentLoop, Fields: Fields{"brief": "think"}}}},
		Whitelist: []string{"read"},
	}
	card := Card(harness)
	for _, banned := range []string{"fixed", "accept", "may ", "checks", "tried on"} {
		if strings.Contains(card, banned) {
			t.Fatalf("the foot says %q about a bound nobody set:\n%s", banned, card)
		}
	}
	if !strings.Contains(card, "can use · reads files") {
		t.Fatalf("the foot drops the one bound that IS in force:\n%s", card)
	}
}

func TestTheCardCallsAnUnregisteredHarnessADraft(t *testing.T) {
	harness := flake()
	harness.Id.Version = 0
	if card := Card(harness); !strings.Contains(card, "draft") {
		t.Fatalf("an unregistered harness does not say it is a draft:\n%s", card)
	}
}

// An empty whitelist is not an unknown, it is the strongest BOUND this card can
// report — a recipe that touches nothing — so it is said rather than left off.
func TestTheCardSaysWhenAHarnessHasNoTools(t *testing.T) {
	harness := Harness{
		Id:      Id{Name: "quiet", Version: 1},
		Program: Program{Nodes: []Node{{Id: "think", Kind: KindAgentLoop, Fields: Fields{"brief": "think"}}}},
	}
	if card := Card(harness); !strings.Contains(card, "can use · nothing") {
		t.Fatalf("a harness with no whitelist does not say so:\n%s", card)
	}
}

// A tool this build has never heard of falls through to its own name. The card
// has to survive a whitelist naming a verb that does not exist, because that is
// exactly the page a person most needs to be able to read.
func TestTheCardFallsBackToAToolsOwnNameWhenItHasNoWordsForIt(t *testing.T) {
	harness := Harness{
		Id: Id{Name: "odd", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "call", Kind: KindToolCall, Fields: Fields{"tool": "telepathy", "args": "now"}},
		}},
		Whitelist: []string{"telepathy"},
	}
	card := Card(harness)
	for _, want := range []string{"telepathy — now", "can use · telepathy"} {
		if !strings.Contains(card, want) {
			t.Fatalf("an unknown tool does not fall back to its own name (%q):\n%s", want, card)
		}
	}
}

// A card is what a person reads to find out a shape is wrong, so it has to
// survive the shapes Validate is about to refuse.
func TestTheCardDrawsAProgramThatCannotBeWalked(t *testing.T) {
	harness := flake()
	harness.Program.Edges = append(harness.Program.Edges, Edge{"land", "name-it"})
	if err := Validate(harness); err == nil {
		t.Fatal("a cycle was accepted, so this test is about nothing")
	}
	if card := Card(harness); !strings.Contains(card, "triage-flake") || !strings.Contains(card, "land") {
		t.Fatalf("a cyclic program drew no card:\n%s", card)
	}
}

func TestTheRunCardShowsThePathTheRunTook(t *testing.T) {
	card := RunCard(Trace{
		Id:      Id{Name: "triage-flake", Version: 2},
		Status:  StatusDeclined,
		Elapsed: 90 * time.Second,
		Spent:   1,
		Trail: []Trail{
			{Step: 1, Id: "name-it", Kind: KindAgentLoop, Out: "it is the reconciler", Elapsed: time.Second},
			{Step: 2, Id: "check", Kind: KindVerify, Err: "go test ./...: exit 1"},
			{Step: 3, Id: "land", Kind: KindHumanGate, Out: "declined"},
		},
	})
	for _, want := range []string{
		"triage-flake · v2 · declined", "it is the reconciler",
		"go test ./...: exit 1", "✗", "✓", "spent  1",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the run card does not say %q:\n%s", want, card)
		}
	}
}

// A trace written by a plain Run carries its own status; one hand-built from an
// older page does not, and is read rather than left blank.
func TestTheRunCardReadsAStatusOffATraceThatHasNone(t *testing.T) {
	card := RunCard(Trace{Id: Id{Name: "old", Version: 1}, Err: "node %q failed"})
	if !strings.Contains(card, "failed") {
		t.Fatalf("a trace with an error but no status does not read as failed:\n%s", card)
	}
}

func hasRow(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			return true
		}
	}
	return false
}

func itoaCard(n int) string { return string(rune('0' + n)) }
