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

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/search"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the scripted completer ──────────────────────────────────────────────────

// step is one provider request's scripted answer. It receives the request's
// context — so it can emit stream events through provider.Emit exactly as the
// real adapter does — and the messages it was sent.
type step func(ctx context.Context, messages []ai.Message) (*ai.Response, error)

type scriptedCompleter struct {
	mu      sync.Mutex
	steps   []step
	seen    [][]ai.Message
	models  []string
	efforts []provider.Effort
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
	s.efforts = append(s.efforts, provider.ReasoningEffortFrom(ctx))
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
		// The product default for the spending row is ON (task.audit — the
		// config row defaults on and the door wires it). A test that wants it
		// off says so in mutate; a test that says nothing gets the product's own
		// posture.
		TaskAudit: true,
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
	// is shortened and marked, and the wire arguments are never touched.
	//
	// THE SHORTENING HAPPENS INSIDE THE STRING. What comes back is still a JSON
	// object a surface can read a field out of — that is the contract, and the
	// version of this that cut at a byte offset ended the payload mid-literal and
	// made internal/tui3 draw an em dash for every write worth opening.
	huge := `{"path":"f.go","edits":[{"oldText":"` + strings.Repeat("x", argsLimit) + `"}]}`
	capped := argsText(ai.ToolCall{
		ID: "c2", Type: "function",
		Function: ai.ToolCallFunction{Name: "edit", Arguments: huge},
	})
	if len(capped) >= len(huge) || len(capped) > argsLimit {
		t.Fatalf("an oversized payload was not capped: %d bytes, limit %d", len(capped), argsLimit)
	}
	var fields struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText string `json:"oldText"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(capped), &fields); err != nil {
		t.Fatalf("a capped payload no longer parses: %v", err)
	}
	if fields.Path != "f.go" || len(fields.Edits) != 1 {
		t.Fatalf("a capped payload lost its shape: %+v", fields)
	}
	if !strings.HasPrefix(fields.Edits[0].OldText, "xxxx") ||
		!strings.HasSuffix(fields.Edits[0].OldText, "more bytes)") {
		t.Fatalf("the capped field is not marked: ends %q", lastRunes(fields.Edits[0].OldText, 24))
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
	pending := steeringQueue(agent)
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
	long := strings.Repeat("working through the parser. ", 40)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role:    "assistant",
				Content: []ai.ContentPart{{Type: "text", Text: long}},
			}}}}, nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// A 200-token window puts the threshold at 100 and the keep-recent tail
		// at 50, so one long reply is already too much.
		config.ContextWindow = 200
		config.CompactEnabled = true
	})

	collected := collect(t, mustSubmit(t, agent, "start"))

	var compacted *Event
	for i := range collected {
		if collected[i].Kind == EventCompacted {
			compacted = &collected[i]
		}
	}
	if compacted == nil {
		t.Fatalf("no EventCompacted; events = %v", kinds(collected))
	}
	if !strings.HasPrefix(compacted.Hint, "compacted · ") || !strings.Contains(compacted.Hint, "folded 1 message") {
		t.Fatalf("compaction hint = %q", compacted.Hint)
	}

	agent.mu.Lock()
	messages := agent.messages
	agent.mu.Unlock()
	if got, want := rolesOf(messages), []string{"system", "user", "user"}; !equalStrings(got, want) {
		t.Fatalf("compacted transcript = %v, want system/question/marker", got)
	}
	if messageText(messages[0]) != "SYSTEM" {
		t.Fatalf("system message was rewritten: %q", messageText(messages[0]))
	}
	// THE PERSON'S OWN WORDS SURVIVE. Nothing else in a transcript can be
	// reconstructed from anywhere, so nothing else is protected this way.
	if messageText(messages[1]) != "start" {
		t.Fatalf("the question was folded away: %q", messageText(messages[1]))
	}
	marker := messageText(messages[2])
	if !strings.HasPrefix(marker, foldMarkerPrefix) || strings.Contains(marker, "working through the parser") {
		t.Fatalf("fold marker = %q", marker)
	}

	// AND NO MODEL RAN IN IT. One request was made — the turn's own — and the
	// pass that followed it cost nothing at all.
	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want the turn's one and no compaction call", got)
	}
	final := collected[len(collected)-1]
	if final.Kind != EventTurnDone {
		t.Fatalf("last event = %v, want EventTurnDone", final.Kind)
	}
	if session := agent.Usage(); session.Input != 0 || session.Output != 0 {
		t.Fatalf("session usage = %+v, want a compaction that bought nothing", session)
	}
}

// A fold takes an assistant message and the results answering it TOGETHER. A
// tool result whose call went is an orphan every provider rejects with a 400 —
// on this request and on every request after it, because the transcript is
// append-only.
func TestCompactionFoldsAToolBatchWhole(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = 200
	})

	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", "read them"),
		ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("looking at the file. ", 40)}},
			ToolCalls: []ai.ToolCall{{
				ID: "c1", Type: "function",
				Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
			}},
		},
		ai.Message{
			Role:       "tool",
			ToolCallID: "c1",
			Content:    []ai.ContentPart{{Type: "text", Text: "package a"}},
		},
		textMessage("assistant", "done"))
	agent.mu.Unlock()

	compacted, err := agent.compact(context.Background(), nil)
	if err != nil || !compacted {
		t.Fatalf("compact = %v, %v; want a pass that ran", compacted, err)
	}

	agent.mu.Lock()
	messages := agent.messages
	agent.mu.Unlock()
	for _, message := range messages {
		if message.Role == "tool" {
			t.Fatalf("an orphaned tool result survived the fold: %v", rolesOf(messages))
		}
	}
	if got, want := rolesOf(messages), []string{"system", "user", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("folded transcript = %v, want %v", got, want)
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
		t.Fatalf("requests = %d, want 1 — a short transcript must not compact", got)
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

	if _, _, err := agent.completeWithRetry(ctx, nil, "test/model", effort.None, partial, &warmBatch{}, &formingBatch{}); err == nil {
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
	if strings.Contains(renderSystem(Config{Workspace: workspace}), "�") {
		t.Fatal("the rendered prompt carries a replacement character")
	}
}

// THE MODEL IS TOLD WHAT TIME IT IS, and told it completely enough to write an
// RFC3339 stamp back without asking anybody.
//
// Written from a person's own transcripts: every "remind me in 2 mins" opened
// with a `bash date +"%Y-%m-%dT%H:%M:%S%z"`, because the prompt carried a bare
// date and nothing else. Four facts have to be there — the minute, the numeric
// offset, the zone by name and the weekday — and a test that checked only that
// the line existed would pass on any three of them.
func TestTheSystemPromptSaysWhatTimeItIsWithOffsetAndZone(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	moment := time.Date(2026, 8, 21, 6, 52, 39, 0, zone)
	if got, want := nowLine(moment), "- Now: 2026-08-21 06:52 -04:00 (America/New_York, Friday)\n"; got != want {
		t.Fatalf("nowLine = %q, want %q", got, want)
	}

	rendered := renderSystem(Config{Workspace: t.TempDir()})
	if !strings.Contains(rendered, "\n- Now: ") {
		t.Fatalf("the rendered prompt carries no Now line:\n%s", rendered)
	}
	// The bare date it replaces is gone: two answers to "what day is it" is the
	// one thing worse than none.
	if strings.Contains(rendered, "- Today: ") {
		t.Fatal("the prompt still carries the old Today line beside Now")
	}
	line := ""
	for _, candidate := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(candidate, "- Now: ") {
			line = candidate
		}
	}
	// It is the machine's real clock, to the minute, in the machine's own zone.
	now := time.Now()
	if !strings.Contains(line, now.Format("2006-01-02 15:04")) {
		t.Fatalf("Now line %q does not carry the local time to the minute", line)
	}
	if !strings.Contains(line, now.Format("-07:00")) {
		t.Fatalf("Now line %q does not carry the numeric offset", line)
	}
	if !strings.Contains(line, now.Format("Monday")) {
		t.Fatalf("Now line %q does not carry the weekday", line)
	}
}

// THE MODEL'S CLOCK MUST NOT GO STALE INSIDE ONE SESSION — and it must not cost
// a cold prompt cache to keep it fresh.
//
// Written from a person's own transcript: at 09:41 the model read the clock, and
// TWO HOURS LATER, in the same session, it worked "in 1 minute" out from that
// stamp and proposed a moment already gone. So the prompt is re-rendered when
// its render is older than [clockRefresh], and NOT ONE BYTE MOVES BEFORE THAT:
// inside the threshold the prefix is identical and the cache is hit exactly as
// it was.
func TestThePromptsClockMovesOnlyOnceItHasGoneStale(t *testing.T) {
	// The prompt has to be the REAL one here: the fixed one every other test
	// runs against has no Now line to move.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.System = ""
	})
	opened := time.Now()
	agent.systemAt = opened
	before := agent.system
	if !strings.Contains(before, "\n- Now: ") {
		t.Fatalf("the agent rendered no Now line:\n%s", before)
	}

	// A turn a minute later re-renders NOTHING: the same bytes are the same
	// cache key.
	agent.refreshClockLocked(opened.Add(time.Minute))
	if agent.system != before {
		t.Fatalf("the prompt moved after a minute:\n%s", agent.system)
	}
	agent.refreshClockLocked(opened.Add(clockRefresh - time.Second))
	if agent.system != before {
		t.Fatalf("the prompt moved just inside the threshold:\n%s", agent.system)
	}

	// Past it, exactly ONE line is different, and it is the Now line.
	later := opened.Add(clockRefresh + 2*time.Hour)
	agent.refreshClockLocked(later)
	if agent.system == before {
		t.Fatal("the prompt still carries the minute the session opened, two hours on")
	}
	oldLines, newLines := strings.Split(before, "\n"), strings.Split(agent.system, "\n")
	if len(oldLines) != len(newLines) {
		t.Fatalf("the re-render changed the prompt's shape: %d lines became %d", len(oldLines), len(newLines))
	}
	var moved []string
	for at := range oldLines {
		if oldLines[at] != newLines[at] {
			moved = append(moved, newLines[at])
		}
	}
	if len(moved) != 1 || !strings.HasPrefix(moved[0], "- Now: ") {
		t.Fatalf("the re-render changed %v, want only the Now line", moved)
	}
	if want := strings.TrimRight(nowLine(later), "\n"); moved[0] != want {
		t.Fatalf("the Now line is %q, want %q", moved[0], want)
	}
	if !agent.systemAt.Equal(later) {
		t.Fatalf("the render stamp is %s, want %s", agent.systemAt, later)
	}
}

// A PROMPT SOMEBODY ELSE WROTE IS NEVER RE-RENDERED OVER. A caller that handed
// [Config.System] in has its own idea of what the worker is told, and there may
// be no Now line in it at all; replacing it with ours would be this file
// deciding what another door's session reads.
func TestAHandedInPromptIsLeftAlone(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.System = "you are somebody else's worker"
	})
	agent.refreshClockLocked(time.Now().Add(clockRefresh + time.Hour))
	if agent.system != "you are somebody else's worker" {
		t.Fatalf("a handed-in prompt was rewritten:\n%s", agent.system)
	}
}

// A machine whose zone has no name in the database still gets a name it can
// use. "Local" is the name of a Go variable and would be the prompt telling the
// model about this program's internals instead of about its clock.
func TestTheNowLineNamesAZoneEvenWithNoZoneDatabase(t *testing.T) {
	line := nowLine(time.Date(2026, 8, 21, 6, 52, 0, 0, time.FixedZone("EDT", -4*60*60)))
	if !strings.Contains(line, "(EDT, Friday)") {
		t.Fatalf("nowLine = %q, want the zone's own abbreviation", line)
	}
	if strings.Contains(line, "Local") {
		t.Fatalf("nowLine = %q names a Go variable rather than a zone", line)
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
	warm.consider(context.Background(), agent, agent.newEpisode(), newEventHub(), string(payload))

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
		warm.consider(context.Background(), agent, agent.newEpisode(), newEventHub(), payload)
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

// ── the announcement ────────────────────────────────────────────────────────

// indexOf is where a kind first appears for a tool, or -1.
func indexOf(events []Event, kind EventKind, tool string) int {
	for i, event := range events {
		if event.Kind == kind && event.Tool == tool {
			return i
		}
	}
	return -1
}

// ASKED FOR IS NOT STARTED. A mutating call is announced while the response is
// still streaming and does not begin until the batch runs it, so the two events
// are ordered and both arrive. The gap between them is the whole reason
// EventToolAnnounced exists — it is the interval a surface used to spin a
// spinner through for work that had not started.
func TestAnAnnouncedCallPrecedesItsBegin(t *testing.T) {
	writeCall := ai.ToolCall{ID: "c-write", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"out.txt","content":"x"}`}}
	readCall := ai.ToolCall{ID: "c-read", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, writeCall)
			emitReady(t, ctx, readCall)
			return callsResponse(writeCall, readCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	collected := collect(t, mustSubmit(t, agent, "write the file and read the note"))

	for _, tool := range []string{"write", "read"} {
		announced := indexOf(collected, EventToolAnnounced, tool)
		began := indexOf(collected, EventToolBegin, tool)
		if announced < 0 {
			t.Fatalf("%s was never announced: %v", tool, kinds(collected))
		}
		if began < 0 {
			t.Fatalf("%s never began: %v", tool, kinds(collected))
		}
		// The read is READ-ONLY, so it may already be running by the time its
		// begin is emitted (the early-start law) — but the announcement still
		// comes first, so one surface rule renders both.
		if announced > began {
			t.Fatalf("%s announced at %d, after its begin at %d", tool, announced, began)
		}
	}

	// The announcement carries what the row is drawn from and nothing that has
	// not happened: the gloss, the payload, no result.
	at := indexOf(collected, EventToolAnnounced, "write")
	if collected[at].Hint != "write out.txt" {
		t.Fatalf("the announcement's hint is %q, want the gloss", collected[at].Hint)
	}
	if !strings.Contains(collected[at].Args, `"content":"x"`) {
		t.Fatalf("the announcement lost the payload: %q", collected[at].Args)
	}
	if collected[at].Output != "" {
		t.Fatalf("an announced call has an output: %q", collected[at].Output)
	}
}

