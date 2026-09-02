package orchestrate

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// WHAT A NODE IS CALLED, and the one thing it may never be called: the head of
// the brief it hands its worker.
//
// The defect these pin is a real screen. A run divided nine ways drew nine rows
// under it, and every one of them read "You are a" — the first words of a
// self-contained goal, which law 4 has the planner open by telling the worker
// what it is. A column of them names every worker after how its instructions
// cleared their throat.

func TestAPlannersOwnNameIsWhatTheNodeIsCalled(t *testing.T) {
	got := NodeTitle(Node{
		ID:    "token-bucket",
		Title: "token bucket",
		Goal:  "You are a systems engineer. Price a token-bucket limiter against the traffic shapes in your input.",
	})
	if got != "token bucket" {
		t.Fatalf("the node is called %q, want the name the planner wrote", got)
	}
}

// THE MECHANICAL ANSWER, and it comes off a structured field. A node with no
// title is named from its id — which the law already requires to be a slug — and
// never from a sentence cut short.
func TestANamelessNodeIsCalledAfterItsIdAndNeverItsGoal(t *testing.T) {
	for _, probe := range []struct{ id, want string }{
		{"token-bucket", "token bucket"},
		{"sliding_window_log", "sliding window log"},
		{"n1", "n1"},
		{SynthesisID, "synthesis"},
	} {
		got := NodeTitle(Node{
			ID:   probe.id,
			Goal: "You are a systems engineer working on one part of a larger goal. Do the following.",
		})
		if got != probe.want {
			t.Errorf("a nameless %q is called %q, want %q", probe.id, got, probe.want)
		}
		if strings.HasPrefix(got, "You are") {
			t.Errorf("a nameless %q took its name out of the goal: %q", probe.id, got)
		}
	}
}

// A NAME IS CAPPED AT THE LENGTH THE COLUMN DRAWS, on a word boundary, and a
// name that is already short is left exactly as it is.
func TestALongNameIsCutToTheWordsAColumnCanShow(t *testing.T) {
	long := NodeTitle(Node{ID: "n1", Title: "price every one of the eleven adapters"})
	if words := strings.Fields(long); len(words) != NameWords {
		t.Fatalf("a long name came back as %q — %d words, want %d", long, len(words), NameWords)
	}
	if short := NodeTitle(Node{ID: "n1", Title: "traffic shapes"}); short != "traffic shapes" {
		t.Fatalf("a two-word name came back as %q", short)
	}
}

// THE FRONTIER IS NEVER WRITTEN WITH A NAMELESS NODE. apply is the one writer,
// so this is the seam that guarantees every surface downstream has something to
// draw — and a planner that forgot the key loses three words, never its whole
// amendment.
func TestEveryNodeOnTheFrontierIsAdmittedWithAName(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{
		{ID: "traffic", Title: "traffic shapes", Goal: "You are a systems engineer. Enumerate the traffic shapes."},
		{ID: "retry-storms", Goal: "You are a systems engineer. Work out what a rejection must carry."},
	}})
	snap := run.Snapshot()
	if len(snap.Nodes) != 2 {
		t.Fatalf("%d nodes on the frontier", len(snap.Nodes))
	}
	for _, node := range snap.Nodes {
		if node.Title == "" {
			t.Fatalf("node %q was admitted nameless", node.ID)
		}
		if strings.HasPrefix(node.Title, "You are") {
			t.Fatalf("node %q is called %q, which is the head of its goal", node.ID, node.Title)
		}
	}
	if snap.Nodes[1].Title != "retry storms" {
		t.Fatalf("the nameless node is called %q, want its id spelled out", snap.Nodes[1].Title)
	}
}

// AND THE LAW ASKS FOR IT, in the figure the code applies rather than a number
// somebody typed into the prose beside it.
func TestTheLawAsksForANameAtTheLengthTheCodeCaps(t *testing.T) {
	if !strings.Contains(PlannerPrompt, `"title"`) {
		t.Fatal("the law never names the title key")
	}
	if strings.Contains(PlannerPrompt, "{{") {
		t.Fatal("the law reached a planner with a placeholder unfilled")
	}
	// The law is prose and wraps where it wraps, so the sentence is read with its
	// whitespace flattened rather than with a line break guessed at.
	flat := strings.Join(strings.Fields(PlannerPrompt), " ")
	if !strings.Contains(flat, "at most "+strconv.Itoa(NameWords)+" words") {
		t.Fatalf("the law does not quote %d as the cap on a name", NameWords)
	}
	if !strings.Contains(flat, "Name the role or the slice, never the instructions") {
		t.Fatal("the law does not say what a name is of")
	}
}

