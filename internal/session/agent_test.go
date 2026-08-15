package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the scripted completer ──────────────────────────────────────────────────

// step is one provider request's scripted answer. It receives the request's
// context — so it can emit stream events through provider.Emit exactly as the
// real adapter does — and the messages it was sent.
type step func(ctx context.Context, messages []ai.Message) (*ai.Response, error)

type scriptedCompleter struct {
	mu     sync.Mutex
	steps  []step
	seen   [][]ai.Message
	models []string
}

func (s *scriptedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// The options are applied to a throwaway request so a test can assert what
	// model each step actually rode.
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}

	s.mu.Lock()
	index := len(s.seen)
	snapshot := make([]ai.Message, len(messages))
	copy(snapshot, messages)
	s.seen = append(s.seen, snapshot)
	s.models = append(s.models, request.Model)
	var next step
	if index < len(s.steps) {
		next = s.steps[index]
	}
	s.mu.Unlock()

	if next == nil {
		// Past the script: answer without a tool call so a loop that ran one
		// step further than the test expected terminates instead of hanging.
		return textResponse("(unscripted)"), nil
	}
	return next(ctx, snapshot)
}

func (s *scriptedCompleter) request(index int) []ai.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.seen) {
		return nil
	}
	return s.seen[index]
}

func (s *scriptedCompleter) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (s *scriptedCompleter) model(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.models) {
		return ""
	}
	return s.models[index]
}

func textResponse(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}},
		}}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

func toolResponse(id, name, arguments string) *ai.Response {
	return toolResponseWithText(id, name, arguments, "")
}

// toolResponseWithText is the shape a real provider returns when the model
// says something before calling a tool: text AND tool calls in one response.
func toolResponseWithText(id, name, arguments, text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}},
			ToolCalls: []ai.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: ai.ToolCallFunction{Name: name, Arguments: arguments},
			}},
		}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

// ── harness ─────────────────────────────────────────────────────────────────

func newTestAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	workspace := t.TempDir()
	config := Config{
		Workspace: workspace,
		Model:     "test/model",
		// A fixed system prompt keeps every assertion about the transcript
		// independent of today's date and the machine's arch.
		System: "SYSTEM",
	}
	if mutate != nil {
		mutate(&config)
	}
	agent, err := newAgent(config, completer)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent, workspace
}

// collect drains one turn's stream to close, failing rather than hanging.
func collect(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
		case <-deadline:
			t.Fatalf("turn did not finish; events so far: %v", kinds(collected))
			return nil
		}
	}
}

func kinds(events []Event) []EventKind {
	out := make([]EventKind, len(events))
	for i, event := range events {
		out[i] = event.Kind
	}
	return out
}

func lastMessage(a *Agent) ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.messages[len(a.messages)-1]
}

func messageText(message ai.Message) string {
	var out strings.Builder
	for _, part := range message.Content {
		out.WriteString(part.Text)
	}
	return out.String()
}

// transcriptRoles is the live transcript's roles in order. It is not named
// `roles` because the package now imports internal/roles, and a package-level
// name may not shadow a file's import.
func transcriptRoles(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.messages))
	for i, message := range a.messages {
		out[i] = message.Role
	}
	return out
}

// ── (a) deltas stream in order and the final text is their concatenation ────

func TestSubmitStreamsDeltasInOrder(t *testing.T) {
	chunks := []string{"the ", "tokenizer ", "is fine"}
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			for _, chunk := range chunks {
				provider.Emit(ctx, provider.StreamDelta, chunk)
			}
			return textResponse(strings.Join(chunks, "")), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "how is the tokenizer?")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	var streamed []string
	for _, event := range collected {
		if event.Kind == EventTextDelta {
			streamed = append(streamed, event.Text)
		}
	}
	if strings.Join(streamed, "|") != strings.Join(chunks, "|") {
		t.Fatalf("deltas = %q, want %q", streamed, chunks)
	}
	final := collected[len(collected)-1]
	if final.Kind != EventTurnDone {
		t.Fatalf("last event = %v, want EventTurnDone", final.Kind)
	}
	if final.Usage.Input != 10 || final.Usage.Output != 5 || final.Usage.Turns != 1 {
		t.Fatalf("turn usage = %+v, want input 10 / output 5 / turns 1", final.Usage)
	}
	if final.Usage.Duration <= 0 {
		t.Fatalf("turn usage carries no duration: %+v", final.Usage)
	}
	if got := messageText(lastMessage(agent)); got != strings.Join(chunks, "") {
		t.Fatalf("recorded reply = %q, want the concatenated deltas", got)
	}
	if session := agent.Usage(); session.Input != 10 || session.Output != 5 || session.Turns != 1 {
		t.Fatalf("session usage = %+v, want it to match the one turn", session)
	}
}

// ── (b) a tool round executes and its result is appended ────────────────────

