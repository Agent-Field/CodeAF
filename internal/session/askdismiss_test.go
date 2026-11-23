package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The laws of a question the person dismissed (askdismiss.go): it comes off
// every surface with their own act as the reason, nothing is decided, and the
// model is told — by the call parked on it, or at its next step.

// A PARKED QUESTION'S CALL COMES BACK WITH THE DISMISSAL, and the turn carries
// on with nothing decided.
func TestDismissingAParkedQuestionTellsTheCallNothingWasDecided(t *testing.T) {
	var toolSaid string
	steps := append(askingSteps(), func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		toolSaid = askResult(messages)
		return textResponse("carrying on without it"), nil
	})
	rig := newQuestioningTurn(t, &scriptedCompleter{steps: steps})
	turn := mustSubmit(t, rig.agent, "ask me about painting")
	question := waitForTheQuestion(t, rig.agent)

	if err := rig.agent.DismissQuestion(question.Token()); err != nil {
		t.Fatal(err)
	}
	gone := waitForAsk(t, rig.asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason != askDismissedReason {
		t.Fatalf("the withdrawal reads %+v, want %q", gone.Question.Withdrawn, askDismissedReason)
	}
	collect(t, turn)
	if toolSaid != askDismissed {
		t.Fatalf("the `ask` result reads %q, want the dismissal", toolSaid)
	}
	if decisions := rig.agent.Decisions(); len(decisions) != 0 {
		t.Fatalf("a dismissed question was recorded as a decision: %+v", decisions)
	}
	if err := rig.agent.DismissQuestion(question.Token()); err == nil {
		t.Fatal("a question already dismissed was dismissed again")
	}
}

// A QUESTION NO CALL WAITS ON IS TOLD AT THE NEXT STEP, and only told: the
// dismissal starts no turn of its own.
func TestDismissingALaterQuestionTellsTheModelAtItsNextStep(t *testing.T) {
	var carried []string
	rig := leaveAQuestionStanding(t, func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		carried = userLines(messages)
		return textResponse("noted"), nil
	})
	question := rig.agent.OpenQuestions()[0]
	if err := rig.agent.DismissQuestion(question.Token()); err != nil {
		t.Fatal(err)
	}
	gone := waitForAsk(t, rig.asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason != askDismissedReason {
		t.Fatalf("the withdrawal reads %+v, want %q", gone.Question.Withdrawn, askDismissedReason)
	}
	if open := rig.agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("a dismissed question still stands: %+v", open)
	}
	rig.agent.mu.Lock()
	running := rig.agent.running
	rig.agent.mu.Unlock()
	if running {
		t.Fatal("a dismissal started a turn")
	}
	collect(t, mustSubmit(t, rig.agent, "what next"))
	if !strings.Contains(strings.Join(carried, "\n"), `Your question "Which colour for the hall?": `+askDismissed) {
		t.Fatalf("the next request did not tell the model: %q", carried)
	}
}

// A DISMISSAL WHILE NOTHING RUNS IS OWED TO THE NEXT TURN, NOT QUEUED AS WORK.
// The note waits beside the queues rather than on steering, so the idle
// session can still be handed off, and the next turn carries it.
func TestDismissingWhileIdleLeavesTheSessionIdleAndTellsTheNextTurn(t *testing.T) {
	var carried []string
	rig := leaveAQuestionStanding(t, func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		carried = userLines(messages)
		return textResponse("noted"), nil
	})
	question := rig.agent.OpenQuestions()[0]
	if err := rig.agent.DismissQuestion(question.Token()); err != nil {
		t.Fatal(err)
	}
	waitForAsk(t, rig.asks, EventQuestionWithdrawn)
	if queued := steeringQueue(rig.agent); len(queued) != 0 {
		t.Fatalf("an idle dismissal queued work on steering: %q", queued)
	}
	rig.agent.mu.Lock()
	held := len(rig.agent.heldNotes)
	rig.agent.mu.Unlock()
	if held != 1 {
		t.Fatalf("%d notes held for the next turn, want the dismissal", held)
	}
	collect(t, mustSubmit(t, rig.agent, "what next"))
	if !strings.Contains(strings.Join(carried, "\n"), `Your question "Which colour for the hall?": `+askDismissed) {
		t.Fatalf("the next turn did not carry the dismissal: %q", carried)
	}
	rig.agent.mu.Lock()
	held = len(rig.agent.heldNotes)
	rig.agent.mu.Unlock()
	if held != 0 {
		t.Fatalf("%d notes still held after the turn that read them", held)
	}
}

// A NOTE OWED TO THE NEXT TURN DOES NOT HOLD AN IDLE SESSION. The hand-off
// gate reads idle before and after it, and the next turn still reads it.
func TestANoteForTheNextTurnLeavesAnIdleSessionReadyForHandoff(t *testing.T) {
	var carried string
	completer := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		carried = userTextIn(messages)
		return textResponse("hello"), nil
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	if !agent.idleForHandoff() {
		t.Fatal("a fresh session is not idle for hand-off")
	}
	if !agent.enqueueNextTurnNote(`Your question "Which colour?": ` + askDismissed) {
		t.Fatal("the note was refused")
	}
	if !agent.idleForHandoff() {
		t.Fatal("a note owed to the next turn kept the idle session from being handed off")
	}
	if count := conversationRequests(completer); count != 0 {
		t.Fatalf("the note started %d turns, want none", count)
	}
	collect(t, mustSubmit(t, agent, "hello"))
	if !strings.Contains(carried, askDismissed) {
		t.Fatalf("the next turn's request did not carry the note: %q", carried)
	}
}
