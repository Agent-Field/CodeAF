package appapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type fakeTaskEngine struct {
	*fakeAgentEngine
	rows    []session.AppTaskView
	quick   []session.AppQuickTaskView
	intents []session.AppTaskIntent
	changed []string
	failure error
}

func (f *fakeTaskEngine) AppTasks() ([]session.AppTaskView, error) { return f.rows, f.failure }
func (f *fakeTaskEngine) AppTask(id string) (session.AppTaskView, bool, error) {
	for _, row := range f.rows {
		if row.ID == id {
			return row, true, f.failure
		}
	}
	return session.AppTaskView{}, false, f.failure
}
func (f *fakeTaskEngine) AppTaskAction(intent session.AppTaskIntent) error {
	f.intents = append(f.intents, intent)
	return f.failure
}
func (f *fakeTaskEngine) AppQuickTasks() ([]session.AppQuickTaskView, error) {
	return f.quick, f.failure
}
func (f *fakeTaskEngine) AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error) {
	f.intents = append(f.intents, intent)
	if len(f.changed) > 0 {
		return nil, f.changed, f.failure
	}
	return []string{"one.txt"}, nil, f.failure
}
func taskHTTPFixture(t *testing.T) (*agentFixture, Agent, *fakeTaskEngine) {
	t.Helper()
	f := newAgentFixture(t)
	agent := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	engine := &fakeTaskEngine{fakeAgentEngine: f.engines[0], rows: []session.AppTaskView{{ID: "1", Title: "Root", State: "running", Cap: 2, Step: 3, Version: 1}, {ID: "2", Parent: "1", Title: "Child", State: "done", Cap: 2, Spent: .40, Step: 4, Waits: []string{"1"}, Version: 2}}, quick: []session.AppQuickTaskView{{ID: "3", Title: "Rename", State: "done", Files: []session.AppQuickFile{{Path: "one.txt", BeforeHash: "before", AfterHash: "after"}}, Added: 1, Removed: 1, Version: 1}}}
	f.registry.live[agent.ID].conn.Engine = engine
	return f, agent, engine
}

func TestA5EveryTaskAndQuickTaskRouteUsesItsEngineDoor(t *testing.T) {
	for _, route := range append(append([]contractRoute{}, taskContractRoutes...), quickTaskContractRoutes...) {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			f, agent, engine := taskHTTPFixture(t)
			path := strings.ReplaceAll(route.Path, "{id}", agent.ID)
			path = strings.ReplaceAll(path, "{tid}", taskResourceID(agent.ID, "1"))
			path = strings.ReplaceAll(path, "{qid}", taskResourceID(agent.ID, "3"))
			body := `{}`
			if strings.HasSuffix(path, "/note") || strings.HasSuffix(path, "/brief") {
				body = `{"text":"new instructions"}`
			}
			if strings.HasSuffix(path, "/model") {
				body = `{"model":"test/other"}`
			}
			if strings.HasSuffix(path, "/decide") {
				body = `{"verb":"accept"}`
			}
			response := f.request(t, route.Method, path, body)
			if response.Code != 200 {
				t.Fatalf("%d %s", response.Code, response.Body)
			}
			if route.Method == "POST" {
				if len(engine.intents) != 1 {
					t.Fatalf("intents=%+v", engine.intents)
				}
				intent := engine.intents[0]
				if intent.ID != "1" && intent.ID != "3" {
					t.Fatalf("engine got app id %+v", intent)
				}
			}
			if strings.HasSuffix(path, "/tasks") {
				var got TasksResponse
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if len(got.Tasks) != 2 || len(got.Groups) != 1 || got.Progress.Done != 1 || got.Progress.Total != 2 || got.Steps != 7 || got.Spent.USD != "0.40" || got.Tasks[0].Cap.USD != "2.00" || got.Tasks[0].Spent != nil {
					t.Fatalf("tree %+v", got)
				}
			}
			if strings.HasSuffix(path, "/quick-tasks") {
				var got QuickTasksResponse
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || len(got) != 1 || got[0].Files[0].AfterHash != "after" {
					t.Fatalf("quick %+v %v", got, err)
				}
			}
		})
	}
}

