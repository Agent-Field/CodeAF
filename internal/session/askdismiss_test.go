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
