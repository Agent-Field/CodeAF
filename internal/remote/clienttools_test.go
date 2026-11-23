package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func clientToolTestSet(id, name string) session.ClientToolSet {
	return session.ClientToolSet{ID: id, Instructions: "Use the attached tool.", DeadlineMS: 1000,
		Tools: []session.ClientTool{{Name: name, Description: "An app action",
			InputSchema: json.RawMessage(`{"type":"object"}`), Category: session.ActionEdit}}}
}

func clientToolTestPair(t *testing.T, held *Session) *Loop {
	t.Helper()
	loop, err := LoopbackAttach(Hello{}, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// C1, C6, C9: capability discovery precedes attachment, and refusals are atomic.
func TestClientToolsRemoteCapabilityAndSnapshot(t *testing.T) {
	hub := &ClientToolHub{}
	engine := engineOn(&fakeAgent{model: "test/model"})
	engine.ClientTools = hub
	held := NewSession(engine, true)
	t.Cleanup(func() { _ = held.Close() })
	loop := clientToolTestPair(t, held)
	if !loop.Client.Agent().SupportsClientTools() {
		t.Fatal("holder did not advertise forwarding")
	}
	if _, sets := hub.ClientToolSets(); len(sets) != 0 {
		t.Fatal("an unattached conversation has client tools")
	}
	set := clientToolTestSet("notes", "app_note")
	if names, err := loop.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil || len(names) != 1 || names[0] != "app_note" {
		t.Fatalf("attach: %v %v", names, err)
	}
	version, sets := hub.ClientToolSets()
	if len(sets) != 1 || sets[0].Instructions != set.Instructions || sets[0].Tools[0].Name != "app_note" {
		t.Fatalf("snapshot: %+v", sets)
	}
	set.Tools[0].InputSchema = json.RawMessage(`{"type":"string"}`)
	if _, err := loop.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err == nil {
		t.Fatal("holder accepted an invalid schema")
	}
	if next, _ := hub.ClientToolSets(); next != version {
		t.Fatal("a refusal changed the snapshot")
	}
	oldHeld := NewSession(engineOn(&fakeAgent{}), true)
	t.Cleanup(func() { _ = oldHeld.Close() })
	older := clientToolTestPair(t, oldHeld)
	before := older.CallsMade()
	if _, err := older.Client.Agent().SetClientTools(context.Background(), nil); !errors.Is(err, ErrClientToolsUnsupported) {
		t.Fatalf("old holder: %v", err)
	}
	if older.CallsMade() != before {
		t.Fatal("sent a new method to an old holder")
	}
}

// C3: all three departures remove sets and settle calls before the ordinary wait.
func TestClientToolsRemoteDisconnectDetachAndClose(t *testing.T) {
	for _, road := range []string{"pipe", "detach", "close"} {
		t.Run(road, func(t *testing.T) {
			hub := &ClientToolHub{}
			engine := engineOn(&fakeAgent{})
			engine.ClientTools = hub
			held := NewSession(engine, true)
			t.Cleanup(func() { _ = held.Close() })
			loop := clientToolTestPair(t, held)
			set := clientToolTestSet("notes", "app_note")
			set.DeadlineMS = 120000
			if _, err := loop.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil {
				t.Fatal(err)
			}
			entered, ended := make(chan struct{}), make(chan struct{})
			loop.Client.Agent().SetClientToolHandler(func(ctx context.Context, _ session.ClientToolCall) (session.ClientToolAnswer, error) {
				close(entered)
				<-ctx.Done()
				close(ended)
				return session.ClientToolAnswer{}, nil
			})
			answer := clientToolTestCall(hub, context.Background(), "app_note")
			clientToolTestSignal(t, entered)
			switch road {
			case "pipe":
				_ = loop.Client.Close()
			case "detach":
				if err := loop.Client.Agent().Detach(); err != nil {
					t.Fatal(err)
				}
			case "close":
				if err := loop.Client.Agent().Close(); err != nil {
					t.Fatal(err)
				}
			}
			got := clientToolTestAnswer(t, answer)
			if !got.IsError || got.Text != "client_unavailable: the app that provides app_note disconnected before answering; the outcome is unknown, so check before trying again." {
				t.Fatalf("departure answer: %+v", got)
			}
			if _, sets := hub.ClientToolSets(); len(sets) != 0 {
				t.Fatal("departed connection still has sets")
			}
			_ = loop.Client.Close()
			clientToolTestSignal(t, ended)
		})
	}
}

func clientToolTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("client tool signal did not arrive")
	}
}