// One call, one announcement. A ready event delivered twice — a provider that
// re-flushes, a retry inside one attempt — must not draw the row twice.
func TestACallIsAnnouncedOnlyOnce(t *testing.T) {
	call := ai.ToolCall{ID: "c-1", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, call)
			emitReady(t, ctx, call)
			return callsResponse(call), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	collected := collect(t, mustSubmit(t, agent, "read the note"))
	announcements := 0
	for _, event := range collected {
		if event.Kind == EventToolAnnounced {
			announcements++
		}
	}
	if announcements != 1 {
		t.Fatalf("the call was announced %d times, want 1", announcements)
	}
}

// ── the web hands ───────────────────────────────────────────────────────────

// scriptedSearch is a back end with no network: it records what it was asked
// and answers what the test told it to.
type scriptedSearch struct {
	mu      sync.Mutex
	queries []string
	limits  []int
	results []search.Result
	err     error
}

func (*scriptedSearch) Name() string { return "scripted" }

func (s *scriptedSearch) Search(_ context.Context, query string, limit int) ([]search.Result, error) {
	s.mu.Lock()
	s.queries = append(s.queries, query)
	s.limits = append(s.limits, limit)
	s.mu.Unlock()
	return s.results, s.err
}

func (s *scriptedSearch) asked() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return "", 0
	}
	return s.queries[len(s.queries)-1], s.limits[len(s.limits)-1]
}

