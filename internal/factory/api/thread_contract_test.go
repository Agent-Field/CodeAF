package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestThreadAndAnsweredQuestionsAreAdditiveContractOne(t *testing.T) {
	if Contract != 1 {
		t.Fatalf("contract = %d", Contract)
	}
	it := factory.Item{ID: 3, State: factory.StateRunning,
		Thread:            []factory.Message{{ID: "reply_1", At: at, From: "sam", Text: "Keep the flag.", State: "delivered"}},
		AnsweredQuestions: []factory.Question{{Text: "Which flag?", State: "answered", Kind: "scope", Answer: &factory.Answer{By: "sam", Text: "the old one", At: at, Event: "evt_1"}}},
	}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	back := ToItem(job)
	if !reflect.DeepEqual(back.Thread, it.Thread) || !reflect.DeepEqual(back.AnsweredQuestions, it.AnsweredQuestions) || back.Question != "" || back.Asking != nil {
		t.Fatalf("thread/history round trip: %+v", back)
	}
	r := factory.RunReport{Thread: []factory.Message{{ID: "line_1", At: at, From: "manager", Text: "The proof is ready."}}, Delivered: []string{"reply_1"}}
	if back := ToWorkerReport(FromRunReport(r)); !reflect.DeepEqual(back, r) {
		t.Fatalf("worker thread round trip: %+v", back)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	answer := fields["questions"].([]any)[0].(map[string]any)["answer"].(map[string]any)
	for _, key := range []string{"by", "text", "at", "event"} {
		if _, ok := answer[key]; !ok {
			t.Fatalf("answer missing %s: %s", key, raw)
		}
	}
	message, _ := json.Marshal(Message{At: at, From: "manager", Text: "ready"})
	fields = nil
	if err := json.Unmarshal(message, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["from"] != "manager" || fields["text"] != "ready" || fields["at"] == nil {
		t.Fatalf("existing message shape changed: %s", message)
	}
}
