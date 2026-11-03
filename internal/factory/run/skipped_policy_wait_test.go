package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestSkippedRequiredCheckExplainsPolicyWaitToClient(t *testing.T) {
	dir := gitRig(t)
	src := &fakeSource{}
	policy := []string{"tests pass before anything posts"}
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			path := "docs.md"
			if job.Round > 1 {
				path = "auth.go"
			}
			if err := os.WriteFile(filepath.Join(job.Dir, path), []byte("change\n"), 0600); err != nil {
				return factory.StageResult{}, err
			}
			return done("written"), nil
		}),
		factory.StageCheck: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "inspect passed", OK: true, Medium: "test"}}}, nil
		}),
		factory.StagePost: NewPostExecutor(func(string) factory.Source { return src }, func(string) []string { return policy }),
	}, func(o *Options) { o.RepoDir = func(string) string { return dir } })
	id := g.add("docs only", chat("write"), factory.Stage{Name: "inspect", Kind: factory.StageCheck, When: "touches auth"}, factory.Stage{Name: "post", Kind: factory.StagePost, Ask: "pr"})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateNeedsYou)
	job := api.FromItem(it)
	want := "Publishing waits: check inspect was skipped (when touches auth does not match the change files). Rerun from write after revising the change files to match, or stop the job."
	if len(job.Questions) != 1 || job.Questions[0].State != "open" || !strings.Contains(job.Questions[0].Text, want) {
		t.Fatalf("client waiting reason missing %q: %+v", want, job.Questions)
	}
	if job.NeedsYou == nil || job.NeedsYou.Question == nil || !strings.Contains(job.NeedsYou.Question.Text, want) {
		t.Fatalf("Needs you lost waiting reason: %+v", job.NeedsYou)
	}
	src.mu.Lock()
	posted := len(src.got)
	src.mu.Unlock()
	if posted != 0 || it.State == factory.StateShipped {
		t.Fatal("skipped required check published without a person")
	}
	if _, err := g.r.SignOff(id, false); err == nil {
		t.Fatal("approval bypassed the policy wait")
	}
	// The step rerun door stops the active runner before rewinding its stored run.
	if err := g.r.Stop(id); err != nil {
		t.Fatal(err)
	}
	if err := g.st.Update(id, func(it *factory.Item) error {
		if err := StepControl(it, "write", true, "revise files to match inspect", g.r.now()); err != nil {
			return err
		}
		it.State, it.Stream.Paused = factory.StateRunning, true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	it = g.wait(id, "published after rerun", func(it factory.Item) bool {
		return it.State == factory.StateLanded || it.State == factory.StateShipped
	})
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.got) != 1 || it.Stream.Phases[1].State != factory.PhaseDone || len(api.FromItem(it).Questions) != 0 {
		t.Fatalf("rerun did not clear wait and publish: posts=%d job=%+v", len(src.got), api.FromItem(it))
	}
}
