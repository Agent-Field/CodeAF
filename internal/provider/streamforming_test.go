package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the forming phase ───────────────────────────────────────────────────────
//
// StreamToolCallForming exists for the seconds between the model starting a long
// call and that call being whole. What is tested here is the whole of what the
// vocabulary promises: it says WHICH call, it says what has arrived so far, and
// it never says any of it after the call has been announced ready.

// formingEvents pulls the forming events for one call index out of the stream.
func formingEvents(observed []StreamEvent, index int) []StreamEvent {
	var forming []StreamEvent
	for _, event := range observed {
		if event.Kind == StreamToolCallForming && event.Index == index {
			forming = append(forming, event)
		}
	}
	return forming
}

// A call streamed a fragment at a time is reported as it grows: one event per
// fragment, naming the call as far as the wire has, and carrying the arguments
// ACCUMULATED so far rather than the piece that just landed.
func TestAFormingCallIsReportedAsItGrows(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\",\"text\":\"pack"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"age main\"}"}}]},"finish_reason":"tool_calls"}]}`,
	))

	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("write a.go")); err != nil {
		t.Fatal(err)
	}

	forming := formingEvents(observed, 0)
	if len(forming) != 4 {
		t.Fatalf("forming events = %d, want one per fragment (4)", len(forming))
	}
	// Identity is known from the first fragment on, and stays known.
	for at, event := range forming {
		if event.ID != "call_1" || event.Tool != "write" {
			t.Fatalf("forming[%d] = id %q tool %q, want the call named throughout", at, event.ID, event.Tool)
		}
		if event.Session == "" && streamSessionFrom(ctx) != "" {
			t.Fatalf("forming[%d] carries no session", at)
		}
	}
	// Delta is the accumulation, so each one contains the one before it.
	want := []string{"", `{"path":`, `{"path":"a.go","text":"pack`, `{"path":"a.go","text":"package main"}`}
	for at, expect := range want {
		if forming[at].Delta != expect {
			t.Fatalf("forming[%d] args = %q, want %q", at, forming[at].Delta, expect)
		}
	}
}

// THE ORDERING LAW. Every forming event for a call lands before that call's
// StreamToolCallReady, and no forming event for it lands after — a surface keyed
// on the call id must never be told a call it has already announced is still
// arriving.
func TestFormingAlwaysPrecedesReadyForTheSameCall(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"b.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
	))

	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("read both")); err != nil {
		t.Fatal(err)
	}

	// ready[id] is where each call was announced; forming for that id must all
	// be before it.
	ready := map[string]int{}
	for at, event := range observed {
		if event.Kind != StreamToolCallReady {
			continue
		}
		var call ai.ToolCall
		if err := json.Unmarshal([]byte(event.Delta), &call); err != nil {
			t.Fatal(err)
		}
		ready[call.ID] = at
	}
	if len(ready) != 2 {
		t.Fatalf("announced %d calls, want both", len(ready))
	}
	for at, event := range observed {
		if event.Kind != StreamToolCallForming || event.ID == "" {
			continue
		}
		announced, known := ready[event.ID]
		if !known {
			t.Fatalf("forming for %q which is never announced", event.ID)
		}
		if at > announced {
			t.Fatalf("forming for %q at %d lands AFTER its ready at %d", event.ID, at, announced)
		}
	}
	// And each call formed at all — the phase is not skipped for either.
	for _, index := range []int{0, 1} {
		if len(formingEvents(observed, index)) == 0 {
			t.Fatalf("call at index %d never formed", index)
		}
	}
}

// A trailing fragment for a call this decoder has already closed says nothing.
// The endpoint is the one behaving oddly, but the law is the consumer's and it
// holds regardless: after ready, silence.
func TestAFragmentAfterReadyFormsNothing(t *testing.T) {
	tools := &toolCallAccumulator{}
	first, second := 0, 1
	tools.add(toolCallDelta{Index: &first, ID: "call_1", Function: toolFunctionDelta{Name: "read", Arguments: `{"path":"a.go"}`}})
	// Opening index 1 closes index 0.
	if _, complete := tools.add(toolCallDelta{Index: &second, ID: "call_2", Function: toolFunctionDelta{Name: "read"}}); !complete {
		t.Fatal("opening a second call did not close the first")
	}
	// Now a straggler for the closed call.
	tools.add(toolCallDelta{Index: &first, Function: toolFunctionDelta{Arguments: " "}})
	if forming, open := tools.current(); open {
		t.Fatalf("current() = %+v after that call was announced, want silence", forming)
	}
}

// A stream whose call dies half-sent announces nothing — and the forming events
// that described it are all a surface ever saw, which is the honest record of
// what happened.
func TestATruncatedCallFormsButNeverReadies(t *testing.T) {
	client := streamClientForTest(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write","arguments":"{\"path\":\"a.go\",\"text\":\"half"}}]}}]}` + "\n\n"))
		// No [DONE]: the call stops half-sent.
	}))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("write it")); err != nil {
		t.Fatal(err)
	}
	if len(formingEvents(observed, 0)) == 0 {
		t.Fatal("a call that arrived and stopped formed nothing")
	}
	for _, event := range observed {
		if event.Kind == StreamToolCallReady {
			t.Fatalf("a half-sent call was announced: %q", event.Delta)
		}
	}
}

// A plain text answer forms nothing, and a NON-STREAMING completion forms
// nothing either: without an observer there is no stream loop at all, which is
// what makes this phase free for every surface that ignores it.
func TestNoToolCallsFormNothing(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"content":"no tools here"},"finish_reason":"stop"}]}`,
	))
	var formed int
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamToolCallForming {
			formed++
		}
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err != nil {
		t.Fatal(err)
	}
	if formed != 0 {
		t.Fatalf("a text-only answer produced %d forming events", formed)
	}

	// The non-streaming path: no observer in the context, so nothing is
	// observed and the response is the whole of the contract.
	plain := streamClientForTest(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	response, err := plain.CompleteWithMessages(context.Background(), userMessages("read a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if calls := response.ToolCalls(); len(calls) != 1 || calls[0].Function.Name != "read" {
		t.Fatalf("non-streaming call = %+v, want the read intact", calls)
	}
}

// The event is fields, not a marshaled payload: a forming event is raised per
// fragment, and the raw arguments must arrive uncopied into JSON. This pins the
// shape so a later refactor cannot quietly put the work back into the read loop.
func TestFormingCarriesRawArgumentsNotJSON(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo "}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"hi\"}"}}]},"finish_reason":"tool_calls"}]}`,
	))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("say hi")); err != nil {
		t.Fatal(err)
	}
	forming := formingEvents(observed, 0)
	if len(forming) == 0 {
		t.Fatal("no forming events")
	}
	first := forming[0]
	if !strings.HasPrefix(first.Delta, `{"command":`) {
		t.Fatalf("first forming Delta = %q, want the raw partial arguments", first.Delta)
	}
	// It is NOT a marshaled ai.ToolCall — that is StreamToolCallReady's shape.
	var call ai.ToolCall
	if err := json.Unmarshal([]byte(first.Delta), &call); err == nil && call.Function.Name != "" {
		t.Fatalf("forming Delta decoded as a whole call: %q", first.Delta)
	}
}