func TestSubmitRunsToolAndAppendsResult(t *testing.T) {
	var workspace string
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "read", `{"path":"note.txt"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, ws := newTestAgent(t, completer, nil)
	workspace = ws
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello from disk\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	collected := collect(t, mustSubmit(t, agent, "read note.txt"))

	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant", "tool", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript roles = %v, want %v", got, want)
	}
	agent.mu.Lock()
	toolMessage := agent.messages[3]
	agent.mu.Unlock()
	if toolMessage.ToolCallID != "call-1" {
		t.Fatalf("tool result id = %q, want call-1", toolMessage.ToolCallID)
	}
	if !strings.Contains(messageText(toolMessage), "hello from disk") {
		t.Fatalf("tool result = %q, want the file's content", messageText(toolMessage))
	}

	// The second request must carry the whole round: append-only transcript.
	second := completer.request(1)
	if len(second) != 4 || second[2].Role != "assistant" || second[3].Role != "tool" {
		t.Fatalf("second request = %v roles, want system/user/assistant/tool", rolesOf(second))
	}

	var begin, end *Event
	for i := range collected {
		switch collected[i].Kind {
		case EventToolBegin:
			begin = &collected[i]
		case EventToolEnd:
			end = &collected[i]
		case EventToolFailed:
			t.Fatalf("tool failed: %s", collected[i].Hint)
		}
	}
	if begin == nil || begin.Tool != "read" || begin.Hint != "read note.txt" {
		t.Fatalf("tool begin = %+v, want read/note.txt gloss", begin)
	}
	if end == nil || end.Tool != "read" {
		t.Fatalf("tool end = %+v, want read", end)
	}

	// The detail a surface expands a row with: the arguments on the begin, the
	// result on the end.
	if begin.Args != `{"path":"note.txt"}` {
		t.Fatalf("begin Args = %q, want the call's arguments", begin.Args)
	}
	if begin.Output != "" {
		t.Fatalf("begin Output = %q, want empty — the call has not run yet", begin.Output)
	}
	if end.Args != `{"path":"note.txt"}` {
		t.Fatalf("end Args = %q, want the call's arguments", end.Args)
	}
	if !strings.Contains(end.Output, "hello from disk") {
		t.Fatalf("end Output = %q, want the tool's result text", end.Output)
	}
	// The event's copy is capped; the transcript's is the whole thing. They
	// agree here because the result is short.
	if end.Output != messageText(toolMessage) {
		t.Fatalf("end Output = %q, want the verbatim result %q", end.Output, messageText(toolMessage))
	}
}

// Event.Output is a display copy, so an oversized result is cut at the cap and
// says how much was left behind — the wire result the model reads stays whole.
func TestToolOutputIsCappedWithAByteCount(t *testing.T) {
	const overflow = 500
	long := strings.Repeat("x", outputLimit+overflow)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.tools = append(agent.tools, bare.Tool{
		Name:        "bulk",
		Description: "returns more than a screen",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return long, false, nil
		},
	})

	hub := newEventHub()
	events := hub.subscribe()
	call := ai.ToolCall{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "bulk", Arguments: "{\n  \"lines\" : 4500\n}"},
	}
	var results []toolResult
	go func() {
		results = agent.runTools(context.Background(), []ai.ToolCall{call}, hub)
		hub.close()
	}()
	collected := collect(t, events)

	var begin, end *Event
	for i := range collected {
		switch collected[i].Kind {
		case EventToolBegin:
			begin = &collected[i]
		case EventToolEnd:
			end = &collected[i]
		case EventToolFailed:
			t.Fatalf("tool failed: %s", collected[i].Hint)
		}
	}
	if begin == nil || end == nil {
		t.Fatalf("events = %v, want a begin and an end", kinds(collected))
	}

	// Whitespace in the model's JSON is compacted away: Args is one line.
	if begin.Args != `{"lines":4500}` {
		t.Fatalf("begin Args = %q, want the compacted arguments", begin.Args)
	}

	want := strings.Repeat("x", outputLimit) + "… (500 more bytes)"
	if end.Output != want {
		t.Fatalf("end Output = %q (%d bytes), want the first %d bytes plus the byte-count suffix",
			clip(end.Output, 80), len(end.Output), outputLimit)
	}
	// The result the model reads is untouched: Output caps the display copy,
	// never the wire.
	if len(results) != 1 || results[0].text != long {
		t.Fatalf("tool result is %d bytes, want the full %d — the cap is display-only",
			len(results[0].text), len(long))
	}
}

// Event.Args carries a WHOLE edit payload, because the surface computes that
// edit's "+N −M" and its unified diff from the old/new strings and from nothing
// else — the tool's own result is one sentence saying it worked. A cap that cut
// a real edit off mid-string would not shorten the diff; it would produce the
// wrong number (docs/CHAT-V3.md D11, rendered in internal/tui3).
func TestToolArgsCarryAWholeEditPayload(t *testing.T) {
	// A replacement the size of a function: far past the old 400-byte cap, and
	// an ordinary thing for a model to send.
	block := strings.Repeat("\tfmt.Println(\"the quick brown fox\")\n", 40)
	arguments, err := json.Marshal(map[string]any{
		"path": "internal/session/loop.go",
		"edits": []map[string]string{{
			"oldText": block,
			"newText": block + "\tfmt.Println(\"and one more\")\n",
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(arguments) <= 400 {
		t.Fatalf("the fixture is %d bytes, which the old cap would not have cut", len(arguments))
	}

	got := argsText(ai.ToolCall{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "edit", Arguments: string(arguments)},
	})
	if got != string(arguments) {
		t.Fatalf("Args is %d bytes, want the whole %d-byte payload", len(got), len(arguments))
	}

	// It is still a cap, and it is still display-only: past the limit the copy
	// is cut and marked, and the wire arguments are never touched.
	huge := `{"path":"f.go","edits":[{"oldText":"` + strings.Repeat("x", argsLimit) + `"}]}`
	capped := argsText(ai.ToolCall{
		ID: "c2", Type: "function",
		Function: ai.ToolCallFunction{Name: "edit", Arguments: huge},
	})
	if len(capped) >= len(huge) || !strings.HasSuffix(capped, "…") {
		t.Fatalf("an oversized payload was not capped: %d bytes, ends %q",
			len(capped), lastRunes(capped, 8))
	}
}

func lastRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// ── (c) Interrupt keeps the partial reply ───────────────────────────────────

func TestInterruptKeepsPartialReply(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "half an ans")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "explain the planner")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never streamed")
	}
	agent.Interrupt()

	collected := collect(t, events)
	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("interrupted turn ended with %v, want EventTurnDone", last.Kind)
	}
	message := lastMessage(agent)
	if message.Role != "assistant" || messageText(message) != "half an ans" {
		t.Fatalf("kept message = %s/%q, want the streamed partial", message.Role, messageText(message))
	}
	// A second Submit must be accepted: the turn is over, not wedged.
	if _, err := agent.Submit(context.Background(), "never mind"); err != nil {
		t.Fatalf("Submit after interrupt: %v", err)
	}
}

// ── (d) steering rides the in-flight turn ───────────────────────────────────

func TestSubmitDuringTurnSteersIt(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("folded it in"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "list the workspace")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}

	steered, err := agent.Submit(context.Background(), "also count the files")
	if err != nil {
		t.Fatalf("steering Submit: %v", err)
	}
	close(release)
	collect(t, events)
	// The steering caller holds a live channel onto the same turn; drain it so
	// the turn's pump is not left parked on a send.
	collect(t, steered)

	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2", completer.requests())
	}
	second := completer.request(1)
	found := false
	for _, message := range second {
		if message.Role == "user" && messageText(message) == "also count the files" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steering message missing from the next request: %v", rolesOf(second))
	}
	// It lands AFTER the tool result — the step boundary, not mid-batch.
	if got, want := rolesOf(second), []string{"system", "user", "assistant", "tool", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v", got, want)
	}
}

// ── (d2) the steering Submit gets a live stream of its own ──────────────────

// A surface that calls Submit per message reads the returned channel as "this
// turn". An already-closed channel would read as "the turn ended", which is
// the opposite of what a steering Submit just did — so the steering caller
// subscribes to the in-flight turn's hub and sees the rest of it.
func TestSteeringSubmitStreamsTheTurn(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "folded it in")
			return textResponse("folded it in"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first := mustSubmit(t, agent, "list the workspace")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}

	// B subscribes while A's turn is blocked inside step 0, so everything the
	// turn emits from here on must reach B.
	second, err := agent.Submit(context.Background(), "also count the files")
	if err != nil {
		t.Fatalf("steering Submit: %v", err)
	}
	close(release)

	collectedA := collect(t, first)
	collectedB := collect(t, second)

	if len(collectedB) == 0 {
		t.Fatal("steering channel carried no events; it must stream the rest of the turn")
	}
	if last := collectedB[len(collectedB)-1]; last.Kind != EventTurnDone {
		t.Fatalf("steering channel ended with %v, want EventTurnDone", last.Kind)
	}
	// Both channels closed (collect only returns on close) and both saw the
	// work that happened after B subscribed: the tool round and the reply.
	for name, collected := range map[string][]Event{"A": collectedA, "B": collectedB} {
		var sawToolBegin, sawDelta bool
		for _, event := range collected {
			switch event.Kind {
			case EventToolBegin:
				sawToolBegin = event.Tool == "ls"
			case EventTextDelta:
				sawDelta = event.Text == "folded it in"
			case EventToolFailed:
				t.Fatalf("%s: tool failed: %s", name, event.Hint)
			}
		}
		if !sawToolBegin || !sawDelta {
			t.Fatalf("%s events = %v, want the ls round and the reply delta", name, kinds(collected))
		}
	}

	// And the steering text still landed in the turn's next request.
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2", completer.requests())
	}
	if got, want := rolesOf(completer.request(1)), []string{"system", "user", "assistant", "tool", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v", got, want)
	}
	if got := messageText(completer.request(1)[4]); got != "also count the files" {
		t.Fatalf("steering message = %q, want the text B submitted", got)
	}
}

// Two Submits racing inside one turn both steer and both stream.
func TestConcurrentSteeringSubmitsAllStream(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return textResponse("answered"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first := mustSubmit(t, agent, "start")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}

	var wg sync.WaitGroup
	streams := make([]<-chan Event, 2)
	errs := make([]error, 2)
	for i, text := range []string{"steer one", "steer two"} {
		wg.Add(1)
		go func(index int, message string) {
			defer wg.Done()
			streams[index], errs[index] = agent.Submit(context.Background(), message)
		}(i, text)
	}
	wg.Wait()
	close(release)

	collect(t, first)
	for index, stream := range streams {
		if errs[index] != nil {
			t.Fatalf("racing Submit %d: %v", index, errs[index])
		}
		collected := collect(t, stream)
		if len(collected) == 0 || collected[len(collected)-1].Kind != EventTurnDone {
			t.Fatalf("racing Submit %d events = %v, want a stream ending in EventTurnDone", index, kinds(collected))
		}
	}

	// Both texts are queued as steering; the turn ended before a second step,
	// so they wait in the transcript for the next one.
	agent.mu.Lock()
	pending := append([]string(nil), agent.steering...)
	agent.mu.Unlock()
	transcript := strings.Join(pending, "|")
	agent.mu.Lock()
	for _, message := range agent.messages {
		if message.Role == "user" {
			transcript += "|" + messageText(message)
		}
	}
	agent.mu.Unlock()
	for _, want := range []string{"steer one", "steer two"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("steering text %q was dropped; saw %q", want, transcript)
		}
	}
}

// ── (e) compaction ──────────────────────────────────────────────────────────

func TestCompactionRebuildsTranscript(t *testing.T) {
	long := strings.Repeat("context that will not fit. ", 40)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role:    "assistant",
				Content: []ai.ContentPart{{Type: "text", Text: "short reply"}},
			}}}}, nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			// The summarizer call: system + the flattened discarded prefix,
			// and no tools.
			if len(messages) != 2 || messages[0].Role != "system" {
				t.Errorf("summarizer request = %v, want system+user", rolesOf(messages))
			}
			if !strings.Contains(messageText(messages[1]), "context that will not fit") {
				t.Errorf("summarizer did not receive the discarded prefix")
			}
			return textResponse("## Goal\nfit the window"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// A 200-token window puts the threshold below zero and the keep-recent
		// tail at 50 tokens, so one long message is already too much.
		config.ContextWindow = 200
		config.CompactEnabled = true
	})

	collected := collect(t, mustSubmit(t, agent, long))

	var compacted *Event
	for i := range collected {
		if collected[i].Kind == EventCompacted {
			compacted = &collected[i]
		}
	}
	if compacted == nil {
		t.Fatalf("no EventCompacted; events = %v", kinds(collected))
	}
	if !strings.HasPrefix(compacted.Hint, "compacted from ~") {
		t.Fatalf("compaction hint = %q", compacted.Hint)
	}

	agent.mu.Lock()
	messages := agent.messages
	agent.mu.Unlock()
	if got, want := rolesOf(messages), []string{"system", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("compacted transcript = %v, want system/summary/tail", got)
	}
	if messageText(messages[0]) != "SYSTEM" {
		t.Fatalf("system message was rewritten: %q", messageText(messages[0]))
	}
	summary := messageText(messages[1])
	if !strings.Contains(summary, "[context compacted]") || !strings.Contains(summary, "fit the window") {
		t.Fatalf("summary note = %q", summary)
	}
	if strings.Contains(summary, "context that will not fit") {
		t.Fatal("the summarized prefix survived verbatim")
	}
	if messageText(messages[2]) != "short reply" {
		t.Fatalf("kept tail = %q, want the last assistant message", messageText(messages[2]))
	}

	// The summary call is the session's cost, not the turn's: the person asked
	// one question and must not read it as three times the price of its
	// neighbours. The first step's response carried no usage at all, so every
	// token here is the summarizer's.
	final := collected[len(collected)-1]
	if final.Kind != EventTurnDone {
		t.Fatalf("last event = %v, want EventTurnDone", final.Kind)
	}
	if final.Usage.Input != 0 || final.Usage.Output != 0 {
		t.Fatalf("turn usage = %+v, want the summarizer's tokens kept off the turn", final.Usage)
	}
	if session := agent.Usage(); session.Input != 10 || session.Output != 5 {
		t.Fatalf("session usage = %+v, want the summarizer's 10/5 folded in", session)
	}
	if session := agent.Usage(); session.Turns != 0 {
		t.Fatalf("session Turns = %d, want the summary not counted as a turn", session.Turns)
	}
}

// A compaction pass with a mid-batch cut must not leave the kept tail starting
// on a tool result: its assistant tool_calls message went into the summary, and
// an orphaned result is a 400 on this request and every request after it.
func TestCompactionDropsOrphanedToolResultsFromKeptTail(t *testing.T) {
	var agent *Agent
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// The summarizer runs with the agent lock released. This is the
			// in-flight batch landing while it works: the results of the very
			// call the cut is about to summarize away.
			agent.record(ai.Message{
				Role:       "tool",
				ToolCallID: "c1",
				Content:    []ai.ContentPart{{Type: "text", Text: "the file"}},
			})
			return textResponse("## Goal\nfit the window"), nil
		},
	}}
	agent, _ = newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = 200
		config.CompactEnabled = true
	})

	// A transcript whose whole tail is one over-budget assistant message with
	// tool calls: the cut lands at the end, so everything appended during the
	// summary becomes the kept tail.
	agent.mu.Lock()
	agent.messages = append(agent.messages, ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("looking at the file. ", 40)}},
		ToolCalls: []ai.ToolCall{{
			ID: "c1", Type: "function",
			Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
		}},
	})
	agent.mu.Unlock()

	compacted, err := agent.compact(context.Background(), nil)
	if err != nil || !compacted {
		t.Fatalf("compact = %v, %v; want a pass that ran", compacted, err)
	}

	if got, want := transcriptRoles(agent), []string{"system", "user"}; !equalStrings(got, want) {
		t.Fatalf("compacted transcript = %v, want %v — the orphaned tool result must be dropped", got, want)
	}
}

func TestCompactBelowTheFloorReportsNothingToCompact(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "hello"))

	// A no-op reported as a success makes /compact lie. The sentinel is what
	// lets the surface say "nothing to compact" in the person's words.
	if err := agent.Compact(context.Background()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("Compact = %v, want ErrNothingToCompact", err)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want 1 — a short transcript must not summarize", got)
	}
}

// The threshold must stay above the verbatim tail at every window size. Below
// ~21.8k the 16k floor put it underneath, and every step compacted forever.
func TestCompactThresholdStaysAboveTheKeptTail(t *testing.T) {
	for _, window := range []int{200, 1000, 4096, 8192, 16384, 21000, 21800, 32000, 128_000, 200_000} {
		agent := &Agent{config: Config{ContextWindow: window}}
		threshold, keep := agent.compactThreshold(), agent.keepRecentTokens()
		if threshold <= keep {
			t.Fatalf("window %d: threshold %d <= keep-recent %d — compaction would fire forever",
				window, threshold, keep)
		}
	}
}

// The provider's context figure describes the request that was SENT. One huge
// tool result appended after it must still be able to trip the threshold.
func TestEstimateCountsMessagesAppendedAfterTheProviderFigure(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = 128_000
	})
	agent.mu.Lock()
	agent.contextTokens = 900 // the honest figure for the request already sent
	before := agent.estimateTokensLocked()
	agent.messages = append(agent.messages, ai.Message{
		Role:       "tool",
		ToolCallID: "c1",
		Content:    []ai.ContentPart{{Type: "text", Text: strings.Repeat("x", 600_000)}},
	})
	after := agent.estimateTokensLocked()
	agent.mu.Unlock()

	if before != 900 {
		t.Fatalf("estimate = %d, want the provider figure as the floor", before)
	}
	if after <= agent.compactThreshold() {
		t.Fatalf("estimate after a 600KB tool result = %d, want it over the threshold %d",
			after, agent.compactThreshold())
	}
}

// ── interruption, retries and errors ────────────────────────────────────────

// An interrupt arriving during a tool batch must not record the step's text a
// second time: the assistant message carrying it is already in the transcript.
func TestInterruptDuringToolBatchDoesNotDuplicateText(t *testing.T) {
	running := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "reading the file first")
			return toolResponseWithText("c1", "block", "{}", "reading the file first"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools, blockingTool("block", running))

	events := mustSubmit(t, agent, "look at it")
	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("the tool never started")
	}
	agent.Interrupt()
	collect(t, events)

	if got := countMessages(agent, "reading the file first"); got != 1 {
		t.Fatalf("the step's text appears %d times in the transcript, want exactly 1", got)
	}
}

// Each retry attempt streams the reply from the start, so an interrupt during
// attempt 2 must keep attempt 2's text — not the two attempts concatenated.
func TestRetryDoesNotConcatenatePartialAttempts(t *testing.T) {
	second := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "first attempt")
			return nil, errors.New("provider returned error: overloaded")
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "second attempt")
			close(second)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	partial := &partialBuffer{}
	ctx, cancel := context.WithCancel(provider.WithStreamObserver(context.Background(),
		func(event provider.StreamEvent) {
			if event.Kind == provider.StreamDelta {
				partial.write(event.Delta)
			}
		}))
	defer cancel()
	go func() {
		select {
		case <-second:
			cancel()
		case <-time.After(30 * time.Second):
		}
	}()

	if _, err := agent.completeWithRetry(ctx, "test/model", partial, &warmBatch{}); err == nil {
		t.Fatal("completeWithRetry returned no error after the cancel")
	}
	if got := partial.take(); got != "second attempt" {
		t.Fatalf("partial = %q, want only the attempt that was interrupted", got)
	}
}

// A permanent provider error mid-stream keeps what was streamed and still
// seals the turn: the person watched that text arrive and the surface is owed
// the turn's duration.
func TestPermanentErrorKeepsPartialAndSealsTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "half an ans")
			return nil, errors.New("insufficient_quota: out of budget")
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collected := collect(t, mustSubmit(t, agent, "explain the planner"))
	last := collected[len(collected)-1]
	if last.Kind != EventError {
		t.Fatalf("last event = %v, want EventError", last.Kind)
	}
	if last.Usage.Duration <= 0 {
		t.Fatalf("failed turn carries no duration: %+v", last.Usage)
	}
	message := lastMessage(agent)
	if message.Role != "assistant" || messageText(message) != "half an ans" {
		t.Fatalf("kept message = %s/%q, want the streamed partial", message.Role, messageText(message))
	}
}

// Steering typed just before an interrupt is never answered by the turn it was
// meant for, so it must at least reach the transcript BEFORE the next thing the
// person types — not after it, answering the older question second.
func TestSteeringSurvivesAnInterruptAndStaysInOrder(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "working")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("both noted"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "start")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never streamed")
	}
	steered, err := agent.Submit(context.Background(), "also check the tests")
	if err != nil {
		t.Fatalf("steering Submit: %v", err)
	}
	agent.Interrupt()
	collect(t, events)
	collect(t, steered)

	agent.mu.Lock()
	pending := len(agent.steering)
	agent.mu.Unlock()
	if pending != 0 {
		t.Fatalf("%d steering messages still queued after the turn ended", pending)
	}

	collect(t, mustSubmit(t, agent, "and now the docs"))
	request := completer.request(1)
	steerAt, nextAt := -1, -1
	for index, message := range request {
		switch messageText(message) {
		case "also check the tests":
			steerAt = index
		case "and now the docs":
			nextAt = index
		}
	}
	if steerAt < 0 || nextAt < 0 {
		t.Fatalf("request = %v, want both the steering and the next message", rolesOf(request))
	}
	if steerAt > nextAt {
		t.Fatalf("steering landed at %d, after the next Submit at %d — the person's order is reversed",
			steerAt, nextAt)
	}
}

// ── the model latch ─────────────────────────────────────────────────────────

// SetModel's contract is that a turn in flight finishes on the model it
// started on. The swap lands at the next turn, not at the next step.
func TestSetModelAppliesFromTheNextTurn(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("on the new one"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "list it")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
	agent.SetModel("test/other")
	close(release)
	collect(t, events)

	if got := completer.model(1); got != "test/model" {
		t.Fatalf("step 2 of the turn rode %q, want the model the turn started on", got)
	}
	collect(t, mustSubmit(t, agent, "again"))
	if got := completer.model(2); got != "test/other" {
		t.Fatalf("the next turn rode %q, want test/other", got)
	}
}

// ── faulted tools ───────────────────────────────────────────────────────────

// A tool that panics must reach the model as a failure. Recorded as the zero
// result it reads as a tool that ran and returned nothing — the one story
// about the fault that is not true.
func TestPanickingToolIsRecordedAsAFailure(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.tools = append(agent.tools, bare.Tool{
		Name:        "boom",
		Description: "panics",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			panic("the tool exploded")
		},
	})

	hub := newEventHub()
	defer hub.close()
	results := agent.runTools(context.Background(), []ai.ToolCall{{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "boom", Arguments: "{}"},
	}}, hub)

	if len(results) != 1 || !results[0].isError {
		t.Fatalf("result = %+v, want an error result", results)
	}
	if !strings.Contains(results[0].text, "panicked") {
		t.Fatalf("result text = %q, want it to say the tool panicked", results[0].text)
	}
}

// ── glosses and construction ────────────────────────────────────────────────

func TestFindGlossIsThePattern(t *testing.T) {
	got := gloss(ai.ToolCall{Function: ai.ToolCallFunction{
		Name:      "find",
		Arguments: `{"pattern":"**/*.go","path":"internal"}`,
	}})
	if got != "find **/*.go" {
		t.Fatalf("gloss = %q, want the glob find actually searches by", got)
	}
}

// A malformed schema must fail at construction rather than ride the wire as
// Parameters:nil — a tool the model is told takes no arguments.
func TestToolDefinitionsRejectAMalformedSchema(t *testing.T) {
	_, err := toolDefinitions([]bare.Tool{{
		Name:   "bent",
		Schema: json.RawMessage(`{"type":"object",`),
	}})
	if err == nil {
		t.Fatal("toolDefinitions accepted a schema that does not parse")
	}
	if !strings.Contains(err.Error(), "bent") {
		t.Fatalf("error = %v, want it to name the tool", err)
	}
	if _, err := toolDefinitions(bare.AllTools(t.TempDir())); err != nil {
		t.Fatalf("the shipped belt must build: %v", err)
	}
}

// AGENTS.md is cut at a byte limit; the cut must not split a rune, or a
// character the person never wrote arrives in the model's house rules.
func TestAgentsFileTruncationKeepsRunesWhole(t *testing.T) {
	workspace := t.TempDir()
	// The é straddles the limit: its first byte is the last byte inside it.
	content := strings.Repeat("a", agentsFileLimit-1) + "é" + strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(workspace, agentsFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got, truncated := readAgentsFile(workspace)
	if !truncated {
		t.Fatal("an over-long AGENTS.md was not reported as truncated")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated AGENTS.md is not valid UTF-8 (ends %q)", got[max(0, len(got)-4):])
	}
	if got != strings.Repeat("a", agentsFileLimit-1) {
		t.Fatalf("truncation kept %d bytes, want the cut backed off to the rune boundary", len(got))
	}
	if strings.Contains(renderSystem(workspace), "�") {
		t.Fatal("the rendered prompt carries a replacement character")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// blockingTool is a belt tool that reports it started and then waits for the
// turn's context — a tool batch a test can interrupt in the middle of.
func blockingTool(name string, started chan struct{}) bare.Tool {
	var once sync.Once
	return bare.Tool{
		Name:        name,
		Description: "blocks until the context ends",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ json.RawMessage) (string, bool, error) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return "cancelled", true, nil
		},
	}
}

func countMessages(a *Agent, text string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, message := range a.messages {
		if messageText(message) == text {
			count++
		}
	}
	return count
}

func mustSubmit(t *testing.T, agent *Agent, text string) <-chan Event {
	t.Helper()
	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	return events
}

func rolesOf(messages []ai.Message) []string {
	out := make([]string, len(messages))
	for i, message := range messages {
		out[i] = message.Role
	}
	return out
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// ── parallel tool execution ────────────────────────────────────────────────

// TestToolBatchRunsInParallel is the wall-clock contract for the batch
// executor: two 3-second sleeps issued in ONE response must overlap, so the
// batch costs ~3s and not ~6s. The margin is 2s against the sequential
// reading — scheduler noise on a loaded box cannot cross it.
func TestToolBatchRunsInParallel(t *testing.T) {
	twoSleeps := &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant",
			ToolCalls: []ai.ToolCall{
				{ID: "a", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 3 && echo a"}`}},
				{ID: "b", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 3 && echo b"}`}},
			},
		}}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) { return twoSleeps, nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	defer agent.Close()

	started := time.Now()
	events, err := agent.Submit(context.Background(), "run both")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	begins := 0
	for event := range events {
		if event.Kind == EventToolBegin {
			begins++
		}
		if event.Kind == EventError {
			t.Fatalf("turn errored: %v", event.Err)
		}
	}
	elapsed := time.Since(started)
	if begins != 2 {
		t.Fatalf("begins = %d, want 2", begins)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("two 3s sleeps took %v — the batch ran sequentially", elapsed)
	}
}

