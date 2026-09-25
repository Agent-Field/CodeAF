package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A PERSON'S CARD ANSWER IS ON THE PAGE THE CHECKER READS, and a gap that
// answer already chose does not carry the turn on.
//
// The measured failure: the person pressed `just once`, the tool said nothing
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
				if strings.Contains(shown, "only now, don't repeat") {
					return textResponse(checkpointNothingLeft), nil
				}
				return textResponse(gap), nil
			}
			switch call := done.Add(1); call {
			case 1:
				return toolResponse("s1", "stand", aReminder()), nil
			case 2:
				// The last act is a write, so the cheap turn is still read
				// (checkpoint.go's exposure gate) without climbing a mark.
				return toolResponse("w1", "write", `{"path":"note.txt","content":"time to leave"}`), nil
			default:
				return textResponse("I stopped. The reminder was never set up."), nil
			}
		}
	}
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointWritingAgent(t, completer, func(config *Config) {
		config.Standing = &Standing{}
		config.standingItems = store
	})

	events, err := agent.Submit(watchedContext(agent), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		if err := agent.ResolveQuestion(Answer{
			Kind: QuestionStanding, ID: event.Standing.ID, Key: StandingOnceKey,
		}); err != nil {
			t.Errorf("ResolveQuestion: %v", err)
		}
	})
	shown, _ := page.Load().(string)
	if !strings.Contains(shown, `the person answered the card "wants to keep an eye on: remind me at 6 to leave": only now, don't repeat`) {
		t.Fatalf("the checker was not shown the card answer:\npage:\n%s\ntranscript:\n%s\nevents: %v", shown, transcriptText(agent), kinds(collected))
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Fatal("a gap the person chose carried the turn on")
	}
	if len(store.created) != 0 {
		t.Fatal("a once answer created a standing item")
	}
	_ = collected
}

func TestPersonCardAnswerLineKeepsOnceAndAnyOtherCard(t *testing.T) {
	standing := Question{
		Kind: QuestionStanding,
		Head: "wants to keep an eye on: run the tests",
	}
	once := personCardAnswerLine(standing, Answer{Kind: QuestionStanding, Key: StandingOnceKey, DecidedBy: DecidedByPerson})
	if once != `the person answered the card "wants to keep an eye on: run the tests": only now, don't repeat` {
		t.Fatalf("once line = %q", once)
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
