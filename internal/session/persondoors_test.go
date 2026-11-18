package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The laws of whose words a turn holds ([Agent.turnPersonWords]): only the
// person's own doors set them. Steer and FollowUp are also engine and runner
// doors, so words that come by them are nobody's; SteerMessage and
// FollowUpMessage are the person's, marked or not.

// steeredPersonWords runs one turn the person opened with "look around", sends
// one correction into it by steer, and answers the person's words the turn's
// next step read.
func steeredPersonWords(t *testing.T, steer func(*Agent, string) (<-chan Event, error)) string {
	t.Helper()
	first := make(chan struct{})
	var agent *Agent
	var seen string
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "looking")
			close(first)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			seen = agent.turnPersonWords()
			return textResponse("folded in"), nil
		},
	}}
	agent, _ = newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "look around")
	<-first
	if _, err := steer(agent, "go left instead"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	collect(t, turn)
	return seen
}

func TestAnEngineSteerDoesNotSetThePersonsWords(t *testing.T) {
	got := steeredPersonWords(t, (*Agent).Steer)
	if got != "look around" {
		t.Fatalf("after an engine steer the person's words read %q, want the person's own %q", got, "look around")
	}
}

func TestThePersonsSteerSetsTheirWords(t *testing.T) {
	got := steeredPersonWords(t, func(a *Agent, words string) (<-chan Event, error) {
		return a.SteerMessage(Submission{Text: words})
	})
	if got != "go left instead" {
		t.Fatalf("after the person's steer their words read %q, want %q", got, "go left instead")
	}
}

// followedPersonWords queues one follow-up behind a running turn the person
// opened, and answers the person's words the follow-up's own turn read.
func followedPersonWords(t *testing.T, follow func(*Agent, string) (<-chan Event, error)) string {
	t.Helper()
	var agent *Agent
	queued := make(chan (<-chan Event), 1)
	var seen string
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			events, err := follow(agent, "then tidy up")
			if err != nil {
				t.Errorf("follow-up: %v", err)
			}
			queued <- events
			return textResponse("done"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			seen = agent.turnPersonWords()
			return textResponse("tidied"), nil
		},
	}}
	agent, _ = newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "first question"))
	collect(t, <-queued)
	return seen
}

// A PERSON'S QUEUED FOLLOW-UP OPENS A TURN THAT IS THEIRS: the tools that act
// only on the person's own words read them from it.
func TestThePersonsQueuedFollowUpIsTheirs(t *testing.T) {
	got := followedPersonWords(t, func(a *Agent, text string) (<-chan Event, error) {
		return a.FollowUpMessage(Submission{Text: text})
	})
	if got != "then tidy up" {
		t.Fatalf("the person's follow-up turn holds %q as their words, want %q", got, "then tidy up")
	}
}

// AN ENGINE'S FOLLOW-UP, AND A STAGE'S, ARE NOBODY'S.
func TestAnEngineOrStageFollowUpIsNotThePersons(t *testing.T) {
	doors := map[string]func(*Agent, string) (<-chan Event, error){
		"FollowUp": (*Agent).FollowUp,
		"FollowUpRequiring": func(a *Agent, text string) (<-chan Event, error) {
			return a.FollowUpRequiring(text, "")
		},
	}
	for name, door := range doors {
		t.Run(name, func(t *testing.T) {
			if got := followedPersonWords(t, door); got != "" {
				t.Fatalf("a %s turn holds %q as the person's words, want none", name, got)
			}
		})
	}
}

// A FOLLOW-UP THE PERSON SENDS TO AN IDLE SESSION starts their turn at once,
// and it is theirs too.
func TestThePersonsFollowUpToAnIdleSessionIsTheirs(t *testing.T) {
	var agent *Agent
	var seen string
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			seen = agent.turnPersonWords()
			return textResponse("on it"), nil
		},
	}}
	agent, _ = newTestAgent(t, completer, nil)
	events, err := agent.FollowUpMessage(Submission{Text: "start the tidy"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if seen != "start the tidy" {
		t.Fatalf("the person's follow-up turn holds %q as their words, want %q", seen, "start the tidy")
	}
}
