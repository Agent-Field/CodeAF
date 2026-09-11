package session

// THE DELIVERY OF AN OWNED RESULT, PROVED (the four-module trial, 2026-09-06).
//
// A person asked for a four-module repair delivered on a branch with a final
// commit. The task did the repair and landed — 29 independent checks passed and
// the protected files were untouched — and the turn that read its report
// cherry-picked the work across. That is several files under the workspace, so
// the write seam fired and handed the INTEGRATION to a second task in a fresh
// worktree that could not see the staged index it was standing in. The
// requested commit never happened.
//
// Every test here drives the real turn: a real node in the real graph, settled
// through the real completion path, whose landing wakes a real turn that writes
// past the allowance. What is asserted is the seam's own decision at that
// boundary, never a helper that restates it.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// deliveryAsk is the person's own words on the measured trial, and it is
	// what the task is admitted with — so the result's reply tag carries it and
	// the woken turn owes it (wakecause.go).
	deliveryAsk = "repair the four dutylog modules and deliver them on branch " +
		"fix/dutylog-pipeline with a final commit"
	deliveryReport = "the four modules are repaired on fix/dutylog-pipeline; " +
		"29 checks pass and the protected files are untouched"
	// deliveryRounds is how many writing rounds the integration takes. It is
	// under the mark ladder's first rung (checkpointPrice) on purpose: this file
	// is about the write seam, and a turn that also crossed a mark would be two
	// seams in one assertion.
	deliveryRounds = 8
)

// THE MEASURED FAILURE, EXACTLY: the delivery of a result this conversation
// already owns finishes HERE, and no second task is started under it.
func TestDeliveringAnOwnedResultIsNotHandedToASecondTask(t *testing.T) {
	completer := &scriptedCompleter{steps: writingSteps(deliveryRounds, checkpointChainSketch,
		"finish the integration")}
	agent, workspace := writeSeamAgent(t, completer)
	graph, ran := deliveryGraph(agent)

	owned := admitOwnedTask(t, agent, deliveryAsk)
	// The landing wakes a turn here, and that turn integrates: it writes across
	// the workspace exactly as the cherry-pick did.
	<-ran
	waitDoneNode(t, owned)

	// THE WHOLE DELIVERY LANDS. The file past the allowance is the assertion: on
	// the measured failure the turn ended two writes in, and everything after it
	// — the staging, the commit — went to a worktree that had none of it.
	// EITHER ENDING ENDS THE WAIT, so the assertions below are what report the
	// failure rather than a timeout: the delivery finishing is the guarantee,
	// and a second task appearing is the measured failure.
	waitFor(t, "the delivery turn to end", func() bool {
		return wroteFile(workspace, deliveryRounds-1) || admitted(agent.graph()) > 1
	})
	waitForQuiet(t, agent)

	// AND EXACTLY ONE TASK EXISTS: the one that did the repair.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks exist after the delivery, want only the one that was owned", count)
	}
	if text := transcriptText(agent); strings.Contains(text, writeSeamNote) {
		t.Fatalf("the seam moved a delivery it owns; the transcript says:\n%s", text)
	}
}

// AND THE REFUSAL IS WRITTEN DOWN, once, naming the result it stood down over —
// because a seam whose decision left no trace is how two completely different
// runs came to leave identical journals (#567).
func TestTheHeldWriteSeamIsWrittenDownOnceWithItsResult(t *testing.T) {
	completer := &scriptedCompleter{steps: writingSteps(deliveryRounds, checkpointChainSketch,
		"finish the integration")}
	journalPath := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := writeSeamAgent(t, completer, func(config *Config) {
		config.SessionFile = journalPath
	})
	_, ran := deliveryGraph(agent)

	owned := admitOwnedTask(t, agent, deliveryAsk)
	<-ran
	waitDoneNode(t, owned)
	// EITHER ENDING ENDS THE WAIT, so the assertions below are what report the
	// failure rather than a timeout: the delivery finishing is the guarantee,
	// and a second task appearing is the measured failure.
	waitFor(t, "the delivery turn to end", func() bool {
		return wroteFile(workspace, deliveryRounds-1) || admitted(agent.graph()) > 1
	})
	waitForQuiet(t, agent)

	journal := readJournalText(t, journalPath)
	if rows := strings.Count(journal, checkpointCeilingDelivering); rows != 1 {
		t.Fatalf("the held seam wrote %d rows saying %q, want exactly one:\n%s",
			rows, checkpointCeilingDelivering, journal)
	}
	if want := fmt.Sprintf("delivering task %d", owned.id); !strings.Contains(journal, want) {
		t.Errorf("the row does not name the result it stood down over (%s):\n%s", want, journal)
	}
	// AND IT ADMITTED NOTHING, which the field a bench counts tasks with has to
	// say: no id rides this row.
	if strings.Contains(journal, fmt.Sprintf(`"decision":%q,"reason":"delivering task %d","taskId"`,
		checkpointCeilingDelivering, owned.id)) {
		t.Errorf("the held row carries a taskId, which is the field that means a task was started:\n%s", journal)
	}
}