// ── the ids that are not names ──────────────────────────────────────────────
//
// The second screen these pin is also real, and it is the one the id fallback
// above could not answer. A planner numbered its own list — `r1` through `r7`,
// with `synth` at the foot of them — and every one of those ids spelt out as
// itself, so the rail drew a family of eight rows named after the machine's
// filing. A slug like `token-bucket` has words in it; `r1` has none.

// THE QUESTION IS ASKED GENERICALLY, and it is one question: is there anything
// here a person could read as the name of the work. Nothing here knows what
// shape an id is, and nothing here may learn — a planner that numbers its nodes
// `step1` next month is the same defect against a blacklist that names `r%d`.
func TestANameThatIsNothingButTheIdIsNotAName(t *testing.T) {
	for _, probe := range []struct {
		node Node
		want bool
	}{
		{Node{ID: "r1"}, true},
		{Node{ID: "r1", Title: "r1"}, true},
		{Node{ID: "synth"}, true},
		{Node{ID: "n3", Title: "  "}, true},
		{Node{ID: "slice-a", Title: "paste 1"}, true},
		{Node{ID: "slice-b", Title: "step-2"}, true},
		{Node{ID: "slice-c", Title: "task_03"}, true},
		{Node{ID: "slice-d", Title: "/tmp/parser.go"}, true},
		{Node{ID: "slice-e", Title: "inspect every parser implementation in the tree"}, true},
		{Node{ID: "token-bucket"}, false},
		{Node{ID: "r1", Title: "retry storms"}, false},
		{Node{ID: "sliding_window_log"}, false},
		{Node{ID: "migration", Title: "phase 2"}, false},
		{Node{ID: "bug", Title: "issue 376"}, false},
		{Node{ID: "parser", Title: "step 2 parser"}, false},
	} {
		if got := NodeNeedsName(probe.node); got != probe.want {
			t.Errorf("a node %q titled %q needs a name: %v, want %v",
				probe.node.ID, probe.node.Title, got, probe.want)
		}
	}
}

func TestPathAndSentencePlannerTitlesGoThroughTheNamerBeforeClipping(t *testing.T) {
	namer := &namerOf{answer: "parser boundary audit"}
	run := withNamer(namer.name)
	run.apply(Amendment{Add: []Node{
		{ID: "path", Title: "/tmp/parser.go", Goal: "Inspect the parser."},
		{ID: "sentence", Title: "inspect every parser implementation in the tree", Goal: "Inspect every parser."},
	}})
	for _, id := range []string{"path", "sentence"} {
		if got := named(t, run, id); got != "parser boundary audit" {
			t.Fatalf("node %q settled as %q, want the namer's answer", id, got)
		}
	}
	asked := namer.seen()
	sort.Strings(asked)
	if strings.Join(asked, ",") != "path,sentence" {
		t.Fatalf("namer saw %v, want both invalid planner titles", asked)
	}
}

func TestGenericOrdinalNamesAreExactAndUnicodeAware(t *testing.T) {
	for _, probe := range []struct {
		name string
		want bool
	}{
		{"paste 1:", true},
		{"PASTE#1", true},
		{"node\u0662", true},
		{"conversation 004", true},
		{"task 21st", true},
		{"step32nd", true},
		{"issue 441", false},
		{"python 3", false},
		{"task 2 parser", false},
		{"paste parser", false},
	} {
		if got := IsGenericOrdinalName(probe.name); got != probe.want {
			t.Errorf("IsGenericOrdinalName(%q) = %v, want %v", probe.name, got, probe.want)
		}
	}
}

// namerOf answers one scripted name and says how many times it was asked, so a
// test can pin both halves: the nodes that reach the namer and the nodes that
// never should.
type namerOf struct {
	mu     sync.Mutex
	answer string
	asked  []string
}

func (n *namerOf) name(_ context.Context, node Node) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.asked = append(n.asked, node.ID)
	return n.answer
}

func (n *namerOf) seen() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.asked...)
}

