package run

import (
    "context"
    "sync/atomic"
    "testing"
    "time"

    "github.com/Agent-Field/codeaf/internal/factory"
    "github.com/Agent-Field/codeaf/internal/factory/store"
)

// reviewStopMergeForge pauses a worker's fresh PR read to deliver Stop on
// the authoritative server. The worker has not received its next heartbeat.
type reviewStopMergeForge struct {
    *fakeForge
    stop func() error
}

func (f *reviewStopMergeForge) Follow(ctx context.Context, it factory.Item) (factory.PullRequest, error) {
    if err := f.stop(); err != nil { return factory.PullRequest{}, err }
    return f.fakeForge.Follow(ctx, it)
}

func TestReviewStopPreventsMergeBeforeNextHeartbeat(t *testing.T) {
    base := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
    runner, mirror, id := mergeRig(t, base, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
    authoritative, err := store.Open(t.TempDir())
    if err != nil { t.Fatal(err) }
    cached, err := mirror.Get(id)
    if err != nil { t.Fatal(err) }
    cached.ID = 0
    saved, err := authoritative.Create(cached)
    if err != nil { t.Fatal(err) }
    var checks atomic.Int32
    runner.SetPublishCheck(id, func(context.Context) error {
        checks.Add(1)
        cur, err := authoritative.Get(saved.ID)
        if err != nil { return err }
        return factory.CheckPersonStop(cur)
    })
    source := &reviewStopMergeForge{fakeForge: base, stop: func() error { return authoritative.Update(saved.ID, func(it *factory.Item) error { if err := StopItem(it, time.Now()); err != nil { return err }; factory.MarkPersonStopped(it); return nil }) }}
    runner.opts.Source = func(string) factory.Source { return source }
    if err := runner.Merge(id, runMergeHead); err != nil { t.Fatal(err) }
    settleMerge(t, runner, mirror, id)
    stopped, err := authoritative.Get(saved.ID)
    if err != nil || !factory.StoppedByPerson(stopped) { t.Fatalf("Stop did not persist: %+v %v", stopped, err) }
    if len(base.wrote) != 0 { t.Fatalf("Stop persisted before forge mutation, but worker sent %d merge(s); authoritative boundary checks=%d", len(base.wrote), checks.Load()) }
}

func TestReviewStopPreventsProofOnlyComment(t *testing.T) {
    g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Prove it", Proof: []factory.Claim{{Text: "checks pass", OK: true}}})
    mustGit(t, g.dir, "restore", "ledger.go")
    base := mustGit(t, g.dir, "rev-parse", "HEAD")
    mustGit(t, g.dir, "update-ref", "refs/remotes/origin/trunk", base)
    mustGit(t, g.dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
    var checks atomic.Int32
    g.r.SetPublishCheck(g.id, func(context.Context) error { checks.Add(1); return factory.ErrPersonStopped })
    g.r.opts.Publisher = func(context.Context, factory.Item) (PublishTarget, error) {
        // The server's Stop persists before the worker can send a comment.
        if err := g.st.Update(g.id, func(it *factory.Item) error { if err := StopItem(it, time.Now()); err != nil { return err }; factory.MarkPersonStopped(it); return nil }); err != nil { return PublishTarget{}, err }
        return PublishTarget{Source: g.src, Push: g.r.opts.Push}, nil
    }
    if err := g.r.Publish(g.id, testTrailer); err != nil { t.Fatal(err) }
    stopped := g.settle(t)
    if !factory.StoppedByPerson(stopped) { t.Fatal("Stop did not persist") }
    if len(g.src.got) != 0 { t.Fatalf("Stop persisted before forge mutation, but proof-only publish sent %d comment(s); authority checks=%d", len(g.src.got), checks.Load()) }
}