// ── reasoning text on the wire ──────────────────────────────────────────────

// fakeHTTP turns a handler into an http.Client, so a session can run against a
// REAL provider adapter fed a scripted event stream. The scripted completer
// below it proves the loop; this proves the wiring underneath the loop.
func fakeHTTP(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func eventStreamOf(payloads ...string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, payload := range payloads {
			_, _ = writer.Write([]byte("data: " + payload + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
}

// A reasoning model's working reaches the surface as EventReasoning, in order,
// behind the one EventThinking that opened the run — and never reaches the
// transcript, where it would come back to the model as something it had said.
//
// This runs against the real adapter over a scripted event stream, so what is
// pinned is the whole path: the wire's "reasoning_content", the provider's
// StreamReasoning, the session's EventReasoning.
func TestReasoningTextArrivesAsEventReasoningAndStaysOutOfTheTranscript(t *testing.T) {
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "test/model",
		HTTPClient: fakeHTTP(eventStreamOf(
			`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"the note is "}}]}`,
			`{"choices":[{"index":0,"delta":{"reasoning_content":"one line long"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"It is one line."},"finish_reason":"stop"}]}`,
		)),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, nil)

	collected := collect(t, mustSubmit(t, agent, "how long is the note?"))

	var reasoning []string
	thinkingAt, firstReasoningAt := -1, -1
	for index, event := range collected {
		switch event.Kind {
		case EventThinking:
			if thinkingAt < 0 {
				thinkingAt = index
			}
		case EventReasoning:
			if firstReasoningAt < 0 {
				firstReasoningAt = index
			}
			reasoning = append(reasoning, event.Text)
		}
	}
	if strings.Join(reasoning, "|") != "the note is |one line long" {
		t.Fatalf("reasoning events = %q, want the chunks whole and in order", reasoning)
	}
	if thinkingAt < 0 || firstReasoningAt < thinkingAt {
		t.Fatalf("thinking at %d, first reasoning at %d — the boundary must open the run",
			thinkingAt, firstReasoningAt)
	}

	// The answer is unaffected: it streamed as text and it is what was recorded.
	var answer []string
	for _, event := range collected {
		if event.Kind == EventTextDelta {
			answer = append(answer, event.Text)
		}
	}
	if strings.Join(answer, "") != "It is one line." {
		t.Fatalf("text deltas = %q, want the answer alone", answer)
	}
	recorded := messageText(lastMessage(agent))
	if recorded != "It is one line." {
		t.Fatalf("recorded reply = %q, want the answer with no reasoning in it", recorded)
	}
	for _, message := range messagesOf(agent) {
		if strings.Contains(messageText(message), "one line long") {
			t.Fatalf("the model's working reached the transcript as a %s message", message.Role)
		}
	}
}

// An interrupted step keeps what the model SAID, never what it was thinking:
// reasoning is not written to the partial buffer.
func TestInterruptDoesNotKeepReasoningAsTheReply(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamThinking, "")
			provider.Emit(ctx, provider.StreamReasoning, "I should check the tests first")
			provider.Emit(ctx, provider.StreamDelta, "Checking")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "have a look")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never streamed")
	}
	agent.Interrupt()
	collect(t, events)

	if got := messageText(lastMessage(agent)); got != "Checking" {
		t.Fatalf("kept message = %q, want only the streamed answer", got)
	}
}

