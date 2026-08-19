package session

// WHAT A WORKER IS TOLD, pinned. The shape of a spawned task's opening message
// is a contract with two audiences at once — the worker that reads it and the
// person whose words are in it — and it is composed by code precisely so that
// nothing has to remember to include the second one.

import (
	"context"
	"strings"
	"testing"
)

// THE OPENING MESSAGE IS A DOCUMENT WITH FOUR PARTS, IN ONE ORDER: what the
// person said, in their own words, then the work, then what must exist at the
// end, then what done means.
func TestASpawnedTaskOpensOnThePersonsOwnWordsThenTheContract(t *testing.T) {
	opening := composeBrief(
		"make the pricing page match the new tiers, and don't touch the tests",
		"edit docs/pricing.md against internal/billing/tiers.go",
		"docs/pricing.md, one table, four rows",
		"go test ./internal/billing/ passes and the page names all four tiers")

	for _, want := range []string{
		briefAskHeading, briefAskRule,
		"make the pricing page match the new tiers, and don't touch the tests",
		briefWorkHeading, "edit docs/pricing.md against internal/billing/tiers.go",
		briefMakeHeading, "docs/pricing.md, one table, four rows",
		briefDoneHeading, "go test ./internal/billing/ passes",
	} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the opening message is missing %q:\n%s", want, opening)
		}
	}

	at := func(heading string) int {
		index := strings.Index(opening, heading)
		if index < 0 {
			t.Fatalf("no %q in:\n%s", heading, opening)
		}
		return index
	}
	if !(at(briefAskHeading) < at(briefWorkHeading) &&
		at(briefWorkHeading) < at(briefMakeHeading) &&
		at(briefMakeHeading) < at(briefDoneHeading)) {
		t.Fatalf("the four parts are out of order:\n%s", opening)
	}
}

// AN EMPTY PART IS ABSENT, never a heading over nothing — the emptiness law
// applied to a document. A node restored from a checkpoint written before
// requests were carried has no request, and reads as it always did.
func TestABriefWithNothingToSayInAPartLeavesThatPartOut(t *testing.T) {
	opening := composeBrief("", "sweep the deprecated calls", "", "the build passes")
	for _, unwanted := range []string{briefAskHeading, briefMakeHeading} {
		if strings.Contains(opening, unwanted) {
			t.Fatalf("an empty part got a heading anyway:\n%s", opening)
		}
	}
	if !strings.Contains(opening, briefWorkHeading) || !strings.Contains(opening, briefDoneHeading) {
		t.Fatalf("the parts that had something to say went missing:\n%s", opening)
	}
}

// A PERSON WHO WROTE THE BRIEF THEMSELVES IS QUOTED ONCE. There is no
// paraphrase on that path, so their words are the whole of the work and
// printing them twice would read as two instructions that happen to agree.
func TestAPersonAuthoredTaskDoesNotSayTheSameThingTwice(t *testing.T) {
	words := "rewrite the importer so it streams"
	opening := composeBrief(words, words, "", "it streams")
	if got := strings.Count(opening, words); got != 1 {
		t.Fatalf("the person's words appear %d times:\n%s", got, opening)
	}
	if strings.Contains(opening, briefWorkHeading) {
		t.Fatalf("an empty work section got a heading:\n%s", opening)
	}
}

// THE VERBATIM ASK IS BOUNDED, because it rides into every node of a run and a
// person who pasted a log must not be paid for once per node. The cut is marked
// so a worker can see it was cut.
func TestTheVerbatimAskIsBoundedAndSaysSo(t *testing.T) {
	opening := composeBrief(strings.Repeat("x", briefAskLimit*2), "work", "", "done")
	if len(opening) > briefAskLimit+2000 {
		t.Fatalf("an unbounded ask reached the worker: %d bytes", len(opening))
	}
	if !strings.Contains(opening, "…") {
		t.Fatalf("the ask was cut without saying so:\n%s", opening[:200])
	}
}

// ── the code captures it, the model does not hand it over ───────────────────

