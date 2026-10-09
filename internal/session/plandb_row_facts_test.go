package session

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestTaskFactsAgreeAcrossRowsDetailsAndNestedChildren(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "running", Title: "Running"},
		plandb.TaskSpec{ID: "queued", Title: "Queued", Dependencies: []plandb.Dependency{{TaskID: "running", Kind: plandb.DepBlocks}}},
		plandb.TaskSpec{ID: "needy", Title: "Needs a person"},
		plandb.TaskSpec{ID: "done", Title: "Done"},
		plandb.TaskSpec{ID: "group", Title: "Nested group"},
		plandb.TaskSpec{ID: "nested", Title: "Nested worker", ParentID: "group"},
	)
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"running", "needy", "done", "nested"} {
		if _, err := store.Claim(id, "worker-"+id); err != nil {
			t.Fatal(err)
		}
		if err := store.AddSpend(id, "recorded/model", "work", .02, 100, 25); err != nil {
			t.Fatal(err)
		}
		writePlanTrajectory(t, dir, id, `{"kind":"step","step":1,"command":"read a file"}`, `{"kind":"step","step":2,"command":"inspect the result"}`)
	}
	if _, err := store.Pause("needy"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Done("done", "worker-done", "finished", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLive("running", 3, "running command"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLive("nested", 3, "nested command"); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	rows := agent.PlanTasks()
	for _, id := range []string{"running", "needy", "done", "nested"} {
		row := planRowByID(t, rows, "t-"+id)
		page, ok := agent.PlanTaskPage(row.ID)
		if !ok {
			t.Fatal("no task page")
		}
		if row.Model != "recorded/model" || row.Tokens != 125 || math.Abs(row.USD-.02) > 1e-9 || row.Steps != 2 {
			t.Fatalf("list lost facts for %s: %+v", id, row)
		}
		if page.Row.Model != row.Model || page.Row.Tokens != row.Tokens || page.Row.USD != row.USD || page.Row.Steps != row.Steps || page.Row.Live != row.Live {
			t.Fatalf("page differs for %s: row %+v page %+v", id, row, page.Row)
		}
	}
	running := planRowByID(t, rows, "t-running")
	if running.Live.Step != 3 || running.Steps != 2 {
		t.Fatalf("confused in-flight ordinal with finished count: %+v", running)
	}
	queued := planRowByID(t, rows, "t-queued")
	if queued.Model != "" || queued.Tokens != 0 || queued.USD != 0 || queued.Steps != 0 {
		t.Fatalf("invented queued facts: %+v", queued)
	}
	if planRowByID(t, rows, "t-needy").Status != "paused" || planRowByID(t, rows, "t-done").Status != "done" {
		t.Fatal("fixture did not exercise needy/done states")
	}
	group, ok := agent.PlanTaskPage("t-group")
	if !ok {
		t.Fatal("missing group")
	}
	if group.Row.Model != "" || group.Row.Tokens != 0 || group.Row.USD != 0 {
		t.Fatalf("child usage leaked into parent: %+v", group.Row)
	}
	if len(group.Children) != 1 || group.Children[0].Model != "recorded/model" || group.Children[0].Tokens != 125 || group.Children[0].Steps != 2 {
		t.Fatalf("nested facts lost: %+v", group.Children)
	}
	page, ok := agent.PlanTaskPage("t-queued")
	if !ok {
		t.Fatal("missing queued detail")
	}
	if len(page.WaitRows) != 1 || page.WaitRows[0].Model != "recorded/model" || page.WaitRows[0].Tokens != 125 {
		t.Fatalf("dependency facts lost: %+v", page.WaitRows)
	}
	// Newly persisted usage must flow through both projections without defaults.
	if err := store.AddSpend("nested", "recorded/second", "work", .05, 200, 40); err != nil {
		t.Fatal(err)
	}
	updated := planRowByID(t, agent.PlanTasks(), "t-nested")
	detail, _ := agent.PlanTaskPage(updated.ID)
	if updated.Model != "recorded/second" || updated.Tokens != 365 || math.Abs(updated.USD-.07) > 1e-9 || detail.Row.Model != updated.Model || detail.Row.Tokens != updated.Tokens || detail.Row.USD != updated.USD {
		t.Fatalf("usage update mismatch %+v %+v", updated, detail.Row)
	}
}

func TestTaskFactsUseBoundWorkerInsteadOfPricierAuxiliary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "actual", Title: "Actual worker"}, plandb.TaskSpec{ID: "untouched", Title: "Untouched"})
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddSpend("actual", "primary/model", "work", .01, 100, 20); err != nil {
		t.Fatal(err)
	}
	if err := store.AddSpend("actual", "auxiliary/model", "work", .03, 10, 5); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	node := spendNode(t, agent.graph(), "actual", 0)
	journal := filepath.Join(dir, "actual-worker.jsonl")
	if err := os.WriteFile(journal, []byte(`{"type":"session","model":"primary/model"}
{"type":"call","model":"primary/model","costUsd":0.01}
{"type":"usage","usage":{"model":"primary/model","input":100,"output":20,"costUsd":0.01}}
{"type":"usage","usage":{"model":"auxiliary/model","input":10,"output":5,"costUsd":0.03,"aux":true}}
{"type":"took","took":{"callId":"one"}}
{"type":"took","took":{"callId":"two"}}
{"type":"took","took":{"callId":"two"}}
{"type":"took","took":`), 0600); err != nil {
		t.Fatal(err)
	}
	agent.graph().mu.Lock()
	node.journal = journal
	node.spec.model = "primary/model"
	agent.graph().mu.Unlock()
	row := planRowByID(t, agent.PlanTasks(), "t-actual")
	page, ok := agent.PlanTaskPage(row.ID)
	if !ok || row.Model != "primary/model" || row.Steps != 2 || row.Tokens != 135 || math.Abs(row.USD-.04) > 1e-9 || page.Row.Model != row.Model || page.Row.Steps != row.Steps {
		t.Fatalf("worker facts wrong: row %+v page %+v", row, page.Row)
	}
	graph := agent.graph()
	graph.mu.Lock()
	node = restoreNode(graph, taskRecord{ID: node.id, Title: "Actual worker", PlanID: "actual", Model: "primary/model", Journal: journal, State: TaskDone})
	graph.nodes[node.id] = node
	graph.mu.Unlock()
	restored := planRowByID(t, agent.PlanTasks(), "t-actual")
	if restored.Model != row.Model || restored.Steps != row.Steps || restored.Tokens != row.Tokens || restored.USD != row.USD {
		t.Fatalf("restored worker facts lost: %+v", restored)
	}
	untouched := planRowByID(t, agent.PlanTasks(), "t-untouched")
	if untouched.Model != "" || untouched.Steps != 0 {
		t.Fatalf("leaked facts %+v", untouched)
	}
	agent.graph().mu.Lock()
	node.spec.model = ""
	agent.graph().mu.Unlock()
	row = planRowByID(t, agent.PlanTasks(), "t-actual")
	if row.Model != "primary/model" {
		t.Fatalf("inherited session model lost: %+v", row)
	}
	agent.graph().mu.Lock()
	node.room = newTaskRoom()
	node.state = TaskRunning
	node.room.live.steps = 2
	node.room.live.call = "bash actual command"
	node.room.live.began = time.Now()
	agent.graph().mu.Unlock()
	row = planRowByID(t, agent.PlanTasks(), "t-actual")
	page, _ = agent.PlanTaskPage(row.ID)
	if row.Steps != 2 || row.Live.Step != 3 || page.Live != row.Live {
		t.Fatalf("live worker facts differ: %+v %+v", row, page.Live)
	}
	agent.graph().mu.Lock()
	node.state = TaskDone
	agent.graph().mu.Unlock()
	row = planRowByID(t, agent.PlanTasks(), "t-actual")
	if !row.Live.Empty() {
		t.Fatalf("settled worker retained live call: %+v", row.Live)
	}
	writePlanTrajectory(t, dir, "actual", `{"kind":"step","step":1,"command":"dedicated"}`)
	row = planRowByID(t, agent.PlanTasks(), "t-actual")
	if row.Steps != 1 {
		t.Fatalf("dedicated trajectory overridden: %+v", row)
	}
}

