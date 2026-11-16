package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type prDecisionDoor struct {
	savedManagerDoor
	action, words string
}

func (d *prDecisionDoor) DecidePR(_ context.Context, _, action, words string) (string, error) {
	d.action, d.words = action, words
	return "Merge pressed for pull request #161", nil
}

// "MERGE IT" IN THE MANAGER'S CHAT PRESSES THE JOB PAGE'S MERGE. On the live
// audit the manager had no verb for the post-PR card and said nothing was
// waiting; factory_answer's merge and mark_ready reach the door the page's
// buttons take, with the person's words, and nothing else answers for them.
func TestFactoryAnswerMergeAndMarkReadyReachThePRDecisionDoor(t *testing.T) {
	for _, action := range []string{ActionMerge, ActionMarkReady, ActionApprove} {
		d := &prDecisionDoor{}
		a := &Agent{config: Config{FactoryRun: d}}
		out, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"merge it"}`))
		if err != nil || bad || !strings.Contains(out, "#161") || d.action != action || d.words != "merge it" || d.answers != 0 {
			t.Fatalf("%s: %q bad=%v err=%v door=%+v", action, out, bad, err, d)
		}
	}
	// A door that cannot press is told so in words, and nothing is answered.
	d := &savedManagerDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	out, bad, _ := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"merge","answer":"merge it"}`))
	if !bad || !strings.Contains(out, "Nothing changed") || d.answers != 0 {
		t.Fatalf("a door with no press: %q bad=%v answers=%d", out, bad, d.answers)
	}
	if !strings.Contains(factoryAnswerSchemaJSON, `"merge","mark_ready","approve"`) {
		t.Fatal("the schema does not offer merge and mark_ready")
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
	if err != nil || bad || d.action != ActionResume || d.words != "carry on, that finding is a false alarm" {
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
