package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type clientToolEmitter struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []appwire.AgentsToolCall
	seen   chan appwire.AgentsToolCall
	answer func(appwire.AgentsToolCall)
}

// Source contract (a), (b): real provider calls name the entry returned by the
// app read projection, across a second send, a reopened journal and takeover resume.
func TestClientToolsSourceMessageWireAndRestart(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprintf("stopped=%t", stopped), func(t *testing.T) {
			b, door, emitter := clientToolBackend(t)
			clientToolAttach(t, b, door)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered := make(chan struct{})
			var mu sync.Mutex
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct {
						Role string `json:"role"`
					} `json:"messages"`
					Tools []json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				tool := len(request.Tools) > 0 && len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "user"
				mu.Lock()
				if tool {
					calls++
				}
				call := calls
				mu.Unlock()
				if tool && stopped && call == 1 {
					close(entered)
					select {
					case <-r.Context().Done():
					case <-ctx.Done():
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if tool {
					fmt.Fprintf(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"provider-source-%d","type":"function","function":{"name":"app_note","arguments":"{}"}}]},"finish_reason":null}]}`+"\n\n", call)
					fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
				} else {
					fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			answer := func(backend *appBackend, emit *clientToolEmitter) {
				emit.answer = func(event appwire.AgentsToolCall) {
					ack, err := backend.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`true`)})
					if err != nil || !ack.Accepted {
						t.Errorf("tool result: %+v %v", ack, err)
					}
				}
			}
			answer(b, emitter)
			path := filepath.Join(t.TempDir(), "session.jsonl")
			cfg := session.Config{Workspace: t.TempDir(), SessionFile: path, Model: "test/model", APIKey: "fixture", BaseURL: server.URL,
				OneModel: true, DeterministicNames: true, PromptProfile: "full", ClientTools: door}
			agent, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			b.locked(func() { b.agents[door.agent].live.conn.Conv = agent })
			drain := func(events <-chan session.Event) {
				t.Helper()
				for {
					select {
					case _, open := <-events:
						if !open {
							return
						}
					case <-ctx.Done():
						t.Fatal("turn did not finish")
					}
				}
			}
			events, err := agent.Submit(ctx, "first question")
			if err != nil {
				t.Fatal(err)
			}
			if stopped {
				clientToolAwait(t, entered)
				agent.InterruptFor(session.StopByTakeover)
			}
			drain(events)
			messageID := func(conv *session.Agent, text string) string {
				t.Helper()
				for _, entry := range appEntriesOf(door.agent, conv.Transcript(), time.Time{}, nil) {
					if entry.Kind == "user" && entry.Text == text && entry.MessageID != "" {
						return entry.MessageID
					}
				}
				t.Fatalf("read omitted message id for %q", text)
				return ""
			}
			firstID := messageID(agent, "first question")
			if !stopped {
				first := clientToolAwait(t, emitter.seen)
				if first.SourceMessageID != firstID {
					t.Fatalf("first call=%+v read message=%q", first, firstID)
				}
				next, err := agent.SubmitMessage(ctx, session.Submission{Text: "second question", ClientID: "second-window"})
				if err != nil {
					t.Fatal(err)
				}
				drain(next)
				second := clientToolAwait(t, emitter.seen)
				secondID := messageID(agent, "second question")
				if second.SourceMessageID != secondID || secondID == firstID {
					t.Fatalf("second call=%+v read ids=%q/%q", second, firstID, secondID)
				}
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			fresh, freshDoor, freshEmitter := clientToolBackend(t)
			clientToolAttach(t, fresh, freshDoor)
			answer(fresh, freshEmitter)
			cfg.ClientTools = freshDoor
			reopened, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			fresh.locked(func() { fresh.agents[freshDoor.agent].live.conn.Conv = reopened })
			if got := messageID(reopened, "first question"); got != firstID {
				t.Fatalf("reopened read changed message id: %q want %q", got, firstID)
			}
			if stopped {
				resumed, ok := reopened.ResumeStoppedTurn(ctx)
				if !ok {
					t.Fatal("reopened stopped turn did not resume")
				}
				drain(resumed)
				call := clientToolAwait(t, freshEmitter.seen)
				if call.SourceMessageID != firstID {
					t.Fatalf("resumed call=%+v read message=%q", call, firstID)
				}
			}
		})
	}
}

// Contract (a), (b), (e): a real session provider request crosses the app door,
// then a turn started after disconnect loses the tools and their instructions.
func TestClientToolsModelWireRoundTripAndDisconnectBoundary(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	clientToolAttach(t, b, door)
	var mu sync.Mutex
	var requests []struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Call    string          `json:"tool_call_id"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Call    string          `json:"tool_call_id"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		foreground := len(request.Tools) > 0
		first := foreground && len(requests) == 0
		if foreground {
			requests = append(requests, request)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if first {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"provider-client-1","type":"function","function":{"name":"app_note","arguments":"{\"id\":3}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	disconnected := make(chan struct{})
	emitter.answer = func(event appwire.AgentsToolCall) {
		ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`{ "note": "found" }`)})
		if err != nil || !ack.Accepted {
			t.Errorf("wire answer: %+v %v", ack, err)
		}
		b.clientTools.disconnect()
		close(disconnected)
	}
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "test/model", APIKey: "fixture", BaseURL: server.URL,
		OneModel: true, DeterministicNames: true, PromptProfile: "full", ClientTools: door})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	b.locked(func() { b.agents[door.agent].live.conn.Conv = agent })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "read the app note")
	if err != nil {
		t.Fatal(err)
	}
	turnCall := ""
	for {
		select {
		case event, open := <-events:
			if !open {
				goto finished
			}
			if event.Kind == session.EventToolBegin && event.Tool == "app_note" {
				turnCall = event.CallID
			}
		case <-ctx.Done():
			t.Fatal("model round trip did not finish")
		}
	}
finished:
	clientToolAwait(t, disconnected)
	// Start this turn after disconnect has completed, independently of sender scheduling.
	next, err := agent.Submit(ctx, "what tools remain?")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case _, open := <-next:
			if !open {
				goto nextFinished
			}
		case <-ctx.Done():
			t.Fatal("post-disconnect turn did not finish")
		}
	}
nextFinished:
	event := clientToolAwait(t, emitter.seen)
	if event.TurnCall != turnCall || turnCall != "provider-client-1" {
		t.Fatalf("turn.tool correlation: %+v / %q", event, turnCall)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("provider requests=%d", len(requests))
	}
	first, second, afterDisconnect := requests[0], requests[1], requests[2]
	if first.Tools[len(first.Tools)-1].Function.Name != "app_note" || !strings.Contains(string(first.Messages[0].Content), "Tools from the app") {
		t.Fatal("initial request lost app definitions or instructions")
	}
	for _, tool := range afterDisconnect.Tools {
		if tool.Function.Name == "app_note" {
			t.Fatal("disconnected tool remained on the next request")
		}
	}
	if strings.Contains(string(afterDisconnect.Messages[0].Content), "Tools from the app") {
		t.Fatal("disconnected instructions remained on the next request")
	}
	found := false
	for _, message := range second.Messages {
		if message.Role != "tool" || message.Call != turnCall {
			continue
		}
		if text := clientToolWireText(message.Content); text != `{"note":"found"}` {
			t.Fatalf("model result: %s", message.Content)
		}
		found = true
	}
	if !found {
		t.Fatal("model did not receive the wire result")
	}
}

func clientToolWireText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	for _, part := range parts {
		text += part.Text
	}
	return text
}

func (e *clientToolEmitter) Emit(event string, data any) {
	if event != appwire.EventAgentsToolCall {
		return
	}
	call := data.(appwire.AgentsToolCall)
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	select {
	case e.seen <- call:
	case <-time.After(2 * time.Second):
		e.t.Error("client tool event could not be delivered within two seconds")
		return
	}
	if e.answer != nil {
		e.answer(call)
	}
}

func clientToolBackend(t *testing.T) (*appBackend, *appClientToolDoor, *clientToolEmitter) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	e := &clientToolEmitter{t: t, seen: make(chan appwire.AgentsToolCall, 20)}
	b := newFakeBackend(e, nil)
	id := "client-agent"
	transcript := filepath.Join(t.TempDir(), id, "transcript.jsonl")
	b.locked(func() {
		b.agents[id] = &appAgent{view: appwire.Agent{ID: id}, transcript: transcript,
			live: &appLive{turn: 7, conn: &appConn{Conv: &fakeConversation{}}}}
	})
	t.Cleanup(func() {
		b.clientTools.disconnect()
		b.locked(func() { b.agents[id].live = nil })
		_ = b.Close()
	})
	return b, &appClientToolDoor{backend: b, agent: id}, e
}

func clientToolParams(agent, set, name string) appwire.ClientToolsSetParams {
	return appwire.ClientToolsSetParams{Agent: agent, Set: set, Instructions: "Use this set.", Tools: []session.ClientTool{
		{Name: name, Description: "An app action", InputSchema: json.RawMessage(`{"type":"object"}`), Category: session.ActionEdit},
	}}
}

func clientToolAttach(t *testing.T, b *appBackend, d *appClientToolDoor) session.ClientToolCall {
	t.Helper()
	if _, err := b.SetClientTools(clientToolParams(d.agent, "notes", "app_note")); err != nil {
		t.Fatal(err)
	}
	return session.ClientToolCall{Set: "notes", Name: "app_note", TurnCall: "provider-1", Args: json.RawMessage(`{"id":3}`)}
}

// Contract (a), (h): the Fresh/Open builder binds the final conversation, even when its config carried another door.
func TestClientToolsFreshAndOpenBindTheirConversation(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	clientToolAttach(t, b, door)
	previous := appClientToolsBackend.Swap(b)
	t.Cleanup(func() { appClientToolsBackend.Store(previous) })
	root := t.TempDir()
	dir := writeV3Session(t, root, "client-b", "", time.Time{})
	file := (session.Place{Dir: dir}).Transcript()
	b.locked(func() { b.agents["client-b"] = &appAgent{view: appwire.Agent{ID: "client-b"}, transcript: file} })
	p := clientToolParams("client-b", "other", "app_other")
	p.Instructions = "Only conversation B's instructions."
	p.Tools[0].Category = session.ActionRead
	if _, err := b.SetClientTools(p); err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{Workspace: root, ClientTools: door}
	b.locked(func() { cfg.SessionFile = b.agents[door.agent].transcript })
	cfg.Place = session.Place{Dir: session.DirOf(cfg.SessionFile), Workspace: root}
	for _, path := range []string{"fresh", "open"} {
		t.Run(path, func(t *testing.T) {
			open := appClientToolsOpener(func(opened session.Config) (*session.Agent, error) {
				bound, ok := opened.ClientTools.(*appClientToolDoor)
				if !ok || bound.agent != "client-b" {
					t.Fatalf("replacement door: %+v", opened.ClientTools)
				}
				_, sets := bound.ClientToolSets()
				if len(sets) != 1 || sets[0].ID != "other" || sets[0].Instructions != p.Instructions ||
					len(sets[0].Tools) != 1 || sets[0].Tools[0].Name != "app_other" || sets[0].Tools[0].Category != session.ActionRead {
					t.Fatalf("replacement inherited the wrong sets: %+v", sets)
				}
				return nil, nil
			})
			var next session.Config
			var err error
			if path == "fresh" {
				next, err = v3PointAt(cfg, session.Place{Dir: dir, Workspace: root})
			} else {
				next, err = v3Reopen(cfg, file, root)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := open(next); err != nil {
				t.Fatal(err)
			}
		})
	}
	if cfg.ClientTools != door {
		t.Fatal("opening a replacement changed the original config")
	}
	appClientToolsBackend.Store(nil)
	open := appClientToolsOpener(func(opened session.Config) (*session.Agent, error) {
		if opened.ClientTools != nil {
			t.Fatal("opening without an app retained a stale door")
		}
		return nil, nil
	})
	if _, err := open(cfg); err != nil {
		t.Fatal(err)
	}
}

func clientToolStart(ctx context.Context, d *appClientToolDoor, call session.ClientToolCall) <-chan session.ClientToolAnswer {
	done := make(chan session.ClientToolAnswer, 1)
	go func() {
		answer, err := d.CallClientTool(ctx, call)
		if err != nil {
			answer = session.ClientToolAnswer{Text: err.Error(), IsError: true}
		}
		done <- answer
	}()
	return done
}

func clientToolAwait[T any](t *testing.T, done <-chan T) T {
	t.Helper()
	select {
	case answer := <-done:
		return answer
	case <-time.After(2 * time.Second):
		t.Fatal("client tool did not settle within two seconds")
	}
	var zero T
	return zero
}

// Contract (b), (d), source contract (b): redispatch preserves the source and emits once.
func TestClientToolsEventResultAndReplay(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	call := clientToolAttach(t, b, door)
	call.SourceMessage = "msg-recorded"
	first := clientToolStart(context.Background(), door, call)
	event := clientToolAwait(t, emitter.seen)
	if event.Agent != door.agent || event.Turn != 7 || event.TurnCall != call.TurnCall || event.Set != call.Set || event.Name != call.Name ||
		string(event.Args) != string(call.Args) || event.DeadlineMS != session.ClientToolDefaultDeadlineMS || event.Call != clientToolCallID(door.agent, call) || event.SourceMessageID != call.SourceMessage {
		t.Fatalf("event shape: %+v", event)
	}
	second := clientToolStart(context.Background(), door, call)
	ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`{ "ok": true }`)})
	if err != nil || !ack.Accepted {
		t.Fatalf("answer: %+v %v", ack, err)
	}
	for _, done := range []<-chan session.ClientToolAnswer{first, second, clientToolStart(context.Background(), door, call)} {
		if answer := clientToolAwait(t, done); answer.IsError || answer.Text != `{"ok":true}` {
			t.Fatalf("result: %+v", answer)
		}
	}
	for _, id := range []string{event.Call, "unknown"} {
		ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: id, Result: json.RawMessage(`null`)})
		if err != nil || ack.Accepted {
			t.Fatalf("duplicate/unknown: %+v %v", ack, err)
		}
	}
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if len(emitter.calls) != 1 {
		t.Fatalf("%d events for one call", len(emitter.calls))
	}
	// A new registry represents a new process, which must name the same call alike.
	fresh, freshDoor, freshEmitter := clientToolBackend(t)
	clientToolAttach(t, fresh, freshDoor)
	fresh.locked(func() { fresh.agents[freshDoor.agent].live.turn = 1 })
	replayed := clientToolStart(context.Background(), freshDoor, call)
	again := clientToolAwait(t, freshEmitter.seen)
	if again.Call != event.Call || again.SourceMessageID != event.SourceMessageID || again.Turn != 1 {
		t.Fatalf("fresh process changed correlation: %+v / %+v", event, again)
	}
	if _, err := fresh.ClientToolResult(appwire.ClientToolResultParams{Call: again.Call, Result: json.RawMessage(`true`)}); err != nil {
		t.Fatal(err)
	}
	clientToolAwait(t, replayed)
}

// Contract (b), (d): a provider id names a call only together with what was
// called. Kimi-family models write their own `name:idx` ids, which repeat after
// a rewind or a compaction, and a faulty server can repeat any id. A reused id
// with another tool or other arguments must reach the app as a call of its own,
// whether the earlier call is still waiting or already answered.
func TestClientToolsReusedProviderIDWithAnotherCallReachesTheApp(t *testing.T) {
	for _, earlier := range []string{"settled", "pending"} {
		t.Run(earlier, func(t *testing.T) {
			b, door, emitter := clientToolBackend(t)
			call := clientToolAttach(t, b, door)
			if _, err := b.SetClientTools(clientToolParams(door.agent, "other", "app_other")); err != nil {
				t.Fatal(err)
			}
			answer := func(event appwire.AgentsToolCall) string {
				t.Helper()
				text := event.Name + " " + string(event.Args)
				result, _ := json.Marshal(text)
				ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: result})
				if err != nil || !ack.Accepted {
					t.Fatalf("answer %+v: %+v %v", event, ack, err)
				}
				return string(result)
			}
			first := clientToolStart(context.Background(), door, call)
			firstEvent := clientToolAwait(t, emitter.seen)
			firstWant := ""
			if earlier == "settled" {
				firstWant = answer(firstEvent)
				if got := clientToolAwait(t, first); got.IsError || got.Text != firstWant {
					t.Fatalf("first answer: %+v", got)
				}
			}
			otherArgs := call
			otherArgs.Args = json.RawMessage(`{"id":4}`)
			otherTool := session.ClientToolCall{Set: "other", Name: "app_other", TurnCall: call.TurnCall, Args: call.Args}
			seen := map[string]bool{firstEvent.Call: true}
			for _, reused := range []session.ClientToolCall{otherArgs, otherTool} {
				done := clientToolStart(context.Background(), door, reused)
				var event appwire.AgentsToolCall
				select {
				case event = <-emitter.seen:
				case got := <-done:
					t.Fatalf("%s %s with reused provider id %q never reached the app; the model was told %+v",
						reused.Name, reused.Args, reused.TurnCall, got)
				case <-time.After(2 * time.Second):
					t.Fatalf("%s %s with reused provider id %q never reached the app: it joined the earlier call's wait",
						reused.Name, reused.Args, reused.TurnCall)
				}
				if seen[event.Call] || event.Name != reused.Name || string(event.Args) != string(reused.Args) || event.TurnCall != reused.TurnCall {
					t.Fatalf("reused provider id did not get its own call: %+v (earlier %v)", event, seen)
				}
				seen[event.Call] = true
				want := answer(event)
				if got := clientToolAwait(t, done); got.IsError || got.Text != want {
					t.Fatalf("%s %s was answered %+v, want its own answer %s", reused.Name, reused.Args, got, want)
				}
			}
			if earlier == "pending" {
				firstWant = answer(firstEvent)
				if got := clientToolAwait(t, first); got.IsError || got.Text != firstWant {
					t.Fatalf("first answer: %+v", got)
				}
			}
			// The same call dispatched again is still the same call.
			if got := clientToolAwait(t, clientToolStart(context.Background(), door, call)); got.IsError || got.Text != firstWant {
				t.Fatalf("true redispatch: %+v want %s", got, firstWant)
			}
			emitter.mu.Lock()
			defer emitter.mu.Unlock()
			if len(emitter.calls) != 3 {
				t.Fatalf("three distinct calls emitted %d events: %+v", len(emitter.calls), emitter.calls)
			}
		})
	}
}

// Contract (b): a call without a provider id gets a call id no later launch
// can mint again. The app keeps answered ids for 30 days, far longer than one
// engine process lives, so each child process below is one launch of the engine.
func TestClientToolsCallWithoutProviderIDNeverRepeatsAcrossLaunches(t *testing.T) {
	const child = "CODEAF_TEST_CLIENT_TOOL_FRESH_ID"
	if os.Getenv(child) == "1" {
		b, door, emitter := clientToolBackend(t)
		call := clientToolAttach(t, b, door)
		call.TurnCall = ""
		ctx, cancel := context.WithCancel(context.Background())
		done := clientToolStart(ctx, door, call)
		event := clientToolAwait(t, emitter.seen)
		cancel()
		clientToolAwait(t, done)
		fmt.Printf("client-tool-call=%s\n", event.Call)
		return
	}
	var ids []string
	for launch := 0; launch < 2; launch++ {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestClientToolsCallWithoutProviderIDNeverRepeatsAcrossLaunches$", "-test.v")
		command.Env = append(os.Environ(), child+"=1")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("launch %d: %v\n%s", launch+1, err, out)
		}
		match := regexp.MustCompile(`(?m)^client-tool-call=(\S+)$`).FindSubmatch(out)
		if match == nil {
			t.Fatalf("launch %d printed no call id:\n%s", launch+1, out)
		}
		ids = append(ids, string(match[1]))
	}
	if ids[0] == ids[1] {
		t.Fatalf("two launches minted the same call id %s for different calls without a provider id", ids[0])
	}
	for _, id := range ids {
		if !regexp.MustCompile(`^ct_[0-9a-f]{64}$`).MatchString(id) {
			t.Fatalf("call id %q is not ct_ and 64 hex digits", id)
		}
	}
}

// Contract (b): the pending entry exists before an emitter can answer synchronously.
func TestClientToolsFastErrorAnswer(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	call := clientToolAttach(t, b, door)
	emitter.answer = func(event appwire.AgentsToolCall) {
		text := "permission denied"
		ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Error: &text})
		if err != nil || !ack.Accepted {
			t.Errorf("fast answer: %+v %v", ack, err)
		}
	}
	answer := clientToolAwait(t, clientToolStart(context.Background(), door, call))
	if !answer.IsError || answer.Text != "notes could not do app_note: permission denied" {
		t.Fatalf("error result: %+v", answer)
	}
}

// Contract (c), (d), (e): each ending is bounded and rejects every late answer.
func TestClientToolsDisconnectDeadlineAndTurnEnd(t *testing.T) {
	for _, ending := range []string{"disconnect", "deadline", "turn end"} {
		t.Run(ending, func(t *testing.T) {
			b, door, emitter := clientToolBackend(t)
			call := clientToolAttach(t, b, door)
			if ending == "deadline" {
				// Only this in-process fixture bypasses the wire's one-second minimum.
				b.clientTools.mu.Lock()
				state := b.clientTools.agents[door.agent]
				state.sets[0].DeadlineMS = 15
				b.clientTools.agents[door.agent] = state
				b.clientTools.mu.Unlock()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := clientToolStart(ctx, door, call)
			event := clientToolAwait(t, emitter.seen)
			want := "client_timeout: the app that provides app_note did not answer within 0.015s; the outcome is unknown, so check before trying again."
			switch ending {
			case "disconnect":
				b.clientTools.disconnect()
				b.clientTools.disconnect()
				want = "client_unavailable: the app that provides app_note disconnected before answering; the outcome is unknown, so check before trying again."
				_, sets := door.ClientToolSets()
				if len(sets) != 0 {
					t.Fatal("disconnect kept sets")
				}
			case "turn end":
				cancel()
				want = clientToolTurnEnded(call.Name).Text
			}
			if answer := clientToolAwait(t, done); !answer.IsError || answer.Text != want {
				t.Fatalf("ending: %+v", answer)
			}
			ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`1`)})
			if err != nil || ack.Accepted {
				t.Fatalf("late answer: %+v %v", ack, err)
			}
		})
	}
}

// Contract (c), (d): a full writer cannot hold the deadline, turn end or disconnect hostage.
func TestClientToolsBlockedEmitterDoesNotBlockEndings(t *testing.T) {
	for _, ending := range []string{"deadline", "turn end", "disconnect"} {
		t.Run(ending, func(t *testing.T) {
			b, door, emitter := clientToolBackend(t)
			call := clientToolAttach(t, b, door)
			if ending == "deadline" {
				// Only this fixture bypasses the wire's one-second minimum.
				b.clientTools.mu.Lock()
				b.clientTools.agents[door.agent].sets[0].DeadlineMS = 100
				b.clientTools.mu.Unlock()
			}
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			emitter.answer = func(appwire.AgentsToolCall) {
				close(entered)
				<-release
				close(returned)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := clientToolStart(ctx, door, call)
			event := clientToolAwait(t, emitter.seen)
			clientToolAwait(t, entered)
			want := "client_timeout: the app that provides app_note did not answer within 0.1s; the outcome is unknown, so check before trying again."
			switch ending {
			case "turn end":
				cancel()
				want = clientToolTurnEnded(call.Name).Text
			case "disconnect":
				b.clientTools.disconnect()
				want = "client_unavailable: the app that provides app_note disconnected before answering; the outcome is unknown, so check before trying again."
			}
			if answer := clientToolAwait(t, done); !answer.IsError || answer.Text != want {
				t.Fatalf("blocked %s: %+v", ending, answer)
			}
			select {
			case <-returned:
				t.Fatal("emitter returned before the call ended")
			default:
			}
			ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`null`)})
			if err != nil || ack.Accepted {
				t.Fatalf("late answer: %+v %v", ack, err)
			}
			unblock()
			clientToolAwait(t, returned)
			b.clientTools.disconnect()
			clientToolAwait(t, b.clientTools.senderDone)
		})
	}
}

// Contract (c), (d): calls ending behind a stalled writer are dropped before delivery.
func TestClientToolsEndedQueuedCallsAreNotEmitted(t *testing.T) {
	for _, ending := range []string{"deadline", "turn end", "disconnect", "result"} {
		t.Run(ending, func(t *testing.T) {
			b, door, emitter := clientToolBackend(t)
			call := clientToolAttach(t, b, door)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			emitter.answer = func(event appwire.AgentsToolCall) {
				if event.TurnCall == "provider-1" {
					<-release
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := clientToolStart(ctx, door, call)
			clientToolAwait(t, emitter.seen)
			cancel()
			clientToolAwait(t, first)

			queuedCtx, cancelQueued := context.WithCancel(context.Background())
			defer cancelQueued()
			call.TurnCall = "queued-call"
			id := clientToolCallID(door.agent, call)
			r := &b.clientTools
			r.mu.Lock()
			pending, _, emit := r.beginLocked(queuedCtx, door.agent, id, call)
			r.mu.Unlock()
			if pending == nil || !emit {
				t.Fatal("queued call was not registered")
			}
			r.queueEmission(b.emit, pending, appwire.AgentsToolCall{Agent: door.agent, Call: id, TurnCall: call.TurnCall})
			switch ending {
			case "deadline":
				r.mu.Lock()
				pending.answerBy = time.Now().Add(-time.Second)
				r.mu.Unlock()
			case "turn end":
				cancelQueued()
			case "disconnect":
				r.disconnect()
			case "result":
				ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: id, Result: json.RawMessage(`true`)})
				if err != nil || !ack.Accepted {
					t.Fatalf("queued answer: %+v %v", ack, err)
				}
			}
			done := make(chan session.ClientToolAnswer, 1)
			go func() { done <- r.wait(queuedCtx, id, pending) }()
			answer := clientToolAwait(t, done)
			if answer.IsError != (ending != "result") {
				t.Fatalf("queued ending: %+v", answer)
			}
			unblock()
			if ending != "disconnect" {
				call.TurnCall = "after-queue"
				after := clientToolStart(context.Background(), door, call)
				event := clientToolAwait(t, emitter.seen)
				if event.TurnCall != "after-queue" {
					t.Fatalf("ended call left the queue: %+v", event)
				}
				r.disconnect()
				clientToolAwait(t, after)
			}
			clientToolAwait(t, r.senderDone)
			select {
			case event := <-emitter.seen:
				t.Fatalf("extra event after queue drain: %+v", event)
			default:
			}
		})
	}
}

// Contract (e), (h): replacement is atomic and removal affects only its own set.
func TestClientToolsSetReplacementRemovalAndValidation(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	clientToolAttach(t, b, door)
	if _, err := b.SetClientTools(clientToolParams(door.agent, "second", "app_other")); err != nil {
		t.Fatal(err)
	}
	beforeVersion, before := door.ClientToolSets()
	for _, p := range []appwire.ClientToolsSetParams{
		clientToolParams(door.agent, "notes", "factory_add"),
		clientToolParams(door.agent, "notes", "app_other"),
		clientToolParams(door.agent, "notes", "change_setting"),
	} {
		if _, err := b.SetClientTools(p); err == nil || !strings.Contains(err.Error(), p.Tools[0].Name) {
			t.Fatalf("collision accepted: %v", err)
		}
		version, sets := door.ClientToolSets()
		if version != beforeVersion || !reflect.DeepEqual(sets, before) {
			t.Fatal("refusal changed the registry")
		}
	}
	replacement := clientToolParams(door.agent, "notes", "app_replaced")
	if _, err := b.SetClientTools(replacement); err != nil {
		t.Fatal(err)
	}
	replacement.Tools[0].InputSchema[0] = '['
	_, snapshots := door.ClientToolSets()
	if !json.Valid(snapshots[0].Tools[0].InputSchema) {
		t.Fatal("registry kept the caller's mutable schema")
	}
	removed, err := b.SetClientTools(appwire.ClientToolsSetParams{Agent: door.agent, Set: "notes"})
	if err != nil || len(removed.Tools) != 0 {
		t.Fatalf("removal: %+v %v", removed, err)
	}
	_, sets := door.ClientToolSets()
	if len(sets) != 1 || sets[0].ID != "second" {
		t.Fatal("removal changed another set")
	}
	answer := clientToolAwait(t, clientToolStart(context.Background(), door, session.ClientToolCall{Set: "notes", Name: "app_replaced", Args: json.RawMessage(`{}`)}))
	if !answer.IsError || answer.Text != "client_unavailable: the app that provides app_replaced is not connected; nothing was done." {
		t.Fatalf("retired closure: %+v", answer)
	}
}

// Contract (h): aggregate limits and remote scope do not open a conversation.
func TestClientToolsAgentLimitsAndScope(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	if _, err := b.SetClientTools(clientToolParams("missing", "notes", "app_note")); err == nil {
		t.Fatal("unknown agent accepted")
	}
	for i := 0; i < session.ClientToolMaxSets; i++ {
		if _, err := b.SetClientTools(clientToolParams(door.agent, fmt.Sprintf("set%d", i), fmt.Sprintf("app_%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.SetClientTools(clientToolParams(door.agent, "fifth", "app_fifth")); err == nil {
		t.Fatal("fifth set accepted")
	}
	b.clientTools.mu.Lock()
	b.clientTools.agents[door.agent] = clientToolAgentSets{}
	b.clientTools.mu.Unlock()
	for i := 0; i < 2; i++ {
		p := clientToolParams(door.agent, fmt.Sprintf("set%d", i), "unused")
		p.Tools = nil
		for j := 0; j < session.ClientToolMaxPerSet; j++ {
			p.Tools = append(p.Tools, clientToolParams(door.agent, p.Set, fmt.Sprintf("app_%d_%d", i, j)).Tools[0])
		}
		if _, err := b.SetClientTools(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.SetClientTools(clientToolParams(door.agent, "overflow", "app_extra")); err == nil {
		t.Fatal("65th tool accepted")
	}
	b.locked(func() { b.agents[door.agent].live.conn.Conv = &remote.Agent{} })
	if _, err := b.SetClientTools(clientToolParams(door.agent, "remote", "app_remote")); err == nil || !strings.Contains(err.Error(), "client tools need a conversation this engine runs") {
		t.Fatal(err)
	}
	b.locked(func() { b.agents[door.agent].live.conn.Assembled = true })
	if _, err := b.SetClientTools(appwire.ClientToolsSetParams{Agent: door.agent, Set: "set0"}); err != nil {
		t.Fatalf("local transport proxy refused: %v", err)
	}
	b.locked(func() { b.agents[door.agent].live = nil })
	if _, err := b.SetClientTools(appwire.ClientToolsSetParams{Agent: door.agent, Set: "set0"}); err != nil {
		t.Fatalf("cold attachment opened a conversation: %v", err)
	}
}

// Contract (d), source contract (b): pending joins retain the source without emitting.
func TestClientToolsPendingJoinAndCancelledAnswer(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	call := clientToolAttach(t, b, door)
	call.SourceMessage = "msg-pending"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := clientToolStart(ctx, door, call)
	event := clientToolAwait(t, emitter.seen)
	if event.SourceMessageID != call.SourceMessage {
		t.Fatalf("pending event lost source: %+v", event)
	}
	r := &b.clientTools
	r.mu.Lock()
	first := r.pending[event.Call]
	joined, _, emit := r.beginLocked(ctx, door.agent, event.Call, call)
	r.mu.Unlock()
	if first != joined || emit {
		t.Fatal("duplicate dispatch did not join")
	}
	cancel()
	ack, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`true`)})
	if err != nil || ack.Accepted {
		t.Fatalf("cancelled turn accepted an answer: %+v %v", ack, err)
	}
	if answer := clientToolAwait(t, done); !answer.IsError || !strings.Contains(answer.Text, "turn ended") {
		t.Fatal(answer)
	}
}

// Contract (h): local proxy inventories include dynamically armed engine names.
func TestClientToolsBuiltinInventoryFromLocalEngine(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	clientToolAttach(t, b, door)
	door.ClientToolBuiltins([]string{"app_reserved"})
	if _, err := b.SetClientTools(clientToolParams(door.agent, "other", "app_reserved")); err == nil || !strings.Contains(err.Error(), "app_reserved") {
		t.Fatal(err)
	}
}

// Contract (d): replay memory expires and never exceeds its entry budget.
func TestClientToolsSettledMemoryBoundAndExpiry(t *testing.T) {
	r := &appClientTools{pending: make(map[string]*clientToolPending)}
	for i := 0; i < clientToolSettledMax+5; i++ {
		id := fmt.Sprint(i)
		pending := &clientToolPending{done: make(chan struct{})}
		r.pending[id] = pending
		r.settleLocked(id, pending, session.ClientToolAnswer{Text: id})
	}
	if len(r.settled) != clientToolSettledMax || len(r.order) != clientToolSettledMax {
		t.Fatal("unbounded replay memory")
	}
	r.pruneLocked(time.Now().Add(clientToolSettledTTL))
	if len(r.settled) != 0 {
		t.Fatal("expired answers remain")
	}
	call := session.ClientToolCall{Set: "s", Name: "n", TurnCall: "p", Args: json.RawMessage(`{}`), SourceMessage: "m", DeadlineMS: 1000}
	shape := regexp.MustCompile(`^ct_[0-9a-f]{64}$`)
	same := call
	same.SourceMessage, same.DeadlineMS = "other message", 2000
	if id := clientToolCallID("a", call); !shape.MatchString(id) || id != clientToolCallID("a", same) {
		t.Fatalf("redispatch identity: %q", id)
	}
	for _, change := range []func(*session.ClientToolCall) string{
		func(c *session.ClientToolCall) string { c.TurnCall = "q"; return "b" },
		func(c *session.ClientToolCall) string { return "b" },
		func(c *session.ClientToolCall) string { c.Set = "t"; return "a" },
		func(c *session.ClientToolCall) string { c.Name = "o"; return "a" },
		func(c *session.ClientToolCall) string { c.Args = json.RawMessage(`{"x":1}`); return "a" },
		func(c *session.ClientToolCall) string { c.Set, c.Name = "s\x00n", ""; return "a" },
	} {
		other := call
		if agent := change(&other); clientToolCallID(agent, other) == clientToolCallID("a", call) {
			t.Fatalf("call identity collision: %+v", other)
		}
	}
	fresh := call
	fresh.TurnCall = ""
	if id := clientToolCallID("a", fresh); !shape.MatchString(id) || id == clientToolCallID("a", fresh) {
		t.Fatalf("fresh identity: %q", id)
	}
}

// Contract (b): compact JSON is bounded with the exact number of dropped bytes.
func TestClientToolsBoundedOutputAndKinds(t *testing.T) {
	data := []byte(`"` + strings.Repeat("é", session.ClientToolOutputMax) + `"`)
	text := session.BoundClientToolOutput(string(data))
	if !strings.HasSuffix(text, fmt.Sprintf("[cut: %d bytes dropped]", len(data)-(session.ClientToolOutputMax-1))) {
		t.Fatalf("cut marker: %.80s", text[len(text)-80:])
	}
	b, door, _ := clientToolBackend(t)
	clientToolAttach(t, b, door)
	if category := b.clientToolEventCategory(b.agents[door.agent], session.Event{Tool: "app_note"}); category != session.ActionEdit {
		t.Fatal(category)
	}
	if kind := appToolKind("app_note", "", session.ActionEdit); kind != appwire.ToolEdit {
		t.Fatal(kind)
	}
}

// Contract (i): the feature remains usable by any app, with no domain vocabulary.
func TestClientToolsSourcesStayGeneric(t *testing.T) {
	for _, path := range []string{"../../internal/appwire/client_tools.go", "../../internal/appwire/client_tools_test.go",
		"../../internal/session/client_tools.go", "../../internal/session/client_tools_test.go", "appclienttools.go", "appclienttools_test.go",
		"../../internal/manual/chat/client-tools.md"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(raw)), "work"+"stream") {
			t.Errorf("%s contains domain vocabulary", path)
		}
	}
}
