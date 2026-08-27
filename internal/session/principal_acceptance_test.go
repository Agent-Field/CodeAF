package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE DONE-CONDITION FOR THE WHOLE ASK IS WRITTEN ONCE, SHOWN ONCE, AND FROZEN.
//
// Every acceptance in this build before it was one unit of work's, read only by
// that unit's auditor. A session could land four verified pieces of work with
// nothing anywhere saying whether the thing the person asked for had happened.
func TestTheSessionAcceptanceIsWrittenOnceAndShownOnce(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"work": true, "goal": "port the parser",
				"acceptance": "every fixture under testdata parses and go build ./... passes",
				"why": "a port across the parser"}`), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: 6 * time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser to the new lexer")

	hub := newEventHub()
	stream := hub.subscribe()
	agent.openAcceptance(context.Background(), hub)

	const want = "every fixture under testdata parses and go build ./... passes"
	if steward.Acceptance() != want {
		t.Fatalf("the acceptance is %q", steward.Acceptance())
	}
	// THE ASK IS WHAT THE WRITER WAS SHOWN, verbatim, because interpretation is
	// what a frozen sentence exists to stop.
	if sent := completer.request(0); len(sent) == 0 ||
		!strings.Contains(messageText(sent[len(sent)-1]), "port the parser to the new lexer") {
		t.Fatalf("the writer was not shown the person's own words: %+v", sent)
	}
	// AND THE PERSON READS IT ONCE, on the label the landed-work card already
	// uses for the same fact.
	var shown string
	go hub.close()
	for event := range stream {
		if event.Kind == EventNotice && strings.HasPrefix(event.Text, sessionAcceptanceLead) {
			shown = event.Text
		}
	}
	if !strings.Contains(shown, want) {
		t.Fatalf("the acceptance was never shown: %q", shown)
	}

	// AND IT IS ASKED ONCE. A second turn does not re-open the question, so a
	// session cannot end up measured against a sentence its own work argued for.
	agent.openAcceptance(context.Background(), newEventHub())
	if completer.requests() != 1 {
		t.Fatalf("the acceptance was written %d times", completer.requests())
	}
}

// A WRITER NOBODY COULD REACH COSTS THE SHARPNESS OF THE SENTENCE, NOT THE
// SENTENCE. The ladder's lower rungs still say something true about a whole ask
// — the person's own words, framed — and a run must not fail to start because a
// sidecar was down.
func TestAnAcceptanceNobodyCouldWriteFallsBackToThePersonsOwnWords(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("I am afraid I cannot help with that."), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: 6 * time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser to the new lexer")
	agent.openAcceptance(context.Background(), newEventHub())

	acceptance := steward.Acceptance()
	if acceptance == "" {
		t.Fatal("a session with an unreachable writer has no acceptance at all")
	}
	if !strings.Contains(acceptance, "port the parser to the new lexer") {
		t.Fatalf("the fallback lost the person's own words: %q", acceptance)
	}
}

// AND A SESSION NOBODY GAVE A GOAL OWNER NEVER ASKS THE QUESTION, which is the
// whole of what this costs an ordinary conversation: one nil check per turn.
func TestAnAttendedSessionNeverWritesAnAcceptance(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)
	agent.hearAsk("port the parser to the new lexer")
	agent.openAcceptance(context.Background(), newEventHub())
	if completer.requests() != 0 {
		t.Fatalf("an attended session paid for %d acceptance calls", completer.requests())
	}
	if agent.who().Acceptance() != "" {
		t.Fatalf("an attended session was given an acceptance: %q", agent.who().Acceptance())
	}
}