type scriptedFetch struct {
	mu   sync.Mutex
	urls []string
	text string
	err  error
}

func (*scriptedFetch) Name() string { return "scripted-fetch" }

func (f *scriptedFetch) Fetch(_ context.Context, url string) (string, error) {
	f.mu.Lock()
	f.urls = append(f.urls, url)
	f.mu.Unlock()
	return f.text, f.err
}

func (f *scriptedFetch) fetched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

// Both read the belt the way the turn reads it, under armMu (connect.go): an
// account can be armed from a goroutine of its own — a connection settled on the
// surface, with no tool call waiting on it — and a test walking the raw slice
// would be reading an array while arming swaps its header.
func beltNames(a *Agent) []string {
	held := a.beltTools()
	out := make([]string, len(held))
	for i, tool := range held {
		out[i] = tool.Name
	}
	return out
}

func hasTool(a *Agent, name string) bool {
	for _, tool := range a.beltTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// A model must never be told about a hand it does not have: with no pair wired
// the two web tools are absent from the belt and from the wire, and with a pair
// they are both there.
func TestTheWebToolsAreOnTheBeltOnlyWhenABackEndIs(t *testing.T) {
	unwired, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if hasTool(unwired, "web_search") || hasTool(unwired, "web_fetch") {
		t.Fatalf("an unwired session carries the web tools: %v", beltNames(unwired))
	}
	for _, definition := range unwired.definitions {
		if strings.HasPrefix(definition.Function.Name, "web_") {
			t.Fatalf("the wire carries %q with nothing behind it", definition.Function.Name)
		}
	}

	wired, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SearchProvider = &scriptedSearch{}
		c.SearchFetcher = &scriptedFetch{}
	})
	if !hasTool(wired, "web_search") || !hasTool(wired, "web_fetch") {
		t.Fatalf("a wired session is missing the web tools: %v", beltNames(wired))
	}

	// The halves resolve independently (search.Resolve returns two), so a
	// binary that can fetch but not search gets exactly the one tool it can
	// honour rather than both or neither.
	half, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SearchFetcher = &scriptedFetch{}
	})
	if hasTool(half, "web_search") || !hasTool(half, "web_fetch") {
		t.Fatalf("a fetch-only session's belt = %v", beltNames(half))
	}
}

