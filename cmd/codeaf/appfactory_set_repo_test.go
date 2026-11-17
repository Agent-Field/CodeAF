package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// OUTSIDE WORK WITH NO REPOSITORY, END TO END ON THE WIRE. Start is refused
// as a definite conflict in the factory's own words ("say which repository
// the work is on"), never as an uncertain failure the desk drops. The job's
// operator gives it a repository with factory.job_set_repo, which the hello
// announces; then Start queues it, and the repository stays.
func TestAppFactoryAJobWithNoRepositoryIsGivenOne(t *testing.T) {
	routed := 0
	f := startActionFactoryWith(t, func(c *serve.Config) {
		c.Route = func(context.Context) error { routed++; return nil }
	})
	// The operator, attached with the factory's own token: a signed-in
	// teammate sees only the jobs of repositories they can read on GitHub,
	// which a job with none is not, so placing one is the operator's.
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	op := &person{w: newFactoryWire(t, hub), handle: "operator"}
	op.w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &op.conn)
	op.w.mu.Lock()
	hello := op.w.lines[0]
	op.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryJobSetRepo) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobSetRepo, hello)
	}
	job := f.create(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Origin: "jira", SourceRef: "jira:FL-1"})
	if err := f.st.Update(job.ID, func(cur *factory.Item) error { cur.Repo, cur.Places = "", nil; return nil }); err != nil {
		t.Fatal(err)
	}
	n := job.ID

	msg, _ := op.refused(t, "factory.job_start", op.params(t, n, nil), appwire.CodeConflict)
	if !strings.Contains(msg, factory.NoRepoRefusal) || strings.Contains(msg, "may have happened") {
		t.Fatalf("start on a job with no repository said %q", msg)
	}
	if f.launched.Load() != 0 {
		t.Fatal("a job with no repository was launched")
	}
	if p := op.job(t, n).StatePresentation; p.Title != factory.NoRepoTitle || p.Detail != factory.NoRepoDetail {
		t.Fatalf("the waiting job reads %+v", p)
	}

	op.w.mustFail("factory.job_set_repo", op.params(t, n, map[string]any{"repo": "api"}), appwire.CodeBadParams)
	set := op.act(t, "factory.job_set_repo", n, map[string]any{"repo": "acme/api"})
	if set.Action != appwire.JobActSetRepo || set.Current == nil || set.Current.Repo != "acme/api" {
		t.Fatalf("set repo: %+v", set)
	}
	if routed != 1 {
		t.Fatalf("routed %d times after the repository was set, want once", routed)
	}
	started := op.act(t, "factory.job_start", n, nil)
	if f.launched.Load() != 1 || started.Current == nil {
		t.Fatalf("start after the repository was set: launched %d, %+v", f.launched.Load(), started.Current)
	}
	op.refused(t, "factory.job_set_repo", op.params(t, n, map[string]any{"repo": "acme/web"}), appwire.CodeConflict)
}
