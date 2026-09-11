package session

// THE LANDING ROAD, FROM THE CHILD'S FINISH TO THE PARENT'S NEXT ACTION.
//
// Every test here drives the real delivery — a node completed through the
// graph, [Agent.reportTaskNode], [Agent.deliverTaskNote] and
// [Agent.taskNoteReaders] — and asserts the PARENT WORKER's half: that the note
// reaches the worker rather than the person's conversation, that the worker is
// given a further round in which it can still act, and that a worker with
// nobody left in it falls back to the conversation rather than nowhere.

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// quickCallInside is one well-formed quick_task call made from inside a worker.
func quickCallInside(id, title, line string) step {
	arguments, _ := json.Marshal(quickArguments{Title: title, Line: line})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, quickTaskToolName, string(arguments)), nil
	}
}

// landsQuickChildren is the graph runner for these tests: the parent is left
// running (a parent that landed the moment it started could hand nothing out),
// and every child lands through the REAL quick landing road — the parent
// worker's own [Agent.landQuickNode], then [TaskGraph.complete], which is the
// pair [Agent.runQuickNode] returns into.
func landsQuickChildren(parentAgent **Agent, release <-chan struct{}, report string, wrote []string, state TaskState) func(*TaskNode) {
	return func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		go func() {
			<-release
			node.keepWorkerConclusion(report, io.Discard)
			landed := (*parentAgent).landQuickNode(node, report, wrote, state)
			node.graph.complete(node, landed)
		}()
	}
}

// runParent drives the parent worker exactly as the runner does and answers a
// channel closed when its whole run is over.
func runLandingParent(t *testing.T, nest *nest) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(context.Background(), nest.node, nest.parent,
			"do the whole job", nest.node.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, nil, io.Discard)
	}()
	return done
}

// ── A: a quick child of a task, done ────────────────────────────────────────

