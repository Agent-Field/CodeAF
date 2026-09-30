package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sseHandler writes the given chunk payloads as one event stream and closes it.
func sseHandler(payloads ...string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, payload := range payloads {
			_, _ = writer.Write([]byte("data: " + payload + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
}

func streamClientForTest(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// ── reasoning text on the wire ──────────────────────────────────────────────

// The names reasoning travels under share an event kind by the time they leave
// here, while each event retains the field spelling needed for replay.
//
// StreamThinking is unchanged — still exactly one per run — because a surface
// that only draws "thinking…" must keep working without knowing this exists.
func TestStreamedReasoningArrivesAsTextInOrderUnderBothNames(t *testing.T) {
	for _, field := range []string{"reasoning", "reasoning_content", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			client := streamClientForTest(t, sseHandler(
				`{"id":"one","choices":[{"index":0,"delta":{"role":"assistant","`+field+`":"the file "}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"`+field+`":"is the read"}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"content":"Reading it."},"finish_reason":"stop"}]}`,
			))

			var observed []StreamEvent
			ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
				observed = append(observed, event)
			})
			response, err := client.CompleteWithMessages(ctx, userMessages("what is in the file"))
			if err != nil {
				t.Fatal(err)
			}

			var reasoning []string
			thinking := 0
			for _, event := range observed {
				switch event.Kind {
				case StreamReasoning:
					reasoning = append(reasoning, event.Delta)
					if event.ReasoningField != field {
						t.Fatalf("reasoning field = %q, want %q", event.ReasoningField, field)
					}
				case StreamThinking:
					thinking++
				}
			}
			if strings.Join(reasoning, "|") != "the file |is the read" {
				t.Fatalf("reasoning deltas = %q, want them whole and in order", reasoning)
			}
			if thinking != 1 {
				t.Fatalf("thinking events = %d, want exactly one for the run", thinking)
			}
			// The run's boundary comes first, then its words: a surface reading
			// only the first still opens the region before anything fills it.
			for index, event := range observed {
				if event.Kind == StreamReasoning {
					if observed[index-1].Kind != StreamThinking && observed[index-1].Kind != StreamReasoning {
						t.Fatalf("reasoning at %d follows %v, want the thinking boundary first",
							index, observed[index-1].Kind)
					}
					break
				}
			}
			// And the thought is not the answer: it is nowhere in the response.
			if response.Text() != "Reading it." {
				t.Fatalf("response text = %q, want only the answer", response.Text())
			}
		})
	}
}

// A model that streams no reasoning must produce no reasoning events at all —
// an empty delta announced would be a claim about a model that said nothing.
func TestStreamWithoutReasoningAnnouncesNone(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
	))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err != nil {
		t.Fatal(err)
	}
	for _, event := range observed {
		if event.Kind == StreamReasoning || event.Kind == StreamThinking {
			t.Fatalf("a plain text answer produced %v", event.Kind)
		}
	}
}

func TestStreamedReasoningDetailsLeaveTheDecoderByteIdentical(t *testing.T) {
	want := json.RawMessage(`[{"type":"reasoning.text","data":{"scale":1e3}}]`)
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking","reasoning_details":`+string(want)+`}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
	))
	var got json.RawMessage
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamReasoning {
			got = append(got[:0], event.ReasoningDetails...)
		}
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("reasoning_details = %s, want byte-identical %s", got, want)
	}
}

// ── streamed tool-call boundaries ───────────────────────────────────────────

// readyCalls decodes the tool calls announced through the observer.
func readyCalls(t *testing.T, observed []StreamEvent) []ai.ToolCall {
	t.Helper()
	var ready []ai.ToolCall
	for _, event := range observed {
		if event.Kind != StreamToolCallReady {
			continue
		}
		var call ai.ToolCall
		if err := json.Unmarshal([]byte(event.Delta), &call); err != nil {
			t.Fatalf("StreamToolCallReady payload %q does not decode: %v", event.Delta, err)
		}
		ready = append(ready, call)
	}
	return ready
}

// Every call is announced exactly once, whole, in stream order — and what is
// announced is what the response ends up carrying.
func TestEachToolCallIsAnnouncedOnceAndWhole(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"write","arguments":"{\"path\":\"b.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
	))

	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("read a.go then write b.go"))
	if err != nil {
		t.Fatal(err)
	}

	ready := readyCalls(t, observed)
	if len(ready) != 2 {
		t.Fatalf("announced %d calls, want 2: %#v", len(ready), ready)
	}
	if ready[0].ID != "call_1" || ready[0].Function.Name != "read" ||
		ready[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("first announced call = %#v, want the whole read", ready[0])
	}
	if ready[1].ID != "call_2" || ready[1].Function.Name != "write" {
		t.Fatalf("second announced call = %#v", ready[1])
	}

	// The announcement and the response agree, call for call. They must: the
	// consumer pairs early work with the response's own assembly by id.
	calls := response.ToolCalls()
	if len(calls) != len(ready) {
		t.Fatalf("response carries %d calls, %d were announced", len(calls), len(ready))
	}
	for index := range calls {
		if calls[index] != ready[index] {
			t.Fatalf("call %d announced as %#v, response says %#v", index, ready[index], calls[index])
		}
	}

	// The first call is announced BEFORE the last chunk: that earliness is the
	// whole point, and a Ready that only ever landed beside StreamFinished would
	// be a feature that does nothing.
	firstReady, finished := -1, -1
	for index, event := range observed {
		if event.Kind == StreamToolCallReady && firstReady < 0 {
			firstReady = index
		}
		if event.Kind == StreamFinished {
			finished = index
		}
	}
	if firstReady < 0 || finished < 0 || firstReady >= finished-1 {
		t.Fatalf("first Ready at %d, finished at %d — the announcement is not early", firstReady, finished)
	}
}

