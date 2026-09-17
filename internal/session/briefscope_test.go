package session

// WHOSE JOB THE PERSON'S MESSAGE IS.
//
// Every descendant inherits the person's whole message verbatim, and it used to
// arrive over the whole-job rule: "where anything below reads differently from
// it, their words are what was asked for". Handed to a part briefed to run one
// script, under a message asking for that script AND a count of something else,
// that sentence reads as instructions to do both — and where the message says
// the work belongs in a task, as licence to hand the piece out again.
//
// Their words still travel, unedited, and they still win about the piece. What
// changed is the one line over them ([briefPieceRule]).
//
// AND A PLAN-BORN WORKER ON THE BASH BELT IS A THIRD CASE, pinned at the end of
// this file: its document reorders and the person's words are labelled as the
// run's background rather than as its assignment (task_brief.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// bothJobs is the counterexample from the review, in a person's own voice: two
// pieces of work, one of them explicitly to be handed to a task.
const bothJobs = "run ./slow.sh in a task, and separately count the services yourself"

// familyOnOneAsk admits a root task carrying the person's message and stands a
// worker in it, ready to hand a piece out.
func familyOnOneAsk(t *testing.T, ask string) (*TaskGraph, *TaskNode, *Agent) {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := session.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "the whole job", brief: "run the script and count the services",
		acceptance: "both answers are reported", depth: 1, request: ask,
	})
	parent := graph.node(id)
	worker, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
		InTask: true, tasker: graph, taskID: id, taskDepth: 1,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the node: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	parent.openRoom().speaking(worker)
	return graph, parent, worker
}

// TestAPieceIsToldItsJobIsThePieceAndTheWholeAskIsStillTheirWords is the whole
// finding, on a piece admitted through the road a worker actually calls.
func TestAPieceIsToldItsJobIsThePieceAndTheWholeAskIsStillTheirWords(t *testing.T) {
	graph, parent, worker := familyOnOneAsk(t, bothJobs)

	// THE ROOT KEEPS THE PERSON'S AUTHORITY WHOLE. Nobody stands between it and
	// the ask, so the rule over their words is the one it always was.
	opening := parent.instruction()
	if !strings.Contains(opening, briefAskRule) {
		t.Fatalf("a top-level task no longer reads the person's words as its whole job:\n%s", opening)
	}
	if strings.Contains(opening, briefPieceRule) {
		t.Error("a top-level task is told it owns one piece of an ask nobody cut up")
	}

	answer, _, err := worker.proposeTask(context.Background(), json.RawMessage(
		`{"title":"the script","summary":"s","brief":"run ./slow.sh and report the marker it writes","deliverable":"the marker line","acceptance":"the marker is reported"}`))
	if err != nil {
		t.Fatalf("handing the script out: %v", err)
	}
	if !strings.Contains(answer, "started") {
		t.Fatalf("the piece was not started: %s", answer)
	}
	kids := graph.children(parent.id)
	if len(kids) != 1 {
		t.Fatalf("the parent has %d pieces, want one", len(kids))
	}
	piece := kids[0].instruction()

	// THEIR WORDS ARE STILL THERE, UNEDITED. The piece rule is not a filter: a
	// worker that needs the whole ask to make sense of its part still has it.
	if !strings.Contains(piece, bothJobs) {
		t.Errorf("the piece no longer carries the person's own message:\n%s", piece)
	}
	if !strings.Contains(piece, briefAskHeading) {
		t.Error("the piece lost the heading that says whose words those are")
	}
	// AND THEY ARRIVE AS THE JOB THIS PIECE WAS CUT FROM.
	if !strings.Contains(piece, briefPieceRule) {
		t.Errorf("the piece is not told its job is the piece:\n%s", piece)
	}
	if strings.Contains(piece, briefAskRule) {
		t.Error("the piece still reads the whole-job rule, which is what told it to do the count and hand the script out again")
	}
	// AND ITS OWN WORK IS STILL WHAT IT WAS HANDED.
	if !strings.Contains(piece, "run ./slow.sh and report the marker it writes") {
		t.Error("the piece lost the work it was actually given")
	}
}