func TestA5YourCallVerbsPreserveWordsAndExpectedState(t *testing.T) {
	for _, verb := range []string{"accept", "not_right", "tell", "recheck", "let_it_decide"} {
		t.Run(verb, func(t *testing.T) {
			f, agent, engine := taskHTTPFixture(t)
			path := "/app/v1/tasks/" + taskResourceID(agent.ID, "1") + "/decide"
			body := fmt.Sprintf(`{"verb":%q,"words":"do this","expect":{"state":"your_call","version":7}}`, verb)
			got := f.request(t, "POST", path, body)
			if got.Code != 200 {
				t.Fatalf("%d %s", got.Code, got.Body)
			}
			intent := engine.intents[0]
			if intent.Verb != verb || intent.Text != "do this" || *intent.State != "your_call" || *intent.Version != 7 {
				t.Fatal(intent)
			}
		})
	}
}
func TestA5TaskInvalidInputsNeverMutate(t *testing.T) {
	f, agent, engine := taskHTTPFixture(t)
	base := "/app/v1/tasks/" + taskResourceID(agent.ID, "1")
	for _, probe := range []struct{ path, body string }{{"/note", `{"text":" "}`}, {"/hold", `{"text":"not allowed"}`}, {"/decide", `{"verb":"unknown"}`}, {"/note", `{"text":null}`}, {"/model", `{"model":""}`}, {"/pause", `{"expect":{"unknown":1}}`}, {"/typing", `null`}, {"/stop", `{} {}`}} {
		got := f.request(t, "POST", base+probe.path, probe.body)
		if got.Code != 422 {
			t.Fatalf("%s %s -> %d %s", probe.path, probe.body, got.Code, got.Body)
		}
	}
	if len(engine.intents) != 0 {
		t.Fatal(engine.intents)
	}
	got := f.request(t, "GET", "/app/v1/tasks/invalid", "{}")
	if got.Code != 404 {
		t.Fatalf("id: %d", got.Code)
	}
	engine.failure = errors.New("stale: that task changed")
	got = f.request(t, "POST", base+"/pause", `{}`)
	if got.Code != 409 || !strings.Contains(got.Body.String(), `"reason":"stale"`) {
		t.Fatalf("stale: %d %s", got.Code, got.Body)
	}
}
func TestA5QuickUndoConflictNamesAllFilesAndReplaysWithoutAnotherAction(t *testing.T) {
	f, agent, engine := taskHTTPFixture(t)
	path := "/app/v1/quick-tasks/" + taskResourceID(agent.ID, "3") + "/undo"
	engine.changed = []string{"one.txt", "two.txt"}
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		req.Host = "127.0.0.1:4000"
		req.Header.Set("Authorization", bearer().Get("Authorization"))
		req.Header.Set("Idempotency-Key", "undo-1")
		out := httptest.NewRecorder()
		f.server.ServeHTTP(out, req)
		return out
	}
	first, second := send(), send()
	if first.Code != 409 || first.Body.String() != second.Body.String() || len(engine.intents) != 1 {
		t.Fatalf("replay %d %s %s %+v", first.Code, first.Body, second.Body, engine.intents)
	}
	var got ConflictResponse
	if err := json.Unmarshal(first.Body.Bytes(), &got); err != nil || len(got.ChangedSince) != 2 || got.Error != "Can't undo cleanly: 2 files changed since" {
		t.Fatalf("conflict %+v %v", got, err)
	}
}
func TestA5TaskClockNotesAndWorkLogUseEngineFacts(t *testing.T) {
	f, agent, engine := taskHTTPFixture(t)
	deadline := time.Date(2026, 10, 13, 12, 0, 12, 0, time.UTC)
	step := 9
	engine.rows[0].State = "proposed"
	engine.rows[0].Deadline = deadline
	engine.rows[0].Notes = []session.AppTaskNote{{Text: "pending"}, {Text: "read", ReadAtStep: &step}}
	engine.rows[0].WorkLog = []string{"write one.txt"}
	engine.rows[0].Result = "Finished the edit"
	got := f.request(t, "GET", "/app/v1/tasks/"+taskResourceID(agent.ID, "1"), "")
	var page TaskResponse
	if err := json.Unmarshal(got.Body.Bytes(), &page); err != nil || page.Clock.State != "running" || !page.StartsAt.Equal(deadline) || page.Notes[0].ReadAtStep != nil || *page.Notes[1].ReadAtStep != 9 || len(page.WorkLog) != 1 || page.Result != "Finished the edit" {
		t.Fatalf("page %+v %v", page, err)
	}
	if taskResourceID("another", "1") == page.ID {
		t.Fatal("task ids alias across agents")
	}
}

func TestA5AgentSnapshotUsesTheSameTaskIdsAsTheTaskPanel(t *testing.T) {
	f, agent, _ := taskHTTPFixture(t)
	var snapshot AgentResponse
	got := f.request(t, "GET", "/app/v1/agents/"+agent.ID, "")
	if err := json.Unmarshal(got.Body.Bytes(), &snapshot); err != nil || len(snapshot.Snapshot.Tasks.Tasks) != 2 {
		t.Fatalf("snapshot %+v %v", snapshot, err)
	}
	if snapshot.Snapshot.Tasks.Tasks[0].ID != taskResourceID(agent.ID, "1") {
		t.Fatal("snapshot does not use A5's scoped ids")
	}
	task := f.request(t, "GET", "/app/v1/tasks/"+snapshot.Snapshot.Tasks.Tasks[0].ID, "")
	if task.Code != 200 {
		t.Fatalf("snapshot click: %d %s", task.Code, task.Body)
	}
}
