package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// hostedStopModel is the real provider boundary for the hosted-stop test. Its
// first answer is exactly the one tasks call at issue #1345, and its second
// answer lets the test read back the tool result the model actually received.
type hostedStopModel struct {
	*httptest.Server

	mu         sync.Mutex
	requests   int
	toolResult string
}

func newHostedStopModel(t *testing.T) *hostedStopModel {
	t.Helper()
	model := &hostedStopModel{}
	model.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.NotFound(writer, request)
			return
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read model request: %v", err)
			return
		}
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode model request: %v", err)
			return
		}
		model.mu.Lock()
		model.requests++
		requestNumber := model.requests
		for _, message := range body.Messages {
			if message.Role == "tool" {
				model.toolResult = message.Content
			}
		}
		model.mu.Unlock()

		writer.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"stop-1\",\"type\":\"function\",\"function\":{\"name\":\"tasks\",\"arguments\":\"{\\\"id\\\":\\\"1\\\",\\\"stop\\\":true}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Stopped.\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)
	return model
}

func (m *hostedStopModel) result() (string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.toolResult, m.requests
}

// hostedSlowRun is the run engine's blocking fixture: it owns a real plan
// store and stays alive until the session's run context is cut.
type hostedSlowRun struct {
	started chan struct{}
}

func (r hostedSlowRun) Start(ctx context.Context, spec session.RunSpec) session.RunSummary {
	close(r.started)
	<-ctx.Done()
	return session.RunSummary{Outcome: "ran and did not finish", Cut: []string{spec.Store.RootID()}, Nodes: 1, Steps: 1}
}

func (hostedSlowRun) Land(context.Context, *plandb.Store, string, string, string) (session.RunLanding, error) {
	return session.RunLanding{}, nil
}

func hostedStopRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "work", dir},
		{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	return dir
}

func hostedTaskFrame(t *testing.T, lane <-chan session.Event, id uint64, stopped bool) session.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-lane:
			if !ok {
				t.Fatal("the hosted task lane closed before the run's frame arrived")
			}
			if event.Kind == session.EventTaskUpdate && event.Task != nil && event.Task.ID == id && event.Task.Stopped == stopped {
				return event
			}
		case <-deadline:
			t.Fatalf("the hosted task lane never carried task %d with stopped=%t", id, stopped)
		}
	}
}

// THE CONVERSATIONAL STOP CROSSES THE REAL WIRE AND ENDS THE REAL RUN RECORD.
// A fake remote agent would only prove its own Cancel method; this test keeps
// session.Agent, Serve, Dial, the task lane and the plan store in the path.
func TestHostedTasksToolStopEndsTheRunAndReportsWhatHappened(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	started := make(chan struct{})
	session.RegisterRunEngine(hostedSlowRun{started: started})
	t.Cleanup(func() { session.RegisterRunEngine(nil) })

	workspace := hostedStopRepo(t)
	place := session.Place{Dir: filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa"), Workspace: workspace}
	model := newHostedStopModel(t)
	engine, err := session.New(session.Config{
		Workspace:  workspace,
		Place:      place,
		Model:      "test/model",
		APIKey:     "fixture",
		BaseURL:    model.URL,
		System:     "Test only.",
		AskConsent: false,
		OneModel:   true,
	})
	if err != nil {
		t.Fatalf("build real session agent: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	loop, err := Loopback(Hello{Version: Version, Workspace: workspace}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: engine, Workspace: workspace}, nil
	}})
	if err != nil {
		t.Fatalf("dial hosted session: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	lane, stopLane := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stopLane)
	id, _, _, err := loop.Client.Agent().StartTask(context.Background(), "write numbers slowly", false)
	if err != nil {
		t.Fatalf("start hosted run: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the hosted run never started")
	}
	running := hostedTaskFrame(t, lane, id, false)
	if running.Task.State != session.TaskRunning {
		t.Fatalf("first hosted row = %+v, want running", running.Task)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := loop.Client.Agent().Submit(ctx, "stop it")
	if err != nil {
		t.Fatalf("submit stop turn: %v", err)
	}
	var answer strings.Builder
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("stop turn failed: %v", event.Err)
		}
		if event.Kind == session.EventTextDelta {
			answer.WriteString(event.Text)
		}
	}

	toolResult, requests := model.result()
	if !strings.Contains(toolResult, "stopping task 1") || !strings.Contains(toolResult, "its branch is kept") {
		t.Fatalf("the hosted model received a false stop result after %d requests:\n%s", requests, toolResult)
	}
	if got := strings.TrimSpace(answer.String()); got != "Stopped." {
		t.Fatalf("hosted stop answer = %q, want Stopped.", got)
	}

	stopped := hostedTaskFrame(t, lane, id, true)
	if got := session.ProjectTask(stopped.Task.StatusFacts()).RowWord(); got != "stopped" {
		t.Fatalf("hosted stopped frame reads %q: %+v", got, stopped.Task)
	}
	rows := loop.Client.Agent().PlanTasks()
	if len(rows) != 1 || rows[0].ID != "t-1" || !rows[0].Stopped || rows[0].Status == string(plandb.StatusRunning) {
		t.Fatalf("hosted run store after stop = %+v, want one stopped root", rows)
	}
	if _, requests = model.result(); requests != 2 {
		t.Fatalf("the stop turn made %d model requests, want the call and its receipt only", requests)
	}
}
