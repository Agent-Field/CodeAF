package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// THE DONE-CONDITION FOR THE WHOLE ASK IS WRITTEN ONCE, SHOWN ONCE, AND FROZEN.
//
// Every acceptance in this build before it was one unit of work's, read only by
// that unit's auditor. A session could land four verified pieces of work with
// nothing anywhere saying whether the thing the person asked for had happened.
func TestTheSessionAcceptanceFreezesTheOriginalAskWithoutACall(t *testing.T) {
	for _, ask := range []string{
		"port the parser to the new lexer",
		"write a warm invitation for the neighbourhood picnic",
		"compare the three rows in sales.csv and report the largest change",
	} {
		t.Run(firstLine(ask), func(t *testing.T) {
			completer := &scriptedCompleter{}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Unattended = true
				c.Budget = Budget{Wall: 6 * time.Hour}
			})
			steward := agent.steward()
			steward.hear(ask)
			hub := newEventHub()
			stream := hub.subscribe()
			agent.openAcceptance(context.Background(), hub)

			if got := steward.Acceptance(); !strings.Contains(got, ask) {
				t.Fatalf("the frozen acceptance lost the original ask: %q", got)
			}
			if completer.requests() != 0 {
				t.Fatalf("opening the request made %d model calls", completer.requests())
			}
			var shown string
			go hub.close()
			for event := range stream {
				if event.Kind == EventNotice && strings.HasPrefix(event.Text, sessionAcceptanceLead) {
					shown = event.Text
				}
			}
			if !strings.Contains(shown, ask) {
				t.Fatalf("the original ask was never shown: %q", shown)
			}

			agent.openAcceptance(context.Background(), newEventHub())
			if completer.requests() != 0 || steward.Acceptance() != routeAcceptance(routeVerdict{}, ask) {
				t.Fatalf("the frozen acceptance moved: %q", steward.Acceptance())
			}
		})
	}
}

func TestTheSessionAcceptanceKeepsTheTailOfALongOriginalAsk(t *testing.T) {
	const tail = "FINAL REQUIREMENT: include the signed decision table"
	ask := strings.Repeat("context ", briefAskLimit) + tail
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().hear(ask)
	agent.openAcceptance(context.Background(), nil)
	if got := agent.steward().Acceptance(); !strings.Contains(got, tail) {
		t.Fatalf("the frozen whole-request contract lost its tail: %q", got[len(got)-100:])
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
