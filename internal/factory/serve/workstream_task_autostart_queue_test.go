package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A TASK THAT STARTS BY ITSELF WAITS ITS TURN LIKE ONE SOMEBODY STARTED
// (owner decisions 2026-11-17 §1 and §10, DELTA §4.1 C4). Five Afi tasks wait
// on one brief; when the brief is done all five start by themselves, and the
// limits hold: three run, the other two read "Queued · 1st" and "· 2nd", and
// each takes a place as one frees, until all five are done.
func TestTasksReleasedTogetherStartByThemselvesUnderTheLimits(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	var mu sync.Mutex
	release := map[string]chan struct{}{}
	gate := func(title string) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if release[title] == nil {
			release[title] = make(chan struct{})
		}
		return release[title]
	}
	close(gate("Brief"))
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		select {
		case <-gate(turn.Task.Title):
		case <-ctx.Done():
			return AgentTurnEnd{}, ctx.Err()
		}
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done","handoff":"Did the task."}`))
		return AgentTurnEnd{}, err
	})
	brief := o.task(t, "santosh", path, api.TaskCreate{Title: "Brief", DoneBy: afi})
	var ids []string
	for i := 1; i <= 5; i++ {
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: fmt.Sprintf("Piece %d", i), DoneBy: afi, WaitingOn: []string{brief.ID}})
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

	o.do(t, "santosh", "PATCH", path+"/tasks/"+brief.ID, brief.Rev, map[string]any{"state": "running"}, 200, nil)
	waitFor(t, func() bool { l, w := places(); return len(l) == 3 && len(w) == 2 })
	live, waiting := places()
	if len(live) != 3 || len(waiting) != 2 || !strings.HasSuffix(waiting[0], ":1") || !strings.HasSuffix(waiting[1], ":2") {
		t.Fatalf("five released tasks: running %v, waiting %v; want three running and two queued 1st and 2nd", live, waiting)
	}

	for i := 1; i <= 5; i++ {
		close(gate(fmt.Sprintf("Piece %d", i)))
	}
	waitFor(t, func() bool {
		for _, id := range ids {
			if o.one(t, path, id).State != factory.TaskDone {
				return false
			}
		}
		return true
	})
	o.settle()
}