type clientToolRawSurface struct {
	conn       net.Conn
	owner      string
	frames     chan Frame
	errors     chan error
	next       uint64
	callFrames atomic.Int32
}

func clientToolTestRaw(t *testing.T, held *Session, watch bool) *clientToolRawSurface {
	t.Helper()
	return clientToolTestRawOutput(t, held, watch, func(out io.Writer) io.Writer { return out })
}

func clientToolTestRawOutput(t *testing.T, held *Session, watch bool, wrap func(io.Writer) io.Writer) *clientToolRawSurface {
	t.Helper()
	surface, engine := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer engine.Close()
		_ = ServeAttach(engine, wrap(engine), AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
	}()
	t.Cleanup(func() {
		_ = surface.Close()
		<-done
	})
	raw := &clientToolRawSurface{conn: surface, owner: newClientID(), frames: make(chan Frame, 64), errors: make(chan error, 1)}
	go func() {
		reader := json.NewDecoder(surface)
		for {
			var frame Frame
			if err := reader.Decode(&frame); err != nil {
				raw.errors <- err
				return
			}
			if frame.Kind == clientToolCallKind {
				raw.callFrames.Add(1)
			}
			raw.frames <- frame
		}
	}()
	if err := json.NewEncoder(surface).Encode(Frame{Kind: "hello", Payload: mustJSON(Hello{Version: Version, Watch: watch})}); err != nil {
		t.Fatal(err)
	}
	raw.frame(t, "welcome")
	return raw
}

func (r *clientToolRawSurface) frame(t *testing.T, kind string) Frame {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame := <-r.frames:
			if frame.Kind == kind {
				return frame
			}
		case err := <-r.errors:
			t.Fatal(err)
		case <-timer.C:
			t.Fatalf("no %s frame", kind)
		}
	}
}

func (r *clientToolRawSurface) call(t *testing.T, method string, args any) Frame {
	t.Helper()
	if method == MethodClientToolsSet {
		args = clientToolSetsWire{Owner: r.owner, Sets: args.([]session.ClientToolSet)}
	}
	r.next++
	if err := json.NewEncoder(r.conn).Encode(Frame{Kind: "call", ID: r.next, Method: method, Payload: mustJSON(args)}); err != nil {
		t.Fatal(err)
	}
	return r.frame(t, "result")
}

