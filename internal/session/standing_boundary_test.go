package session

// The seam between `watch` and `stand`, pinned where the model actually reads
// it — in the two tool descriptions, and not only in the manual.
//
// THIS IS A DEFECT THAT WAS FOUND WITH A REAL MODEL. Asked "tell me when the
// file …/flag says red", deepseek-v4-flash reached for `watch` and answered
// "I'll be told the moment it appears with content exactly red" — from a job
// that stops the instant the window closes. The manual has said the rule
// correctly all along ("a watch is the wrong tool and there is a right one"),
// but the manual is not what a model reads when it is choosing a verb: the
// descriptions are, and neither of them said which of the two outlives the
// conversation.
//
// So the sentence lives in both, and this test is what keeps it there.

import (
	"context"
	"strings"
	"testing"
)

func TestTheWatchToolSaysItDiesWithTheConversation(t *testing.T) {
	if !strings.Contains(watchDescription, "stand") {
		t.Fatalf("the watch description never names the tool for work that outlives the window:\n%s", watchDescription)
	}
	for _, phrase := range []string{"DIES WITH THIS CONVERSATION", "AFTER this window is closed"} {
		if !strings.Contains(watchDescription, phrase) {
			t.Errorf("the watch description does not say %q", phrase)
		}
	}
}

func TestTheStandToolSaysWhichOfTheTwoOutlivesTheWindow(t *testing.T) {
	if !strings.Contains(standDescription, "`watch`") {
		t.Fatalf("the stand description never names its near neighbour:\n%s", standDescription)
	}
	if !strings.Contains(standDescription, "stops the moment the window closes") {
		t.Errorf("the stand description does not say what a watch does when the window closes")
	}
}

// AND THE CLOSING LINE IS THE WHOLE REPLY.
//
// The second defect a real model found. "say in ONE line what now stands and
// what it costs" was read as a rule about the good part of the answer, and
// deepseek-v4-flash answered a ratified card with a line of working out first:
//
//	The person wants a reminder in 1 minute. This is a `stand` with
//	`op=propose`, `when.kind=at`, `when.in="1m"`.
//
//	Set. A reminder in 1 minute to drink water — about a cent, once.
//
// The first paragraph is machinery vocabulary in something a person reads,
// which this codebase bans everywhere else, and it is the model saying it where
// no surface could scrub it. The instruction now says what the WHOLE reply is.
func TestARatifiedCardAsksForTheWholeReplyAndNotJustItsGoodPart(t *testing.T) {
	for _, phrase := range []string{
		"WHOLE reply is one short line",
		"no preamble",
		"no working out",
		"no naming this tool or its arguments",
		"do not ask again",
	} {
		if !strings.Contains(standingRatifiedLine, phrase) {
			t.Errorf("the line a ratified card sends back does not say %q:\n%s", phrase, standingRatifiedLine)
		}
	}
}

// AND IT REACHES THE MODEL. The constant is only worth anything if a ratified
// proposal actually appends it, so the tool result a yes produces is checked
// too — the same round trip [TestStandingApprovedCreatesTheItemItShowed] makes.
func TestARatifiedProposalCarriesThatLine(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.Submit(context.Background(), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, standingRatifiedLine) {
		t.Fatalf("the tool result does not carry the closing instruction:\n%s", output)
	}
}
