package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
	mu     sync.Mutex
	calls  []appwire.AgentsToolCall
	seen   chan appwire.AgentsToolCall
	answer func(appwire.AgentsToolCall)
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
	e.seen <- call
	if e.answer != nil {
		e.answer(call)
	}
}

func clientToolBackend(t *testing.T) (*appBackend, *appClientToolDoor, *clientToolEmitter) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	e := &clientToolEmitter{seen: make(chan appwire.AgentsToolCall, 20)}
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

// Contract (b), (d): fast answers, duplicate dispatches and replay share one event.
func TestClientToolsEventResultAndReplay(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	call := clientToolAttach(t, b, door)
	first := clientToolStart(context.Background(), door, call)
	event := clientToolAwait(t, emitter.seen)
	if event.Agent != door.agent || event.Turn != 7 || event.TurnCall != call.TurnCall || event.Set != call.Set || event.Name != call.Name ||
		string(event.Args) != string(call.Args) || event.DeadlineMS != session.ClientToolDefaultDeadlineMS || event.Call != clientToolCallID(door.agent, call.TurnCall) {
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
			id := clientToolCallID(door.agent, call.TurnCall)
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
				pending.deadline = time.Now().Add(-time.Second)
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

// Contract (d): a concurrent dispatch joins the actual pending object without emitting.
func TestClientToolsPendingJoinAndCancelledAnswer(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	call := clientToolAttach(t, b, door)
	ctx, cancel := context.WithCancel(context.Background())
	done := clientToolStart(ctx, door, call)
	event := clientToolAwait(t, emitter.seen)
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
	if clientToolCallID("a", "p") == clientToolCallID("b", "p") || clientToolCallID("a", "") == clientToolCallID("a", "") {
		t.Fatal("call identity collision")
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
