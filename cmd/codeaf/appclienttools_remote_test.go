package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type clientToolRemoteRig struct {
	backend *appBackend
	door    *appClientToolDoor
	emitter *clientToolEmitter
	hub     *remote.ClientToolHub
	held    *remote.Session
	agent   *appAgent
	loops   []*remote.Loop
	file    string
	root    string
}

func clientToolRemoteBackend(t *testing.T, endpoint string) *clientToolRemoteRig {
	t.Helper()
	return clientToolRemoteBackendSupport(t, endpoint, true)
}

func clientToolRemoteBackendSupport(t *testing.T, endpoint string, forwards bool) *clientToolRemoteRig {
	t.Helper()
	b, door, emitter := clientToolBackend(t)
	hub := &remote.ClientToolHub{}
	root := t.TempDir()
	file := filepath.Join(root, "transcript.jsonl")
	if endpoint == "" {
		endpoint = "https://example.invalid"
	}
	engineAgent, err := session.New(session.Config{Workspace: root, SessionFile: file,
		Model: "test/model", APIKey: "fixture", BaseURL: endpoint, OneModel: true,
		DeterministicNames: true, PromptProfile: "full", ClientTools: hub})
	if err != nil {
		t.Fatal(err)
	}
	engine := &remote.Engine{Agent: engineAgent, SessionFile: file, Workspace: root}
	if forwards {
		engine.ClientTools = hub
	}
	held := remote.NewSession(engine, true)
	a, err := b.find(door.agent)
	if err != nil {
		t.Fatal(err)
	}
	b.locked(func() { a.live = nil })
	rig := &clientToolRemoteRig{backend: b, door: door, emitter: emitter, hub: hub, held: held, agent: a, root: root, file: file}
	t.Cleanup(func() {
		a.gate.Lock()
		b.release(a)
		a.gate.Unlock()
		for _, loop := range rig.loops {
			_ = loop.Close()
		}
		_ = held.Close()
	})
	rig.hold(t)
	return rig
}