// The whole round through a real turn: the model calls web_search, the back end
// answers, the rendered results land in the transcript as the tool result, and
// the model reads them.
func TestWebSearchRunsThroughATurnAndItsResultsReachTheTranscript(t *testing.T) {
	backEnd := &scriptedSearch{results: []search.Result{
		{Title: "Go 1.24 release notes", URL: "https://go.dev/doc/go1.24", Snippet: "what changed"},
		{Title: "Go 1.23 release notes", URL: "https://go.dev/doc/go1.23", Snippet: "what changed before"},
	}}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "web_search", `{"query":"go 1.24 release notes","count":2}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Go 1.24 is out."), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SearchProvider = backEnd })

	collected := collect(t, mustSubmit(t, agent, "what is new in go?"))

	if query, limit := backEnd.asked(); query != "go 1.24 release notes" || limit != 2 {
		t.Fatalf("the back end was asked %q/%d, want the model's query and count", query, limit)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant", "tool", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript roles = %v, want %v", got, want)
	}
	agent.mu.Lock()
	result := messageText(agent.messages[3])
	agent.mu.Unlock()
	if !strings.Contains(result, "https://go.dev/doc/go1.24") || !strings.Contains(result, "2 results") {
		t.Fatalf("the tool result is not the rendered list: %q", result)
	}

	var begin *Event
	for i := range collected {
		switch collected[i].Kind {
		case EventToolBegin:
			begin = &collected[i]
		case EventToolFailed:
			t.Fatalf("the search failed: %s", collected[i].Hint)
		}
	}
	// What the person watching reads is the sentence that was searched.
	if begin == nil || begin.Tool != "web_search" || begin.Hint != "web_search go 1.24 release notes" {
		t.Fatalf("tool begin = %+v, want the query as its gloss", begin)
	}
}

// A fetch runs the same way, and a back end that fails does so as a TOOL error
// the model can read — never as a failed turn.
func TestWebFetchReturnsThePageAndAFailureStaysInsideTheToolResult(t *testing.T) {
	fetcher := &scriptedFetch{text: "  # Go 1.24\n\nIt is out.  "}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "web_fetch", `{"url":"https://go.dev/doc/go1.24"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SearchFetcher = fetcher })

	collect(t, mustSubmit(t, agent, "read the release notes"))
	if got := fetcher.fetched(); len(got) != 1 || got[0] != "https://go.dev/doc/go1.24" {
		t.Fatalf("the fetcher saw %v, want the model's url", got)
	}
	agent.mu.Lock()
	result := messageText(agent.messages[3])
	agent.mu.Unlock()
	if result != "# Go 1.24\n\nIt is out." {
		t.Fatalf("the tool result = %q, want the trimmed page", result)
	}

	// The failure path, straight at the tool: an unreachable back end is
	// something the model works around, not something that ends the turn.
	broken, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SearchProvider = &scriptedSearch{err: errors.New("dial tcp: no route to host")}
		c.SearchFetcher = &scriptedFetch{err: errors.New("404 not found")}
	})
	for _, call := range []struct {
		tool string
		args string
		want string
	}{
		{"web_search", `{"query":"anything"}`, "no route to host"},
		{"web_fetch", `{"url":"https://example.com"}`, "404 not found"},
		{"web_search", `{"query":"   "}`, "query is required"},
		{"web_fetch", `{"url":""}`, "url is required"},
		{"web_search", `{"query":`, "Invalid arguments"},
	} {
		tool := beltTool(t, broken, call.tool)
		text, isError, err := tool.Execute(context.Background(), json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s returned a Go error: %v", call.tool, err)
		}
		if !isError {
			t.Fatalf("%s(%s) was not reported as an error result: %q", call.tool, call.args, text)
		}
		if !strings.Contains(text, call.want) {
			t.Fatalf("%s(%s) = %q, want it to carry %q", call.tool, call.args, text, call.want)
		}
	}
}

