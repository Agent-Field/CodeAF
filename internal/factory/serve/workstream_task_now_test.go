package serve

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A RUNNING ROW SAYS WHAT IT IS DOING NOW (v2 screens 2 and 6; DELTA §4.1,
// L6). A task Afi is working on carries its last step in a person's words,
// its steps and what it has spent so far; a task waiting for its place carries
// its place instead, and no step it never took.
func TestARunningTaskSaysWhatItIsDoingNowAndAQueuedOneItsPlace(t *testing.T) {
	o, path := taskOrg(t, "edit")
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		turn.Step(api.AgentEntryStep, "Reading crash log 9 of 14", "workstream", false)
		turn.Progress(0.02, 1)
		<-ctx.Done()
		return AgentTurnEnd{}, ctx.Err()
	})
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	var ids []string
	for i := 1; i <= taskRunsPerWorkstream+1; i++ {
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: fmt.Sprintf("Piece %d", i), DoneBy: afi})
		o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
		ids = append(ids, tk.ID)
	}
	first := ids[0]
	waitFor(t, func() bool {
		r := o.one(t, path, first).Run
		return r != nil && r.Now == "Reading crash log 9 of 14" && r.Steps == 1 && r.USD == 0.02
	})
	last := o.one(t, path, ids[len(ids)-1])
	if last.Queue == nil || last.Queue.Place != 1 {
		t.Fatalf("the task over the limit waits for its place: %+v", last.Queue)
	}
	if last.Run != nil && last.Run.Now != "" {
		t.Fatalf("a queued task said it was doing %q", last.Run.Now)
	}
}
