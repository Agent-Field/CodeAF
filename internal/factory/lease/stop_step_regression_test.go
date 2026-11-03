package lease_test

import (
    "context"
    "errors"
    "sync/atomic"
    "testing"
    "time"

    "github.com/Agent-Field/codeaf/internal/factory"
    "github.com/Agent-Field/codeaf/internal/factory/lease"
    "github.com/Agent-Field/codeaf/internal/factory/run"
    "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestReviewStopRevokedLeaseCannotAdmitNextStep(t *testing.T) {
    f := setup(t)
    stages := []factory.Stage{
        {Name: "first", Kind: factory.StageChat, On: true, Ask: "first", Until: "done"},
        {Name: "second", Kind: factory.StageChat, On: true, Ask: "second", Until: "done"},
    }
    it := f.add(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Stages: stages})
    held, err := f.desk.Pull(worker("w1", "sam"))
    if err != nil { t.Fatal(err) }
    snapshot, err := f.store.Get(it.ID)
    if err != nil { t.Fatal(err) }
    snapshot.State = factory.StateNew
    mirror, err := store.Open(t.TempDir())
    if err != nil { t.Fatal(err) }
    if err := mirror.Save(snapshot); err != nil { t.Fatal(err) }
    entered, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
    var lost atomic.Int32
    r := run.New(run.Options{Store: mirror, Benches: 1, Exec: map[factory.StageKind]run.Executor{
        factory.StageChat: run.ExecutorFunc(func(ctx context.Context, job run.Job) (factory.StageResult, error) {
            if job.Stage.Name == "first" {
                close(entered)
                select { case <-release: case <-ctx.Done(): return factory.StageResult{}, ctx.Err() }
            } else { second <- struct{}{} }
            return factory.StageResult{Done: true, Output: "done", Spent: .01}, nil
        }),
    }})
    t.Cleanup(func(){ _ = r.Stop(it.ID) })
    // This is workerStopAfterStep's real lease check, without periodic polls.
    r.SetStopAfterStepReader(it.ID, func(context.Context) (bool, error) {
        _, err := f.desk.Renew(held.ID, held.Worker)
        if errors.Is(err, lease.ErrNoLease) { lost.Add(1) }
        return false, err
    })
    if err := r.Launch(it.ID); err != nil { t.Fatal(err) }
    select { case <-entered: case <-time.After(2*time.Second): t.Fatal("first step never started") }
    if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil { t.Fatal(err) }
    stopped, err := f.store.Get(it.ID)
    if err != nil || !factory.StoppedByPerson(stopped) { t.Fatalf("authoritative Stop missing: %+v %v", stopped, err) }
    close(release)
    select {
    case <-second:
        t.Fatalf("second paid step ran after authoritative Stop; boundary had observed ErrNoLease %d times", lost.Load())
    case <-time.After(2*time.Second):
        if lost.Load() == 0 { t.Fatal("runner did not check the revoked lease") }
    }
}