// C3, C5: cancellation reaches the owner, and foreign or late answers change nothing.
func TestClientToolsRemoteWaitEndAndAnswerOwnership(t *testing.T) {
	for _, timedOut := range []bool{false, true} {
		t.Run(fmt.Sprintf("timeout=%t", timedOut), func(t *testing.T) {
			hub := &ClientToolHub{}
			engine := engineOn(&fakeAgent{})
			engine.ClientTools = hub
			held := NewSession(engine, true)
			t.Cleanup(func() { _ = held.Close() })
			owner := clientToolTestRaw(t, held, false)
			foreign := clientToolTestPair(t, held)
			if reply := owner.call(t, MethodClientToolsSet, []session.ClientToolSet{clientToolTestSet("notes", "app_note")}); reply.Error != "" {
				t.Fatal(reply.Error)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			answer := clientToolTestCall(hub, ctx, "app_note")
			var call clientToolCallWire
			if err := json.Unmarshal(owner.frame(t, clientToolCallKind).Payload, &call); err != nil {
				t.Fatal(err)
			}
			payload, err := foreign.Client.call(ctx, MethodClientToolAnswer, clientToolAnswerWire{Call: call.Call, Text: "wrong connection"})
			if err != nil || string(payload) != `{"accepted":false}` {
				t.Fatalf("foreign answer: %s %v", payload, err)
			}
			select {
			case got := <-answer:
				t.Fatalf("foreign answer settled the call: %+v", got)
			default:
			}
			if !timedOut {
				cancel()
			}
			got := clientToolTestAnswer(t, answer)
			want := "the turn ended before the app answered app_note; the outcome is unknown, so check before trying again."
			if timedOut {
				want = "client_timeout: the app that provides app_note did not answer within 1s; the outcome is unknown, so check before trying again."
			}
			if !got.IsError || got.Text != want {
				t.Fatalf("wait answer: %+v", got)
			}
			var ended clientToolEndedWire
			if err := json.Unmarshal(owner.frame(t, clientToolEndedKind).Payload, &ended); err != nil || ended.Call != call.Call {
				t.Fatalf("call-ended frame: %+v %v", ended, err)
			}
			late := owner.call(t, MethodClientToolAnswer, clientToolAnswerWire{Call: call.Call, Text: "too late"})
			if late.Error != "" || string(late.Payload) != `{"accepted":false}` {
				t.Fatalf("late answer: %+v", late)
			}
		})
	}
}

// C1, C5: holder validation applies to raw surfaces and counts all connections.
func TestClientToolsRemoteHolderValidationAndAggregateLimits(t *testing.T) {
	hub := &ClientToolHub{}
	hub.ClientToolBuiltins([]string{"fixture_builtin"})
	engine := engineOn(&fakeAgent{})
	engine.ClientTools = hub
	held := NewSession(engine, true)
	t.Cleanup(func() { _ = held.Close() })
	a, b := clientToolTestPair(t, held), clientToolTestPair(t, held)
	ctx := context.Background()
	for _, change := range []func(*session.ClientToolSet){
		func(s *session.ClientToolSet) { s.ID = "Bad id" },
		func(s *session.ClientToolSet) {
			s.Instructions = strings.Repeat("x", session.ClientToolInstructionsMax+1)
		},
		func(s *session.ClientToolSet) { s.DeadlineMS = 999 },
		func(s *session.ClientToolSet) { s.DeadlineMS = 120001 },
		func(s *session.ClientToolSet) { s.Tools[0].Name = "Bad-name" },
		func(s *session.ClientToolSet) { s.Tools[0].Description = "" },
		func(s *session.ClientToolSet) { s.Tools[0].InputSchema = json.RawMessage(`null`) },
		func(s *session.ClientToolSet) { s.Tools[0].InputSchema = json.RawMessage(`{"type":"array"}`) },
		func(s *session.ClientToolSet) { s.Tools[0].Category = session.ActionWork },
		func(s *session.ClientToolSet) { s.Tools[0].Name = "fixture_builtin" },
		func(s *session.ClientToolSet) { s.Tools[0].Name = "bash" },
	} {
		set := clientToolTestSet("notes", "app_note")
		change(&set)
		if _, err := a.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{set}); err == nil {
			t.Fatalf("holder accepted invalid declaration: %+v", set)
		}
		if _, sets := hub.ClientToolSets(); len(sets) != 0 {
			t.Fatal("refusal partially attached tools")
		}
	}
	sets := []session.ClientToolSet{}
	for i := range 4 {
		sets = append(sets, clientToolTestSet(fmt.Sprintf("set%d", i), fmt.Sprintf("app_%d", i)))
	}
	if _, err := a.Client.Agent().SetClientTools(ctx, sets); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("extra", "app_extra")}); err == nil {
		t.Fatal("aggregate fifth set accepted")
	}
	for i := range 2 {
		sets[i].Tools = nil
		for j := range 32 {
			tool := clientToolTestSet("unused", fmt.Sprintf("app_%d_%d", i, j)).Tools[0]
			sets[i].Tools = append(sets[i].Tools, tool)
		}
	}
	if _, err := a.Client.Agent().SetClientTools(ctx, sets[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("extra", "app_extra")}); err == nil {
		t.Fatal("aggregate 65th tool accepted")
	}
	watcher := clientToolTestRaw(t, held, true)
	if reply := watcher.call(t, MethodClientToolsSet, []session.ClientToolSet{}); reply.Error == "" {
		t.Fatal("a watcher changed the belt")
	}
}

