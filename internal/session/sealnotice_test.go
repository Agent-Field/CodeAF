package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// queuedSeals is a seal watch holding sentences, as the seat's watch does.
type queuedSeals struct{ said []string }

func (q *queuedSeals) Failing() bool { return false }

func (q *queuedSeals) Take() string {
	if len(q.said) == 0 {
		return ""
	}
	first := q.said[0]
	q.said = q.said[1:]
	return first
}

func TestCompletedTurnCarriesWhatTheSealWatchHasToSay(t *testing.T) {
	seals := &queuedSeals{said: []string{
		"zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it",
		"sealing works again, and the calls made meanwhile are sealed now",
	}}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) { config.Seals = seals })

	events := collect(t, mustSubmit(t, agent, "finish this"))

	var told []string
	for _, event := range events {
		if event.Kind == EventNotice {
			told = append(told, event.Text)
		}
	}
	if len(told) != 2 || told[0] != "zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it" {
		t.Fatalf("the turn did not carry the watch's sentences, in order: %q", told)
	}
	if len(seals.said) != 0 {
		t.Fatalf("a sentence told is still held: %q", seals.said)
	}
}

func TestATurnWithNothingToSayCarriesNoSealNotice(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) { config.Seals = &queuedSeals{} })

	for _, event := range collect(t, mustSubmit(t, agent, "finish this")) {
		if event.Kind == EventNotice {
			t.Fatalf("a quiet watch produced a notice: %q", event.Text)
		}
	}
}