// The count the model may ask for is bounded at both ends, silently: a call
// that did the sensible thing beats a round trip spent arguing about a number.
func TestTheSearchCountIsClampedRatherThanRefused(t *testing.T) {
	backEnd := &scriptedSearch{}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SearchProvider = backEnd })
	tool := beltTool(t, agent, "web_search")

	for _, call := range []struct {
		args string
		want int
	}{
		{`{"query":"q"}`, searchDefaultCount},
		{`{"query":"q","count":0}`, searchDefaultCount},
		{`{"query":"q","count":-3}`, searchDefaultCount},
		{`{"query":"q","count":3}`, 3},
		{`{"query":"q","count":1000}`, searchMaxCount},
	} {
		if _, isError, err := tool.Execute(context.Background(), json.RawMessage(call.args)); err != nil || isError {
			t.Fatalf("%s failed: %v", call.args, err)
		}
		if _, limit := backEnd.asked(); limit != call.want {
			t.Fatalf("%s asked for %d results, want %d", call.args, limit, call.want)
		}
	}
}

// The two web rows of the gloss map: what a person reads while their agent is
// off the machine is what it went looking for.
func TestTheWebGlossesAreTheQueryAndTheURL(t *testing.T) {
	for _, want := range []struct {
		call ai.ToolCall
		text string
	}{
		{ai.ToolCall{Function: ai.ToolCallFunction{
			Name: "web_search", Arguments: `{"query":"how to vendor a go module","count":5}`,
		}}, "web_search how to vendor a go module"},
		{ai.ToolCall{Function: ai.ToolCallFunction{
			Name: "web_fetch", Arguments: `{"url":"https://go.dev/ref/mod"}`,
		}}, "web_fetch https://go.dev/ref/mod"},
	} {
		if got := gloss(want.call); got != want.text {
			t.Fatalf("gloss = %q, want %q", got, want.text)
		}
	}
}