// C4: roaming republishes its last acknowledged snapshot on the replacement link.
func TestClientToolsRemoteRedialRestoresSnapshot(t *testing.T) {
	hub := &ClientToolHub{}
	engine := engineOn(&fakeAgent{})
	engine.ClientTools = hub
	held := NewSession(engine, true)
	t.Cleanup(func() { _ = held.Close() })
	var mu sync.Mutex
	var first io.ReadWriteCloser
	dialled := 0
	client, err := Roam("engine", Hello{}, Roaming{Window: 3 * time.Second, Dial: func() (io.ReadWriteCloser, error) {
		surface, pipe := Pipe()
		func() {
			mu.Lock()
			defer mu.Unlock()
			dialled++
			if first == nil {
				first = surface
			}
		}()
		go func() {
			defer pipe.Close()
			_ = ServeAttach(pipe, pipe, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
		}()
		return surface, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{clientToolTestSet("notes", "app_note")}); err != nil {
		t.Fatal(err)
	}
	version, _ := hub.ClientToolSets()
	initial := func() io.ReadWriteCloser {
		mu.Lock()
		defer mu.Unlock()
		return first
	}()
	_ = initial.Close()
	waitFor(t, "client tool snapshot after redial", func() bool {
		mu.Lock()
		defer mu.Unlock()
		redialled := dialled > 1
		next, sets := hub.ClientToolSets()
		return redialled && next > version && len(sets) == 1 && sets[0].Tools[0].Name == "app_note"
	})
}

// C3, C4, C5: a replay moves ownership before the half-open old link detaches.
func TestClientToolsRemoteRedialMovesAttachedOwner(t *testing.T) {
	hub := &ClientToolHub{}
	held := NewSession(&Engine{Agent: &fakeAgent{}, ClientTools: hub, SessionFile: "/conversation.jsonl"}, true)
	t.Cleanup(func() { _ = held.Close() })
	var first net.Conn
	var dialled atomic.Int32
	served := make(chan struct{}, 8)
	oldDetached := make(chan struct{})
	client, err := Roam("engine", Hello{}, Roaming{Window: 3 * time.Second, Dial: func() (io.ReadWriteCloser, error) {
		n := dialled.Add(1)
		surface, pipe := net.Pipe()
		if n == 1 {
			first = surface
		}
		go func() {
			defer func() { served <- struct{}{} }()
			defer pipe.Close()
			if n == 1 {
				defer close(oldDetached)
			}
			_ = ServeAttach(pipe, pipe, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
		}()
		return surface, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = first.Close()
		for range int(dialled.Load()) {
			clientToolTestSignal(t, served)
		}
	})
	set := clientToolTestSet("notes", "app_note")
	set.DeadlineMS = 120000
	if _, err := client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil {
		t.Fatal(err)
	}
	version, _ := hub.ClientToolSets()
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	client.Agent().SetClientToolHandler(func(ctx context.Context, _ session.ClientToolCall) (session.ClientToolAnswer, error) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			<-ctx.Done()
			return session.ClientToolAnswer{}, ctx.Err()
		}
		return session.ClientToolAnswer{Text: "replacement connection"}, nil
	})
	pending := clientToolTestCall(hub, context.Background(), "app_note")
	clientToolTestSignal(t, entered)
	if err := first.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reader leaving the old connection", func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client.reconnecting
	})
	if err := first.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Keep the old server attached and drain its room notices independently of
	// the roaming reader, so its ordinary driver broadcast can finish as usual.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(io.Discard, first)
	}()
	waitFor(t, "replacement replay while old owner is attached", func() bool {
		held.mu.Lock()
		defer held.mu.Unlock()
		next, sets := hub.ClientToolSets()
		return dialled.Load() == 2 && len(held.surfaces) == 2 && next > version && len(sets) == 1
	})
	got := clientToolTestAnswer(t, pending)
	if !got.IsError || got.Text != "client_unavailable: the app that provides app_note disconnected before answering; the outcome is unknown, so check before trying again." {
		t.Fatalf("old owner's pending call: %+v", got)
	}
	_ = first.Close()
	clientToolTestSignal(t, drained)
	clientToolTestSignal(t, oldDetached)
	if _, sets := hub.ClientToolSets(); len(sets) != 1 {
		t.Fatal("old detach removed the replacement's tools")
	}
	got = clientToolTestAnswer(t, clientToolTestCall(hub, context.Background(), "app_note"))
	if got.IsError || got.Text != "replacement connection" {
		t.Fatalf("call after old detach: %+v", got)
	}
}

