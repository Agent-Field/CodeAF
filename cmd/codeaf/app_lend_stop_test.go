package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A LENT STAGE IS ONLY INTERRUPTED BY THE APP'S STOP.
//
// The app's Stop carries a bound, and a conversation whose turn has not let go
// when it runs out is abandoned ([session.Agent.InterruptWithin]). A stage's
// turn belongs to the factory's runner, which is still reading it, so the lent
// view drops the bound ([lentAgent.InterruptWithin]). Without the override the
// embedded agent's InterruptWithin is promoted and every other test still
// passes; this one drives a real agent parked on a call that ignores its
// context and watches what the bound does to it, through the view and without.
// A runner's turn through a job manager the app holds is the same case, kept by
// the engine itself ([session.Agent.InterruptWithin]); a person's own turn in
// that conversation keeps the bound.

// wedgedClientTools is a client tool door whose call never looks at its
// context: the shape of wait a bounded stop exists for.
type wedgedClientTools struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once

	mu    sync.Mutex
	armed int
	fire  func()
}

func (w *wedgedClientTools) ClientToolSets() (uint64, []session.ClientToolSet) {
	return 1, []session.ClientToolSet{{ID: "notes", Instructions: "Use this set.", DeadlineMS: int(time.Hour / time.Millisecond), Tools: []session.ClientTool{
		{Name: "app_note", Description: "An app action", InputSchema: json.RawMessage(`{"type":"object"}`), Category: session.ActionRead},
	}}}
}

func (w *wedgedClientTools) CallClientTool(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return session.ClientToolAnswer{Text: "noted"}, nil
}

// wedgedAgent is a real agent whose next turn parks on [wedgedClientTools].
func wedgedAgent(t *testing.T) (*session.Agent, *wedgedClientTools) {
	t.Helper()
	var mu sync.Mutex
	asked := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		// The turn's first request is the one with the belt on it; any other
		// (a naming errand) is answered with words.
		mu.Lock()
		first := len(request.Tools) > 0 && asked == 0
		if len(request.Tools) > 0 {
			asked++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if first {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"app_note","arguments":"{}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(provider.Close)
	tools := &wedgedClientTools{entered: make(chan struct{}), release: make(chan struct{})}
	root := t.TempDir()
	agent, err := session.New(session.Config{Workspace: root, SessionFile: filepath.Join(root, "transcript.jsonl"),
		Model: "test/model", APIKey: "fixture", BaseURL: provider.URL, OneModel: true,
		DeterministicNames: true, PromptProfile: "full", ClientTools: tools,
		AbandonAfter: func(_ time.Duration, fire func()) *time.Timer {
			tools.mu.Lock()
			tools.armed++
			tools.fire = fire
			tools.mu.Unlock()
			return time.NewTimer(24 * time.Hour)
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-tools.release:
		default:
			close(tools.release)
		}
		_ = agent.Close()
	})
	return agent, tools
}

func TestTheAppsBoundedStopOnlyInterruptsTheFactorysTurns(t *testing.T) {
	const grace = 50 * time.Millisecond
	itself := func(a *session.Agent) appBoundedStop { return a }
	persons := func(a *session.Agent) (<-chan session.Event, error) {
		return a.Submit(context.Background(), "use the app note")
	}
	for _, tc := range []struct {
		name    string
		submit  func(*session.Agent) (<-chan session.Event, error)
		view    func(*session.Agent) appBoundedStop
		letGoOf bool
	}{
		{"a person's own turn", persons, itself, true},
		{"a lent stage", persons, func(a *session.Agent) appBoundedStop { return lentAgent{liveStepView{a}} }, false},
		// THE LIVE MANAGER ROAD: the runner's turn runs through the
		// conversation a window holds, and the app's Stop reaches that
		// conversation directly. The runner reads the turn to its end.
		{"a runner's turn through a held manager", func(a *session.Agent) (<-chan session.Event, error) {
			return liveManagerTurn{agent: a}.SubmitRunnerNote(context.Background(), "use the app note")
		}, itself, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, tools := wedgedAgent(t)
			events, err := tc.submit(agent)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-tools.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the turn never reached the call that ignores its context")
			}
			tc.view(agent).InterruptWithin(grace)
			ended := make(chan struct{})
			go func() {
				for range events {
				}
				close(ended)
			}()
			tools.mu.Lock()
			armed, fire := tools.armed, tools.fire
			tools.mu.Unlock()
			if tc.letGoOf {
				if armed != 1 || fire == nil {
					t.Fatalf("the bounded stop armed %d timer(s), want 1", armed)
				}
				fire()
				select {
				case <-ended:
				case <-time.After(10 * time.Second):
					t.Fatal("the bound never let go of the wedged turn")
				}
			} else {
				if armed != 0 {
					t.Fatalf("the app's bounded stop armed %d timer(s) on %s, want 0", armed, tc.name)
				}
				select {
				case <-ended:
					t.Fatal("the app's bounded stop let go of a turn the factory is still reading")
				default:
				}
				if !agent.TurnRunning() {
					t.Fatal("the turn is not running, though its stream is open")
				}
			}
			close(tools.release)
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Fatal("the interrupted turn did not end once its call returned")
			}
		})
	}
}
