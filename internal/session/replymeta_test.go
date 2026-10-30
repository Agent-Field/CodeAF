package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHEN A CONVERSATION LAST ANSWERED THE PERSON IS READABLE WITHOUT ITS
// TRANSCRIPT: the seal stamps [Meta.LastReplyAt] beside the spend when the turn
// ended on an answer, and leaves it alone when the turn ended on a tool step.
func TestATurnThatAnswersStampsWhenItRepliedAndAToolStepDoesNot(t *testing.T) {
	dir := t.TempDir()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("here is the answer"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, errors.New("400 bad request: the provider gave up")
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	collect(t, mustSubmit(t, agent, "what is it?"))
	first := stampedMeta(t, agent, dir)
	if first.LastReplyAt.IsZero() {
		t.Fatal("a turn that answered stamped no reply time")
	}
	if first.LastUserAt.IsZero() || first.LastReplyAt.Before(first.LastUserAt) {
		t.Fatalf("the reply (%v) is not after the person's message (%v)", first.LastReplyAt, first.LastUserAt)
	}

	collect(t, mustSubmit(t, agent, "list the workspace"))
	second := stampedMeta(t, agent, dir)
	if !second.LastReplyAt.Equal(first.LastReplyAt) {
		t.Fatalf("a turn of tool steps moved the reply time from %v to %v", first.LastReplyAt, second.LastReplyAt)
	}
}
