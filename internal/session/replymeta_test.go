package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
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

// ONLY A FINISHED ANSWER STAMPS: a partial reply a stop cut short and a
// checkpoint's hand-over note end a turn on assistant words, but neither is
// the model's answer to the person, so the reply time stays where it was.
func TestOnlyAFinishedAnswerStampsWhenItReplied(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})
	if err := SaveMeta(dir, Meta{ID: "replies", Title: "replies"}); err != nil {
		t.Fatal(err)
	}
	agent.recordPresentedAssistant(textMessage("assistant", "the first half of"), provider.MessageReasoning{}, interruptedPresentation("the first half of"))
	agent.stampSealed()
	if got := stampedMeta(t, agent, dir).LastReplyAt; !got.IsZero() {
		t.Fatalf("a partial reply a stop cut short stamped %v", got)
	}
	agent.recordPresentedAssistant(textMessage("assistant", "Handed the rest to task 4."), provider.MessageReasoning{}, &messagePresentation{Audience: "operational"})
	agent.stampSealed()
	if got := stampedMeta(t, agent, dir).LastReplyAt; !got.IsZero() {
		t.Fatalf("a checkpoint's hand-over note stamped %v", got)
	}
	agent.recordPresentedAssistant(textMessage("assistant", "here is the answer"), provider.MessageReasoning{}, humanPresentation("here is the answer"))
	agent.stampSealed()
	if stampedMeta(t, agent, dir).LastReplyAt.IsZero() {
		t.Fatal("a finished answer stamped no reply time")
	}
}