// C4: a changed welcome forgets the snapshot instead of attaching it elsewhere.
func TestClientToolsRemoteRedialConversationIdentity(t *testing.T) {
	for _, files := range [][2]string{{"/a.jsonl", "/b.jsonl"}, {"", ""}, {"/a.jsonl", "/a.jsonl"}} {
		t.Run(fmt.Sprintf("%s-to-%s", files[0], files[1]), func(t *testing.T) {
			aHub, bHub := &ClientToolHub{}, &ClientToolHub{}
			a := NewSession(&Engine{Agent: &fakeAgent{}, ClientTools: aHub, SessionFile: files[0]}, true)
			b := NewSession(&Engine{Agent: &fakeAgent{}, ClientTools: bHub, SessionFile: files[1]}, true)
			t.Cleanup(func() {
				_ = a.Close()
				_ = b.Close()
			})
			var first io.ReadWriteCloser
			var dialled atomic.Int32
			served := make(chan struct{}, 8)
			client, err := Roam("engine", Hello{}, Roaming{Window: 3 * time.Second, Dial: func() (io.ReadWriteCloser, error) {
				n := dialled.Add(1)
				surface, pipe := Pipe()
				held := b
				if n == 1 {
					first, held = surface, a
				}
				go func() {
					defer func() { served <- struct{}{} }()
					defer pipe.Close()
					_ = ServeAttach(pipe, pipe, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
				}()
				return surface, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = client.Close()
				_ = first.Close()
				for range int(dialled.Load()) {
					clientToolTestSignal(t, served)
				}
			})
			set := clientToolTestSet("notes", "app_note")
			set.DeadlineMS = 120000
			if _, err := client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil {
				t.Fatal(err)
			}
			callsBefore := client.CallsMade()
			entered, canceled := make(chan struct{}), make(chan struct{})
			client.Agent().SetClientToolHandler(func(ctx context.Context, _ session.ClientToolCall) (session.ClientToolAnswer, error) {
				close(entered)
				<-ctx.Done()
				close(canceled)
				return session.ClientToolAnswer{}, ctx.Err()
			})
			pending := clientToolTestCall(aHub, context.Background(), "app_note")
			clientToolTestSignal(t, entered)
			_ = first.Close()
			clientToolTestSignal(t, canceled)
			clientToolTestAnswer(t, pending)
			wantReplay := files[0] != "" && files[0] == files[1]
			waitFor(t, "client tool replay decision after changed welcome", func() bool {
				client.mu.Lock()
				defer client.mu.Unlock()
				if client.reconnecting || client.welcome.SessionInstance != b.instance {
					return false
				}
				_, sets := bHub.ClientToolSets()
				if wantReplay {
					return len(sets) == 1
				}
				return !client.clientTools.attached && len(client.clientTools.sets) == 0 && len(client.clientTools.incoming) == 0
			})
			if !wantReplay {
				if client.CallsMade() != callsBefore {
					t.Fatal("sent a tool snapshot to another conversation")
				}
				if _, sets := bHub.ClientToolSets(); len(sets) != 0 {
					t.Fatal("old tools reached another conversation")
				}
				got := clientToolTestAnswer(t, clientToolTestCall(bHub, context.Background(), "app_note"))
				if !got.IsError || !strings.HasPrefix(got.Text, "client_unavailable:") {
					t.Fatalf("another conversation used the old tools: %+v", got)
				}
			}
		})
	}
}

// C1, C2, C9: the real holder refreshes its belt and instructions at model steps.
func TestClientToolsRemoteModelBoundary(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	type request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	var mu sync.Mutex
	var requests []request
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got request
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		attached := false
		for _, tool := range got.Tools {
			attached = attached || tool.Function.Name == "app_note"
		}
		call := attached && len(got.Messages) > 0 && got.Messages[len(got.Messages)-1].Role == "user"
		if len(got.Tools) > 0 {
			func() {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, got)
			}()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if call {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"provider-boundary-1","type":"function","function":{"name":"app_note","arguments":"{}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(provider.Close)
	hub := &ClientToolHub{}
	agent, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"),
		Model: "test/model", APIKey: "fixture", BaseURL: provider.URL, OneModel: true,
		DeterministicNames: true, PromptProfile: "full", ClientTools: hub})
	if err != nil {
		t.Fatal(err)
	}
	held := NewSession(&Engine{Agent: agent, ClientTools: hub}, true)
	t.Cleanup(func() { _ = held.Close() })
	loop := clientToolTestPair(t, held)
	loop.Client.Agent().SetClientToolHandler(func(_ context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
		if call.SourceMessage == "" || call.TurnCall != "provider-boundary-1" {
			t.Errorf("model call identity: %+v", call)
		}
		return session.ClientToolAnswer{Text: `{"saved":true}`}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	turn := func() {
		t.Helper()
		events, err := loop.Client.Agent().Submit(ctx, "inspect the available tools")
		if err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case event, open := <-events:
				if !open {
					return
				}
				if event.Kind == session.EventError {
					t.Errorf("model turn: %v", event.Err)
				}
			case <-ctx.Done():
				t.Fatal("holder model turn did not finish")
			}
		}
	}
	turn()
	if _, err := loop.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("notes", "app_note")}); err != nil {
		t.Fatal(err)
	}
	turn()
	if _, err := loop.Client.Agent().SetClientTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
	turn()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("provider requests: %d", len(requests))
	}
	before, attached, after := requests[0], requests[1], requests[3]
	if len(before.Tools) != len(after.Tools) || len(attached.Tools) != len(before.Tools)+1 {
		t.Fatalf("belt counts: %d, %d, %d", len(before.Tools), len(attached.Tools), len(after.Tools))
	}
	for i := range before.Tools {
		if before.Tools[i].Function.Name != after.Tools[i].Function.Name {
			t.Fatal("a conversation without sets changed its belt")
		}
	}
	if strings.Contains(string(before.Messages[0].Content), "Tools from the app:") || strings.Contains(string(after.Messages[0].Content), "Tools from the app:") {
		t.Fatal("an empty hub added instructions")
	}
	if !strings.Contains(string(attached.Messages[0].Content), "Tools from the app: notes") || !strings.Contains(string(attached.Messages[0].Content), "Use the attached tool.") {
		t.Fatal("holder did not refresh its instructions")
	}
}

