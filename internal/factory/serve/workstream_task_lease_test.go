package serve

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// RUNS SURVIVE A RESTART (owner decisions 2026-11-17 §10; live2 WL, where a
// restart six seconds into a burst left six of eight tasks Running with
// nothing behind them for over nine minutes, and Start on one answered 200
// and did nothing). The server is restarted while Afi works a task: the new
// process finds the run's lease, says the task was interrupted, and resumes
// it once by itself from its own conversation. A task cut off before leases
// existed is resumed the same way; one already resumed once stays
// Interrupted, with Start. Start on a task that says running with nothing
// behind it really starts it again, and a run that goes quiet while the
// server is up is "Didn't report back".
func TestARunCutOffByARestartIsInterruptedThenResumesOnce(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	wsID := strings.TrimPrefix(path, "/v1/workstreams/")

	// The first process: Afi works until the process ends.
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		<-ctx.Done()
		return AgentTurnEnd{}, ctx.Err()
	})
	cut := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the release notes", Request: "Short.", DoneBy: afi})
	o.do(t, "abir", "PATCH", path+"/tasks/"+cut.ID, cut.Rev, map[string]any{"state": "running"}, 200, nil)
	lease := waitLease(t, o.st, wsID, cut.ID)
	if got := o.one(t, path, cut.ID); got.Run == nil || !got.Run.Live || got.Run.Since == "" {
		t.Fatalf("a working task does not say a run is going: %+v", got.Run)
	}
	if lease.Starter != "abir" || lease.Resumes != 0 || lease.Words != "Write the release notes\n\nShort." {
		t.Fatalf("the run's lease is %+v", lease)
	}
	// One cut off before leases were kept: running, and nothing in the store
	// says who held it.
	legacy := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the email", DoneBy: afi})
	setRunningByAgent(t, o, wsID, legacy.ID)
	// One that was already resumed once.
	twice := o.task(t, "santosh", path, api.TaskCreate{Title: "Store text", DoneBy: afi})
	setRunningByAgent(t, o, wsID, twice.ID)
	if err := o.st.PutTaskRun(store.TaskRun{WS: wsID, Task: twice.ID, Owner: "srv_gone", Starter: "santosh",
		Started: o.now.Format(time.RFC3339Nano), Beat: o.now.Format(time.RFC3339Nano), Resumes: 1}); err != nil {
		t.Fatal(err)
	}

	// The restart: the process closes with its runs going.
	o.h.Close()
	if got := o.one(t, path, cut.ID); got.State != factory.TaskRunning {
		t.Fatalf("closing changed the task: %+v", got)
	}
	kept, ok, _ := o.st.TaskRunOf(wsID, cut.ID)
	if !ok {
		t.Fatal("closing dropped the lease the next start must find")
	}
	// What a person typed for the run is kept on its lease, so the resumed
	// run may still follow an address in it.
	kept.Typed = "Use https://typed.example/notes"
	if err := o.st.PutTaskRun(kept); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := o.st.TaskRunOf(wsID, cut.ID); again.Typed != kept.Typed {
		t.Fatalf("the lease kept typed %q", again.Typed)
	}
	o.now = o.now.Add(time.Second)
	var mu sync.Mutex
	asked := map[string]AgentTurn{}
	cfg := o.cfg
	cfg.WorkstreamAgent = func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		asked[turn.Task.Title] = turn
		mu.Unlock()
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done","handoff":"Wrote the release notes."}`))
		return AgentTurnEnd{}, err
	}
	o.h = New(o.seam, cfg)
	t.Cleanup(o.h.Close)

	// Before the settle wait, a lease another process may still be beating
	// is left alone.
	o.h.sweepTaskRuns()
	if got := o.one(t, path, cut.ID); got.State != factory.TaskRunning || len(asked) != 0 {
		t.Fatalf("the sweep settled a run before it could know it was cut off: %+v", got)
	}

	o.now = o.now.Add(taskRestartSettle)
	o.h.sweepTaskRuns()
	o.settle()
	for _, tk := range []api.WorkstreamTask{cut, legacy} {
		if got := o.one(t, path, tk.ID); got.State != factory.TaskDone {
			t.Fatalf("%q was not resumed: %+v", tk.Title, got)
		}
		if !o.threadSays(t, path, tk.ID, taskInterruptedLine) || !o.threadSays(t, path, tk.ID, taskResumedLine) {
			t.Fatalf("%q's thread does not say it was interrupted and resumed", tk.Title)
		}
		if _, ok, _ := o.st.TaskRunOf(wsID, tk.ID); ok {
			t.Fatalf("%q's lease outlived its run", tk.Title)
		}
	}
	if turn := asked["Write the release notes"]; !slices.Contains(turn.LeashWords, "Use https://typed.example/notes") {
		t.Fatalf("the resumed run lost what the person typed: %q", turn.LeashWords)
	}
	if turn := asked["Write the release notes"]; turn.Asker != "abir" || !strings.Contains(turn.Text, "Carry on from where you stopped") ||
		!strings.Contains(turn.Text, "Write the release notes\n\nShort.") || !strings.Contains(turn.Dir, "/tasks/"+cut.ID+"/") {
		t.Fatalf("the resumed run was handed %+v", turn)
	}
	if got := o.one(t, path, twice.ID); got.State != factory.TaskInterrupted || got.StateReason != taskInterruptedWhy {
		t.Fatalf("a run cut off a second time: %+v", got)
	}
	if _, ran := asked["Store text"]; ran {
		t.Fatal("a run already resumed once was resumed again")
	}

	// Start on a task that says running with nothing behind it starts it.
	stuck := o.task(t, "santosh", path, api.TaskCreate{Title: "Help article", DoneBy: afi})
	setRunningByAgent(t, o, wsID, stuck.ID)
	now := o.one(t, path, stuck.ID)
	if now.Run == nil || now.Run.Live {
		t.Fatalf("a stuck task does not say nothing is working on it: %+v", now.Run)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+stuck.ID, now.Rev, map[string]any{"state": "running"}, 200, nil)
	o.settle()
	if _, ran := asked["Help article"]; !ran {
		t.Fatal("Start on a stuck task did nothing")
	}
	if got := o.one(t, path, stuck.ID); got.State != factory.TaskDone || !o.threadSays(t, path, stuck.ID, "Started again by") {
		t.Fatalf("Start on a stuck task left %+v", got)
	}

	// A run that goes quiet while the server is up is Didn't report back.
	quiet := o.task(t, "santosh", path, api.TaskCreate{Title: "Support macro", DoneBy: afi})
	setRunningByAgent(t, o, wsID, quiet.ID)
	o.h.sweepTaskRuns()
	if got := o.one(t, path, quiet.ID); got.State != factory.TaskRunning {
		t.Fatalf("a fresh run was settled: %+v", got)
	}
	o.now = o.now.Add(taskNoReportAfter)
	o.h.sweepTaskRuns()
	if got := o.one(t, path, quiet.ID); got.State != factory.TaskNoReport || got.StateReason != taskNoReportWhy {
		t.Fatalf("a quiet run: %+v", got)
	}
}

// waitLease waits for task's run to keep its lease: the run keeps it from its
// own goroutine once the write that started it has ended.
func waitLease(t *testing.T, st *store.Store, ws, task string) store.TaskRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if l, ok, err := st.TaskRunOf(ws, task); err == nil && ok {
			return l
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the run never kept its lease")
	return store.TaskRun{}
}

// setRunningByAgent marks task running through Afi's own door, which starts
// no run: a task saying running with nothing behind it.
func setRunningByAgent(t *testing.T, o *wsOrg, ws, task string) {
	t.Helper()
	d := &agentDoor{h: o.h, id: ws, asker: "santosh"}
	if _, err := d.Work(context.Background(), "ws_task_update", json.RawMessage(`{"task":"`+task+`","state":"running"}`)); err != nil {
		t.Fatal(err)
	}
}
