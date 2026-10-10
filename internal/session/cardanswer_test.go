package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// A PERSON'S CARD ANSWER IS ON THE PAGE THE CHECKER READS, and a gap that
// answer already chose does not carry the turn on.
//
// The measured failure: the person declined the card, the tool said nothing
// was set up, the digest clipped that result, and the checker raised "the
// recurring reminder was never set up" three times. This drives [Agent.checkerPage]
// through [Agent.checkpointReopen]. A checker that cannot see the answer line
// raises the gap, so the test is red without the line on the page.
func TestAPersonsCardAnswerReachesTheCheckerAndClosesTheGap(t *testing.T) {
	const gap = "The recurring reminder was never set up"
	var page atomic.Value
	var done atomic.Int64
	steps := make([]step, 20)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse("one job, still in this conversation"), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from"), nil
			}
			if askedForRemains(messages) {
				shown := messageText(messages[len(messages)-1])
				page.Store(shown)
				if strings.Contains(shown, "Don't save") {
					return textResponse(checkpointNothingLeft), nil
				}
				return textResponse(gap), nil
			}
			switch call := done.Add(1); call {
			case 1:
				return toolResponse("s1", "automation", aRepeatingReminder()), nil
			case 2:
				// The last act is a write, so the cheap turn is still read
				// (checkpoint.go's exposure gate) without climbing a mark.
				return toolResponse("w1", "write", `{"path":"note.txt","content":"time to leave"}`), nil
			default:
				return textResponse("I stopped. The reminder was never set up."), nil
			}
		}
	}
	store := automationStoreFor(t)
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointWritingAgent(t, completer, func(config *Config) {
		config.Automations = &Automations{Store: store, Zone: "UTC"}
	})

	events, err := agent.Submit(watchedContext(agent), "check the marketing slack every 3 hours")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringAutomation(t, events, func(event Event) {
		if err := agent.ResolveQuestion(Answer{
			Kind: QuestionAutomation, ID: event.Automation.ID, Key: DeclineKey,
		}); err != nil {
			t.Errorf("ResolveQuestion: %v", err)
		}
	})
	shown, _ := page.Load().(string)
	if !strings.Contains(shown, `the person answered the card "wants to remind you": Don't save`) {
		t.Fatalf("the checker was not shown the card answer:\npage:\n%s\ntranscript:\n%s\nevents: %v", shown, transcriptText(agent), kinds(collected))
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Fatal("a gap the person chose carried the turn on")
	}
	if saved, err := store.List(); err != nil || len(saved) != 0 {
		t.Fatalf("a declined card saved %v (%v)", saved, err)
	}
	_ = collected
}

// aRepeatingReminder is the measured proposal: a line said every three hours.
func aRepeatingReminder() string {
	body := map[string]any{
		"op":    "propose",
		"title": "marketing slack",
		"words": "check the marketing slack every 3 hours",
		"when":  map[string]any{"every": "3h", "words": "every 3 hours"},
		"say":   "check the marketing slack",
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func TestPersonCardAnswerLineNamesThePickedAnswer(t *testing.T) {
	card := Question{
		Kind:    QuestionAutomation,
		Head:    "wants to schedule work",
		Options: AutomationOptions(automation.Automation{Schedule: automation.Schedule{Every: "1h"}, Action: automation.Action{Do: "run the tests"}}),
	}
	declined := personCardAnswerLine(card, Answer{Kind: QuestionAutomation, Key: DeclineKey, DecidedBy: DecidedByPerson})
	if declined != `the person answered the card "wants to schedule work": Don't save` {
		t.Fatalf("decline line = %q", declined)
	}
	consent := Question{
		Kind: QuestionConsent, Head: "may I run the tests",
		Options: AnswerOptions(QuestionConsent),
	}
	allowed := personCardAnswerLine(consent, Answer{Kind: QuestionConsent, Key: "1", DecidedBy: DecidedByPerson})
	if allowed != `the person answered the card "may I run the tests": allow once` {
		t.Fatalf("consent line = %q", allowed)
	}
	if personCardAnswerLine(consent, Answer{Kind: QuestionConsent, Key: "1", DecidedBy: DecidedByDial}) != "" {
		t.Fatal("a dial's answer was written as the person's")
	}
}