// C3: sets belong to one conversation. When another window swaps the session
// to a new or another conversation, the app that joined the old one must not
// lend its tools to the replacement, and a pending call ends at once.
func TestClientToolsRemoteSessionSwapDropsTheOldConversationsSets(t *testing.T) {
	for _, door := range []string{"new", "open"} {
		t.Run(door, func(t *testing.T) {
			hub := &ClientToolHub{}
			engine := &Engine{Agent: &fakeAgent{}, ClientTools: hub, SessionFile: "/a.jsonl",
				Fresh: func() (WrappedAgent, string, error) { return &fakeAgent{}, "/b.jsonl", nil },
				Open:  func(string) (WrappedAgent, bool, error) { return &fakeAgent{}, true, nil }}
			held := NewSession(engine, true)
			t.Cleanup(func() { _ = held.Close() })
			app, err := LoopbackAttach(Hello{Session: "/a.jsonl", Join: true}, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Close() })
			set := clientToolTestSet("notes", "app_note")
			set.DeadlineMS = 120000
			if _, err := app.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil {
				t.Fatal(err)
			}
			entered, ended := make(chan struct{}, 4), make(chan struct{}, 4)
			app.Client.Agent().SetClientToolHandler(func(ctx context.Context, _ session.ClientToolCall) (session.ClientToolAnswer, error) {
				entered <- struct{}{}
				<-ctx.Done()
				ended <- struct{}{}
				return session.ClientToolAnswer{}, ctx.Err()
			})
			pending := clientToolTestCall(hub, context.Background(), "app_note")
			clientToolTestSignal(t, entered)
			window := clientToolTestPair(t, held)
			if door == "new" {
				_, err = window.Client.NewSession()
			} else {
				_, err = window.Client.OpenSession("/c.jsonl")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, sets := hub.ClientToolSets(); len(sets) != 0 {
				t.Fatalf("the replacement conversation got the joined app's sets: %+v", sets)
			}
			got := clientToolTestAnswer(t, clientToolTestCall(hub, context.Background(), "app_note"))
			if !got.IsError || got.Text != session.ClientToolUnavailable("app_note").Text {
				t.Fatalf("the replacement conversation reached the joined app: %+v", got)
			}
			if got := clientToolTestAnswer(t, pending); !got.IsError || !strings.HasPrefix(got.Text, "the turn ended before the app answered app_note") {
				t.Fatalf("the old conversation's pending call: %+v", got)
			}
			clientToolTestSignal(t, ended)
			select {
			case <-entered:
				t.Fatal("the joined app received a call from the replacement conversation")
			default:
			}
			if _, err := app.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err == nil {
				t.Fatal("the joined app attached its tools to the replacement conversation")
			}
			if _, sets := hub.ClientToolSets(); len(sets) != 0 {
				t.Fatalf("a refused snapshot reached the replacement conversation: %+v", sets)
			}
			// A snapshot already past the session's check when the swap landed.
			late := &server{joined: "/a.jsonl"}
			if _, err := hub.replace(late, strings.Repeat("t", clientToolOwnerMin), []session.ClientToolSet{set}); !errors.Is(err, ErrJoinedGone) {
				t.Fatalf("a late snapshot for the old conversation: %v", err)
			}
			// A window that follows the session may attach to what is open now.
			if _, err := window.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{set}); err != nil {
				t.Fatalf("a following window could not attach to the open conversation: %v", err)
			}
			if _, sets := hub.ClientToolSets(); len(sets) != 1 {
				t.Fatalf("the following window's sets: %+v", sets)
			}
		})
	}
}

func clientToolTestAnswer(t *testing.T, answer <-chan session.ClientToolAnswer) session.ClientToolAnswer {
	t.Helper()
	select {
	case got := <-answer:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("client tool wait did not settle")
		return session.ClientToolAnswer{}
	}
}