// ── the numbers: context, cache affinity, cache accounting ──────────────────

// THE METER READS WHAT THE MODEL CARRIES, NOT WHAT THE PEOPLE SAID.
//
// The surface used to size its context meter from the display transcript's
// bytes, which is the words in the conversation and nothing else. A working
// session's context is mostly the other things: the system prompt, the tool
// schemas, and above all the tool RESULTS — a file read is thirty times the
// weight of the sentence that asked for it. This is the test that the exposed
// figure sees them.
func TestContextTokensCountsToolOutputAndTheSystemPrompt(t *testing.T) {
	workspace := t.TempDir()
	big := strings.Repeat(strings.Repeat("x", 39)+"\n", 200) // 8000 bytes
	if err := os.WriteFile(filepath.Join(workspace, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	system := strings.Repeat("S", 4000)

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "read", `{"path":"big.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = workspace
		config.System = system
	})

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "hi")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// What anybody SAID in this conversation is "hi" and "done" — nine bytes,
	// two tokens, and the whole of what a transcript-counting meter could see.
	// What the model is carrying is the 4000-byte system prompt and the
	// 8000-byte file: 3000 tokens before anything else is counted.
	got := agent.ContextTokens()
	if got < 3000 {
		t.Fatalf("ContextTokens = %d, want at least the 3000 tokens of system prompt "+
			"and tool output alone — the tool result is invisible to it", got)
	}
}

// The provider's own figure is the floor, and the content is what speaks for
// everything appended since the last response.
func TestContextTokensPrefersTheProviderFigureUntilTheContentPassesIt(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := textResponse("ok")
			response.Usage = &ai.Usage{PromptTokens: 40_000, CompletionTokens: 100, TotalTokens: 40_100}
			return response, nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "hi")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// The transcript is three short messages; the provider says 40,100 tokens,
	// which is the honest number for the request it actually served.
	if got := agent.ContextTokens(); got != 40_100 {
		t.Fatalf("ContextTokens = %d, want the provider's 40100", got)
	}

	// Now append more than that in content. The provider's figure describes a
	// request that no longer exists, and the estimate has to take over — this is
	// the 300KB tool result that would otherwise sit invisible until the next
	// response corrected the figure.
	agent.record(textMessage("user", strings.Repeat("y", 400_000)))
	if got := agent.ContextTokens(); got < 100_000 {
		t.Fatalf("ContextTokens = %d, want the content estimate (~140k) once it "+
			"passed the provider's stale figure", got)
	}
}

// AN IMAGE IS NOT FREE AND IT IS NOT ITS OWN BASE64 EITHER.
//
// Counting nothing made a conversation of screenshots read as a few hundred
// tokens right up to the provider's overflow error. Counting the data URL would
// charge a photograph two megabytes and compact the turn after it was pasted.
func TestTheContextCountsImagePartsAtAFlatRate(t *testing.T) {
	words := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "look"}}}
	small := ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,iVBOR"}},
	}}
	huge := ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &ai.ImageURLData{
			URL: "data:image/png;base64," + strings.Repeat("A", 2<<20),
		}},
	}}

	want := imagePartTokens * bytesPerToken
	if got := messageBytes(small) - messageBytes(words); got != want {
		t.Fatalf("an image weighed %d bytes of estimate, want the flat %d", got, want)
	}
	// A 2MB picture and a 27-byte one cost the same, which is the point of a
	// flat rate: what the model is billed has nothing to do with the file size.
	if messageBytes(huge) != messageBytes(small) {
		t.Fatalf("the estimate followed the payload: %d vs %d",
			messageBytes(huge), messageBytes(small))
	}
	// And it is visible to the estimator, not just to this arithmetic.
	if messageBytes(small) <= messageBytes(words) {
		t.Fatal("an image part is invisible to the context estimate")
	}
}

// A SESSION IS A LINEAGE, AND IT SAYS SO ON EVERY REQUEST.
//
// internal/exec/bare deliberately sends no prompt-cache key: a leaf runs once
// and its prefix is never asked for again. A session re-sends its whole
// transcript on every step of every turn and picks it up again tomorrow, so it
// pins one router instance and keeps the prefix warm. This reads the key off the
// wire body, so a regression anywhere in the chain — agent, wrapper, context,
// encode — fails here rather than in a bill.
func TestTheSessionStampsItsCacheKeyOnTheWire(t *testing.T) {
	var mu sync.Mutex
	var keys []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			PromptCacheKey string `json:"prompt_cache_key"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		keys = append(keys, body.PromptCacheKey)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	file := filepath.Join(t.TempDir(), "session.jsonl")
	config := Config{
		Workspace:   t.TempDir(),
		Model:       "vendor/model",
		APIKey:      "test",
		BaseURL:     server.URL,
		SessionFile: file,
	}

	turn := func(agent *Agent, text string) {
		t.Helper()
		ctx, cancel := deadline(10 * time.Second)
		defer cancel()
		events, err := agent.Submit(ctx, text)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		for event := range events {
			if event.Kind == EventError {
				t.Fatalf("turn errored: %v", event.Err)
			}
		}
	}

	first, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	turn(first, "one")
	// A model swap must NOT split the lineage: the transcript is the same
	// transcript, and the key names the conversation rather than the model.
	first.SetModel("vendor/other")
	turn(first, "two")
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Tomorrow. Same file, new process — and it has to rejoin the prefix it
	// paid to write, which is the whole reason the key is the session id and
	// not something the process invented.
	resumed, err := New(config)
	if err != nil {
		t.Fatalf("New (resume): %v", err)
	}
	turn(resumed, "three")
	if err := resumed.Close(); err != nil {
		t.Fatalf("Close (resume): %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Three turns and the auxiliary calls the session made of its own accord —
	// the name it gave itself (title.go). Those ride the lineage too, which is
	// what asserting on every key rather than on the three turns' proves.
	if len(keys) < 3 {
		t.Fatalf("the server saw %d requests, want at least the 3 turns", len(keys))
	}
	if keys[0] == "" {
		t.Fatal("no prompt_cache_key on the wire; the session sent bare's no-key posture")
	}
	if !strings.HasPrefix(keys[0], "aforge-") {
		t.Fatalf("prompt_cache_key = %q, want the aforge- lineage shape", keys[0])
	}
	for index, key := range keys {
		if key != keys[0] {
			t.Fatalf("request %d rode key %q, want the session's one lineage %q",
				index, key, keys[0])
		}
	}
}

// Two sessions are two lineages, or the router is being asked to serve two
// unrelated growing transcripts off one instance's cache — which is the exact
// failure WithLeafCacheKey exists to prevent (provider/hints.go).
func TestTwoSessionsAreTwoLineages(t *testing.T) {
	first, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "a.jsonl")
	})
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "b.jsonl")
	})
	if first.cacheKey == "" || second.cacheKey == "" {
		t.Fatal("a session with a file has no cache lineage")
	}
	if first.cacheKey == second.cacheKey {
		t.Fatalf("two sessions share one lineage: %q", first.cacheKey)
	}

	// And a conversation with no file still has ONE key of its own, rather than
	// falling in with every other memory-only session.
	memory, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if memory.cacheKey == "" {
		t.Fatal("a memory-only session sent no key at all")
	}
	if memory.cacheKey == first.cacheKey {
		t.Fatal("a memory-only session joined a file-backed session's lineage")
	}
}

