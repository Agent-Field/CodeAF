package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHO IS ASKED ABOUT WORK NOBODY COULD CHECK.
//
// A node that lands TaskUnverified is a decision waiting for somebody, and
// until the `task.settle` row there was exactly one somebody: the person, who
// had to talk the model into calling `tasks … resolve` on their behalf. The row
// adds a second posture — the model reads the work and decides — and every test
// here is about the seam between the two being ONE seam: one list of verbs, one
// sentence that changes, and nothing else about a landing moving with it.

// settleNotice is one landed node nobody could check, as the graph publishes it.
func settleNotice() TaskNotice {
	return TaskNotice{
		ID: 9, Title: "Port the parser", State: TaskUnverified,
		Report: needsLookLead + "the checker answered neither way",
	}
}

// THE VERBS ARE INTERPOLATED AND NEVER TYPED TWICE. The note tells the model
// what to type back and the tool's schema decides what it may type, and a note
// that offered a fourth word would be the harness teaching a model something the
// schema rejects.
func TestTheLandingNoteAndTheToolOfferTheSameVerbs(t *testing.T) {
	verbs := TaskResolveVerbs()
	if verbs != "accept|reaudit|refute" {
		t.Fatalf("the verbs read %q, want accept|reaudit|refute", verbs)
	}
	note := taskNote(settleNotice(), "", TaskSettleAsk, landingAddress{person: true})
	if !strings.Contains(note, "tasks id 9 resolve "+verbs) {
		t.Fatalf("the note does not offer the tool's own verbs:\n%s", note)
	}
	for _, one := range TaskResolutions {
		if !strings.Contains(tasksSchemaJSON, `"`+string(one)+`"`) {
			t.Fatalf("the tasks schema does not accept %q", one)
		}
	}
	if !strings.Contains(tasksSchemaJSON, `"enum":`+TaskResolveEnum()) {
		t.Fatalf("the schema's enum is not the one list:\n%s", tasksSchemaJSON)
	}
}

// ASK IS INFORMATIONAL. The person has the decision on the card in front of
// them, so the note says what happened and leaves the choice where it is.
func TestUnderAskTheNoteLeavesTheDecisionWithThePerson(t *testing.T) {
	note := taskNote(settleNotice(), "", TaskSettleAsk, landingAddress{person: true})
	if !strings.Contains(note, "waits until somebody decides") {
		t.Fatalf("the ask note does not say who is waiting:\n%s", note)
	}
	if !strings.Contains(note, "leave the choice with them") {
		t.Fatalf("the ask note does not leave the choice with the person:\n%s", note)
	}
	for _, never := range []string{"settle it yourself", "Only ask the person"} {
		if strings.Contains(note, never) {
			t.Fatalf("the ask note instructs the model to decide (%q):\n%s", never, note)
		}
	}
}

// AUTO IS AN INSTRUCTION, WITH THE ESCAPE STILL OPEN. The model is told to read
// the work and settle it, and told when to come back — a policy that said
// "decide" with no way to say "I cannot" would produce a confident guess about
// work nobody read.
func TestUnderAutoTheNoteTellsTheModelToDecide(t *testing.T) {
	note := taskNote(settleNotice(), "", TaskSettleAuto, landingAddress{person: true})
	for _, want := range []string{
		"settle it yourself with tasks id 9 resolve " + TaskResolveVerbs(),
		"Only ask the person when you genuinely cannot tell",
		"Read the report above and the work itself",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the auto note is missing %q:\n%s", want, note)
		}
	}
	// EVERYTHING ELSE ABOUT THE LANDING IS THE SAME. The policy is about who
	// decides, not about what happened, so the facts a person reads are identical
	// either way.
	ask := taskNote(settleNotice(), "", TaskSettleAsk, landingAddress{person: true})
	for _, shared := range []string{"task 9 needs your look: Port the parser", needsLookLead} {
		if !strings.Contains(ask, shared) || !strings.Contains(note, shared) {
			t.Fatalf("the two notes disagree about %q", shared)
		}
	}
}

// A ROW THIS BUILD DOES NOT RECOGNISE READS AS ASKING. Deciding on somebody's
// behalf is a thing they say yes to, never a thing they get from a typo.
func TestAnUnknownSettleWordAsksThePerson(t *testing.T) {
	for _, word := range []string{"", "  ", "yes", "AUTO "} {
		if got := settleOrAsk(word); got != TaskSettleAsk {
			t.Fatalf("settleOrAsk(%q) = %q, want %q", word, got, TaskSettleAsk)
		}
	}
	if got := settleOrAsk(string(TaskSettleAuto)); got != TaskSettleAuto {
		t.Fatalf("settleOrAsk(%q) = %q, want auto", TaskSettleAuto, got)
	}
}