func clientToolTestCall(hub *ClientToolHub, ctx context.Context, name string) <-chan session.ClientToolAnswer {
	answer := make(chan session.ClientToolAnswer, 1)
	go func() {
		got, _ := hub.CallClientTool(ctx, session.ClientToolCall{Set: "notes", Name: name,
			TurnCall: "provider-1", SourceMessage: "message-1", Args: json.RawMessage(`{"id":3}`)})
		answer <- got
	}()
	return answer
}

// C2, C5: the owning connection alone receives and answers its calls.
func TestClientToolsRemoteOwnersAndAnswers(t *testing.T) {
	hub := &ClientToolHub{}
	engine := engineOn(&fakeAgent{})
	engine.ClientTools = hub
	held := NewSession(engine, true)
	t.Cleanup(func() { _ = held.Close() })
	a, b := clientToolTestPair(t, held), clientToolTestPair(t, held)
	ctx := context.Background()
	if _, err := a.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("notes", "app_note")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("other", "app_note")}); err == nil || !strings.Contains(err.Error(), "app_note") {
		t.Fatalf("collision: %v", err)
	}
	called := make(chan session.ClientToolCall, 1)
	foreign := make(chan bool, 1)
	b.Client.Agent().SetClientToolHandler(func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error) {
		foreign <- true
		return session.ClientToolAnswer{}, nil
	})
	a.Client.Agent().SetClientToolHandler(func(ctx context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
		called <- call
		return session.ClientToolAnswer{Text: `{"saved":true}`}, nil
	})
	answer := clientToolTestAnswer(t, clientToolTestCall(hub, ctx, "app_note"))
	if answer.IsError || answer.Text != `{"saved":true}` {
		t.Fatalf("model answer: %+v", answer)
	}
	call := <-called
	if call.SourceMessage != "message-1" || call.TurnCall != "provider-1" || string(call.Args) != `{"id":3}` {
		t.Fatalf("call identity: %+v", call)
	}
	select {
	case <-foreign:
		t.Fatal("a call reached a different connection")
	default:
	}
	if _, err := b.Client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("notes", "app_other")}); err != nil {
		t.Fatal(err)
	}
	a.Client.Agent().SetClientToolHandler(func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error) {
		foreign <- true
		return session.ClientToolAnswer{}, nil
	})
	b.Client.Agent().SetClientToolHandler(func(_ context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
		return session.ClientToolAnswer{Text: "other connection"}, nil
	})
	got := clientToolTestAnswer(t, clientToolTestCall(hub, ctx, "app_other"))
	if got.Text != "other connection" || got.IsError {
		t.Fatalf("second owner answer: %+v", got)
	}
	select {
	case <-foreign:
		t.Fatal("the second owner's call reached the first")
	default:
	}
}

type clientToolBlockedAgent struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
}

func (a *clientToolBlockedAgent) SetModel(model string) {
	close(a.entered)
	<-a.release
	a.fakeAgent.SetModel(model)
}

// C2: tool answers bypass an ordered call that is still holding its lane.
func TestClientToolsRemoteAnswerBypassesOrderedLane(t *testing.T) {
	hub := &ClientToolHub{}
	agent := &clientToolBlockedAgent{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), release: make(chan struct{})}
	defer close(agent.release)
	held := NewSession(&Engine{Agent: agent, ClientTools: hub}, true)
	t.Cleanup(func() { _ = held.Close() })
	loop := clientToolTestPair(t, held)
	if _, err := loop.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{clientToolTestSet("notes", "app_note")}); err != nil {
		t.Fatal(err)
	}
	loop.Client.Agent().SetClientToolHandler(func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error) {
		return session.ClientToolAnswer{Text: "answered while the lane was held"}, nil
	})
	go loop.Client.Agent().SetModel("another/model")
	clientToolTestSignal(t, agent.entered)
	got := clientToolTestAnswer(t, clientToolTestCall(hub, context.Background(), "app_note"))
	if got.IsError || got.Text != "answered while the lane was held" {
		t.Fatalf("answer queued behind ordered work: %+v", got)
	}
}

type clientToolStalledOutput struct {
	io.Writer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *clientToolStalledOutput) Write(data []byte) (int, error) {
	var frame Frame
	if json.Unmarshal(data, &frame) == nil && frame.Kind == clientToolCallKind {
		w.once.Do(func() {
			close(w.entered)
			<-w.release
		})
	}
	return w.Writer.Write(data)
}

