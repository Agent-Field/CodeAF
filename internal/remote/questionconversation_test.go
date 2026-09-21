package remote

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type replacingAgent struct{ *askingAgent }

func (a *replacingAgent) ReplaceQuestion(_ context.Context, answer session.Answer) (<-chan session.Event, error) {
	a.mu.Lock()
	a.answered = append(a.answered, answer)
	a.mu.Unlock()
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "updated request received"}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	return events, nil
}

func TestQuestionConversationReplacementCrossesTheRealWire(t *testing.T) {
	far := &replacingAgent{newAskingAgent()}
	loop := laneLoop(t, far)
	answer := session.Answer{Kind: session.QuestionConsent, ID: 7, Change: "explain instead"}
	stream, err := loop.Client.Agent().ReplaceQuestion(context.Background(), answer)
	if err != nil {
		t.Fatal(err)
	}
	ev := nextLane(t, stream)
	if ev.Kind != session.EventTextDelta || ev.Text != "updated request received" {
		t.Fatalf("replacement reply: %+v", ev)
	}
	if got := far.heard(); !reflect.DeepEqual(got, []session.Answer{answer}) {
		t.Fatalf("replacement changed: %+v", got)
	}
}

func TestQuestionConversationClarificationUpdatesCrossTheRealWire(t *testing.T) {
	far := newAskingAgent()
	loop := laneLoop(t, far)
	lane, stop := loop.Client.Agent().WatchQuestions()
	defer stop()
	waitFor(t, "questions lane opened", func() bool { return far.subscriptions() == 1 })
	update := session.QuestionDiscussion{ID: "clarify-1", Seq: 2, Event: &session.Event{Kind: session.EventTextDelta, Text: "Because it matters"}}
	far.raise(session.Event{Kind: session.EventQuestionDiscussion, Discussion: &update})
	got := nextLane(t, lane)
	if got.Discussion == nil || !reflect.DeepEqual(*got.Discussion, update) {
		t.Fatalf("clarification changed: %+v", got)
	}
	answer := session.Answer{Kind: session.QuestionConsent, ID: 7, Clarify: true, AskedBack: []session.Exchange{{Asked: "why?"}}}
	if err := loop.Client.Agent().ResolveQuestion(answer); err != nil {
		t.Fatal(err)
	}
	if got := far.heard(); !reflect.DeepEqual(got, []session.Answer{answer}) {
		t.Fatalf("clarification request changed: %+v", got)
	}
}