// ── the early start: read-only tools only ───────────────────────────────────

// countingTool is a belt tool that reports every execution on runs and blocks
// until release is closed (a nil release does not block).
func countingTool(name string, runs chan<- string, release <-chan struct{}) bare.Tool {
	return bare.Tool{
		Name:        name,
		Description: "records that it ran",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ json.RawMessage) (string, bool, error) {
			select {
			case runs <- name:
			default:
			}
			if release != nil {
				select {
				case <-release:
				case <-ctx.Done():
					return "cancelled", true, nil
				}
			}
			return name + " ran", false, nil
		},
	}
}

func callsResponse(calls ...ai.ToolCall) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:      "assistant",
			Content:   []ai.ContentPart{{Type: "text", Text: ""}},
			ToolCalls: calls,
		}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

func emitReady(t *testing.T, ctx context.Context, call ai.ToolCall) {
	t.Helper()
	payload, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal %v: %v", call, err)
	}
	provider.Emit(ctx, provider.StreamToolCallReady, string(payload))
}

// A read announced mid-stream RUNS mid-stream; the write announced beside it
// does not. That asymmetry is the safety law in loop.go, and this is the test
// that says it out loud: the read is proved started while the response is still
// being produced, the write is proved not started at the same instant.
func TestReadStartsDuringTheStreamAndTheWriteWaits(t *testing.T) {
	readCall := ai.ToolCall{ID: "c-read", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}
	writeCall := ai.ToolCall{ID: "c-write", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"out.txt","content":"x"}`}}

	reads := make(chan string, 4)
	writes := make(chan string, 4)
	releaseRead := make(chan struct{})

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The write is announced FIRST, so an implementation that started
			// calls early without reading the law would have started it first
			// too — and the read below proves the early path was live.
			emitReady(t, ctx, writeCall)
			emitReady(t, ctx, readCall)

			select {
			case <-reads:
			case <-time.After(10 * time.Second):
				t.Error("the read never started while the response was streaming")
			}
			// The read has run. If early execution ignored the law, the write —
			// announced first — would be running too.
			select {
			case <-writes:
				t.Error("the write started before the response completed")
			case <-time.After(250 * time.Millisecond):
			}
			close(releaseRead)
			return callsResponse(readCall, writeCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}

	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = []bare.Tool{
		countingTool("read", reads, releaseRead),
		countingTool("write", writes, nil),
	}

	collected := collect(t, mustSubmit(t, agent, "read the note and write the summary"))

	// THE JOURNAL IS UNCHANGED. Results append in call order, paired by id,
	// after the response completed — an early start is a warm result, not a
	// different transcript.
	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant", "tool", "tool", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript roles = %v, want %v", got, want)
	}
	recorded := messagesOf(agent)
	if recorded[3].ToolCallID != "c-read" || messageText(recorded[3]) != "read ran" {
		t.Fatalf("first tool message = %+v, want the read's result", recorded[3])
	}
	if recorded[4].ToolCallID != "c-write" || messageText(recorded[4]) != "write ran" {
		t.Fatalf("second tool message = %+v, want the write's result", recorded[4])
	}

	// And so is the narrative: begins for the whole batch, in call order.
	var begun []string
	for _, event := range collected {
		switch event.Kind {
		case EventToolBegin:
			begun = append(begun, event.Tool)
		case EventToolFailed:
			t.Fatalf("tool failed: %s", event.Hint)
		}
	}
	if !equalStrings(begun, []string{"read", "write"}) {
		t.Fatalf("tool begins = %v, want read then write in call order", begun)
	}

	// Each call ran exactly once. The warm result is claimed by id, so the batch
	// must not have started the read a second time.
	if len(reads) != 0 {
		t.Fatalf("the read ran %d extra times — an early start must not double-run it", len(reads))
	}
	select {
	case <-writes:
	default:
		t.Fatal("the write never ran at all")
	}
	if len(writes) != 0 {
		t.Fatalf("the write ran %d extra times", len(writes))
	}
}

// A retryable failure after an early read: the retry is a NEW response, so the
// read runs again — harmless, it is idempotent, which is the whole reason it was
// allowed to start early — and the write, which never started early, runs
// exactly once.
func TestRetryRerunsTheEarlyReadAndNeverTheWrite(t *testing.T) {
	readCall := ai.ToolCall{ID: "c-read", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}
	writeCall := ai.ToolCall{ID: "c-write", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"out.txt","content":"x"}`}}

	reads := make(chan string, 8)
	writes := make(chan string, 8)

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, readCall)
			emitReady(t, ctx, writeCall)
			select {
			case <-reads:
			case <-time.After(10 * time.Second):
				t.Error("the read never started during the failing attempt")
			}
			// The stream dies after the read already ran.
			return nil, errors.New("provider returned error: 502 bad gateway")
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, readCall)
			emitReady(t, ctx, writeCall)
			return callsResponse(readCall, writeCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}

	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = []bare.Tool{
		countingTool("read", reads, nil),
		countingTool("write", writes, nil),
	}

	collect(t, mustSubmit(t, agent, "read the note and write the summary"))

	// The read ran once more, on the attempt that succeeded. Twice in total is
	// the accepted cost of the law, and the file is the same file both times.
	if got := len(reads); got != 1 {
		t.Fatalf("the read ran %d times after the retry, want exactly one more", got+1)
	}
	if got := len(writes); got != 1 {
		t.Fatalf("the write ran %d times, want exactly once — a retried mutation is the thing the law forbids", got)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant", "tool", "tool", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript roles = %v, want the one batch recorded once: %v", got, want)
	}
}

