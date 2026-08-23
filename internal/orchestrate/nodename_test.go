package orchestrate

import (
	"strconv"
	"strings"
	"testing"
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
