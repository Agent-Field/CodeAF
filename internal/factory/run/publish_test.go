package run

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const testTrailer = "Factory-Approval: evt_0123abcd @octocat"

// prSource answers a pull request receipt with its number.
type prSource struct{ fakeSource }

func (p *prSource) Write(ctx context.Context, a factory.Action) (factory.Receipt, error) {
	if _, err := p.fakeSource.Write(ctx, a); err != nil {
		return factory.Receipt{}, err
	}
	return factory.Receipt{URL: "https://github.com/acme/api/pull/7", Number: 7}, nil
}

// publishRig is a landed job in a real worktree whose checkout pushes to a
// bare remote, and a runner that publishes through a fake source.
type publishRig struct {
	st   *store.Store
	r    *Runner
	src  *prSource
	bare string
	dir  string
	id   int
}

func newPublishRig(t *testing.T, it factory.Item) publishRig {
	t.Helper()
	checkout := gitRig(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "-q", "--bare", bare)
	mustGit(t, checkout, "remote", "add", "origin", bare)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it.State = factory.StateLanded
	it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone}}}
	saved, err := st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	dir, err := w.For(saved, checkout)
	if err != nil {
		t.Fatal(err)
	}
	// The job's work, left uncommitted in its worktree as a stage leaves it.
	if err := os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("package ledger\n\nconst fixed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "config", "user.name", "t")
	mustGit(t, dir, "config", "user.email", "t@t")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	src := &prSource{}
	r := New(Options{
		Store:   st,
		Source:  func(string) factory.Source { return src },
		Workdir: func(it factory.Item) (string, error) { return w.For(it, checkout) },
		Push:    GitPush(ExecGit{}),
	})
	return publishRig{st: st, r: r, src: src, bare: bare, dir: dir, id: saved.ID}
}

func (g publishRig) settle(t *testing.T) factory.Item {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); g.r.Running(g.id); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the publish never finished")
		}
	}
	it, err := g.st.Get(g.id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// TestPublishCommitsPushesAndOpensThePullRequestWithTheTrailer is the
// approval carried out: one commit holding the job's work, its message the
// title and the trailer; the branch on the remote; the pull request opened
// from that branch with the proof and the trailer as its last line; the job
// shipped with its pr.
func TestPublishCommitsPushesAndOpensThePullRequestWithTheTrailer(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Total double-counts", Proof: []factory.Claim{{Text: "go test passes", OK: true}}})
	if err := g.r.Publish(g.id, testTrailer); err != nil {
		t.Fatal(err)
	}
	it := g.settle(t)
	if it.State != factory.StateShipped || it.PullRequest == nil || it.PullRequest.Number != 7 || it.PullRequest.URL != "https://github.com/acme/api/pull/7" {
		t.Fatalf("published job = %s %+v", it.State, it.PullRequest)
	}
	branch := "factory/12-total-double-counts"
	if msg := mustGit(t, g.bare, "log", "-1", "--format=%B", branch); msg != "Total double-counts\n\n"+testTrailer {
		t.Fatalf("the remote's commit says %q", msg)
	}
	if status := mustGit(t, g.dir, "status", "--porcelain"); status != "" {
		t.Fatalf("work left uncommitted: %q", status)
	}
	if len(g.src.got) != 1 {
		t.Fatalf("writes = %+v", g.src.got)
	}
	a := g.src.got[0]
	if a.Verb != "pr" || a.Branch != branch || a.Draft {
		t.Fatalf("action = %+v", a)
	}
	for _, want := range []string{"Closes #12.", "Proof · 1 of 1 claims shown", "✓ go test passes"} {
		if !strings.Contains(a.Body, want) {
			t.Errorf("body lacks %q: %q", want, a.Body)
		}
	}
	if !strings.HasSuffix(a.Body, "\n"+testTrailer) || strings.Count(a.Body, "Factory-Approval:") != 1 {
		t.Fatalf("body = %q", a.Body)
	}
}

// TestPublishWithoutAnApprovalPublishesNothing: no trailer, or one that is
// not a recorded approval's, is refused before anything is touched.
func TestPublishWithoutAnApprovalPublishesNothing(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Repo: "acme/api", Title: "Fix it"})
	for _, trailer := range []string{"", "Factory-Approval: evt_nothex00 @octocat", "Approved by the model"} {
		if err := g.r.Publish(g.id, trailer); !errors.Is(err, ErrNoApproval) {
			t.Fatalf("Publish(%q) = %v", trailer, err)
		}
	}
	it := g.settle(t)
	if it.State != factory.StateLanded || it.PullRequest != nil || len(g.src.got) != 0 {
		t.Fatalf("an unapproved publish moved the job: %s %+v %+v", it.State, it.PullRequest, g.src.got)
	}
	if out := mustGit(t, g.bare, "branch", "--list"); out != "" {
		t.Fatalf("an unapproved publish pushed: %q", out)
	}
}

// TestApprovingAPullRequestPostsAReviewComment: codeaf never pushes to the
// branch of a pull request it was asked to look at; the approval is a comment
// on it, with the trailer, and the job's pr is that pull request.
func TestApprovingAPullRequestPostsAReviewComment(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.Create(factory.Item{Num: 40, Kind: factory.KindPR, Repo: "acme/api", Title: "Speed up", URL: "https://github.com/acme/api/pull/40", State: factory.StateLanded, Stream: &factory.Stream{}})
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{}
	pushed := false
	r := New(Options{Store: st, Source: func(string) factory.Source { return src }, Push: func(string, string) error { pushed = true; return nil }})
	if err := r.Publish(saved.ID, testTrailer); err != nil {
		t.Fatal(err)
	}
	g := publishRig{st: st, r: r, id: saved.ID}
	it := g.settle(t)
	if it.State != factory.StateShipped || it.PullRequest == nil || it.PullRequest.Number != 40 || it.PullRequest.URL != saved.URL {
		t.Fatalf("job = %s %+v", it.State, it.PullRequest)
	}
	if pushed || len(src.got) != 1 || src.got[0].Verb != "comment" || !strings.HasSuffix(src.got[0].Body, "\n"+testTrailer) {
		t.Fatalf("pushed=%t writes=%+v", pushed, src.got)
	}
}

// TestAPublishThatFailsLandsTheJobAgain: a push with no remote opens nothing
// and the job is back at the proof gate with the reason on its log.
func TestAPublishThatFailsLandsTheJobAgain(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Repo: "acme/api", Title: "Fix it"})
	mustGit(t, g.dir, "remote", "remove", "origin")
	if err := g.r.Publish(g.id, testTrailer); err != nil {
		t.Fatal(err)
	}
	it := g.settle(t)
	if it.State != factory.StateLanded || it.PullRequest != nil || len(g.src.got) != 0 {
		t.Fatalf("failed publish: %s %+v %+v", it.State, it.PullRequest, g.src.got)
	}
	if last := it.Stream.Log[len(it.Stream.Log)-1]; last.Tone != "fail" || last.Text != "could not publish: #12 has no remote to push to" {
		t.Fatalf("last line = %+v", last)
	}
}

func TestTrailerHandle(t *testing.T) {
	if got := TrailerHandle(testTrailer); got != "octocat" {
		t.Fatalf("handle = %q", got)
	}
	if got := TrailerHandle("Factory-Approval: evt_x @octocat"); got != "" {
		t.Fatalf("handle of a malformed trailer = %q", got)
	}
}