// ── the hierarchy ───────────────────────────────────────────────────────────

// quietAgent is an agent that will NOT start a turn on the notes it is handed,
// so a test can read what was queued instead of racing the drain.
//
// A note that lands on an idle session wakes it, and the first thing that turn
// does is drain the queue ([Agent.enqueueSteering]) — so "is the note there" is
// a question whose answer depends on whether a goroutine got there first. The
// agent is marked as already working, which is the one condition wakeLocked
// declines on that costs nothing else, and it is put back before the agent is
// closed (cleanups run in reverse, so this one runs before newTestAgent's).
func quietAgent(t *testing.T) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.running = true
	agent.mu.Unlock()
	t.Cleanup(func() {
		agent.mu.Lock()
		agent.running = false
		agent.mu.Unlock()
	})
	return agent
}

// unverifiedFamily lands one parent with one child under it, the child needing a
// look, and hands back the agent whose queue the person reads.
func unverifiedFamily(t *testing.T) (*Agent, *TaskNode, *TaskNode) {
	t.Helper()
	agent := quietAgent(t)
	graph := stubbedGraph(agent, func(node *TaskNode) {})
	parentID := graph.reserve()
	graph.admit(parentID, taskSpec{title: "Rebuild the index", brief: "b", acceptance: "a"})
	childID := graph.reserve()
	graph.admit(childID, taskSpec{title: "Port the parser", brief: "b", acceptance: "a", parent: parentID})
	parent, child := graph.node(parentID), graph.node(childID)
	child.finish(needsLookLead+"the checker answered neither way", nil, "task/parser", mergeAborted)
	graph.complete(child, TaskUnverified)
	if state := child.stateNow(); state != TaskUnverified {
		t.Fatalf("the child landed %q, want unverified", state)
	}
	return agent, parent, child
}

// A CHILD DOES NOT ROT WHEN ITS PARENT GOES HOME.
//
// While the parent is alive its own agent is the reader of the child's landing
// note and the decider of it. The moment the parent settles, that reader is
// gone — and before [Agent.bubbleUnverifiedChildren] the child simply sat there
// forever, settled, waiting on somebody nobody had told.
func TestAnUnverifiedChildBubblesWhenItsParentSettles(t *testing.T) {
	agent, parent, child := unverifiedFamily(t)
	before := len(agent.steering)

	agent.bubbleUnverifiedChildren(parent)

	if len(agent.steering) != before+1 {
		t.Fatalf("the person was handed %d notes, want one", len(agent.steering)-before)
	}
	// AND IT RE-ADDRESSES THE ROW RATHER THAN RE-DELIVERING IT (pending.go). The
	// demand is on [Agent.PendingDecisions] and on every surface that draws it;
	// what this note owes is one sentence saying whose question it is now.
	note := agent.steering[len(agent.steering)-1].text()
	for _, want := range []string{
		"task " + itoa64(parent.id) + " has finished",
		"task " + itoa64(child.id) + ", Port the parser",
		"waiting on you",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the re-addressed note is missing %q:\n%s", want, note)
		}
	}
	// THE CHILD IS ON THE LIST BEFORE AND AFTER, which is what makes one sentence
	// enough: the question never depended on this note to exist.
	pending := agent.PendingDecisions()
	if len(pending) != 1 || pending[0].Notice.ID != child.id {
		t.Fatalf("PendingDecisions() = %+v, want the child that needs a look", pending)
	}
	if pending[0].Depth != 1 {
		t.Fatalf("the child sits at depth %d, want one level down", pending[0].Depth)
	}
}

// AND NOTHING ELSE UNDER THAT PARENT IS BUBBLED. A child that finished, failed,
// or was stopped has already said everything it has to say; handing those to a
// person again is the "needs you" heading filling up with things nobody needs to
// do (internal/tui3's railGroupOf states the same law on the column).
func TestOnlyTheUndecidedChildrenBubble(t *testing.T) {
	agent, parent, _ := unverifiedFamily(t)
	graph := agent.graph()
	doneID := graph.reserve()
	graph.admit(doneID, taskSpec{title: "Collect sources", brief: "b", acceptance: "a", parent: parent.id})
	done := graph.node(doneID)
	done.finish("collected", nil, "", mergeMerged)
	graph.complete(done, TaskDone)

	before := len(agent.steering)
	agent.bubbleUnverifiedChildren(parent)
	if got := len(agent.steering) - before; got != 1 {
		t.Fatalf("%d notes bubbled, want exactly the one that needs deciding", got)
	}
}

