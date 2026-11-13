package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type prBaseFixture struct {
	rig      *rig
	source   *recoveringPRSource
	id       int
	remote   string
	dir      string
	local    string
	prBranch string
	head     string
	base     string
}

func newPRBaseFixture(t *testing.T, conflict bool) prBaseFixture {
	t.Helper()
	remote := gitRig(t)
	prBranch := "contributor/7-published-name"
	mustGit(t, remote, "switch", "-qc", prBranch)
	changed := "feature.txt"
	if conflict {
		changed = "ledger.go"
	}
	prBaseFixtureCommit(t, remote, changed, "the PR's change\n")
	head := mustGit(t, remote, "rev-parse", "HEAD")
	mustGit(t, remote, "switch", "-q", "main")
	clone := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, "", "clone", "-q", "--no-local", remote, clone)
	changed = "base.txt"
	if conflict {
		changed = "ledger.go"
	}
	prBaseFixtureCommit(t, remote, changed, "the base's change\n")
	base := mustGit(t, remote, "rev-parse", "HEAD")
	dir := filepath.Join(t.TempDir(), "job-worktree")
	local := "contributor/7-local-name"
	mustGit(t, clone, "worktree", "add", "-q", "-b", local, dir, "origin/"+prBranch)
	mustGit(t, clone, "config", "--local", "commit.gpgsign", "false")
	src := &recoveringPRSource{fresh: factory.PullRequest{Number: 17, State: factory.PROpen, HeadSHA: head, BaseSHA: base, HeadBranch: prBranch}}
	g := newRig(t, nil, func(o *Options) {
		o.Workdir = func(factory.Item) (string, error) { return dir, nil }
		o.RepoDir = func(string) string { return clone }
		o.Publisher = func(context.Context, factory.Item) (PublishTarget, error) {
			return PublishTarget{Source: src, Git: ExecGit{}}, nil
		}
	})
	id := g.add("update a PR branch", chat(factory.FollowPRStep))
	err := g.st.Update(id, func(it *factory.Item) error {
		it.State, it.Num, it.Tier = factory.StateRunning, 7, factory.TierCollab
		it.PullRequest = &factory.PullRequest{Number: 17, State: factory.PROpen, HeadSHA: head, HeadBranch: prBranch}
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: head}
		it.Stream = &factory.Stream{
			Phases: []factory.Phase{{Name: factory.FollowPRStep, State: factory.PhaseRunning}},
			Log:    []factory.LogLine{{Text: "branch: " + local}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return prBaseFixture{g, src, id, remote, dir, local, prBranch, head, base}
}

func prBaseFixtureCommit(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "--", name)
	mustGit(t, dir, "commit", "-qm", "change "+name)
}

func TestFollowPRSyncBaseUpdatesOwnedWorktreeAndLeavesRemoteUntouched(t *testing.T) {
	f := newPRBaseFixture(t, false)
	lp := f.rig.r.loop()
	base, words, err := lp.syncPRBase(context.Background(), f.id, f.dir)
	if err != nil || base != f.base || !strings.Contains(words, "merged locally") {
		t.Fatalf("sync base = %q, %q, %v", base, words, err)
	}
	parents := strings.Fields(mustGit(t, f.dir, "show", "-s", "--format=%P", "HEAD"))
	if len(parents) != 2 || parents[0] != f.head || parents[1] != f.base {
		t.Fatalf("merge parents = %v, expected %s and %s", parents, f.head, f.base)
	}
	if got := mustGit(t, f.dir, "branch", "--show-current"); got != f.local {
		t.Fatalf("working branch changed to %q", got)
	}
	if got := mustGit(t, f.remote, "rev-parse", f.prBranch); got != f.head {
		t.Fatalf("sync wrote the remote PR branch to %s", got)
	}
	if got := mustGit(t, f.remote, "rev-parse", "main"); got != f.base {
		t.Fatalf("sync wrote the base branch to %s", got)
	}
	after := mustGit(t, f.dir, "rev-parse", "HEAD")
	base, words, err = lp.syncPRBase(context.Background(), f.id, f.dir)
	if err != nil || base != f.base || !strings.Contains(words, "already included") || mustGit(t, f.dir, "rev-parse", "HEAD") != after || f.source.reads != 2 {
		t.Fatalf("repeated sync = %q, %q, %v, after %d fresh reads", base, words, err, f.source.reads)
	}
}

func TestFollowPRSyncBaseKeepsConflictForStageToResolve(t *testing.T) {
	f := newPRBaseFixture(t, true)
	lp := f.rig.r.loop()
	for i := 0; i < 2; i++ {
		base, words, err := lp.syncPRBase(context.Background(), f.id, f.dir)
		if err != nil || base != f.base || !strings.Contains(words, "conflict") {
			t.Fatalf("sync base round %d = %q, %q, %v", i, base, words, err)
		}
		if got := mustGit(t, f.dir, "rev-parse", "MERGE_HEAD"); got != f.base {
			t.Fatalf("merge in progress points at %q", got)
		}
		if got := mustGit(t, f.dir, "diff", "--name-only", "--diff-filter=U"); got != "ledger.go" {
			t.Fatalf("conflicts = %q", got)
		}
	}
	if got := mustGit(t, f.dir, "rev-parse", "HEAD"); got != f.head {
		t.Fatalf("unresolved merge changed HEAD to %q", got)
	}
}

func TestFollowPRSyncBaseActionBindsApprovalAndChangeToTheUpdatedCheckout(t *testing.T) {
	f := newPRBaseFixture(t, false)
	lp := f.rig.r.loop()
	it, err := f.rig.st.Get(f.id)
	if err != nil {
		t.Fatal(err)
	}
	it.ChangeBase = mustGit(t, f.dir, "rev-parse", "HEAD^")
	c := &loopCtl{id: f.id, ctx: context.Background()}
	readiness := it
	readiness.FollowUp = &factory.PRFollowUp{Kind: "readiness"}
	if lp.baseSyncAction(c, readiness, chat(factory.FollowPRStep), f.dir) != nil || lp.baseSyncAction(c, it, chat("write"), f.dir) != nil {
		t.Fatal("base sync was exposed outside a PR repair")
	}
	action := lp.baseSyncAction(c, it, chat(factory.FollowPRStep), f.dir)
	if action == nil {
		t.Fatal("Follow PR did not expose a base sync action")
	}
	if _, err := action(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := f.rig.st.Get(f.id)
	if err != nil {
		t.Fatal(err)
	}
	if current.FollowUp.BaseSyncSHA != f.base {
		t.Fatalf("pinned base = %s, want %s", current.FollowUp.BaseSyncSHA, f.base)
	}
	if err := lp.checkCIScope(current); err != nil {
		t.Fatal(err)
	}
	common := followChangeBase(ExecGit{}, f.dir, current)
	change, err := readChange(f.dir, common)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Files) != 1 || change.Files[0].Path != "feature.txt" {
		t.Fatalf("PR diff includes base-only changes: %+v", change.Files)
	}
	mustGit(t, f.dir, "reset", "--hard", f.head)
	if err := lp.checkCIScope(current); err == nil || !strings.Contains(err.Error(), "requested PR base") {
		t.Fatalf("checkout without the pinned base passed scope: %v", err)
	}
}

func TestFollowPRSyncBaseRefusesUnrelatedOrStaleWork(t *testing.T) {
	for _, mode := range []string{"dirty", "other branch", "other worktree", "moved head", "different PR", "closed PR", "bad base", "stopped lease"} {
		t.Run(mode, func(t *testing.T) {
			f := newPRBaseFixture(t, false)
			dir := f.dir
			switch mode {
			case "dirty":
				if err := os.WriteFile(filepath.Join(f.dir, "untouched.txt"), []byte("retained work"), 0600); err != nil {
					t.Fatal(err)
				}
			case "other branch":
				mustGit(t, f.dir, "switch", "-qc", "different/7-local-name")
			case "other worktree":
				dir = f.rig.r.opts.RepoDir("acme/api")
			case "moved head":
				f.source.fresh.HeadSHA = f.base
			case "different PR":
				f.source.fresh.Number++
			case "closed PR":
				f.source.fresh.State = factory.PRClosed
			case "bad base":
				f.source.fresh.BaseSHA = "origin/main"
			case "stopped lease":
				f.rig.r.SetPublishCheck(f.id, func(context.Context) error { return factory.ErrPersonStopped })
			}
			base, words, err := f.rig.r.loop().syncPRBase(context.Background(), f.id, dir)
			if err == nil || base != "" || words != "" {
				t.Fatalf("sync base = %q, %q, %v", base, words, err)
			}
			if got := mustGit(t, f.dir, "rev-parse", "HEAD"); got != f.head {
				t.Fatalf("refused sync changed HEAD to %q", got)
			}
			if mode == "dirty" {
				got, err := os.ReadFile(filepath.Join(f.dir, "untouched.txt"))
				if err != nil || string(got) != "retained work" {
					t.Fatalf("dirty work changed to %q, %v", got, err)
				}
			}
		})
	}
}

type prBaseAfterFetch struct {
	Git
	after func()
}

func (g prBaseAfterFetch) Run(dir string, args ...string) (string, error) {
	out, err := g.Git.Run(dir, args...)
	if err == nil && len(args) > 0 && args[0] == "fetch" {
		g.after()
	}
	return out, err
}

func TestFollowPRSyncBaseRechecksAuthorityAfterFetching(t *testing.T) {
	f := newPRBaseFixture(t, false)
	f.rig.r.opts.Publisher = func(context.Context, factory.Item) (PublishTarget, error) {
		git := prBaseAfterFetch{Git: ExecGit{}, after: func() {
			f.rig.r.SetPublishCheck(f.id, func(context.Context) error { return factory.ErrPersonStopped })
		}}
		return PublishTarget{Source: f.source, Git: git}, nil
	}
	base, words, err := f.rig.r.loop().syncPRBase(context.Background(), f.id, f.dir)
	if err != factory.ErrPersonStopped || base != "" || words != "" {
		t.Fatalf("sync after revocation = %q, %q, %v", base, words, err)
	}
	if got := mustGit(t, f.dir, "rev-parse", "HEAD"); got != f.head {
		t.Fatalf("revoked sync changed HEAD to %q", got)
	}
}
