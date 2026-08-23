package session

// WHAT A PERSON IS PART OF WHILE THEY STAND IN A NODE'S ROOM.
//
// A design thread is the one node somebody TALKS INSIDE rather than watches, and
// until [TaskNode.context] existed nothing on the wire said so — every surface
// drew a sentence typed at a design exactly as it draws a remark to nobody in
// particular. These hold the engine's half: the node names itself, the name
// reaches the notice, an ordinary node names nothing, and the name is the page's
// own once there is a page.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// parkedGraph is a graph whose frontier starts nodes and then leaves them alone.
// These tests are about what a node SAYS about itself, and a scripted body is how
// every other test in this package holds a node still long enough to ask.
func parkedGraph() *TaskGraph {
	graph := newTaskGraph()
	graph.run = func(*TaskNode) {}
	return graph
}

// A NODE THAT NAMES A CONTEXT PUTS IT ON EVERY NOTICE, which is how a surface
// standing in the room learns what the room IS.
func TestANamedWorkingContextRidesOnTheNodesNotice(t *testing.T) {
	graph := parkedGraph()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "harness · chase a flaky test"})
	node := graph.nodes[id]

	if got := node.notice().Context; got != "" {
		t.Fatalf("a node named a context before anything set one: %q", got)
	}
	node.contextNow(designContextWord)
	if got := node.notice().Context; got != designContextWord {
		t.Fatalf("the notice carries %q, want the node's own context", got)
	}
}

// AND AN ORDINARY NODE NAMES NOTHING, for as long as it lives. Work handed to a
// worker in a worktree is not a place somebody is inside, so there is nothing to
// say — the emptiness law, which is what makes the word mean something on the one
// kind of node that does carry it.
func TestAnOrdinaryNodeNamesNoWorkingContext(t *testing.T) {
	graph := parkedGraph()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "fix the nil-map crash", brief: "the parser panics"})
	node := graph.nodes[id]
	node.finish("done", nil, "", "")

	if got := node.notice().Context; got != "" {
		t.Fatalf("an ordinary node named a working context: %q", got)
	}
}

// THE NAME IS THE PAGE'S OWN, and it cannot be said any earlier than the page
// exists. A design admitted a moment ago has written nothing, so it says what it
// is and no more; the moment a page is written the context carries that page's
// real name.
func TestTheDesignContextTakesThePagesRealName(t *testing.T) {
	page := subharness.Harness{}
	page.Id.Name = "flake-triage"

	if got := namedDesignContext(""); got != designContextWord {
		t.Fatalf("a nameless design says %q, want the unnamed context", got)
	}
	named := namedDesignContext(page.Id.Name)
	if named == designContextWord {
		t.Fatal("a written page did not change the context word")
	}
	if !strings.Contains(named, page.Id.Name) {
		t.Fatalf("the context %q does not carry the page's name %q", named, page.Id.Name)
	}
}

// AND THE NOUN IS "subharness" WHEREVER A PERSON READS IT. There is one system
// and one word for it; a context that said anything else would teach a second
// name for one thing on the one line somebody reads about their own sentence.
func TestTheDesignContextSpeaksOfASubharness(t *testing.T) {
	for _, word := range []string{designContextWord, namedDesignContext("flake-triage")} {
		if !strings.Contains(word, "subharness") {
			t.Fatalf("the context %q does not name a subharness", word)
		}
	}
}
