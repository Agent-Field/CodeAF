package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// AT MOST THREE RUNS AT ONCE IN A WORKSTREAM, AND A FAIR QUEUE (owner
// decisions 2026-11-17 §10; DESIGN §12 L6 acceptance 1). Eight independent
// Afi tasks are started at once: three run, the other five wait with their
// places 1 to 5, each starts as a place frees, and all eight finish. A
// second workstream's task starts at once beside them, Stop takes a waiting
// task out of the queue, and Start on a waiting task is not a second start.
func TestEightStartsRunThreeAtATimeAndTheRestWaitTheirTurn(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	var mu sync.Mutex
	release := map[string]chan struct{}{}
	began := []string{}
	gate := func(title string) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if release[title] == nil {
			release[title] = make(chan struct{})
		}
		return release[title]
	}
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		began = append(began, turn.Task.Title)
		mu.Unlock()
		select {
		case <-gate(turn.Task.Title):
		case <-ctx.Done():
			return AgentTurnEnd{}, ctx.Err()
		}
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	})
	var ids []string
	for i := 1; i <= 8; i++ {
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: fmt.Sprintf("Piece %d", i), DoneBy: afi})
		o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
		ids = append(ids, tk.ID)
	}
	places := func() (live, waiting []string) {
		for _, tk := range o.tasks(t, "santosh", path) {
			if tk.State != factory.TaskRunning {
				continue
			}
			if tk.Queue != nil {
				waiting = append(waiting, fmt.Sprintf("%s:%d", tk.Title, tk.Queue.Place))
			} else if tk.Run != nil && tk.Run.Live {
				live = append(live, tk.Title)
			}
		}
		return live, waiting
	}
	live, waiting := places()
	if strings.Join(live, ",") != "Piece 1,Piece 2,Piece 3" || strings.Join(waiting, ",") != "Piece 4:1,Piece 5:2,Piece 6:3,Piece 7:4,Piece 8:5" {
		t.Fatalf("running %v, waiting %v", live, waiting)
	}
	// Start on a waiting task is not a second start, and the sweep leaves it.
	o.h.sweepTaskRuns()
	if _, w := places(); len(w) != 5 {
		t.Fatalf("the sweep touched waiting tasks: %v", w)
	}

	// Another workstream's task does not wait behind this one's queue.
	other := o.create(t, "santosh", api.WorkstreamNew{Name: "Billing"})
	solo := o.task(t, "santosh", "/v1/workstreams/"+other.ID, api.TaskCreate{Title: "Solo", DoneBy: afi})
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+other.ID+"/tasks/"+solo.ID, solo.Rev, map[string]any{"state": "running"}, 200, nil)
	if got := o.one(t, "/v1/workstreams/"+other.ID, solo.ID); got.Queue != nil || got.Run == nil || !got.Run.Live {
		t.Fatalf("a second workstream's task waited: %+v %+v", got.Queue, got.Run)
	}

	// Stop on a waiting task takes it out of the queue; the places close up.
	eight := o.one(t, path, ids[7])
	o.do(t, "santosh", "PATCH", path+"/tasks/"+ids[7], eight.Rev, map[string]any{"state": "stopped"}, 200, nil)
	if _, w := places(); strings.Join(w, ",") != "Piece 4:1,Piece 5:2,Piece 6:3,Piece 7:4" {
		t.Fatalf("after a stop, waiting %v", w)
	}

	// A place frees: the first waiting task takes it.
	close(gate("Piece 1"))
	waitFor(t, func() bool { l, _ := places(); return strings.Contains(strings.Join(l, ","), "Piece 4") })
	if _, w := places(); strings.Join(w, ",") != "Piece 5:1,Piece 6:2,Piece 7:3" {
		t.Fatalf("after one finished, waiting %v", w)
	}
	for i := 2; i <= 7; i++ {
		close(gate(fmt.Sprintf("Piece %d", i)))
	}
	close(gate("Solo"))
	waitFor(t, func() bool {
		for _, id := range ids[:7] {
			if o.one(t, path, id).State != factory.TaskDone {
				return false
			}
		}
		return true
	})
	o.settle()
	mu.Lock()
	defer mu.Unlock()
	if strings.Count(strings.Join(began, ","), "Piece 8") != 0 || len(began) != 8 {
		t.Fatalf("runs began: %v", began)
	}
}

// A FREE PLACE GOES TO THE WORKSTREAM THAT WAITED LONGEST FOR ONE (DESIGN §12
// L6 acceptance 4): with the organization full, a second workstream's one
// waiting task starts before the first workstream's next.
func TestAFreePlaceGoesRoundTheWorkstreams(t *testing.T) {
	d := &agentDesk{tasks: map[taskRunKey]context.CancelFunc{}}
	for i := range taskRunsPerOrg {
		ws := "big"
		if i >= 2 {
			ws = fmt.Sprintf("w%d", i)
		}
		d.tasks[taskRunKey{ws, fmt.Sprint(i)}] = func() {}
		d.servedLocked(ws)
	}
	for i := range 5 {
		d.enqueueLocked(pendingRun{key: taskRunKey{"big", fmt.Sprintf("q%d", i)}})
	}
	d.enqueueLocked(pendingRun{key: taskRunKey{"small", "s"}})
	if d.nextLocked() != -1 {
		t.Fatal("a run started over the organization's limit")
	}
	delete(d.tasks, taskRunKey{"w2", "2"})
	if i := d.nextLocked(); i < 0 || d.queue[i].key.ws != "small" {
		t.Fatalf("the free place went to %+v", d.queue[i])
	}
	if d.placeLocked(taskRunKey{"big", "q2"}) != 3 || d.placeLocked(taskRunKey{"small", "s"}) != 1 {
		t.Fatal("places count inside each workstream")
	}
}

// waitFor waits for ok, failing after five seconds.
func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A TASK WAITING FOR ITS PLACE WHEN THE SERVER RESTARTS WAITS AGAIN: it was
// never working, so it is neither Interrupted nor resumed, and it is asked
// what it was asked.
func TestATaskWaitingForItsPlaceWaitsAgainAfterARestart(t *testing.T) {
	o, path := taskOrg(t, "edit")
	wsID := strings.TrimPrefix(path, "/v1/workstreams/")
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) { return AgentTurnEnd{}, nil })
	tk := o.task(t, "santosh", path, api.TaskCreate{Title: "Waiting", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	setRunningByAgent(t, o, wsID, tk.ID)
	if err := o.st.PutTaskRun(store.TaskRun{WS: wsID, Task: tk.ID, Owner: "srv_gone", Starter: "abir", Started: o.now.Format(time.RFC3339Nano),
		Beat: o.now.Format(time.RFC3339Nano), Words: "Waiting\n\nAs asked.", Queued: true}); err != nil {
		t.Fatal(err)
	}
	o.h.Close()
	o.now = o.now.Add(time.Second)
	var asked AgentTurn
	cfg := o.cfg
	cfg.WorkstreamAgent = func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		asked = turn
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	}
	o.h = New(o.seam, cfg)
	t.Cleanup(o.h.Close)
	o.now = o.now.Add(taskRestartSettle)
	o.h.sweepTaskRuns()
	o.settle()
	if got := o.one(t, path, tk.ID); got.State != factory.TaskDone || o.threadSays(t, path, tk.ID, "Interrupted") {
		t.Fatalf("a waiting task after a restart: %+v", got)
	}
	if asked.Asker != "abir" || asked.Text != "Waiting\n\nAs asked." {
		t.Fatalf("it was asked %+v", asked)
	}
}