// A NEW BROAD ASK IS UNTOUCHED. The conversation owns a settled result; the
// person then types something else entirely, and the seam converts it exactly as
// it always did. Owning a result is not a licence over everything typed after.
func TestANewBroadAskAfterADeliveryStillMovesToATask(t *testing.T) {
	completer := &scriptedCompleter{steps: writingSteps(12, checkpointChainSketch,
		"rewrite the ingest modules\nwhat is left, and what this turn found out")}
	agent, _ := writeSeamAgent(t, completer)
	graph, ran := deliveryGraph(agent)

	// The result lands while nothing is listening, and is read off the queue, so
	// the turn below is the person's own and nothing else.
	holdTheWake(agent)
	owned := admitOwnedTask(t, agent, deliveryAsk)
	<-ran
	waitDoneNode(t, owned)
	agent.drainSteering(nil)

	events, err := agent.Submit(context.Background(),
		"now rewrite every ingest module and update all of their tests")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if node := <-ran; node.id == owned.id {
		t.Fatalf("the owned task ran again instead of the new work")
	}
	if count := admitted(graph); count != 2 {
		t.Fatalf("%d tasks exist, want the owned one and the new broad ask's", count)
	}
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("a new broad ask was not moved; notices were %q", noticeTexts(collected))
	}
}

// AND A LANDING INSIDE THE PERSON'S OWN TURN IS NOT A DELIVERY EITHER. They
// typed a broad request and a result arrived while it ran: their sentence is
// still the request, and the seam protects it.
func TestALandingInsideAPersonsOwnTurnDoesNotHoldTheSeam(t *testing.T) {
	completer := &scriptedCompleter{steps: writingSteps(12, checkpointChainSketch,
		"rewrite the ingest modules\nwhat is left, and what this turn found out")}
	agent, _ := writeSeamAgent(t, completer)
	graph, ran := deliveryGraph(agent)

	holdTheWake(agent)
	owned := admitOwnedTask(t, agent, deliveryAsk)
	<-ran
	waitDoneNode(t, owned)

	// The landing is still on the queue, so it drains into the turn the person
	// opens — the mixed turn, which owes both.
	events, err := agent.Submit(context.Background(),
		"now rewrite every ingest module and update all of their tests")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d tasks exist, want the owned one and the one the person's ask moved", count)
	}
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("a turn the person opened was held by a landing in it; notices were %q",
			noticeTexts(collected))
	}
}

// AND OWNERSHIP MUST BE PROVABLE. A result whose node this conversation's graph
// cannot vouch for — a restored node, another window's work — is not something
// this conversation can be said to own, and the seam fires exactly as it does
// for any other writing turn. A doubt is not a delivery.
func TestAResultThisConversationCannotProveItOwnsStillMovesTheWrites(t *testing.T) {
	completer := &scriptedCompleter{steps: writingSteps(12, checkpointChainSketch,
		"finish the integration\nwhat is left, and what this turn found out")}
	agent, _ := writeSeamAgent(t, completer)
	graph, ran := deliveryGraph(agent)
	agent.mu.Lock()
	agent.personAsk = deliveryAsk
	agent.mu.Unlock()

	// A landing whose id names nothing in this graph.
	note := wakeNote("task 41 finished")
	note.replyTags = []TaskReplyTag{{ID: 41, Title: "somebody else's repair", Request: deliveryAsk}}
	if !agent.enqueueNote(note) {
		t.Fatal("the landing was refused")
	}

	if node := <-ran; node == nil {
		t.Fatal("no work was moved")
	}
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted for an unprovable result, want the one the seam moved", count)
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// deliveryGraph is the real graph with a recording runner over it: every node
// it is handed is reported, and a node admitted as this conversation's own work
// (admitOwnedTask) settles through the real completion path.
func deliveryGraph(agent *Agent) (*TaskGraph, ranNodes) {
	ran := make(ranNodes, 4)
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		ran <- node
		if node.spec.request == deliveryAsk {
			node.finish(deliveryReport, nil, "", "")
			node.graph.complete(node, TaskDone)
		}
	}
	graph.mu.Unlock()
	return graph, ran
}

// admitOwnedTask puts one piece of work into the graph through the door every
// task comes through, WITH THE PERSON'S OWN WORDS ON IT — which is what the
// result's reply tag is composed from and what its landing turn is read
// against (wakecause.go). [settleTask] carries no request, so it cannot stand
// for the shape this file is about.
func admitOwnedTask(t *testing.T, agent *Agent, request string) *TaskNode {
	t.Helper()
	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title:      "repair the dutylog modules",
		request:    request,
		brief:      "repair the four modules and leave them on the branch",
		acceptance: "the four modules pass their checks on fix/dutylog-pipeline",
	})
	node := graph.node(id)
	if node == nil {
		t.Fatalf("task %d was not admitted", id)
	}
	return node
}

// holdTheWake keeps a landing on the queue instead of letting it start a turn,
// so a test can decide which turn the result arrives in.
func holdTheWake(agent *Agent) {
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
}

// wroteFile says whether the integration's nth write landed on the disk.
func wroteFile(workspace string, round int) bool {
	_, err := os.Stat(filepath.Join(workspace, fmt.Sprintf("file%d.txt", round)))
	return err == nil
}