// named waits for one node's title to settle on something, because the namer
// runs on a goroutine of its own and nothing in the run waits for it.
func named(t *testing.T, run *Orchestrator, id string) string {
	t.Helper()
	for waited := time.Duration(0); waited < 2*time.Second; waited += 5 * time.Millisecond {
		for _, node := range run.Snapshot().Nodes {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ""
}

// withNamer is a run wired to one, which is what every run in the product is:
// the session hands this seam its own small naming model (internal/session's
// taskname.go).
func withNamer(namer func(context.Context, Node) string) *Orchestrator {
	return New("the goal", plannerFunc(func(context.Context, View) (Amendment, error) {
		return Amendment{}, nil
	}), execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		return "", 0, nil
	}), Options{Name: namer})
}

// A NODE FILED UNDER A NUMBER IS NAMED BY THE NAMER, and the row it is drawn on
// never shows the number: the first publish carries no name at all, and the name
// arrives as a rename a moment later.
func TestANodeFiledUnderAnIdIsNamedAndTheIdIsNeverPublished(t *testing.T) {
	namer := &namerOf{answer: "vendor pricing"}
	run := withNamer(namer.name)

	var drawn [][]NodeStatus
	var mu sync.Mutex
	run.onNodes = func(nodes []NodeStatus) {
		mu.Lock()
		drawn = append(drawn, nodes)
		mu.Unlock()
	}
	run.apply(Amendment{Add: []Node{
		{ID: "r1", Goal: "You are a competitive intelligence analyst. Collect every published price."},
		{ID: "traffic-shapes", Goal: "You are a systems engineer. Enumerate the traffic shapes."},
	}})

	if got := named(t, run, "r1"); got != "vendor pricing" {
		t.Fatalf("the node filed under r1 is called %q, want the namer's answer", got)
	}
	if asked := namer.seen(); len(asked) != 1 || asked[0] != "r1" {
		t.Fatalf("the namer was asked about %v, want only the node with no name of its own", asked)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, publish := range drawn {
		for _, node := range publish {
			if node.ID == "r1" && node.Title == "r1" {
				t.Fatal("a row of this run was published under the id r1")
			}
			if node.ID == "traffic-shapes" && node.Title != "traffic shapes" {
				t.Errorf("the node with words in its id is called %q", node.Title)
			}
		}
	}
	if len(drawn) < 2 {
		t.Fatalf("%d publishes: a name that lands after the row is drawn has to redraw it", len(drawn))
	}
}

// AND THE PLANNER'S OWN NAME IS NEVER PAID FOR TWICE. A node that arrived named
// is not sent to a model to be named again — the expensive call already did that
// work, and a second one would be the harness disagreeing with itself.
func TestANamedNodeIsNeverSentToTheNamer(t *testing.T) {
	namer := &namerOf{answer: "something else"}
	run := withNamer(namer.name)
	run.apply(Amendment{Add: []Node{
		{ID: "r1", Title: "retry storms", Goal: "You are a systems engineer."},
		{ID: "token-bucket", Goal: "You are a systems engineer."},
	}})
	time.Sleep(50 * time.Millisecond)
	if asked := namer.seen(); len(asked) != 0 {
		t.Fatalf("the namer was asked about %v, and both of those nodes already had names", asked)
	}
}

// A NAMER THAT ANSWERS NOTHING COSTS A GOOD NAME AND NOTHING ELSE. The node
// falls back to where it would have been standing all along, which is the id —
// the last resort, and the only place it is ever drawn.
func TestANamerThatAnswersNothingLeavesTheIdAsTheLastResort(t *testing.T) {
	run := withNamer(func(context.Context, Node) string { return "" })
	run.apply(Amendment{Add: []Node{{ID: "r1", Goal: "You are a systems engineer."}}})
	if got := named(t, run, "r1"); got != "r1" {
		t.Fatalf("a node whose namer said nothing is called %q, want its id as the last resort", got)
	}
}

// AND A RUN WITH NOBODY TO ASK KEEPS THE MECHANICAL NAME IT ALWAYS HAD. This is
// every headless caller and every scripted test: there is no conversation behind
// the run, so there is no small model to bill three words to.
func TestARunWithNoNamerNamesItsNodesFromTheirIds(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{{ID: "r1", Goal: "You are a systems engineer."}}})
	nodes := run.Snapshot().Nodes
	if len(nodes) != 1 || nodes[0].Title != "r1" {
		t.Fatalf("the frontier is %+v, want one node named from its id", nodes)
	}
}
