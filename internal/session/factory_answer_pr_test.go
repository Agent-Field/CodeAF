package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type prDecisionDoor struct {
	savedManagerDoor
	action, words, said string
	pressed             []ManagerPress
	refuse              error
}

func (d *prDecisionDoor) Press(_ context.Context, _ string, p ManagerPress) (string, error) {
	d.action, d.words, d.said = p.Action, p.Words, p.Said
	d.pressed = append(d.pressed, p)
	if d.refuse != nil {
		return "", d.refuse
	}
	return "Merge pressed for pull request #161", nil
}

// "MARK IT READY" AND "APPROVE IT" IN THE MANAGER'S CHAT PRESS THE JOB PAGE'S
// OWN DECISIONS, through the one door every manager press takes, and the
// press carries the person's own words of the turn beside the model's: the
// door reads authority from them, never from what the model wrote.
func TestFactoryAnswerMergeAndMarkReadyReachThePRDecisionDoor(t *testing.T) {
	for _, action := range []string{ActionMarkReady, ActionApprove} {
		d := &prDecisionDoor{}
		a := &Agent{config: Config{FactoryRun: d}}
		out, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"the person said yes"}`))
		if err != nil || bad || !strings.Contains(out, "#161") || d.action != action || d.words != "the person said yes" || d.said != "" || d.answers != 0 {
			t.Fatalf("%s on a turn with no person words: %q bad=%v err=%v door=%+v", action, out, bad, err, d)
		}
		a.personWords = "yes, go ahead"
		if _, bad, _ = a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"the person said yes"}`)); bad || d.said != "yes, go ahead" {
			t.Fatalf("%s did not carry the person's own words: bad=%v door=%+v", action, bad, d)
		}
	}
	// A door that cannot press is told so in words, and nothing is answered.
	d := &savedManagerDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	a.personWords = "merge it"
	out, bad, _ := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"merge","answer":"merge it"}`))
	if !bad || !strings.Contains(out, "Nothing changed") || d.answers != 0 {
		t.Fatalf("a door with no press: %q bad=%v answers=%d", out, bad, d.answers)
	}
	if !strings.Contains(factoryAnswerSchemaJSON, `"merge","mark_ready","approve"`) {
		t.Fatal("the schema does not offer merge and mark_ready")
	}
}

// THE DOOR HOLDS AN APPROVAL TO WHAT THE PERSON HAS READ, and it can only do
// that if it is told when the turn began: approving publication also approves
// the public acts that wait beside the push, so a press that names no turn
// start would let a reply proposed in this turn go out unseen.
func TestFactoryAnswerApprovalCarriesTheStartOfTheTurnToTheDoor(t *testing.T) {
	began := time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)
	for _, action := range []string{ActionApprove, ActionMarkReady, ActionMerge} {
		d := &prDecisionDoor{}
		a := &Agent{config: Config{FactoryRun: d}, personWords: "yes, go ahead", teamTurnAt: began}
		if _, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"the person said yes"}`)); err != nil || bad || len(d.pressed) != 1 {
			t.Fatalf("%s: bad=%v err=%v pressed=%+v", action, bad, err, d.pressed)
		}
		if got := d.pressed[0].TurnAt; !got.Equal(began) {
			t.Errorf("%s reached the door with the turn starting at %v, want %v", action, got, began)
		}
	}
}

// MERGING IS THE PERSON'S IN EVERY MODE. `factory_answer` with action=merge
// reaches the door only on a turn the person spoke in; a turn the engine
// started is refused before the door is asked, whatever the model writes in
// answer, and the refusal says how the person merges.
func TestFactoryAnswerMergeNeedsThePersonsOwnWordsInTheTurn(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	args := json.RawMessage(`{"action":"merge","answer":"the person told me to merge it"}`)
	out, bad, err := a.factoryAnswerTool().Execute(context.Background(), args)
	if err != nil || !bad || len(d.pressed) != 0 || !strings.Contains(out, "merging is the person's alone") || !strings.Contains(out, "Merge card") {
		t.Fatalf("a turn with no person words merged: %q bad=%v err=%v pressed=%+v", out, bad, err, d.pressed)
	}
	a.personWords = "merge it now"
	out, bad, err = a.factoryAnswerTool().Execute(context.Background(), args)
	if err != nil || bad || d.action != ActionMerge || d.said != "merge it now" || !strings.Contains(out, "#161") {
		t.Fatalf("the person's merge: %q bad=%v err=%v door=%+v", out, bad, err, d)
	}
}

// RESUMING A PAUSED AUTO IS THE PERSON'S ALONE. The press carries the words
// the person sent in this turn, read from the session rather than the model's
// arguments, and a turn with nothing from them — a runner's note, factory
// work — presses nothing whatever the model writes.
func TestFactoryAnswerResumeCarriesOnlyThePersonsOwnWords(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	args := json.RawMessage(`{"action":"resume","answer":"the person said to resume"}`)
	out, bad, err := a.factoryAnswerTool().Execute(context.Background(), args)
	if err != nil || !bad || !strings.Contains(out, "only the person") || d.action != "" {
		t.Fatalf("a turn with no person words resumed: %q bad=%v err=%v door=%+v", out, bad, err, d)
	}

	a.personWords = "carry on, that finding is a false alarm"
	out, bad, err = a.factoryAnswerTool().Execute(context.Background(), args)
	if err != nil || bad || d.action != ActionResume || d.words != "carry on, that finding is a false alarm" || d.said != d.words {
		t.Fatalf("the person's resume: %q bad=%v err=%v door=%+v", out, bad, err, d)
	}
	if !strings.Contains(factoryAnswerSchemaJSON, `"approve","resume"`) {
		t.Fatal("the schema does not offer resume")
	}
}

// THE WORDS FOLLOW THE TURN: a person's message opens a turn with them, and a
// turn the engine opens starts with none.
func TestTheTurnsPersonWordsAreThePersonsAlone(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
	}}, nil)
	collect(t, mustSubmit(t, agent, "resume the job"))
	if got := agent.turnPersonWords(); got != "resume the job" {
		t.Fatalf("the person's turn holds %q", got)
	}
	note := userText("Shape the run for this item now.")
	note.runner = true
	events, err := agent.submitUser(watchedContext(agent), note)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := agent.turnPersonWords(); got != "" {
		t.Fatalf("a runner's turn holds the person's words %q", got)
	}
}

// "IS IT READY TO MERGE?" SHOWS THE MERGE CARD AGAIN: offer_merge reaches the
// door the page's buttons take, with the person's words.
func TestFactoryAnswerOfferMergeReachesThePRDecisionDoor(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	_, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"offer_merge","answer":"is it ready to merge?"}`))
	if err != nil || bad || d.action != ActionOfferMerge || d.words != "is it ready to merge?" || d.answers != 0 {
		t.Fatalf("offer_merge: bad=%v err=%v door=%+v", bad, err, d)
	}
	if !strings.Contains(factoryAnswerSchemaJSON, `"offer_merge"`) || !strings.Contains(factoryAnswerDescription, "action=offer_merge") {
		t.Fatal("the schema or description does not offer offer_merge")
	}
}
