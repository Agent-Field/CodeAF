package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestCompletedTurnCarriesANewerBuildNotice(t *testing.T) {
	const notice = "a newer aforge was built at 13:28 — restart to use it"
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}, func(config *Config) {
		config.newerBuild = func() string { return notice }
	})

	events := collect(t, mustSubmit(t, agent, "finish this"))
	found := -1
	done := -1
	for index, event := range events {
		if event.Kind == EventNotice && event.Text == notice {
			found = index
		}
		if event.Kind == EventTurnDone {
			done = index
		}
	}
	if found < 0 {
		t.Fatalf("the turn carried no newer-build notice: %v", kinds(events))
	}
	if done < 0 || found > done {
		t.Fatalf("the notice did not arrive before turn done: %v", kinds(events))
	}
}