// THE PERSON'S MESSAGE REACHES THE NODE WITHOUT THE MODEL BEING ASKED FOR IT.
// The proposal below names a brief, a deliverable and an acceptance and never
// repeats what was typed; the node still opens on the typed sentence.
func TestTheNodeIsHandedThePersonsMessageTheModelNeverPassedOn(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Sweep the deprecated calls", "replace every call to Frobnicate"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.graph.complete(node, TaskDone)
	})

	typed := "sweep the Frobnicate calls, but leave internal/legacy alone"
	events, err := agent.Submit(context.Background(), typed)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := ran.await(t)
	opening := node.instruction()
	if !strings.Contains(opening, typed) {
		t.Fatalf("the person's own message never reached the node:\n%s", opening)
	}
	if !strings.Contains(opening, briefAskHeading) {
		t.Fatalf("their words are in there unlabelled:\n%s", opening)
	}
	// And the contract the model DID groom is still all there, under it.
	for _, want := range []string{"replace every call to Frobnicate", briefMakeHeading, briefDoneHeading} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the contract is missing %q:\n%s", want, opening)
		}
	}
	if strings.Index(opening, typed) > strings.Index(opening, "replace every call to Frobnicate") {
		t.Fatalf("the paraphrase came before the person:\n%s", opening)
	}
}

// A NOTE THE SESSION WROTE IS NOT THE PERSON ASKING. A turn woken by a task
// landing must not quote "task 4 has finished" as somebody's request, so the
// last thing THEY typed is what stands.
func TestASessionAuthoredNoteNeverBecomesThePersonsRequest(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.rememberAskLocked(userText("audit the pricing code"))
	agent.rememberAskLocked(wakeNote("task 4 has finished: the audit is done"))
	note := userText("the watch on build.log saw an error")
	note.authored = true
	agent.rememberAskLocked(note)
	agent.mu.Unlock()

	if got := agent.taskRequest(); got != "audit the pricing code" {
		t.Fatalf("the session's own line became the person's request: %q", got)
	}
}

// A STEERING MESSAGE IS THE NEWEST THING THEY ASKED FOR. It arrives mid-turn
// and is often the correction the work about to be handed off must carry.
func TestSteeringBecomesTheRequestWorkHandedOffAfterItCarries(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.rememberAskLocked(userText("audit the pricing code"))
	agent.steering = append(agent.steering, userText("actually only internal/billing"))
	agent.drainSteeringLocked()
	agent.mu.Unlock()

	if got := agent.taskRequest(); got != "actually only internal/billing" {
		t.Fatalf("the correction never became the request: %q", got)
	}
}

// A NODE INHERITS THE REQUEST OF THE TASK IT WAS HANDED OUT BY. There is nobody
// in a worktree to type anything, so a sub-task three levels down is still
// working against the sentence that started all of it.
func TestASubTaskInheritsTheSentenceThatStartedTheFamily(t *testing.T) {
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := conversation.graph()
	graph.run = func(*TaskNode) {}
	parent := graph.reserve()
	graph.admit(parent, taskSpec{
		title: "t", request: "port the whole client to v2", brief: "b", acceptance: "a",
	})

	node, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.taskID = parent
		config.taskDepth = 1
		config.tasker = graph
	})
	if got := node.taskRequest(); got != "port the whole client to v2" {
		t.Fatalf("a sub-task lost the person's words: %q", got)
	}
}

// A PROPOSAL WITH NO DELIVERABLE IS REFUSED IN PLAIN WORDS, in the order the
// fields are read. Work groomed without saying what must exist at the end is
// how a task comes back having thought about something rather than made it.
func TestAProposalMustSayWhatMustExistWhenItIsOver(t *testing.T) {
	_, problem := parseTaskArguments([]byte(
		`{"title":"t","summary":"s","brief":"b","acceptance":"a"}`))
	if problem != "Invalid arguments: deliverable is required" {
		t.Fatalf("problem = %q", problem)
	}
	if _, ok := parseTaskArguments([]byte(
		`{"title":"t","summary":"s","brief":"b","deliverable":"d","acceptance":"a"}`)); ok != "" {
		t.Fatalf("a whole contract was refused: %q", ok)
	}
}
