package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A REPLY TO AN OPEN QUESTION IS NEVER REFUSED (run7's board deck, 2026-11-19).
// The model asks the person a question with `ask`, which holds its turn open,
// and the person types their answer as an ordinary message. The send used to
// find the turn running and refuse it with "this agent is already working on a
// turn"; now the words reach the model as the reply, the question closes, and
// the turn finishes on them.
func TestAppSendWhileTheTurnWaitsOnAQuestionIsDeliveredNotRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	t.Setenv(config.ProfileDirEnv, root)
	const reply = "its the q3 board review"
	var calls atomic.Int32
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		var delta any
		finish := "tool_calls"
		// Errands beside the turn (a title, a caption) carry no tools and
		// take no step of the script.
		if !strings.Contains(string(body), `"tools"`) {
			delta, finish = map[string]string{"content": "Board deck"}, "stop"
		} else {
			mu.Lock()
			bodies = append(bodies, string(body))
			mu.Unlock()
			switch calls.Add(1) {
			case 1:
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "load", "type": "function", "function": map[string]string{"name": "load_capability", "arguments": `{"group":"questions"}`}}}}
			case 2:
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "about", "type": "function", "function": map[string]string{"name": "ask", "arguments": `{"head":"What is the board meeting about?","kind":"clarification","reason":"The deck needs its subject","stakes":"reversible"}`}}}}
			default:
				delta, finish = map[string]string{"content": "Building the q3 deck."}, "stop"
			}
		}
		chunk, _ := json.Marshal(map[string]any{"id": "held", "choices": []any{map[string]any{"index": 0, "delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"held\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", chunk, finish)
	}))
	defer server.Close()
	engine, err := session.New(session.Config{Workspace: root, Place: session.Place{Dir: filepath.Join(root, "chat"), Workspace: root}, ProfileDir: root, Model: "test/model", APIKey: "dummy", BaseURL: server.URL, Interactive: true, OneModel: true, DeterministicNames: true})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: engine}, nil }})
	if err != nil {
		engine.Close()
		t.Fatal(err)
	}
	defer loop.Close()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: loop.Client.Agent(), Workspace: root, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	backend.profileDir = root
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "5 slide deck for the board meeting", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	if _, err := backend.Send(agent.ID, reply, "reply-1"); err != nil {
		t.Fatalf("a reply to the open question was refused: %v", err)
	}
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskClosed))
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.ClientID == "reply-1" && added.Entry.Text == reply
	})
	rec.waitFor(t, 10*time.Second, func(event string, _ any) bool { return event == appwire.EventTurnDone })
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 3 || !strings.Contains(bodies[len(bodies)-1], reply) {
		t.Fatalf("the model never read the reply; it took %d turn requests", len(bodies))
	}
}