// HANDING ONE OVER DOES NOT SETTLE IT. The node stays exactly where it is —
// unverified, branch kept, dependents waiting — and what moves is who is holding
// the question.
func TestHandingOneToTheModelLeavesTheNodeWhereItIs(t *testing.T) {
	agent, _, node := unverifiedFamily(t)
	before := len(agent.steering)

	if err := agent.HandUnverifiedToModel(node.id); err != nil {
		t.Fatalf("HandUnverifiedToModel: %v", err)
	}
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("the node moved to %q; handing a decision over is not making it", state)
	}
	if len(agent.steering) != before+1 {
		t.Fatalf("the model was handed %d notes, want one", len(agent.steering)-before)
	}
	note := agent.steering[len(agent.steering)-1].text()
	for _, want := range []string{handOverLead, "settle it yourself with tasks id "} {
		if !strings.Contains(note, want) {
			t.Fatalf("the hand-over note is missing %q:\n%s", want, note)
		}
	}

	// And a node somebody has already decided answers with the same sentence
	// [Agent.ResolveUnverified] gives, rather than queueing a note about work
	// that has moved on.
	if err := agent.ResolveUnverified(node.id, TaskRefute, "the eleventh company is missing"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	if err := agent.HandUnverifiedToModel(node.id); err == nil {
		t.Fatal("a node somebody had already decided was handed over anyway")
	}
}

// ── the checker's window, and what a person reads when it runs out (#268) ────

// A TIMER MAY EXPIRE ONLY INTO "UNANSWERED".
//
// The measured run's journal read "no answer in 5m0s, so nothing was accepted"
// about a part two levels down that no person was ever shown. Both halves of
// that sentence were doing damage: a person reads the five minutes as THEIR five
// minutes, missed, and "nothing was accepted" is a sentence about a decision
// said by a clock that made none. What the clock knows is who could not answer
// and how long they had (pending.go carries the law).
//
// The window is the seam a test moves ([Agent.auditWindowFor]); everything else
// here is the real road — a real repository, the real audit, and a checker that
// really does not answer.
func TestACheckerThatRanOutSaysSoAndNeverSaysAccepted(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go with the greeting."),
		},
		// THE CHECKER NEVER ANSWERS. It waits on its own window and the window
		// closes under it, which is exactly the shape the sentence is about.
		audit: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// The test clock: a window a test can actually wait out.
		config.auditWindow = 40 * time.Millisecond
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskUnverified {
		t.Fatalf("state = %q, want it waiting on a person (report %q)", notice.State, notice.Report)
	}
	if !strings.Contains(notice.Report, "nobody could check it in") {
		t.Fatalf("the report does not say who could not answer:\n%s", notice.Report)
	}
	// AND NEVER A WORD ABOUT A DECISION. "Accepted" is what a person says, or
	// what the settle policy says on their behalf; a clock says neither.
	for _, banned := range []string{"nothing was accepted", "no answer in"} {
		if strings.Contains(notice.Report, banned) {
			t.Fatalf("the report says %q about a window running out:\n%s", banned, notice.Report)
		}
	}
	// AND IT IS ON THE ONE LIST, which is what makes it answerable at all.
	pending := agent.PendingDecisions()
	if len(pending) != 1 || pending[0].Notice.ID != node.id {
		t.Fatalf("PendingDecisions() = %+v, want the node nobody could check", pending)
	}
	if pending[0].Waiting() != taskUnverifiedNews {
		t.Fatalf("the row says it is %q, want the person's own words", pending[0].Waiting())
	}
}

// AND IT LEAVES THE LIST ONLY ON A RESOLUTION. Accepting is the person's answer;
// nothing else takes a decision off the list, and no clock ever does.
func TestADecisionLeavesTheListOnlyWhenSomebodyAnswers(t *testing.T) {
	agent, parent, child := unverifiedFamily(t)

	if pending := agent.PendingDecisions(); len(pending) != 1 || pending[0].Notice.ID != child.id {
		t.Fatalf("PendingDecisions() = %+v, want the child before its parent settles", pending)
	}
	// The parent settles. WHO is being asked changes; WHETHER anybody is does
	// not, and the row is the same row.
	parent.finish("the index is rebuilt", nil, "", mergeInPlace)
	agent.graph().complete(parent, TaskDone)
	agent.bubbleUnverifiedChildren(parent)
	if pending := agent.PendingDecisions(); len(pending) != 1 || pending[0].Notice.ID != child.id {
		t.Fatalf("PendingDecisions() = %+v, want the child still waiting after its parent settled", pending)
	}

	// The person answers. "Not right" is the answer used here because it needs no
	// working copy to land — this fixture's child never had one — and the list is
	// indifferent to WHICH answer was given: what takes a row off it is that
	// somebody said something.
	if err := agent.ResolveUnverified(child.id, TaskRefute, "it is not finished"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	if pending := agent.PendingDecisions(); len(pending) != 0 {
		t.Fatalf("PendingDecisions() = %+v after somebody answered, want nothing", pending)
	}
}
