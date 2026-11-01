package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// R7-11 and contract 2 preserve lifecycle records without inventing tool steps.
func TestMirroredBeginAndEndAreNotDrawnAsSteps(t *testing.T) {
	taskComputerChoiceHome(t)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		if err := spec.SaveRemote(r, true); err != nil {
			t.Error(err)
		}
		for i, raw := range []string{`{"kind":"begin","brief":"repair"}`, `{"kind":"step","step":1,"command":"inspect"}`, `{"kind":"notes","observation":"progress"}`, `{"kind":"end","outcome":"done"}`} {
			if err := spec.OnRemoteStep(uint64(i+1), json.RawMessage(raw)); err != nil {
				t.Error(err)
			}
		}
		_ = spec.Store.CompleteRoot("done")
		return RunSummary{Outcome: "done"}
	}}
	a, _ := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	page, ok := a.PlanTaskPage("t-1")
	if !ok || len(page.Steps) != 1 || page.Steps[0].Command != "inspect" {
		t.Fatalf("drawn steps = %+v", page.Steps)
	}
	raw, err := os.ReadFile(filepath.Join(plandb.TaskDir(filepath.Dir(a.graph().planPath()), "1"), "trajectory.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"begin", "notes", "end"} {
		var found bool
		for _, line := range bytes.Split(raw, []byte{'\n'}) {
			var record struct {
				Kind string `json:"kind"`
			}
			_ = json.Unmarshal(line, &record)
			found = found || record.Kind == kind
		}
		if !found {
			t.Errorf("lost %s kind in %s", kind, raw)
		}
	}
}

type ackOwedEngine struct {
	scriptedTaskEngine
	ack chan TaskRemoteRecord
}

func (e *ackOwedEngine) Acknowledge(_ context.Context, r TaskRemoteRecord) error {
	e.ack <- r
	return nil
}

// R7-13 and contract 10 recover the sidecar obligation even when the checkpoint predates it.
func TestAnAckOwedWhenTheConversationCrashedIsSentOnReopen(t *testing.T) {
	taskComputerChoiceHome(t)
	ready := make(chan struct{})
	remote := &ackOwedEngine{ack: make(chan TaskRemoteRecord, 2)}
	remote.start = func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		close(ready)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace = repo
			c.Place = Place{Dir: place}
			c.SessionFile = c.Place.Transcript()
			c.ProfileDir = profile
			c.TaskComputers = testTaskComputers{engine: remote}
		})
		return a
	}
	local := newBeltRunDouble("done")
	local.real = true
	registerBeltRunEngine(t, local)
	a := open()
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	_ = a.Close()
	waitRemoteRun(t, a)
	store, err := openRemoteTaskPlan(a.graph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := runRowOf(a.graph(), 1)
	r := readRemoteRecord(store, row.Remote)
	r.Settled = true
	r.Finished = true
	r.AckPending = true
	if err := writeRemoteRecord(store, *r); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	b := open()
	defer b.Close()
	select {
	case got := <-remote.ack:
		if got.Attempt != r.Attempt {
			t.Fatalf("wrong ack: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("sidecar acknowledgement was not sent on reopen")
	}
}

// R7-10 and contract 11 give the local worker a clean record of effects in its own tree.
func TestRunItHereStartsWithAFreshRecord(t *testing.T) {
	taskComputerChoiceHome(t)
	ready := make(chan struct{})
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		r.Unreachable = true
		if err := spec.SaveRemote(r, true); err != nil {
			t.Error(err)
		}
		if err := spec.OnRemoteStep(1, json.RawMessage(`{"kind":"step","step":1,"command":"remote-only effect"}`)); err != nil {
			t.Error(err)
		}
		close(ready)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	if _, err := a.RunTaskHere(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	<-local.entered
	defer endBeltRun(t, a, local)
	local.mu.Lock()
	dir := plandb.TaskDir(filepath.Dir(local.spec.Store.Path()), local.spec.Store.RootID())
	local.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(dir, "trajectory.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("local worker inherited remote effects: %s", raw)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "trajectory-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("abandoned record not archived: %v", archives)
	}
	saved, err := os.ReadFile(archives[0])
	if err != nil || !strings.Contains(string(saved), "remote-only effect") {
		t.Fatalf("remote record lost: %s, %v", saved, err)
	}
}
