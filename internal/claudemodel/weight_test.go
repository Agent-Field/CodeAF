package claudemodel

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/claudeagent"
)

// streamOnce runs one of codeaf's requests against a held turn and returns
// what it wrote. feed sends that request's events and, for a tool step, the
// call that ends it.
func streamOnce(t *testing.T, l *live, feed func(events chan<- claudeagent.Event)) string {
	t.Helper()
	events := make(chan claudeagent.Event)
	reader, writer := io.Pipe()
	l.turn.Lock()
	go l.stream(context.Background(), writer, events, "sonnet", false, "")
	go feed(events)
	got := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(reader)
		got <- string(raw)
	}()
	select {
	case out := <-got:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never ended")
		return ""
	}
}

// promptTokensOf is the prompt_tokens the stream's closing chunk reported.
func promptTokensOf(t *testing.T, stream string) int {
	t.Helper()
	for _, line := range strings.Split(stream, "\n") {
		body, ok := strings.CutPrefix(line, "data: ")
		if !ok || !strings.Contains(body, `"usage"`) {
			continue
		}
		var chunk struct {
			Usage struct {
				PromptTokens int `json:"prompt_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(body), &chunk); err != nil {
			t.Fatalf("usage chunk %q: %v", body, err)
		}
		return chunk.Usage.PromptTokens
	}
	t.Fatalf("no usage in stream %q", stream)
	return 0
}

// A TURN'S RESULT REPORTS WHAT ITS LAST CALL READ, EVEN WHEN THAT CALL WAS
// SEEN BY AN EARLIER REQUEST. Each call Claude Code makes to a codeaf tool ends
// one of codeaf's requests, so the request that carries the turn's result may
// see no call at all — an API error comes back as the result alone — and it
// used to report the result's input, which is every call of the turn added up.
// On 2026-11-25 a seven-step turn on a ~58k conversation read as 409k, and the
// next message was refused before sending as too long for a 200k window.
func TestAResultWithNoCallOfItsOwnReportsTheTurnsLastCallNotItsSum(t *testing.T) {
	l := &live{
		session:  &claudeagent.Session{},
		calls:    make(chan *pendingCall, 4),
		awaiting: map[string]*pendingCall{},
		started:  map[string]startedStep{},
	}
	step := streamOnce(t, l, func(events chan<- claudeagent.Event) {
		events <- claudeagent.Event{Type: "assistant", Raw: json.RawMessage(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__codeaf__work_tasks","input":{}}],"usage":{"input_tokens":12,"cache_read_input_tokens":58000,"cache_creation_input_tokens":400}}}`)}
		l.calls <- &pendingCall{id: stepPrefix + "t1", name: "work_tasks", args: json.RawMessage(`{}`), result: make(chan claudeagent.ToolResult, 1)}
	})
	if !strings.Contains(step, `"tool_calls"`) {
		t.Fatalf("the tool step did not hand codeaf its call: %q", step)
	}
	result := streamOnce(t, l, func(events chan<- claudeagent.Event) {
		events <- claudeagent.Event{Type: "result", Raw: json.RawMessage(`{"type":"result","result":"API Error: the request was refused","usage":{"input_tokens":84,"cache_read_input_tokens":406000,"cache_creation_input_tokens":2925,"output_tokens":563}}`)}
	})
	if got, want := promptTokensOf(t, result), 58412; got != want {
		t.Fatalf("prompt_tokens = %d, want %d: the result must report the turn's last call, not every call added up", got, want)
	}
}

// A ONE-CALL TURN WITH NO WEIGHT YET KNOWN STILL REPORTS ITS RESULT'S INPUT,
// which for one call is that call's own.
func TestAResultWithNoCallEverSeenReportsItsOwnInput(t *testing.T) {
	l := &live{
		session:  &claudeagent.Session{},
		calls:    make(chan *pendingCall, 1),
		awaiting: map[string]*pendingCall{},
		started:  map[string]startedStep{},
	}
	result := streamOnce(t, l, func(events chan<- claudeagent.Event) {
		events <- claudeagent.Event{Type: "result", Raw: json.RawMessage(`{"type":"result","result":"done","usage":{"input_tokens":10,"cache_read_input_tokens":2000,"output_tokens":5}}`)}
	})
	if got, want := promptTokensOf(t, result), 2010; got != want {
		t.Fatalf("prompt_tokens = %d, want %d", got, want)
	}
}

// A NEW TURN DOES NOT REPORT THE LAST TURN'S WEIGHT: a turn whose only stream
// sees no call of its own reports its result's input, not what the previous
// turn's newest call read.
func TestANewTurnStartsItsWeightAfresh(t *testing.T) {
	l := &live{
		session:  &claudeagent.Session{},
		calls:    make(chan *pendingCall, 1),
		awaiting: map[string]*pendingCall{},
		started:  map[string]startedStep{},
		weight:   58412, // the previous turn's last call
	}
	l.mu.Lock()
	l.newTurnLocked()
	l.mu.Unlock()
	result := streamOnce(t, l, func(events chan<- claudeagent.Event) {
		events <- claudeagent.Event{Type: "result", Raw: json.RawMessage(`{"type":"result","result":"done","usage":{"input_tokens":10,"cache_read_input_tokens":2000,"output_tokens":5}}`)}
	})
	if got, want := promptTokensOf(t, result), 2010; got != want {
		t.Fatalf("prompt_tokens = %d, want %d: the new turn reported the last turn's weight", got, want)
	}
}
