package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

func TestIdleRelease_C2_ParkedTaskRetainsOwnership(t *testing.T) {
	w := &cellstore.SealWatch{}
	w.Waiting()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Seals = w })
	a.SettleWrites()
	if !w.IdleReady() {
		t.Fatal("fixture did not become idle")
	}
	p := &stagedProposal{id: a.graph().reserve()}
	_, parked := a.parkUnroutedHandoff(context.Background(), p, taskSpec{title: "Waiting task", brief: "repair fixtures"}, "", nil, &crewroute.NoEligibleError{Seat: crewroute.Checker, Mode: crewroute.ModeAuto})
	if !parked {
		t.Fatal("fixture did not park the task")
	}
	row, ok := a.appRunRow(p.id)
	if !ok || row.State != TaskQueued {
		t.Fatalf("fixture lost queued task: %+v", row)
	}
	a.SettleWrites()
	if w.IdleReady() {
		t.Errorf("queued task state=%s waiting=%s, but IdleReady=true", row.State, row.Waiting)
	}
}

func TestIdleRelease_C2_TaskPreparationRetainsOwnership(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	w := &cellstore.SealWatch{}
	w.Waiting()
	repo, dir := newTestRepo(t), t.TempDir()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = repo
		c.Place = Place{Dir: dir}
		c.SessionFile = filepath.Join(dir, placeTranscript)
		c.Seals = w
		c.TaskMaxLoad, c.TaskMinFreeMB = 0, 0
	})
	a.SettleWrites()
	entered, release := make(chan struct{}), make(chan struct{})
	previous := beltRunPrepare
	beltRunPrepare = func(context.Context, Place, string, string, uint64, string, taskStand) (taskTree, error) {
		close(entered)
		<-release
		return taskTree{}, errors.New("review preparation stopped")
	}
	t.Cleanup(func() { beltRunPrepare = previous })
	done := make(chan error, 1)
	go func() { _, _, _, err := a.StartTask(t.Context(), "repair fixtures", true); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("did not reach preparation: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("preparation did not start")
	}
	idle := w.IdleReady()
	close(release)
	<-done
	if idle {
		t.Error("StartTask is still preparing its working copy, but IdleReady=true")
	}
}