// C3, C5: a stuck owner blocks one sender while queued calls expire and peers work.
func TestClientToolsRemoteBlockedSenderDropsExpiredCalls(t *testing.T) {
	hub := &ClientToolHub{}
	held := NewSession(&Engine{Agent: &fakeAgent{}, ClientTools: hub}, true)
	t.Cleanup(func() { _ = held.Close() })
	stalled := &clientToolStalledOutput{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	owner := clientToolTestRawOutput(t, held, false, func(out io.Writer) io.Writer {
		stalled.Writer = out
		return stalled
	})
	t.Cleanup(func() { release.Do(func() { close(stalled.release) }) })
	set := clientToolTestSet("notes", "app_note")
	if reply := owner.call(t, MethodClientToolsSet, []session.ClientToolSet{set}); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	peer := clientToolTestPair(t, held)
	if _, err := peer.Client.Agent().SetClientTools(context.Background(), []session.ClientToolSet{clientToolTestSet("notes", "app_other")}); err != nil {
		t.Fatal(err)
	}
	peer.Client.Agent().SetClientToolHandler(func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error) {
		return session.ClientToolAnswer{Text: "other owner still works"}, nil
	})
	first := clientToolTestCall(hub, context.Background(), "app_note")
	clientToolTestSignal(t, stalled.entered)
	var queued []<-chan session.ClientToolAnswer
	for range 16 {
		queued = append(queued, clientToolTestCall(hub, context.Background(), "app_note"))
	}
	if got := clientToolTestAnswer(t, clientToolTestCall(hub, context.Background(), "app_other")); got.IsError || got.Text != "other owner still works" {
		t.Fatalf("blocked one owner stalled another: %+v", got)
	}
	for _, answer := range append(queued, first) {
		got := clientToolTestAnswer(t, answer)
		if !got.IsError || !strings.HasPrefix(got.Text, "client_timeout:") {
			t.Fatalf("blocked send did not respect its wait: %+v", got)
		}
	}
	func() {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		if len(hub.pending) != 0 {
			t.Fatal("expired queued calls retained their arguments")
		}
	}()
	release.Do(func() { close(stalled.release) })
	owner.frame(t, clientToolCallKind)
	owner.frame(t, clientToolEndedKind)
	if reply := owner.call(t, MethodClientToolsSet, []session.ClientToolSet{set}); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	if got := owner.callFrames.Load(); got != 1 {
		t.Fatalf("sent %d calls after a blocked writer resumed; queued expired calls must be skipped", got)
	}
}

type clientToolStalledLink struct {
	io.ReadWriteCloser
	writes  int
	entered chan struct{}
	release chan struct{}
}

func (p *clientToolStalledLink) Write(data []byte) (int, error) {
	p.writes++
	if p.writes == 2 {
		close(p.entered)
		<-p.release
		return 0, io.ErrClosedPipe
	}
	return p.ReadWriteCloser.Write(data)
}

// C1: tool publication shares ordinary calls' writer ownership and write failures.
func TestClientToolsRemoteAttachmentUsesCallPath(t *testing.T) {
	for _, method := range []string{MethodModel, MethodClientToolsSet} {
		t.Run(method, func(t *testing.T) {
			hub := &ClientToolHub{}
			held := NewSession(&Engine{Agent: &fakeAgent{}, ClientTools: hub}, true)
			t.Cleanup(func() { _ = held.Close() })
			surface, engine := Pipe()
			served := make(chan struct{})
			go func() {
				defer close(served)
				defer engine.Close()
				_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return held, nil }})
			}()
			link := &clientToolStalledLink{ReadWriteCloser: surface, entered: make(chan struct{}), release: make(chan struct{})}
			client, err := Dial(link, "engine", Hello{})
			if err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(link.release) })
				_ = client.Close()
				clientToolTestSignal(t, served)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failed := make(chan error, 1)
			go func() {
				if method == MethodClientToolsSet {
					_, err := client.Agent().SetClientTools(ctx, []session.ClientToolSet{clientToolTestSet("notes", "app_note")})
					failed <- err
					return
				}
				_, err := client.call(ctx, method, nil)
				failed <- err
			}()
			clientToolTestSignal(t, link.entered)
			cancel()
			select {
			case err := <-failed:
				t.Fatalf("call abandoned its synchronous writer: %v", err)
			default:
			}
			release.Do(func() { close(link.release) })
			select {
			case err := <-failed:
				if err == nil || err.Error() != "the connection to engine is gone — run the same command to pick the conversation back up" {
					t.Fatalf("ordinary write failure: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("call retained a writer after its transport write ended")
			}
			if _, sets := hub.ClientToolSets(); len(sets) != 0 {
				t.Fatal("a failed write changed the holder")
			}
		})
	}
}