// THE EARLINESS IS REAL, not an artifact of a buffered test body. This server
// holds the second half of the stream back until the observer has seen the
// first call announced: an implementation that only announced calls at the end
// of the message would deadlock here rather than fail an assertion.
func TestToolCallIsAnnouncedWhileTheConnectionIsStillOpen(t *testing.T) {
	announced := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("the test server cannot flush; this test proves nothing without it")
			return
		}
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]}}]}` + "\n\n"))
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ls","arguments":""}}]}}]}` + "\n\n"))
		flusher.Flush()
		select {
		case <-announced:
		case <-time.After(20 * time.Second):
			t.Error("the first call was never announced while the stream was open")
		}
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\".\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewClient(Config{
		APIKey: "k", BaseURL: server.URL, Model: "sim/model", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var observed []StreamEvent
	closed := false
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
		if event.Kind == StreamToolCallReady && !closed {
			closed = true
			close(announced)
		}
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("read a.go and list the tree"))
	if err != nil {
		t.Fatal(err)
	}
	ready := readyCalls(t, observed)
	if len(ready) != 2 || ready[0].ID != "call_1" || ready[1].ID != "call_2" {
		t.Fatalf("announced calls = %#v, want call_1 then call_2", ready)
	}
	if ready[1].Function.Arguments != `{"path":"."}` {
		t.Fatalf("the second call was announced as %#v — its last fragment is missing", ready[1])
	}
	if len(response.ToolCalls()) != 2 {
		t.Fatalf("response carries %#v", response.ToolCalls())
	}
}

// A call whose arguments have not finished arriving is NOT announced. The
// consumer is starting work off this, and half an instruction is worse than a
// late one: the response's own assembly is always the authority.
func TestATruncatedCallIsNeverAnnounced(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.g"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("read it")); err != nil {
		t.Fatal(err)
	}
	ready := readyCalls(t, observed)
	if len(ready) != 1 || ready[0].ID != "call_2" {
		t.Fatalf("announced %#v, want only the call whose arguments parse", ready)
	}
}

// A call with no name is not an instruction, and assembled() drops it for the
// same reason. Announcing one would offer a consumer work it cannot do.
func TestANamelessCallIsNeverAnnounced(t *testing.T) {
	client := streamClientForTest(t, sseHandler(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("do it")); err != nil {
		t.Fatal(err)
	}
	if ready := readyCalls(t, observed); len(ready) != 1 || ready[0].ID != "call_2" {
		t.Fatalf("announced %#v, want only the named call", ready)
	}
}

// A stream that stops without saying [DONE] is truncated. It must not return a
// response, while announcements still follow the same rule: whole calls, never
// the one the stream stopped in the middle of.
func TestAStreamThatStopsShortAnnouncesOnlyWholeCalls(t *testing.T) {
	client := streamClientForTest(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]}}]}` + "\n\n"))
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ls","arguments":"{\"pa"}}]}}]}` + "\n\n"))
		// No [DONE]: the stream simply stops, mid-instruction.
	}))
	var observed []StreamEvent
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		observed = append(observed, event)
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("read it"))
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutTruncated {
		t.Fatalf("err = %v, want a truncated stream cut", err)
	}
	if response != nil {
		t.Fatalf("truncated stream returned a response: %+v", response)
	}
	ready := readyCalls(t, observed)
	if len(ready) != 1 || ready[0].ID != "call_1" {
		t.Fatalf("announced %#v, want only the call that was whole", ready)
	}
}

// ── the accumulator's own boundaries ────────────────────────────────────────

func TestAccumulatorReportsEachCallExactlyOnce(t *testing.T) {
	index := func(value int) *int { return &value }
	var tools toolCallAccumulator
	fragments := []toolCallDelta{
		{Index: index(0), ID: "a", Function: toolFunctionDelta{Name: "read", Arguments: `{"path":`}},
		{Index: index(0), Function: toolFunctionDelta{Arguments: `"a.go"}`}},
		{Index: index(1), ID: "b", Function: toolFunctionDelta{Name: "ls", Arguments: `{}`}},
	}
	var announced []ai.ToolCall
	for _, fragment := range fragments {
		if call, ready := tools.add(fragment); ready {
			announced = append(announced, call)
		}
	}
	if len(announced) != 1 || announced[0].ID != "a" || announced[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("announced mid-stream = %#v, want the whole first call", announced)
	}
	announced = append(announced, tools.flush()...)
	if len(announced) != 2 || announced[1].ID != "b" {
		t.Fatalf("after flush = %#v, want both calls once each", announced)
	}
	// A second flush is a no-op: a call announced twice would be work started
	// twice by anyone acting on it.
	if again := tools.flush(); len(again) != 0 {
		t.Fatalf("a second flush announced %#v", again)
	}
	if assembled := tools.assembled(); len(assembled) != 2 ||
		assembled[0].Function.Arguments != `{"path":"a.go"}` || assembled[1].ID != "b" {
		t.Fatalf("assembled = %#v — announcing must not disturb the response's own assembly", assembled)
	}
}
