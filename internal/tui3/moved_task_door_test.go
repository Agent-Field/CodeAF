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

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract I1 and I4 keep transferred steps and worker replies behind the task key.
func TestMovedFinishedTaskRealDoorShowsStepsAndWorker(t *testing.T) {
	for _, roster := range []bool{true, false} {
		name := "index before roster"
		if roster {
			name = "restored roster"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".cell"), 0700); err != nil {
				t.Fatal(err)
			}
			place := session.Place{Dir: root, Workspace: filepath.Join(root, "work")}
			files := map[string]string{
				place.Tasks(): `{"type":"tasks","version":1,"seq":1,"nodes":[],"runs":[{"id":1,"title":"Create hello-task.txt with 'hi from a task'","state":"done","planTask":"t-1","crewState":{"version":1,"routed":true,"Decision":{}},"report":"Created the greeting file.","result":"Created the greeting file.","model":"z-ai/glm-5.3-flash","elapsed_ms":26000,"costUsd":0.08,"startedAt":"2026-10-27T01:14:48Z","endedAt":"2026-10-27T01:15:14Z"}]}`,
				filepath.Join(place.NodeJournals(), "1", "20261027-011448.092944_worker.jsonl"): `{"type":"session","version":1,"id":"worker","model":"z-ai/glm-5.3-flash"}
{"type":"message","role":"assistant","content":"The worker wrote hi from a task.\n\nThe saved conversation stays readable.","toolCalls":[{"id":"finish","function":{"name":"bash","arguments":"{\"command\":\"plandb done 1 --result 'Created the greeting file.'\"}"}}]}
{"type":"message","role":"tool","toolCallId":"finish","content":"done"}
`,
				filepath.Join(place.NodeJournals(), "1", "trajectory.jsonl"):                         "{\"kind\":\"step\",\"step\":1,\"command\":\"printf 'hi from a task' > hello-task.txt\",\"observation\":\"greeting saved\"}\n",
				filepath.Join(place.NodeJournals(), "1", "fixes.json"):                               "{}\n",
				filepath.Join(place.NodeJournals(), "qe6q8c", "20261027-011504.157512_worker.jsonl"): "{\"type\":\"session\",\"version\":1,\"id\":\"checker\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"The checker reply belongs to another task.\"}\n",
				filepath.Join(place.NodeJournals(), "qe6q8c", "trajectory.jsonl"):                    "{\"kind\":\"step\",\"step\":1,\"command\":\"cat checker-only.txt\",\"observation\":\"checker output\"}\n",
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
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
			defer provider.Close()
			agent, err := session.New(session.Config{Workspace: place.Workspace, Place: place, SessionFile: place.Transcript(), ProfileDir: t.TempDir(), Model: "stub/chat", APIKey: "stub-key", BaseURL: provider.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
				Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: agent, SessionFile: place.Transcript(), Workspace: place.Workspace}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			a := newTestApp(loop.Client.Agent())
			a.width, a.height = 180, 48
			a.homeRoot = t.TempDir()
			a.file, a.title = place.Transcript(), "moved chat"
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
				events, stop := loop.Client.Agent().WatchTaskUpdates()
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
			a.taskSheetPointAt(a.taskSheet.reading.items[0].entry)
			drive(t, a, key("enter"))
			if calls := modelCalls.Load(); calls != 0 {
				t.Errorf("opening finished work made %d model calls", calls)
			}
			screen := planRoomText(t, a)
			for _, want := range []string{"Create hello-task.txt with 'hi from a task'", "done", "printf 'hi from a task'", "The worker wrote hi from a task", "The saved conversation stays readable."} {
				if !strings.Contains(screen, want) {
					t.Errorf("task page missing %q:\n%s", want, screen)
				}
			}
			for _, unwanted := range []string{"checker reply", "checker-only.txt"} {
				if strings.Contains(screen, unwanted) {
					t.Errorf("task page includes another task's history %q", unwanted)
				}
			}
			// Contract I2 keeps the same saved conversation on the transcript tab.
			drive(t, a, key("tab"))
			if !strings.Contains(planRoomText(t, a), "the run's working copy is not here any more") {
				t.Errorf("missing working copy did not explain itself:\n%s", planRoomText(t, a))
			}
			drive(t, a, key("tab"))
			screen = planRoomText(t, a)
			if !strings.Contains(screen, "The saved conversation stays readable.") || !strings.Contains(screen, "printf 'hi from a task'") {
				t.Errorf("returning to transcript hid saved work:\n%s", screen)
			}
			a.input.setText("resume this task")
			drive(t, a, key("enter"))
			if a.room == nil || !a.room.done || !strings.Contains(planRoomText(t, a), "this task has finished") {
				t.Error("historical page did not refuse to resume finished work")
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
		})
	}
}
