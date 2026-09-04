package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// THE SPAWN FLOOR, on the two questions it exists to get right: a trivial
// ask never starts a task, and a genuine multi-part ask still can.
//
// F26 is the measured failure: "commit everything" became task 5 and the
// commit never happened. The floor is the person's words, enforced at every
// door that starts a task from a conversation — not another sentence in a
// prompt.

// "commit", "undo", "fix this one line" are the issue's own needles. A
// longer commit phrase is F26's own wording. A multi-part ask is the
// fixture the route judge already converts.
func TestATrivialAskNeverSpawnsATask(t *testing.T) {
	for _, asked := range []string{"commit", "undo", "fix this one line"} {
		if !trivialAsk(asked) {
			t.Fatalf("%q is not on the floor, so the doors below cannot refuse it", asked)
		}
		assertAskNeverSpawns(t, asked)
	}
}

// AND A REAL MULTI-PART ASK STILL CAN. The floor is a closed set; lifting
// it for every short sentence would be the old conversion coming back as a
// refusal of work somebody wanted handed over.
func TestAGenuineMultiPartAskCanStillBecomeATask(t *testing.T) {
	if trivialAsk(routeEnumerated) {
		t.Fatal("the four-deliverable ask is on the floor, so nothing can convert it")
	}
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes, confirm: routeYes}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	noCard(t, collected)
	waitFor(t, "the task a multi-part ask still starts", func() bool { return nodes.count() == 1 })
	if agent.graph().node(1) == nil {
		t.Fatal("a genuine multi-part ask started nothing")
	}
	if notice := routeNotice(collected); !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the turn said %q, want the work it started", notice)
	}
}

// THE MATCHER IS A CLOSED SET. These are the phrasings the floor must
// catch and the ones it must leave alone — pinned so a later widening
// cannot eat "fix the crash" (an existing propose_task door) or "read the
// four files" (an existing write-seam door).
func TestTheSpawnFloorIsTheKnownTrivialVerbsAndNothingElse(t *testing.T) {
	for _, asked := range []string{
		"commit",
		"undo",
		"fix this one line",
		"commit everything with a sensible message",
		"please commit everything",
		"can you undo that",
		"git commit",
		"read this file",
		"edit this one file",
	} {
		if !trivialAsk(asked) {
			t.Errorf("%q should stay in the conversation", asked)
		}
	}
	for _, asked := range []string{
		"fix the crash",
		"rewrite it",
		"read the four files and tell me what they do",
		"rename the parser and fix everything that calls it",
		routeAsk,
		routeEnumerated,
		"commit everything and rewrite the tests",
		"work through the four things I listed and report back",
	} {
		if trivialAsk(asked) {
			t.Errorf("%q is real work and the floor ate it", asked)
		}
	}
}

// assertAskNeverSpawns drives the three conversation doors — propose_task,
// the post-turn judge, and the checkpoint handover — and fails if any of
// them started a task.
func assertAskNeverSpawns(t *testing.T, asked string) {
	t.Helper()
	t.Run("propose_task/"+asked, func(t *testing.T) {
		completer := &routedCompleter{parent: []step{
			proposeCall("property commit", "commit the staged work"),
			finalText("done inline"),
		}}
		agent, _ := newTestAgent(t, completer, func(config *Config) {
			config.AskConsent = true
			config.TaskAutoApproveSeconds = 1
		})
		graph := stubbedGraph(agent, func(*TaskNode) {
			t.Fatal("propose_task started a node on a trivial ask")
		})

		collected := collect(t, mustSubmit(t, agent, asked))
		if _, ok := firstOfKind(collected, EventTaskProposal); ok {
			t.Fatalf("a card was raised for %q: %v", asked, kinds(collected))
		}
		if admitted(graph) != 0 || agent.graph().node(1) != nil {
			t.Fatalf("propose_task admitted a node for %q", asked)
		}
		if output := toolOutput(t, collected, "propose_task"); !strings.Contains(output, spawnFloorRefusal) {
			t.Fatalf("propose_task answered %q, want the floor's refusal", output)
		}
		if notice := routeNotice(collected); notice != "" {
			t.Fatalf("a task was announced for %q: %q", asked, notice)
		}
	})
	t.Run("route/"+asked, func(t *testing.T) {
		completer := &routeCompleter{answer: "done.", verdict: routeYes, confirm: routeYes, ahead: routeAheadYes, aheadConfirm: routeAheadYes}
		agent, _, nodes := routeAgent(t, completer)

		collected := collect(t, mustSubmit(t, agent, asked))
		if completer.asked() != 0 {
			t.Fatalf("the post-turn judge was asked about %q", asked)
		}
		if completer.preAsked() != 0 {
			t.Fatalf("the pre-turn judge was asked about %q", asked)
		}
		noCard(t, collected)
		if nodes.count() != 0 || agent.graph().node(1) != nil {
			t.Fatalf("the route judge started work on %q", asked)
		}
		if notice := routeNotice(collected); notice != "" {
			t.Fatalf("the route judge announced %q for %q", notice, asked)
		}
	})
	t.Run("checkpoint/"+asked, func(t *testing.T) {
		agent := checkpointAgent(t, &scriptedCompleter{})
		agent.personAsk = asked
		if agent.checkpoints(context.Background(), userText(asked)) {
			t.Fatalf("the checkpoint metered %q", asked)
		}
		over := agent.handOverRunningTurn(context.Background(), newEventHub(), &Usage{},
			time.Time{}, agent.model, checkpointSplitNote, checkpointSeamMark, 0,
			routeVerdict{Work: true, Goal: asked}, checkpointRead{}, nil)
		if over.moved {
			t.Fatalf("the handover moved %q", asked)
		}
		if over.decision != checkpointCeilingTrivial {
			t.Fatalf("the handover decided %q, want %q", over.decision, checkpointCeilingTrivial)
		}
		if agent.graph().node(1) != nil {
			t.Fatalf("the handover admitted a node for %q", asked)
		}
	})
}
