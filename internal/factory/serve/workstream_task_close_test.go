package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// CLOSE RETURNS ONLY WHEN EVERY RUN HAS WRITTEN ITS LAST LINE. A task's run
// ends its Working conversation and tells everyone watching after the model
// has stopped, and that used to happen after the run had already counted
// itself out of what Close waits for, so Close returned with a write still to
// come. A caller that then removed the factory's folder lost the race (a test's
// TempDir failed with "directory not empty" about one run in three on a slow
// runner). Here the events' lock is held, so that last write cannot finish:
// Close must still be waiting, and must return once the lock is let go.
func TestCloseWaitsForARunsLastEventBeforeItReturns(t *testing.T) {
	o, path := taskOrg(t, "edit")
	started := make(chan struct{}, 1)
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return AgentTurnEnd{}, ctx.Err()
	})
	tk := o.task(t, "santosh", path, api.TaskCreate{Title: "Piece 1", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never started")
	}

	lock, err := os.OpenFile(filepath.Join(o.h.eventRoot(), "live-events.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			_ = filelock.Unlock(lock)
		}
	}()

	closed := make(chan struct{})
	go func() {
		o.h.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while the run's last event was still waiting to be written")
	case <-time.After(300 * time.Millisecond):
	}
	_ = filelock.Unlock(lock)
	held = false
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned once the events could be written")
	}
}
