package run

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type writeAccessGitFunc func(string, ...string) (string, error)

func (f writeAccessGitFunc) Run(dir string, args ...string) (string, error) { return f(dir, args...) }

// Jobs without write access refuse merging and save the reason.
func TestStrangerMergeIsRefused(t *testing.T) {
	f := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
	r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
	if err := st.Update(id, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil }); err != nil {
		t.Fatal(err)
	}
	var refused WriteAccessRefusal
	if err := r.Merge(id, runMergeHead); !errors.As(err, &refused) {
		t.Fatalf("merge refusal = %v", err)
	}
	saved, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Failure == nil || saved.Failure.Detail != refuseStrangerWrite {
		t.Fatalf("saved refusal = %+v", saved.Failure)
	}
	if len(f.wrote) > 0 {
		t.Fatalf("merge must not write: %q", f.wrote[0].Verb)
	}
}

// Jobs without write access refuse publication and save the reason.
func TestStrangerPublicationIsRefused(t *testing.T) {
	g := newPublishRig(t, factory.Item{Title: "outside issue", Repo: "acme/api", Num: 12, Kind: factory.KindIssue, Tier: factory.TierStranger})
	mustGit(t, g.dir, "add", "ledger.go")
	mustGit(t, g.dir, "commit", "-q", "-m", "prepared before tier refresh")
	head := mustGit(t, g.dir, "rev-parse", "HEAD")
	if err := g.st.Update(g.id, func(it *factory.Item) error { it.HeadCommit = head; return nil }); err != nil {
		t.Fatal(err)
	}
	var refused WriteAccessRefusal
	if err := g.r.Publish(g.id, testTrailer+" "+head); !errors.As(err, &refused) {
		t.Fatalf("publication refusal = %v", err)
	}
	it, err := g.st.Get(g.id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Failure == nil || it.Failure.Detail != refuseStrangerWrite {
		t.Fatalf("saved refusal = %+v", it.Failure)
	}
	if len(g.src.got) > 0 {
		remote := mustGit(t, g.bare, "rev-parse", "refs/heads/"+branchFor(it))
		t.Fatalf("publication must not write: head=%s verb=%q", remote, g.src.got[0].Verb)
	}
}

// Approval alone may run; preparing a commit requires write access.
func TestStrangerLandingRefusesCommit(t *testing.T) {
	var staged atomic.Int32
	g := newRig(t, nil, func(o *Options) {
		o.Workdir = func(factory.Item) (string, error) { return t.TempDir(), nil }
		o.Git = writeAccessGitFunc(func(_ string, args ...string) (string, error) {
			if args[0] == "status" {
				return " M victim.txt", nil
			}
			if args[0] == "add" {
				staged.Add(1)
			}
			return runMergeHead, nil
		})
	})
	id := strangerJob(t, g, factory.KindIssue, []factory.Stage{{Name: "approve", Kind: factory.StageGate, On: true}})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "saved refusal", func(it factory.Item) bool { return it.Failure != nil && factory.RunEnded(it) })
	if staged.Load() > 0 {
		t.Fatal("landing must not stage changes")
	}
}

// Outside jobs need a recorded tier before running work that can write.
func TestOutsideJobWithoutTierIsRefused(t *testing.T) {
	for _, form := range []struct {
		name string
		item factory.Item
	}{
		{"origin", factory.Item{Origin: factory.OriginForge}},
		{"number", factory.Item{Num: 12}},
		{"url", factory.Item{URL: "https://example.test/jobs/12"}},
	} {
		for _, add := range []bool{false, true} {
			t.Run(form.name+map[bool]string{false: "/saved", true: "/Add"}[add], func(t *testing.T) {
				var calls atomic.Int32
				g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { calls.Add(1); return done(""), nil })}, nil)
				it := form.item
				it.Title = "outside job"
				it.Repo = "acme/api"
				it.Kind = factory.KindIssue
				it.Stages = []factory.Stage{{Name: "review", Kind: factory.StageChat, On: true}}
				var id int
				if add {
					var err error
					id, err = g.st.Add(context.Background(), it)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					saved, err := g.st.Create(it)
					if err != nil {
						t.Fatal(err)
					}
					id = saved.ID
				}
				var refused WriteAccessRefusal
				if err := g.r.Launch(id); !errors.As(err, &refused) {
					t.Fatalf("permission refusal = %v", err)
				}
				saved, err := g.st.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 0 || saved.Tier != "" || saved.Failure == nil || saved.Failure.Detail != refuseStrangerWrite {
					t.Fatalf("calls=%d tier=%q refusal=%+v", calls.Load(), saved.Tier, saved.Failure)
				}
			})
		}
	}
}