// The law, enumerated. A tool joins this set only by being idempotent, so the
// membership is asserted rather than inferred — the day somebody adds a verb to
// the belt, this test is the conversation about whether it may start early.
func TestOnlyIdempotentToolsMayStartEarly(t *testing.T) {
	for _, name := range []string{"read", "grep", "find", "ls"} {
		if !earlyTools[name] {
			t.Fatalf("%q is read-only and must be allowed to start early", name)
		}
	}
	for _, name := range []string{"write", "edit", "bash", "task", "change", "stop", "todo", ""} {
		if earlyTools[name] {
			t.Fatalf("%q may mutate and must never start early", name)
		}
	}
	// Every name in the set is a tool the belt actually carries: a typo here
	// would be a permission granted to nothing, which is the kind of dead law
	// that reads as coverage.
	belt := make(map[string]bool)
	for _, tool := range bare.AllTools(t.TempDir()) {
		belt[tool.Name] = true
	}
	for name := range earlyTools {
		if !belt[name] {
			t.Fatalf("earlyTools names %q, which is not on the belt", name)
		}
	}
}

// The warm result is claimed by id AND by the instruction. The id alone is
// enough for every endpoint that exists — but the early sighting is assembled
// from stream fragments and the response's call from all of them, and if those
// ever disagreed the response is right.
func TestAWarmResultIsRefusedWhenTheCallDisagrees(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	reads := make(chan string, 4)
	agent.tools = []bare.Tool{countingTool("read", reads, nil)}

	call := ai.ToolCall{ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`}}
	warm := &warmBatch{}
	payload, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	warm.consider(context.Background(), agent, newEventHub(), string(payload))

	// Same id, different arguments: the response won the disagreement.
	changed := call
	changed.Function.Arguments = `{"path":"b.go"}`
	if got := warm.take(changed); got != nil {
		t.Fatal("a warm result was paired with an instruction the model did not send")
	}
	// And the original pairing is gone with it — the entry is claimed either
	// way, so a stale result cannot be picked up by a later call reusing the id.
	if got := warm.take(call); got != nil {
		t.Fatal("the refused entry survived to be claimed again")
	}
}

// Nothing but a read-only call on the live belt is ever started early: a
// payload that will not parse, a mutating call, and a tool this agent does not
// have are all silent refusals.
func TestConsiderRefusesEverythingOutsideTheLaw(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	runs := make(chan string, 4)
	agent.tools = []bare.Tool{countingTool("read", runs, nil)}

	warm := &warmBatch{}
	for _, payload := range []string{
		`not json`,
		`{"id":"c1","function":{"name":"write","arguments":"{}"}}`,
		`{"id":"c2","function":{"name":"grep","arguments":"{}"}}`, // read-only, but not on this belt
		`{"id":"","function":{"name":"read","arguments":"{}"}}`,   // no id to pair by
	} {
		warm.consider(context.Background(), agent, newEventHub(), payload)
	}
	warm.mu.Lock()
	started := len(warm.started)
	warm.mu.Unlock()
	if started != 0 {
		t.Fatalf("%d calls were started early, want none", started)
	}
	select {
	case name := <-runs:
		t.Fatalf("%q was executed early", name)
	default:
	}
}

func messagesOf(a *Agent) []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ai.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// A tool that panics during an EARLY start must reach the model as a failure,
// exactly as one that panics inside the batch does. Left as the zero result it
// reads as a tool that ran and returned nothing.
func TestAPanickingEarlyToolIsStillRecordedAsAFailure(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	readCall := ai.ToolCall{ID: "c-read", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, readCall)
			return callsResponse(readCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = []bare.Tool{{
		Name:        "read",
		Description: "panics",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			panic("the tool exploded")
		},
	}}

	collected := collect(t, mustSubmit(t, agent, "read the note"))

	recorded := messagesOf(agent)
	if got := messageText(recorded[3]); !strings.Contains(got, "panicked") {
		t.Fatalf("tool message = %q, want it to say the tool panicked", got)
	}
	failed := false
	for _, event := range collected {
		if event.Kind == EventToolFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("events = %v, want the batch to report a failure", kinds(collected))
	}
}

// ── (d3) the follow-up queue ────────────────────────────────────────────────

// A follow-up waits for the turn to finish and then starts one of its own —
// where steering would have landed inside the turn it was meant to follow.
func TestFollowUpStartsTheNextTurn(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return textResponse("the first answer"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the second answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first := mustSubmit(t, agent, "why is the tokenizer slow?")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}

	queued, err := agent.FollowUp("then write it up")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	close(release)

	collect(t, first)
	followed := collect(t, queued)

	if last := followed[len(followed)-1]; last.Kind != EventTurnDone {
		t.Fatalf("the follow-up's stream ended with %v, want a normal turn", last.Kind)
	}
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2 — the follow-up did not start a turn", completer.requests())
	}
	// It rode as a NEW turn: its message is the last thing in the second
	// request, after the first turn's whole exchange.
	second := completer.request(1)
	if got, want := rolesOf(second), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v", got, want)
	}
	if got := messageText(second[len(second)-1]); got != "then write it up" {
		t.Fatalf("the follow-up's turn opened with %q", got)
	}
	if got := messageText(lastMessage(agent)); got != "the second answer" {
		t.Fatalf("transcript tail = %q", got)
	}
}

// One at a time: a turn's end takes exactly one message off the queue, and the
// rest wait for the end of the turn that one started.
func TestFollowUpsDrainOneAtATime(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return textResponse("one"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("two"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("three"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first := mustSubmit(t, agent, "the question")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
	secondEvents, err := agent.FollowUp("the follow-up")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	thirdEvents, err := agent.FollowUp("and one more")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	close(release)

	collect(t, first)
	collect(t, secondEvents)
	collect(t, thirdEvents)

	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want 3", completer.requests())
	}
	// Each turn carried exactly one queued message, in the order they were
	// typed: two turns' worth of transcript, not one turn with both spliced in.
	if got := messageText(completer.request(1)[3]); got != "the follow-up" {
		t.Fatalf("second turn opened with %q", got)
	}
	if got, want := rolesOf(completer.request(2)),
		[]string{"system", "user", "assistant", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("third request roles = %v, want %v", got, want)
	}
}

// A DRAIN MUST NEVER RESURRECT A STOPPED TURN. An interrupt drops the queue:
// a stop that was followed by the session working again is not a stop.
func TestInterruptClearsTheFollowUpQueue(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "working")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "start")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never streamed")
	}
	queued, err := agent.FollowUp("and then this")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	agent.Interrupt()

	collect(t, events)
	if dropped := collect(t, queued); len(dropped) != 0 {
		t.Fatalf("the dropped follow-up streamed %v, want a closed channel", kinds(dropped))
	}
	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1 — the interrupt started another turn", completer.requests())
	}
	if countMessages(agent, "and then this") != 0 {
		t.Fatal("a dropped follow-up reached the transcript")
	}
	agent.mu.Lock()
	pending := len(agent.followups)
	agent.mu.Unlock()
	if pending != 0 {
		t.Fatalf("%d follow-ups still queued", pending)
	}
}

// Queued with nothing running, a follow-up starts at once: there is no turn
// end coming to drain it.
func TestFollowUpOnAnIdleAgentStartsImmediately(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("answered"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.FollowUp("do this when you can")
	if err != nil {
		t.Fatalf("FollowUp: %v", err)
	}
	collected := collect(t, events)

	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("stream ended with %v, want EventTurnDone", last.Kind)
	}
	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1", completer.requests())
	}
}
