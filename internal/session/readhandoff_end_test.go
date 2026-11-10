package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestReadHandoffEndsEveryInterceptedCall(t *testing.T) {
	completer := newQuickLanes(nil)
	completer.lane("HANDOFF-ANSWER", finalText("The three files agree."))
	a, _, _ := quickAgent(t, completer)
	hub := newEventHub()
	events := hub.subscribe()
	calls := []ai.ToolCall{
		readCall("read", `{"path":"one.go"}`),
		readCall("read", `{"path":"two.go"}`),
		readCall("read", `{"path":"three.go"}`),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := a.handoffReadSweep(ctx, a.newEpisode(), hub, userText("HANDOFF-ANSWER: compare the files"), calls, &readSweep{}, &warmBatch{})
	hub.close()
	if len(results) != len(calls) {
		t.Fatal("handoff did not return the reading")
	}
	var got []Event
	for event := range events {
		got = append(got, event)
	}
	for _, call := range calls {
		finished, ended := -1, -1
		for i, ev := range got {
			if ev.CallID != call.ID {
				continue
			}
			switch ev.Kind {
			case EventToolFinished:
				finished = i
			case EventToolEnd:
				ended = i
			}
		}
		if finished < 0 || ended <= finished {
			t.Errorf("%s: finished event=%d end event=%d; reading is still open after return", call.ID, finished, ended)
		}
	}
}