// A QUICK CHILD'S NOTE REACHES THE PARENT'S WORKER, AND BUYS IT A ROUND IT CAN
// STILL ACT IN. The parent said its last sentence and stopped talking; the note
// re-enters it ([childRun.foldParts]), and what it gets is a whole turn — it
// makes a tool call in it — rather than a fold it can only report.
func TestAQuickChildsDoneNoteReachesTheParentWorkerAndBuysItARound(t *testing.T) {
	release := make(chan struct{})
	var parentAgent *Agent
	acted := make(chan []ai.Message, 2)
	completer := &scriptedCompleter{steps: []step{
		quickCallInside("q1", "read the law", "read ALPHA and say what it holds"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed the reading out; I will fold it when it lands"), nil
		},
		// THE ROUND THE NOTE BOUGHT. A parent that could only report would have
		// no move here; this one reads a directory, which is a step of its own.
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			acted <- messages
			return toolResponse("c2", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("folded the reading in"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	nest := newNest(t, completer, nil)
	parentAgent = nest.node
	nest.graph.run = landsQuickChildren(&parentAgent, release,
		"ALPHA holds the tariff table", []string{"alpha.md"}, TaskDone)

	done := runLandingParent(t, nest)

	// The parent's first turn ends with the child still out. Nothing ends here:
	// the runner PARKS on the piece ([childRun.park]), and a parked runner is in
	// a select that only a report or a cut context releases — so once it is seen
	// parked, a run that had ended would already have closed `done`.
	waitRequests(t, completer, 2)
	waitQuiet(t, nest.node)
	waitFor(t, "the parent to park on its quick child", nest.parent.waitingOnItsPieces)
	select {
	case <-done:
		t.Fatal("the parent ended while its quick child was still running")
	default:
	}

	close(release)
	select {
	case messages := <-acted:
		text := userTextIn(messages)
		for _, want := range []string{"ALPHA holds the tariff table", "done", "changed: alpha.md"} {
			if !strings.Contains(text, want) {
				t.Fatalf("the round the note bought does not carry %q:\n%s", want, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the quick child's note never re-entered the parent worker")
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the parent never ended after its child reported")
	}

	// FOUR REQUESTS: two working, then the round the note bought and the step
	// after the tool call. A parent handed the note only to fold with would stop
	// at three.
	if got := completer.requests(); got < 4 {
		t.Fatalf("the parent was asked %d times, want the round it could act in", got)
	}

	// AND THE PERSON'S CONVERSATION WAS NOT TOLD. The parent is the only reader
	// that can fold the piece back into the whole ([Agent.taskNoteReaders]).
	if steeringContains(nest.session, "ALPHA holds the tariff table") {
		t.Fatalf("the child's news also reached the person's conversation: %v", steeringQueue(nest.session))
	}
}

// ── A: the same child out of rounds ─────────────────────────────────────────

// A CHILD THAT DID NOT FINISH IS NEWS ON THE SAME ROAD. The parent is owed the
// failure exactly as it is owed the answer, and it gets the same round to decide
// what to do about it.
func TestAQuickChildThatRanOutOfRoundsStillBuysTheParentARound(t *testing.T) {
	release := make(chan struct{})
	var parentAgent *Agent
	acted := make(chan []ai.Message, 2)
	completer := &scriptedCompleter{steps: []step{
		quickCallInside("q1", "read the law", "read ALPHA and say what it holds"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed the reading out"), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			acted <- messages
			return toolResponse("c2", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("it did not come back; I will read the file myself"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	nest := newNest(t, completer, nil)
	parentAgent = nest.node
	nest.graph.run = landsQuickChildren(&parentAgent, release,
		"out of rounds — it stopped at its step cap. What it last said it was doing, in its own words:\n“opening ALPHA”",
		nil, TaskFailed)

	done := runLandingParent(t, nest)
	waitRequests(t, completer, 2)
	waitQuiet(t, nest.node)
	close(release)

	select {
	case messages := <-acted:
		text := userTextIn(messages)
		if !strings.Contains(text, "out of rounds") {
			t.Fatalf("the parent was not told why its child did not come back:\n%s", text)
		}
		// THE MACHINERY'S OWN WORDS ARE NOT IN IT (CLAUDE.md's vocabulary law).
		for _, banned := range []string{"failed", "needs your look", "verdict", "auditor"} {
			if strings.Contains(text, banned) {
				t.Fatalf("the note said %q to the parent:\n%s", banned, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a child that ran out of rounds never reached the parent worker")
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the parent never ended after its child failed")
	}
	if got := completer.requests(); got < 4 {
		t.Fatalf("the parent was asked %d times, want the round the failure bought", got)
	}
}

// ── A: the parent whose reading is over but whose work is not ───────────────

// A CHILD LANDING WHILE ITS PARENT IS STILL OPEN IS FOLDED INTO THE PARENT'S
// REPORT. The seat is empty — the runner withdraws it the instant the worker's
// reading is over ([childRun.foldParts]) — and the node is still minutes from
// landing, so the fold takes the piece rather than the person
// ([landingFold], task_latefold.go). Nobody's conversation is told about work
// it did not commission, and the piece reaches the deliverable it was cut from.
func TestAChildLandingAfterItsParentStoppedReadingIsFoldedIntoItsReport(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	// The worker's reading is over: the runner withdraws the seat at exactly
	// this moment ([childRun.foldParts]).
	nest.parent.openRoom().speaking(nil)
	// And the parent's own landing has been written, the way every road writes
	// it before the node settles ([TaskNode.finish]).
	nest.parent.finish("the whole job is done", nil, "", "")

	kid.finish("the law is in section four", nil, "", "")
	nest.graph.complete(kid, TaskDone)

	waitFor(t, "the piece's result to reach its parent's report", func() bool {
		return strings.Contains(nest.parent.notice().Report, "the law is in section four")
	})
	// THE REPORT KEEPS THE PIECE AS A RECORD KEEPS IT: the parent's own account,
	// then the piece's own head line — its number and its name — and its report
	// ([landingRecord]). It never keeps the sentence a landing note opens with
	// for a model, which tells its reader which word to say back
	// ([landingNoteLead]) and would be an order about somebody else's word to
	// whoever reads this report next.
	report := nest.parent.notice().Report
	for _, want := range []string{"the whole job is done", "task " + strconv.FormatUint(kid.id, 10) + " ", kid.title()} {
		if !strings.Contains(report, want) {
			t.Fatalf("the parent's report does not say %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "say that word back") {
		t.Fatalf("the parent's report carries a model's instruction:\n%s", report)
	}
	if steeringContains(nest.session, "the law is in section four") {
		t.Fatal("the person's conversation was told about a piece its own task handed out")
	}
	if steeringContains(nest.node, "the law is in section four") {
		t.Fatal("the news was left on a worker that had stopped reading")
	}
	// AND THE PIECE IS AS DELIVERED AS ONE READ BY A WORKER. The mark is what
	// stops it being told twice and what a parked family reads as "nothing is
	// outstanding" ([Agent.postTaskMessage]).
	if !kid.reported() {
		t.Fatal("the folded piece is still outstanding, so its landing could be delivered a second time")
	}
}

// A PARENT THAT HAS SETTLED SENDS THE NEWS TO THE PERSON, and that fallback is
// unchanged: the node has landed, its report is told, and news with nowhere to
// go belongs in front of somebody rather than nowhere
// ([Agent.taskNoteReaders]).
func TestAChildLandingAfterItsParentSettledReachesTheConversation(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	nest.parent.openRoom().speaking(nil)
	nest.parent.finish("the whole job is done", nil, "", "")
	nest.graph.complete(nest.parent, TaskDone)

	kid.finish("the law is in section four", nil, "", "")
	nest.graph.complete(kid, TaskDone)

	waitFor(t, "the orphaned news to reach the person's conversation", func() bool {
		return steeringContains(nest.session, "the law is in section four")
	})
	if report := nest.parent.notice().Report; strings.Contains(report, "the law is in section four") {
		t.Fatalf("a settled parent's report was rewritten after it was told:\n%s", report)
	}
}

// ── A: a worker never starts a turn of its own ──────────────────────────────

// THE PARENT DOES NOT WAKE, AND MUST NOT. Its turns are its runner's to start
// ([Agent.wakeLocked] declines inside a task), so a note handed to a worker
// nobody is driving is COUNTED and queued and nothing else.
func TestAChildsNoteQueuesOnTheParentWorkerWithoutStartingATurn(t *testing.T) {
	completer := &scriptedCompleter{}
	answerTheNamerOffTheQueue(completer)
	nest := newNest(t, completer, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]
	before := turnsStarted(nest.node)

	kid.finish("the law is in section four", nil, "", "")
	nest.graph.complete(kid, TaskDone)

	waitFor(t, "the note to reach the parent worker's queue", func() bool {
		return steeringContains(nest.node, "the law is in section four")
	})
	// The fact the parked runner reads: a note owed, so the fold will take a
	// round for it. [Agent.taskNewsStanding] takes the handover lock the whole
	// delivery's last writes — the mark, the count and the wake — are made under
	// ([Agent.handOverTaskNews]), so once it answers, the wake has already been
	// asked and has already declined or not. Nothing has to be waited out.
	if owed, _ := nest.node.taskNewsStanding(); owed != 1 {
		t.Fatalf("the parent is owed %d reports, want the one that just landed", owed)
	}
	// A turn is numbered the instant it starts, under the agent's own lock
	// ([Agent.startTurnLocked]), so an unmoved number is a turn never started.
	if got := turnsStarted(nest.node); got != before {
		t.Fatalf("the parent worker started %d turns of its own", got-before)
	}
	if got := completer.requests(); got != 0 {
		t.Fatalf("the parent worker asked its model %d times", got)
	}
}

// ── B: the merged files are there before the parent continues ───────────────

// THE MERGE HAPPENS BEFORE THE NOTE. [TaskGraph.complete] announces only after
// the runner has already written the node's leavings through [TaskNode.finish],
// and every road home merges before it finishes ([landHome]) — so the parent's
// round opens on a tree that already holds its child's work.
func TestTheChildsWorkIsHomeBeforeItsNoteIsDelivered(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	// finish is the write and complete is the announcement, in that order: by the
	// time a reader can be handed the news the leavings are already on the node.
	kid.finish("the law is in section four", []string{"law.md"}, "aforge/task-2-law", mergeMerged)
	nest.graph.complete(kid, TaskDone)

	waitFor(t, "the note to reach the parent worker", func() bool {
		return steeringContains(nest.node, "the law is in section four")
	})
	note := strings.Join(steeringQueue(nest.node), "\n")
	for _, want := range []string{"changed: law.md", "its branch aforge/task-2-law merged into yours"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the parent's note does not say %q:\n%s", want, note)
		}
	}
	// And the node's own record agrees, which is what a later accept reads.
	if notice := kid.notice(); notice.Merge != mergeMerged {
		t.Fatalf("the node settled with merge %q, want it home before the note", notice.Merge)
	}
}

// turnsStarted is how many turns this agent has begun, read under its own lock.
func turnsStarted(agent *Agent) uint64 {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return uint64(agent.turnSeq)
}
