package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A MEMBER'S QUESTION GOES UP TO ITS MANAGER AND THE ANSWER COMES BACK, on the
// ordinary launch and on the engine a window over --host talks to: the
// engine's own [bootEngine] builds the member with nothing set that a person
// does not set, the team is written where the ordinary launch keeps it, and the
// model endpoint is scripted to do what a member does, load the questions
// group and `ask`. Nothing reaches the person: the question is a packet for the
// manager, the member is told so, and when the manager decides it the member
// is woken with the answer marked `◆ answered`.
func TestTeamQuestionsUpTheOrdinaryLaunchSendsAMembersQuestionToItsManager(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	script := newScriptedModel(t)
	t.Setenv("CODEAF_BASE_URL", script.server.URL)
	_ = model

	engine, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	defer func() { _ = engine.Agent.Close() }()
	member := strings.TrimSpace(engine.SessionFile)
	if member == "" {
		t.Fatal("the engine named no transcript")
	}
	teamID := managedTeamWith(t, root, member, workspace)

	events, err := engine.Agent.Submit(t.Context(), "build the signup form")
	if err != nil {
		t.Fatalf("the turn did not start: %v", err)
	}
	for range events {
	}

	waiting, _, err := teams.OpenPackets("", teamID)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("no packet waits on the manager: %+v %v\nthe model saw:\n%s", waiting, err, script.transcript())
	}
	p := waiting[0]
	if p.Kind != teams.PacketQuestion || p.RaisedBy != "web" || p.Question != "JSON or form data?" {
		t.Fatalf("the packet: %+v", p)
	}
	if mine, _, _ := teams.OpenPackets("", teams.Person); len(mine) != 0 {
		t.Fatalf("the member's question reached the person: %+v", mine)
	}
	script.waitFor(t, "went to your manager (@boss", 10*time.Second)

	if _, err := teams.Decide("", p.ID, teams.FromManager, "1", "the other endpoints take JSON"); err != nil {
		t.Fatalf("the manager could not decide: %v", err)
	}
	script.waitFor(t, "◆ answered: JSON (by ◆ @boss, because the other endpoints take JSON)", 20*time.Second)
}

// scriptedModel is an OpenAI-shaped endpoint that answers a member's first
// turn with two tool calls (load the questions group, then `ask`) and every
// other request with "ok", recording what it was sent.
type scriptedModel struct {
	server *httptest.Server
	mu     sync.Mutex
	turn   int
	seen   []recordedRequest
}

func newScriptedModel(t *testing.T) *scriptedModel {
	t.Helper()
	m := &scriptedModel{}
	ask := `{"head":"JSON or form data?","kind":"clarification","reason":"the handler and the form disagree","stakes":"reversible",` +
		`"options":[{"key":"1","label":"JSON","consequence":"the handler changes"},{"key":"2","label":"form data","consequence":"the form changes"}]}`
	m.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		var envelope recordedRequest
		if err := json.Unmarshal(raw, &envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.seen = append(m.seen, envelope)
		step := -1
		if len(envelope.Tools) > 0 {
			step = m.turn
			m.turn++
		}
		m.mu.Unlock()
		switch step {
		case 0:
			writeToolCall(writer, envelope.Stream, "c1", "load_capability", `{"group":"questions"}`)
		case 1:
			writeToolCall(writer, envelope.Stream, "c2", "ask", ask)
		default:
			writeText(writer, envelope.Stream, "ok")
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func writeText(writer http.ResponseWriter, stream bool, text string) {
	if stream {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"id":"q","choices":[{"index":0,"delta":{"role":"assistant","content":` + strconv.Quote(text) + `},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
		return
	}
	_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":` + strconv.Quote(text) + `},"finish_reason":"stop"}]}`))
}

func writeToolCall(writer http.ResponseWriter, stream bool, id, name, arguments string) {
	call := `{"index":0,"id":"` + id + `","type":"function","function":{"name":"` + name + `","arguments":` + strconv.Quote(arguments) + `}}`
	if stream {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"id":"q","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[` + call + `]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
		return
	}
	_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[` + call + `]},"finish_reason":"tool_calls"}]}`))
}

// waitFor waits until some request carried a message holding want.
func (m *scriptedModel) waitFor(t *testing.T, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, request := range m.seen {
			if request.messageContaining(want) != "" {
				m.mu.Unlock()
				return
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the model was never sent %q; it saw:\n%s", want, m.transcript())
}

func (m *scriptedModel) transcript() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) == 0 {
		return "(nothing)"
	}
	return m.seen[len(m.seen)-1].allText()
}