// A person's own job with no recorded tier retains its existing permission.
func TestPersonalJobWithoutTierRuns(t *testing.T) {
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { calls.Add(1); return done(""), nil })}, nil)
	it, err := g.st.Create(factory.Item{Title: "my job", Repo: "acme/api", Kind: factory.KindIssue, Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	saved := g.waitState(it.ID, factory.StateLanded)
	if calls.Load() != 1 || saved.Tier != "" || saved.Failure != nil {
		t.Fatalf("calls=%d tier=%q refusal=%+v", calls.Load(), saved.Tier, saved.Failure)
	}
}

// Both local creation doors and Store.Add produce runnable personal jobs.
func TestPersonalCreationPathsRun(t *testing.T) {
	for _, path := range []string{"localNew", "NewJob", "Store.Add"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			g := newRig(t, map[factory.StageKind]Executor{factory.StageCheck: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { calls.Add(1); return done(""), nil })}, nil)
			seam := factory.LocalSeam(g.st, time.Now())
			var id int
			var err error
			switch path {
			case "localNew":
				id, err = seam.New("acme/api", "my job")
			case "NewJob":
				id, err = seam.NewJob("acme/api", "my job", "my work")
			case "Store.Add":
				id, err = g.st.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "my job"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Stages = []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			g.waitState(id, factory.StateLanded)
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}

// Permission is checked before follow-up reconciliation.
func TestStrangerFollowUpRefusesReconciliation(t *testing.T) {
	var merges atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		t.Error("model must be refused")
		return done(""), nil
	})}, func(o *Options) {
		o.Workdir = func(it factory.Item) (string, error) {
			err := o.Store.Update(it.ID, func(cur *factory.Item) error { cur.Tier = factory.TierStranger; return nil })
			return t.TempDir(), err
		}
		o.Git = writeAccessGitFunc(func(_ string, args ...string) (string, error) {
			if args[0] == "merge-base" {
				return "", errors.New("not ancestor")
			}
			if args[0] == "fetch" || strings.Contains(strings.Join(args, " "), "merge --no-edit") {
				merges.Add(1)
			}
			return "", nil
		})
	})
	id := g.add("follow external issue", factory.Stage{Name: factory.FollowPRStep, Kind: factory.StageChat, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error {
		pr := cleanPR()
		it.PullRequest = &pr
		it.FollowUp = &factory.PRFollowUp{State: "queued", HeadSHA: runMergeHead}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "refused", func(it factory.Item) bool { return it.Failure != nil && factory.RunEnded(it) })
	if merges.Load() > 0 {
		t.Fatalf("follow-up must not reconcile without write access: calls=%d", merges.Load())
	}
}

// Publication checks current permission at each external write.
func TestPublicationRechecksPermission(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "prepared work"})
	g.r.opts.Push = func(string, string, ...func() error) error {
		return g.st.Update(g.id, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil })
	}
	if err := g.r.Publish(g.id, testTrailer); err != nil {
		t.Fatal(err)
	}
	it := g.settle(t)
	if len(g.src.got) != 0 || it.Failure == nil || it.Failure.Detail != refuseStrangerWrite {
		t.Fatalf("writes=%d refusal=%+v", len(g.src.got), it.Failure)
	}
}

// Merging checks current permission after reading the source again.
func TestMergeRechecksPermission(t *testing.T) {
	base := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
	r, st, id := mergeRig(t, base, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
	r.opts.Source = func(string) factory.Source {
		return &reviewStopMergeForge{fakeForge: base, stop: func() error {
			return st.Update(id, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil })
		}}
	}
	if err := r.Merge(id, runMergeHead); err != nil {
		t.Fatal(err)
	}
	it := settleMerge(t, r, st, id)
	if len(base.wrote) != 0 || it.Failure == nil || it.Failure.Detail != refuseStrangerWrite {
		t.Fatalf("writes=%d refusal=%+v", len(base.wrote), it.Failure)
	}
}
