package tui3

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contracts L1 and L2 keep received finished work readable without starting it.
func TestHostedMovedFinishedTaskRealDoorShowsStepsAndWorker(t *testing.T) {
	for _, identity := range []string{"t-1", ""} {
		name := "saved plan identity"
		if identity == "" {
			name = "no plan identity"
		}
		t.Run(name, func(t *testing.T) {
			for _, roster := range []bool{true, false} {
				name := "index first"
				if roster {
					name = "roster first"
				}
				t.Run(name, func(t *testing.T) { hostedMovedFinishedTask(t, identity, roster) })
			}
		})
	}
}

func hostedMovedFinishedTask(t *testing.T, identity string, roster bool) {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "codeaf-l-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("CODEAF_HOME", home)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	physical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "tmp")
	// An alias above the chat mirrors temporary folders on systems whose
	// temporary root is a symlink, while the retained task files remain ordinary.
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(alias, "1234567890abcdef")
	place := session.Place{Dir: root, Workspace: filepath.Join(root, "work")}
	files := map[string]string{
		filepath.Join(root, ".cell", "tasks.json"):       `{"type":"tasks","version":1,"seq":1,"nodes":[],"runs":[{"id":1,"title":"Create hello-task.txt with exact content","state":"done","planTask":"t-1","crewState":{"version":1,"routed":true,"Decision":{}},"report":"Created the greeting file.","result":"Created the greeting file.","model":"z-ai/glm-5.3-flash","elapsed_ms":12000,"costUsd":0.07,"startedAt":"2026-10-27T01:14:48Z","endedAt":"2026-10-27T01:15:00Z"}]}`,
		filepath.Join(root, ".cell", "transcript.jsonl"): `{"type":"session","version":1,"id":"1234567890abcdef","model":"stub/chat"}` + "\n",
		filepath.Join(root, ".cell", "tasks", "1", "20261027-011448.092944_worker.jsonl"): `{"type":"session","version":1,"id":"worker","model":"z-ai/glm-5.3-flash"}
{"type":"message","role":"assistant","content":"The worker wrote hi from a task.\n\nThe saved conversation stays readable.","toolCalls":[{"id":"finish","function":{"name":"bash","arguments":"{\"command\":\"plandb done 1 --result 'Created the greeting file.'\"}"}}]}
{"type":"message","role":"tool","toolCallId":"finish","content":"done"}
`,
		filepath.Join(root, ".cell", "tasks", "1", "trajectory.jsonl"):                         "{\"kind\":\"step\",\"step\":1,\"command\":\"printf 'hi from a task' > hello-task.txt\",\"observation\":\"greeting saved\"}\n",
		filepath.Join(root, ".cell", "tasks", "ao88ym", "20261027-011504.157512_worker.jsonl"): "{\"type\":\"session\",\"version\":1,\"id\":\"checker\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"The checker reply belongs to another task.\"}\n",
		filepath.Join(root, ".cell", "tasks", "ao88ym", "trajectory.jsonl"):                    "{\"kind\":\"step\",\"step\":1,\"command\":\"cat checker-only.txt\",\"observation\":\"checker output\"}\n",
	}
	if identity == "" {
		path := filepath.Join(root, ".cell", "tasks.json")
		files[path] = strings.Replace(files[path], `"planTask":"t-1",`, "", 1)
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(place.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	var modelCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/models/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"endpoints":[]}}`))
			return
		}
		modelCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(provider.Close)
	engine, err := session.New(session.Config{Workspace: place.Workspace, Place: place, SessionFile: place.Transcript(), ProfileDir: t.TempDir(), Model: "stub/chat", APIKey: "stub-key", BaseURL: provider.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	stopped := make(chan error, 1)
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close finished conversation: %v", err)
		}
		if _, err := enginehost.Stop(place.Workspace); err != nil {
			t.Errorf("stop fixture host: %v", err)
		}
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("host stopped: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("host did not stop")
		}
	})
	conn, err := enginehost.Attach(place.Workspace, func() error {
		go func() {
			stopped <- enginehost.Run(place.Workspace, enginehost.Options{
				Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: engine, SessionFile: place.Transcript(), Workspace: place.Workspace, Place: place}, nil
				},
			})
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.Dial(conn, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	a := newTestApp(client.Agent())
	a.width, a.height = 180, 48
	a.homeRoot = home
	a.file, a.title, a.workspace = place.Transcript(), "background task creates hello file", place.Workspace
	a.clock = func() time.Time { return time.Date(2026, 10, 27, 1, 16, 0, 0, time.UTC) }
	a.comp.tasks = session.RebuiltTaskRows(place.Dir)
	for path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(body)
	}
	if roster {
		events, stop := client.Agent().WatchTaskUpdates()
		defer stop()
		select {
		case event := <-events:
			a.taskUpdate(event)
		case <-time.After(5 * time.Second):
			t.Fatal("finished task missing from restored roster")
		}
	}
	a.input.setText("/task")
	drive(t, a, key("enter"))
	openTaskFolds(a)
	if len(a.taskSheet.reading.items) != 1 {
		t.Fatalf("Tasks must list the run once:\n%s", taskSheetText(a))
	}
	t.Logf("Tasks screen:\n%s", taskSheetText(a))
	a.taskSheetPointAt(a.taskSheet.reading.items[0].entry)
	drive(t, a, key("enter"))
	screen := planRoomText(t, a)
	t.Logf("Task screen:\n%s", screen)
	for _, want := range []string{"Create hello-task.txt with exact content", "done", "printf 'hi from a task'", "The worker wrote hi from a task", "The saved conversation stays readable."} {
		if !strings.Contains(screen, want) {
			t.Errorf("task page missing %q", want)
		}
	}
	if a.room == nil || a.room.plan == nil || !a.room.plan.page.Historical || !a.room.done {
		t.Fatal("the task row did not open a finished historical page")
	}
	t.Logf("Enter opened historical page %q", a.room.plan.id)
	for _, unwanted := range []string{"checker reply", "checker-only.txt"} {
		if strings.Contains(screen, unwanted) {
			t.Errorf("task page includes another task's history %q", unwanted)
		}
	}
	if words := a.tasksPlanKeyWords(a.room.plan.page.Row); len(words) != 0 {
		t.Errorf("finished task offers actions: %v", words)
	}
	drive(t, a, key("tab"))
	if !strings.Contains(planRoomText(t, a), "the run's working copy is not here any more") {
		t.Errorf("missing working copy did not explain itself:\n%s", planRoomText(t, a))
	}
	drive(t, a, key("tab"))
	screen = planRoomText(t, a)
	if !strings.Contains(screen, "The saved conversation stays readable.") || !strings.Contains(screen, "printf 'hi from a task'") {
		t.Errorf("returning to transcript hid saved work:\n%s", screen)
	}
	drive(t, a, key("r"), key("R"))
	if !a.room.done || a.input.String() != "rR" {
		t.Error("finished task accepted a restart action")
	}
	drive(t, a, key("enter"))
	if !a.room.done || !strings.Contains(planRoomText(t, a), "this task has finished") {
		t.Error("finished task did not refuse new work")
	}
	for path, want := range files {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != want {
			t.Errorf("reading finished work changed %s: %v", filepath.Base(path), err)
		}
	}
	if calls := modelCalls.Load(); calls != 0 {
		t.Errorf("reading finished work made %d model calls", calls)
	}
}