// AND A PIECE STAYS A PIECE ACROSS A RESTART. The role is read off the parent on
// the spec, which is what the checkpoint carries, so a node rebuilt from disk
// composes the same document — the run that reopens it (task_continue.go) and
// every repair round go through this one road ([TaskNode.instructionOn]).
func TestAPieceRestoredFromACheckpointStillReadsTheRuleForAPiece(t *testing.T) {
	graph, parent, worker := familyOnOneAsk(t, bothJobs)
	if _, _, err := worker.proposeTask(context.Background(), json.RawMessage(
		`{"title":"the script","summary":"s","brief":"run ./slow.sh","deliverable":"the marker","acceptance":"the marker is reported"}`)); err != nil {
		t.Fatalf("handing the script out: %v", err)
	}
	kid := graph.children(parent.id)[0]

	encoded, err := json.Marshal(graph.document())
	if err != nil {
		t.Fatalf("encoding the graph: %v", err)
	}
	document, err := decodeTasks(encoded)
	if err != nil {
		t.Fatalf("the checkpoint does not load: %v", err)
	}
	restored := map[uint64]*TaskNode{}
	rebuilt := newTaskGraph()
	for _, record := range document.Nodes {
		restored[record.ID] = restoreNode(rebuilt, record)
	}
	piece, root := restored[kid.id], restored[parent.id]
	if piece == nil || root == nil {
		t.Fatalf("the family did not come back: %v", restored)
	}
	if opening := piece.instruction(); !strings.Contains(opening, briefPieceRule) {
		t.Errorf("a restored piece reads the whole-job rule:\n%s", opening)
	}
	if opening := root.instruction(); !strings.Contains(opening, briefAskRule) {
		t.Errorf("a restored root lost the person's authority over its own ask:\n%s", opening)
	}
}

// AND THE RULE SAYS THE THREE THINGS THE READING TURNED ON. It is pinned by its
// own words because a reword that dropped any of them would put the defect back
// while every other test still passed.
func TestTheRuleForAPieceScopesTheWorkWithoutSilencingThePerson(t *testing.T) {
	for _, want := range []string{
		// what this worker owes,
		"do what THE WORK and DONE WHEN below name",
		// what it does not, including the instruction that was recursive,
		"leave the rest of that message to whoever kept it",
		"which is not yours to do again",
		// and that a real disagreement is theirs to win and is reported, not
		// quietly worked around.
		"theirs are what was asked for",
		"say so in your report rather than widening the work",
	} {
		if !strings.Contains(briefPieceRule, want) {
			t.Errorf("the rule a piece opens on no longer says %q", want)
		}
	}
	if strings.Contains(briefPieceRule, fmt.Sprint(taskFanLimit)) {
		t.Error("the rule carries a number, which belongs to the fan-out page and the tool that enforces it")
	}
}

// ── a plan-born worker on the bash belt ─────────────────────────────────────

// WHAT A PLAN-BORN WORKER IS TOLD, pinned. A bash-belt node is born from one
// task in the run's plan, and its assignment was fixed from the plan when it was
// born; the whole objective above it is background rather than more work. The
// document says so plainly, in the one place the section order is decided
// (task_brief.go), and every other worker's document is untouched by it.