func (r *clientToolRemoteRig) hold(t *testing.T) *remote.Loop {
	t.Helper()
	loop, err := remote.LoopbackAttach(remote.Hello{Join: r.file}, remote.AttachOptions{
		Open: func(remote.Hello) (*remote.Session, error) { return r.held, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.loops = append(r.loops, loop)
	conn := &appConn{Conv: loop.Client.Agent(), Transcript: r.file, Workspace: r.root, Release: func() { _ = loop.Client.Close() }}
	r.agent.gate.Lock()
	r.backend.hold(r.agent, conn, r.root)
	r.agent.gate.Unlock()
	return loop
}

// C1, C2: a joined app sends declarations and returns results through the holder's model loop.
func TestClientToolsRemoteJoinedModelRoundTrip(t *testing.T) {
	for _, isError := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%t", isError), func(t *testing.T) {
			var mu sync.Mutex
			var requests []json.RawMessage
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				var request struct {
					Messages []struct {
						Role string `json:"role"`
					} `json:"messages"`
					Tools []json.RawMessage `json:"tools"`
				}
				if err := json.Unmarshal(body, &request); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				mu.Lock()
				first := len(request.Tools) > 0 && len(requests) == 0
				if len(request.Tools) > 0 {
					requests = append(requests, body)
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if first {
					fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"provider-remote-1","type":"function","function":{"name":"app_note","arguments":"{}"}}]},"finish_reason":null}]}`+"\n\n")
					fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
				} else {
					fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(provider.Close)
			rig := clientToolRemoteBackend(t, provider.URL)
			if _, err := rig.backend.SetClientTools(clientToolParams(rig.door.agent, "notes", "app_note")); err != nil {
				t.Fatal(err)
			}
			rig.emitter.answer = func(event appwire.AgentsToolCall) {
				params := appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`{ "saved": true }`)}
				if isError {
					text := "no access"
					params.Result, params.Error = nil, &text
				}
				ack, err := rig.backend.ClientToolResult(params)
				if err != nil || !ack.Accepted {
					t.Errorf("answer: %+v %v", ack, err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			events, err := rig.loops[0].Client.Agent().Submit(ctx, "use the app note")
			if err != nil {
				t.Fatal(err)
			}
			for {
				select {
				case event, open := <-events:
					if !open {
						goto finished
					}
					if event.Kind == session.EventError {
						t.Errorf("turn: %v", event.Err)
					}
				case <-ctx.Done():
					t.Fatal("joined model loop did not finish")
				}
			}
		finished:
			call := clientToolAwait(t, rig.emitter.seen)
			if call.Call != clientToolCallID(rig.door.agent, "provider-remote-1") || call.TurnCall != "provider-remote-1" || call.SourceMessageID == "" {
				t.Fatalf("call identity: %+v", call)
			}
			matchedMessage := false
			for _, entry := range appEntriesOf(rig.door.agent, rig.loops[0].Client.Agent().Transcript(), time.Time{}, nil) {
				matchedMessage = matchedMessage || entry.Kind == "user" && entry.Text == "use the app note" && entry.MessageID == call.SourceMessageID
			}
			if !matchedMessage {
				t.Fatalf("call did not name the holder's visible message: %+v", call)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("model requests: %d", len(requests))
			}
			if !strings.Contains(string(requests[0]), "Tools from the app: notes") || !strings.Contains(string(requests[0]), "Use this set.") || !strings.Contains(string(requests[0]), "app_note") {
				t.Fatalf("holder belt or instructions omitted: %s", requests[0])
			}
			var resultRequest struct {
				Messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(requests[1], &resultRequest); err != nil {
				t.Fatal(err)
			}
			want := `{"saved":true}`
			if isError {
				want = "notes could not do app_note: no access"
			}
			found := false
			for _, message := range resultRequest.Messages {
				found = found || message.Role == "tool" && strings.Contains(clientToolWireText(message.Content), want)
			}
			if !found {
				t.Fatalf("model did not receive %q: %s", want, requests[1])
			}
			rig.emitter.mu.Lock()
			count := len(rig.emitter.calls)
			rig.emitter.mu.Unlock()
			if count != 1 {
				t.Fatalf("one model call emitted %d events", count)
			}
		})
	}
}

// C3: losing the remote link settles the app registry and rejects later answers.
func TestClientToolsRemoteAppDropSettlesPending(t *testing.T) {
	rig := clientToolRemoteBackend(t, "")
	call := clientToolAttach(t, rig.backend, rig.door)
	answer := make(chan session.ClientToolAnswer, 1)
	go func() {
		got, _ := rig.hub.CallClientTool(context.Background(), call)
		answer <- got
	}()
	event := clientToolAwait(t, rig.emitter.seen)
	_ = rig.loops[0].Client.Close()
	got := clientToolAwait(t, answer)
	if !got.IsError || !strings.HasPrefix(got.Text, "client_unavailable:") {
		t.Fatalf("holder did not settle: %+v", got)
	}
	ack, err := rig.backend.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`true`)})
	if err != nil || ack.Accepted {
		t.Fatalf("late app answer: %+v %v", ack, err)
	}
	rig.backend.clientTools.mu.Lock()
	_, pending := rig.backend.clientTools.pending[event.Call]
	rig.backend.clientTools.mu.Unlock()
	if pending {
		t.Fatal("app retained a pending call after losing its link")
	}
}

// C2: transport preserves the app's bounded JSON and its original dropped-byte count.
func TestClientToolsRemoteBoundedResult(t *testing.T) {
	rig := clientToolRemoteBackend(t, "")
	call := clientToolAttach(t, rig.backend, rig.door)
	data := json.RawMessage(`"` + strings.Repeat("é", session.ClientToolOutputMax) + `"`)
	rig.emitter.answer = func(event appwire.AgentsToolCall) {
		ack, err := rig.backend.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: data})
		if err != nil || !ack.Accepted {
			t.Errorf("bounded result: %+v %v", ack, err)
		}
	}
	answer := make(chan session.ClientToolAnswer, 1)
	go func() {
		got, _ := rig.hub.CallClientTool(context.Background(), call)
		answer <- got
	}()
	got := clientToolAwait(t, answer)
	want := fmt.Sprintf("[cut: %d bytes dropped]", len(data)-(session.ClientToolOutputMax-1))
	if got.IsError || !strings.HasSuffix(got.Text, want) || strings.Count(got.Text, "[cut:") != 1 {
		t.Fatalf("forwarding changed the truncation receipt: %.80s", got.Text[max(0, len(got.Text)-80):])
	}
}

// C4: every hold sends the current registry, without another tools_set.
func TestClientToolsRemoteReholdPushesSets(t *testing.T) {
	rig := clientToolRemoteBackend(t, "")
	clientToolAttach(t, rig.backend, rig.door)
	rig.agent.gate.Lock()
	rig.backend.release(rig.agent)
	rig.agent.gate.Unlock()
	_ = rig.loops[0].Close()
	if _, sets := rig.hub.ClientToolSets(); len(sets) != 0 {
		t.Fatal("old link left its sets attached")
	}
	rig.hold(t)
	if _, sets := rig.hub.ClientToolSets(); len(sets) != 1 || sets[0].Tools[0].Name != "app_note" {
		t.Fatalf("re-hold snapshot: %+v", sets)
	}
}

// C6, C7: old holders retain their refusal, while relay-held attachments stay cold.
func TestClientToolsRemoteScopeAndElsewhereStorage(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	a, _ := b.find(door.agent)
	b.locked(func() { a.live.conn.Conv = &remote.Agent{} })
	_, err := b.SetClientTools(clientToolParams(door.agent, "notes", "app_note"))
	var refusal *appwire.Error
	if !errors.As(err, &refusal) || refusal.Code != appwire.CodeBadScope || refusal.Message != "client tools need a conversation this engine runs" {
		t.Fatalf("old holder refusal: %v", err)
	}
	if _, sets := door.ClientToolSets(); len(sets) != 0 {
		t.Fatal("scope refusal changed registry")
	}
	b.locked(func() { a.view.Location = &appwire.Location{Here: false} })
	if _, err := b.SetClientTools(clientToolParams(door.agent, "notes", "app_note")); err != nil {
		t.Fatalf("relay-held attachment: %v", err)
	}
	b.locked(func() {
		a.view.Location.Here = true
		a.live = nil
	})
	if _, sets := door.ClientToolSets(); len(sets) != 1 || sets[0].Tools[0].Name != "app_note" {
		t.Fatalf("sets after returning here: %+v", sets)
	}
	older := clientToolRemoteBackendSupport(t, "", false)
	_, err = older.backend.SetClientTools(clientToolParams(older.door.agent, "notes", "app_note"))
	if !errors.As(err, &refusal) || refusal.Code != appwire.CodeBadScope || refusal.Message != "client tools need a conversation this engine runs" {
		t.Fatalf("older live holder refusal: %v", err)
	}
}

// C3: a holder's turn-ended frame cancels the app wait before its set's long limit.
func TestClientToolsRemoteAppTurnEnded(t *testing.T) {
	rig := clientToolRemoteBackend(t, "")
	p := clientToolParams(rig.door.agent, "notes", "app_note")
	p.DeadlineMS = 120000
	if _, err := rig.backend.SetClientTools(p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	answer := make(chan session.ClientToolAnswer, 1)
	go func() {
		got, _ := rig.hub.CallClientTool(ctx, session.ClientToolCall{Set: "notes", Name: "app_note", TurnCall: "ended-call", Args: json.RawMessage(`{}`)})
		answer <- got
	}()
	event := clientToolAwait(t, rig.emitter.seen)
	cancel()
	got := clientToolAwait(t, answer)
	if !got.IsError || !strings.Contains(got.Text, "the turn ended") {
		t.Fatalf("holder answer: %+v", got)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		rig.backend.clientTools.mu.Lock()
		pending := rig.backend.clientTools.pending[event.Call] != nil
		rig.backend.clientTools.mu.Unlock()
		if !pending {
			break
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("holder end frame did not clear the app wait")
		}
	}
	ack, err := rig.backend.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`true`)})
	if err != nil || ack.Accepted {
		t.Fatalf("late result after holder cancellation: %+v %v", ack, err)
	}
}

// C8: the app's real hello advertises forwarding alongside its existing features.
func TestClientToolsRemoteHelloFeature(t *testing.T) {
	features, _ := slashCatalogOverTheWire(t)
	for _, want := range []string{appwire.FeatureClientTools, appwire.FeatureClientToolsSource, appwire.FeatureClientToolsRemote} {
		found := false
		for _, feature := range features {
			found = found || feature == want
		}
		if !found {
			t.Fatalf("hello omitted %s: %v", want, features)
		}
	}
}

// C1: the holder's additional collision checks refuse without changing the app registry.
func TestClientToolsRemoteHolderRefusalIsAtomic(t *testing.T) {
	rig := clientToolRemoteBackend(t, "")
	rig.hub.ClientToolBuiltins([]string{"app_note"})
	_, err := rig.backend.SetClientTools(clientToolParams(rig.door.agent, "notes", "app_note"))
	var refusal *appwire.Error
	if !errors.As(err, &refusal) || refusal.Code != appwire.CodeBadParams || !strings.Contains(refusal.Message, "app_note") {
		t.Fatalf("holder refusal: %v", err)
	}
	if _, sets := rig.door.ClientToolSets(); len(sets) != 0 {
		t.Fatal("holder refusal changed the app registry")
	}
	if _, sets := rig.hub.ClientToolSets(); len(sets) != 0 {
		t.Fatal("holder refusal changed its own snapshot")
	}
}

// C1: a transport failure is retryable and no registry lock is held on the wire.
func TestClientToolsRemoteTransportFailureLeavesRegistry(t *testing.T) {
	b, door, _ := clientToolBackend(t)
	surface, holder := remote.Pipe()
	t.Cleanup(func() {
		_ = surface.Close()
		_ = holder.Close()
	})
	readRegistry := make(chan struct{})
	go func() {
		defer holder.Close()
		reader := json.NewDecoder(holder)
		var hello remote.Frame
		if reader.Decode(&hello) != nil {
			return
		}
		payload, _ := json.Marshal(remote.Welcome{Version: remote.Version, ClientTools: true})
		if json.NewEncoder(holder).Encode(remote.Frame{Kind: "welcome", Payload: payload}) != nil {
			return
		}
		var call remote.Frame
		if reader.Decode(&call) != nil || call.Method != remote.MethodClientToolsSet {
			return
		}
		_, _ = door.ClientToolSets()
		close(readRegistry)
	}()
	client, err := remote.Dial(surface, "engine", remote.Hello{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	b.locked(func() { b.agents[door.agent].live.conn.Conv = client.Agent() })
	failed := make(chan error, 1)
	go func() {
		_, err := b.SetClientTools(clientToolParams(door.agent, "notes", "app_note"))
		failed <- err
	}()
	err = clientToolAwait(t, failed)
	var refusal *appwire.Error
	if !errors.As(err, &refusal) || refusal.Code != appwire.CodeUnreachable {
		t.Fatalf("transport failure: %v", err)
	}
	clientToolAwait(t, readRegistry)
	if _, sets := door.ClientToolSets(); len(sets) != 0 {
		t.Fatal("a failed transport committed the app snapshot")
	}
}

// C4: each opening uses the same host hub, while the app retains its own door.
func TestClientToolsRemoteEngineOpenings(t *testing.T) {
	previous := appClientToolsBackend.Swap(nil)
	t.Cleanup(func() { appClientToolsBackend.Store(previous) })
	var doors []session.ClientToolDoor
	open := func(cfg session.Config) (*session.Agent, error) {
		doors = append(doors, cfg.ClientTools)
		return nil, nil
	}
	hostOpen, hub := appClientToolsEngineOpener(open)
	if hub == nil {
		t.Fatal("a host process has no forwarding hub")
	}
	for _, file := range []string{"initial", "fresh", "resumed"} {
		_, _ = hostOpen(session.Config{SessionFile: file})
	}
	for _, door := range doors {
		if door != hub {
			t.Fatal("an opening lost the conversation's hub")
		}
	}
	b, _, _ := clientToolBackend(t)
	appClientToolsBackend.Store(b)
	appOpen, appHub := appClientToolsEngineOpener(open)
	if appHub != nil {
		t.Fatal("the app replaced its own registry with a host hub")
	}
	_, _ = appOpen(session.Config{SessionFile: "app-conversation"})
	if _, ok := doors[len(doors)-1].(*appClientToolDoor); !ok {
		t.Fatal("the app lost its existing conversation door")
	}
}
