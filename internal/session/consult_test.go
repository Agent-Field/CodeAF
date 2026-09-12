package session

// THE SECOND-OPINION DOOR, THROUGH THE REAL BELT: the question reaches the
// tier it was registered on with the caller's facts beside it, the budget is
// per turn and says so when it is gone, and a worker's belt has no door at
// all — the armed version of the instinct is the division review's, and two
// doors to one tier is two answers to one question.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// consultCall is the model asking for the second opinion, with the question
// and the facts the answering model cannot see.
func consultCall(id, question, facts string) step {
	arguments, _ := json.Marshal(map[string]string{"question": question, "context": facts})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "consult", string(arguments)), nil
	}
}

// consultAside answers the role's own request — detected by the consult page's
// own words — without spending a configured step, the way every fixture's
// errand is answered beside the script.
func consultAside(answer string) func([]ai.Message) (*ai.Response, bool) {
	return func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) == 0 || snapshot[0].Role != "system" ||
			!strings.Contains(messageText(snapshot[0]), "one question") {
			return nil, false
		}
		return textResponse(answer), true
	}
}

func TestAConsultCarriesItsQuestionToTheTierThatThinks(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		consultCall("call-consult", "two quick tasks or one with items?", "the four configs differ in three keys"),
		finalText("one with items, then"),
	}}
	completer.aside = consultAside("one quick task with four items; they share the third key")
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "how should I split this?"))

	// THE ANSWER IS THE TIER'S OWN SENTENCE, whole, as the tool's result.
	responses := toolTexts(agent)
	if len(responses) != 1 {
		t.Fatalf("the consult answered %d times, want one", len(responses))
	}
	if !strings.Contains(responses[0], "one quick task with four items") {
		t.Fatalf("the consult came back with %q, want the tier's answer", responses[0])
	}
	// AND WHAT CAME WITH IT IS THE QUESTION AND ITS FACTS, because the
	// answering model holds none of the caller's conversation.
	found := false
	for _, snapshot := range completer.asides {
		if len(snapshot) > 1 && snapshot[0].Role == "system" &&
			strings.Contains(messageText(snapshot[0]), "one question") {
			last := messageText(snapshot[len(snapshot)-1])
			found = strings.Contains(last, "two quick tasks or one with items?") &&
				strings.Contains(last, "the four configs differ in three keys")
		}
	}
	if !found {
		t.Fatalf("no consult request carried the question and its facts: %v", completer.asides)
	}

	// AND THE THIRD DOOR IS THE BUDGET'S, said with the arithmetic it is.
	fresh := context.Background()
	if _, _, err := agent.startConsult(fresh, json.RawMessage(`{"question":"another?"}`)); err != nil {
		t.Fatal(err)
	}
	answer, _, err := agent.startConsult(fresh, json.RawMessage(`{"question":"again?"}`))
	if err != nil {
		t.Fatal(err)
	} else if !strings.Contains(answer, "2 consults are already spent") {
		t.Fatalf("the budget's refusal read %q, want the arithmetic", answer)
	}
	if got := agent.consultCalls; got != 2 {
		t.Fatalf("the turn paid %d consults, want the two it bought", got)
	}
}

func TestAWorkerIsHandedNoConsult(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	if agent.offers("consult") {
		t.Fatal("a task node has a second opinion of its own: the armed door is the division review")
	}
	if !agent.offers("read") {
		t.Fatal("the task node's own belt stopped carrying what it needs")
	}
}
