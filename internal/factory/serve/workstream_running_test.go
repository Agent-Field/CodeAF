package serve

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// WHAT EVERY AGENT IS DOING NOW (owner decisions 2026-11-17 §10; DESIGN
// §11.4). Across the workstreams the reader can see, each task an agent is on
// says what it is doing now, since when, its steps and its cost so far, or its
// place in the queue; a workstream the reader cannot see is not listed; Mine
// keeps their own; and the operator reads the organization's count.
func TestTheRunningViewSaysWhatEveryAgentIsDoingNow(t *testing.T) {
	o, path := taskOrg(t, "edit")
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		turn.Step(api.AgentEntryStep, "read the workstream", "workstream", false)
		turn.Progress(0.02, 1)
		<-ctx.Done()
		return AgentTurnEnd{}, ctx.Err()
	})
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	for i := 1; i <= 4; i++ {
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: fmt.Sprintf("Piece %d", i), DoneBy: afi})
		o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
	}
	var got api.RunningWork
	waitFor(t, func() bool {
		o.do(t, "santosh", "GET", "/v1/workstreams/running", -1, nil, 200, &got)
		n := 0
		for _, it := range got.Items {
			if it.Task.Run != nil && it.Task.Run.Now == "read the workstream" && it.Task.Run.USD == 0.02 && it.Task.Run.Steps == 1 {
				n++
			}
		}
		return n == 3
	})
	if len(got.Items) != 4 || got.Items[3].Task.Queue == nil || got.Items[3].Task.Queue.Place != 1 || got.Items[0].WSName != "Pantry" {
		t.Fatalf("the running view: %+v", got.Items)
	}
	if got.Org != nil {
		t.Fatal("a person who is not the operator read the organization's count")
	}
	var mine api.RunningWork
	o.do(t, "abir", "GET", "/v1/workstreams/running?scope=mine", -1, nil, 200, &mine)
	if len(mine.Items) != 0 {
		t.Fatalf("abir's own: %+v", mine.Items)
	}
	var none api.RunningWork
	o.do(t, "mia", "GET", "/v1/workstreams/running", -1, nil, 200, &none)
	if len(none.Items) != 0 {
		t.Fatalf("mia, who cannot see Pantry, read %+v", none.Items)
	}
	var op api.RunningWork
	o.do(t, "owner", "GET", "/v1/workstreams/running", -1, nil, 200, &op)
	if op.Org == nil || op.Org.Running != 3 || op.Org.Queued != 1 || op.Org.Most != taskRunsPerOrg {
		t.Fatalf("the operator's count: %+v", op.Org)
	}
}