func TestWorkerJournalFactsCacheTracksAppendReplacementAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.jsonl")
	write := func(p, body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "{\"type\":\"session\",\"model\":\"model/one\"}\n{\"type\":\"took\",\"took\":{\"callId\":\"1\"}}\n")
	for i := 0; i < 2; i++ {
		model, steps := planWorkerJournalFacts(path)
		if model != "model/one" || steps != 1 {
			t.Fatalf("cached read %q %d", model, steps)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"type\":\"took\",\"took\":{\"callId\":\"2\"}}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, steps := planWorkerJournalFacts(path)
	if steps != 2 {
		t.Fatalf("append stale: %d", steps)
	}
	next := path + ".next"
	write(next, "{\"type\":\"session\",\"model\":\"model/two\"}\n")
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	model, steps := planWorkerJournalFacts(path)
	if model != "model/two" || steps != 0 {
		t.Fatalf("replacement stale %q %d", model, steps)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	model, steps = planWorkerJournalFacts(path)
	if model != "" || steps != 0 {
		t.Fatalf("deleted stale %q %d", model, steps)
	}
}

func TestBoundWorkerWithoutRecordedPrimaryDoesNotBorrowAuxiliaryModel(t *testing.T) {
	row := PlanTaskRow{Model: "auxiliary/model", USD: .03, Tokens: 15}
	facts := planWorkerFacts{"actual": {journal: filepath.Join(t.TempDir(), "missing.jsonl")}}
	facts.apply(&row, t.TempDir(), "actual")
	if row.Model != "" || row.USD != .03 || row.Tokens != 15 {
		t.Fatalf("unknown worker borrowed billing model or lost billing: %+v", row)
	}
}