// The cache figures are accounted per turn AND per session, in both dialects
// the endpoints speak them in.
func TestCacheTokensAccumulatePerTurnAndPerSession(t *testing.T) {
	// Step 1 speaks Anthropic-native, step 2 the OpenAI nesting. ai.Usage
	// reconciles the two spellings; this asserts the loop reads them through it
	// rather than reaching for one field.
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("call-1", "ls", `{"path":"."}`)
			response.Usage = &ai.Usage{
				PromptTokens: 1000, CompletionTokens: 20,
				CacheReadInputTokens: 800, CacheCreationInputTokens: 200,
			}
			return response, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := textResponse("done")
			response.Usage = &ai.Usage{
				PromptTokens: 1200, CompletionTokens: 30,
				PromptTokensDetails: &ai.PromptTokensDetails{CachedTokens: 1000},
			}
			return response, nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "hi")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var turn Usage
	for _, event := range collect(t, events) {
		if event.Kind == EventTurnDone {
			turn = event.Usage
		}
	}
	if turn.CacheRead != 1800 || turn.CacheWrite != 200 {
		t.Fatalf("the turn read %d and wrote %d, want 1800 and 200",
			turn.CacheRead, turn.CacheWrite)
	}
	session := agent.Usage()
	if session.CacheRead != 1800 || session.CacheWrite != 200 {
		t.Fatalf("the session read %d and wrote %d, want 1800 and 200",
			session.CacheRead, session.CacheWrite)
	}
	// Nothing said and nothing counted is the one honest zero.
	quiet, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if usage := quiet.Usage(); usage.CacheRead != 0 || usage.CacheWrite != 0 {
		t.Fatalf("a session that has not run reported cache traffic: %+v", usage)
	}
}

