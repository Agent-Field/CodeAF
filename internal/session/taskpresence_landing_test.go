package session

import (
	"strings"
	"testing"
)

// THE PRESENCE LINE MUST AGREE WITH THE RECORD CARD (C3). A landed node whose
// accept is in flight is ANSWERED: the card says `accepted · still working on
// it` (question.go), so home must not say `your call` about it. These two tests
// pin the home side, which the question-lifecycle tests never did: that is how
// the two readers drifted apart while every test stayed green.

// ONE LANDING, ACCEPTED, MERGE IN FLIGHT: home must not say `your call`.
func TestAnAcceptedLandingInFlightDoesNotSayYourCall(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	if _, err := node.claimSettle("your accept"); err != nil {
		t.Fatalf("the settle could not claim the node: %v", err)
	}
	ask := agent.waitingOnPerson()
	if ask.waiting {
		t.Fatalf("home says %q about a question already answered (the accept is in flight)", ask.reason)
	}
}

// TWO LANDINGS, THE OLDER ONE SETTLING: the row must name the one still
// waiting, not the answered one the graph admitted first.
func TestTheYourCallRowNamesTheLandingStillWaiting(t *testing.T) {
	agent, settled := unverifiedNode(t, nil)
	if _, err := settled.claimSettle("your accept"); err != nil {
		t.Fatalf("the settle could not claim the older node: %v", err)
	}
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish(yourCallLead(TaskFacts{})+"the checker could not be asked: dial tcp: connection refused", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Sweep the cache", brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))
	ask := agent.waitingOnPerson()
	if !ask.waiting {
		t.Fatal("an open landing is waiting and home did not say so")
	}
	if !strings.Contains(ask.reason, "Sweep the cache") || strings.Contains(ask.reason, "Add the guard") {
		t.Fatalf("the row names the answered landing instead of the one still waiting: %q", ask.reason)
	}
}