// TestBashBeltPlanBornBrief is the both-arms proof of the reordered document.
// A bash-belt plan-born brief opens on the plan task and the sentence that its
// ancestors are background, then lays out the assignment, then quotes the
// person's message labelled as background; a plan-born node off the belt and an
// ordinary child are byte-identical to the document that was there before.
func TestBashBeltPlanBornBrief(t *testing.T) {
	const (
		request     = "port the whole client to v2 and count the callers"
		work        = "port internal/importer to the streaming API"
		deliverable = "internal/importer/stream.go"
		acceptance  = "go test ./internal/importer/ passes"
	)
	born := composeBriefScoped(briefScopeFor("7", true), briefPiece,
		request, work, deliverable, acceptance, "", AdmissionContext{}, taskOrigin{}, taskCopy{})

	// (a) IT OPENS ON THE PLAN TASK AND THE BACKGROUND SENTENCE.
	id := planStoreID("7") // the CLI's own `t-7` spelling
	if !strings.HasPrefix(born, id) {
		t.Fatalf("the plan-born brief does not open with the plan task %q:\n%s", id, born)
	}
	if !strings.Contains(born, briefPlanRule) {
		t.Fatalf("the plan-born brief is missing %q:\n%s", briefPlanRule, born)
	}
	at := func(s string) int {
		i := strings.Index(born, s)
		if i < 0 {
			t.Fatalf("no %q in the plan-born brief:\n%s", s, born)
		}
		return i
	}

	// (b) THE ASSIGNMENT — work, what to produce, done — COMES BEFORE THE
	// PERSON'S WORDS, and their words follow it rather than open it.
	if !(at(briefWorkHeading) < at(briefMakeHeading) &&
		at(briefMakeHeading) < at(briefDoneHeading) &&
		at(briefDoneHeading) < at(briefAskHeading)) {
		t.Fatalf("the assignment does not come before the person's words:\n%s", born)
	}
	for _, want := range []string{request, work, deliverable, acceptance} {
		if !strings.Contains(born, want) {
			t.Fatalf("the plan-born brief is missing %q:\n%s", want, born)
		}
	}

	// (c) THE PERSON'S WORDS ARE LABELLED AS BACKGROUND, not as the assignment
	// that wins, and the whole-job rules are gone with it.
	if !strings.Contains(born, briefBackgroundRule) {
		t.Fatalf("the person's words are not labelled background:\n%s", born)
	}
	if strings.Contains(born, briefPieceRule) || strings.Contains(born, briefAskRule) {
		t.Fatalf("the plan-born brief still carries the whole-job rule:\n%s", born)
	}

	// THE OTHER ROADS DO NOT MOVE ONE BYTE. A plan-born node whose session is
	// not on the bash belt gets the document it always got, an empty scope is
	// the direct caller's document, and an ordinary child is untouched.
	ordinary := composeBrief(briefPiece, request, work, deliverable, acceptance, "", AdmissionContext{}, taskOrigin{}, taskCopy{})
	if got := composeBriefScoped(briefScope{}, briefPiece, request, work, deliverable, acceptance, "", AdmissionContext{}, taskOrigin{}, taskCopy{}); got != ordinary {
		t.Fatalf("an empty scope changed the document:\n%s", got)
	}
	if got := composeBriefScoped(briefScopeFor("7", false), briefPiece, request, work, deliverable, acceptance, "", AdmissionContext{}, taskOrigin{}, taskCopy{}); got != ordinary {
		t.Fatalf("a plan-born node off the belt changed the document:\n%s", got)
	}
	if strings.Contains(ordinary, briefPlanRule) {
		t.Fatalf("an ordinary child carries the plan sentence:\n%s", ordinary)
	}
}

// TestBashBeltPlanBornBriefOnlyOnTheBelt proves the NODE threads both facts
// itself: a spec carrying a plan id composes the reordered document only while
// the session is on the bash belt, and the same node off the belt composes what
// every plan-born node composed before.
func TestBashBeltPlanBornBriefOnlyOnTheBelt(t *testing.T) {
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := conversation.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "t", request: "port the whole client to v2",
		brief: "port internal/importer", deliverable: "stream.go", acceptance: "it streams",
		planID: "7",
	})
	node := graph.node(id)
	if node == nil {
		t.Fatal("the plan-born node was not admitted")
	}

	// OFF THE BELT: no plan sentence, and the person's words still open it.
	off := node.instruction()
	if strings.Contains(off, briefPlanRule) {
		t.Fatalf("a belt-off plan-born node got the plan sentence:\n%s", off)
	}
	if !strings.Contains(off, briefAskHeading) {
		t.Fatalf("a belt-off plan-born node lost the person's words:\n%s", off)
	}

	// ON THE BELT: it opens on the plan task and labels the person's words
	// background.
	t.Setenv("CODEAF_TASK_BELT", "bash")
	on := node.instruction()
	if !strings.Contains(on, planStoreID("7")) || !strings.Contains(on, briefPlanRule) {
		t.Fatalf("a belt-on plan-born node did not open on its plan task:\n%s", on)
	}
	if !strings.Contains(on, briefBackgroundRule) {
		t.Fatalf("a belt-on plan-born node did not label the person's words background:\n%s", on)
	}
}