// THE TWO DIALECTS DISAGREE ABOUT A FACT, NOT A NAME. OpenAI-style endpoints
// count cached tokens INSIDE prompt_tokens; Anthropic-native ones count them
// BESIDE input_tokens. Nothing on the wire says which, so the shape does.
func TestCachedShareReconcilesBothProviderDialects(t *testing.T) {
	// OpenAI: 800 of the 1000 prompt tokens were cached. 80%, not 44%.
	share, ok := Usage{Input: 1000, CacheRead: 800}.CachedShare()
	if !ok || share < 0.799 || share > 0.801 {
		t.Fatalf("subset dialect: share = %v (ok=%v), want 0.8", share, ok)
	}
	// Anthropic: 200 fresh input tokens beside 800 read from cache. 80%, not
	// 400%.
	share, ok = Usage{Input: 200, CacheRead: 800}.CachedShare()
	if !ok || share < 0.799 || share > 0.801 {
		t.Fatalf("disjoint dialect: share = %v (ok=%v), want 0.8", share, ok)
	}
	// No cache reads is absence, and absence is not "0% cached".
	if _, ok := (Usage{Input: 1000}).CachedShare(); ok {
		t.Fatal("a session with no cache accounting reported a share")
	}
}

// THE SSE USAGE CHUNK HAS TO CARRY THE CACHE FIELDS, or every number above it
// is arithmetic on zero. This stands up a streaming endpoint that answers the
// way OpenRouter does with usage.include set, and reads the figures back off
// the agent — the whole path: SSE decode, chunk usage, response usage, loop.
func TestTheStreamedUsageChunkCarriesTheCacheFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		// The final chunk: no choices, usage only, exactly as the providers
		// send it. Both spellings ride together here because rows in the wild
		// carry either.
		io.WriteString(w, `data: {"id":"x","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":20,`+
			`"total_tokens":1020,"prompt_tokens_details":{"cached_tokens":768},`+
			`"cache_creation_input_tokens":232,"cost":0.0021}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	agent, err := New(Config{
		Workspace: t.TempDir(),
		Model:     "vendor/model",
		APIKey:    "test",
		BaseURL:   server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer agent.Close()

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "hi")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	usage := agent.Usage()
	if usage.CacheRead != 768 {
		t.Fatalf("CacheRead = %d, want the 768 the stream reported", usage.CacheRead)
	}
	if usage.CacheWrite != 232 {
		t.Fatalf("CacheWrite = %d, want the 232 the stream reported", usage.CacheWrite)
	}
	if usage.Input != 1000 {
		t.Fatalf("Input = %d, want 1000", usage.Input)
	}
}
