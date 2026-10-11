package worker_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// pullSource is the forge, faked: it keeps what was written and answers a
// pull request as #7.
type pullSource struct {
	mu  sync.Mutex
	got []factory.Action
}

func (s *pullSource) Name() string { return "fake" }
func (s *pullSource) Read(context.Context, string) ([]factory.Item, string, error) {
	return nil, "", nil
}
func (s *pullSource) Write(_ context.Context, a factory.Action) (factory.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, a)
	return factory.Receipt{URL: "https://github.com/acme/api/pull/7", Number: 7}, nil
}

func (s *pullSource) writes() []factory.Action {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]factory.Action(nil), s.got...)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestApprovingTheProofPublishesThePullRequest is the whole road of an
// approval on loopback: the job lands at the proof gate on the worker; an
// approve with no recorded approval publishes nothing; with the record's
// event, the factory queues the job with a publish signal, the worker that
// pulls it commits the worktree with the trailer, pushes the branch to the
// remote and opens the pull request through the source with the trailer as
// the body's last line; the pr reaches the factory and its wire, the job is
// done, and the record says what was posted.
func TestApprovingTheProofPublishesThePullRequest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	checkout := filepath.Join(t.TempDir(), "api")
	git(t, "", "init", "-q", "-b", "main", checkout)
	if err := os.WriteFile(filepath.Join(checkout, "ledger.go"), []byte("package ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "ledger.go")
	git(t, checkout, "commit", "-q", "-m", "first")
	git(t, checkout, "config", "user.name", "t")
	git(t, checkout, "config", "user.email", "t@t")
	git(t, checkout, "config", "commit.gpgsign", "false")
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, "", "init", "-q", "--bare", bare)
	git(t, checkout, "remote", "add", "origin", bare)

	workdirs := run.NewWorkdirs(filepath.Join(t.TempDir(), "work"), run.ExecGit{})
	src := &pullSource{}
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Source = func(string) factory.Source { return src }
		o.Workdir = func(it factory.Item) (string, error) { return workdirs.For(it, checkout) }
		o.Push = run.GitPush(run.ExecGit{})
	})
	id := g.add(t, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	landed := g.waitFor(t, id, factory.StateLanded)
	if landed.WorktreeMachine != "laptop" {
		t.Fatalf("worktree machine was not reported to the floor: %q", landed.WorktreeMachine)
	}
	// A second online machine continuously pulls through approval. It has
	// no worktree and must receive no execution, even between worker slots.
	other := client.Open(g.base, "").Worker("")
	if _, err := other.Attach(context.Background(), api.WorkerAttach{Machine: "desktop", Name: "desktop (pid 123)", Handle: "me", TakesUnassigned: map[string]bool{"*": true}}); err != nil {
		t.Fatal(err)
	}
	pollCtx, stopPoll := context.WithCancel(context.Background())
	pollDone := make(chan error, 1)
	go func() {
		for pollCtx.Err() == nil {
			pulled, ok, err := other.Pull(pollCtx)
			if ok || (err != nil && pollCtx.Err() == nil) {
				pollDone <- fmt.Errorf("desktop pull = %+v, taken %v, error %v", pulled, ok, err)
				return
			}
		}
		pollDone <- nil
	}()
	t.Cleanup(func() {
		stopPoll()
		if err := <-pollDone; err != nil {
			t.Error(err)
		}
	})
	// The job's work, left in its worktree as a stage leaves it.
	if err := os.WriteFile(filepath.Join(workdirs.Dir(landed), "ledger.go"), []byte("package ledger\n\nconst fixed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := g.doors.SignOff(id, false); err == nil || !strings.Contains(err.Error(), "requires a recorded approval") {
		t.Fatalf("an approve with no recorded approval = %v", err)
	}
	if it, _ := g.st.Get(id); it.State != factory.StateLanded || len(src.writes()) != 0 {
		t.Fatalf("an unrecorded approve moved the job: %s %+v", it.State, src.writes())
	}

	event, err := g.record.Append(context.Background(), record.Event{Kind: record.Approved, Job: id, Handle: "octocat", Detail: "proof"})
	if err != nil {
		t.Fatal(err)
	}
	trailer := "Factory-Approval: " + event.ID + " @octocat"
	if _, err := g.doors.SignOff(id, false); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if it.PullRequest == nil || it.PullRequest.Number != 7 || it.PullRequest.URL != "https://github.com/acme/api/pull/7" {
		t.Fatalf("the factory's pr = %+v", it.PullRequest)
	}
	writes := src.writes()
	if len(writes) != 1 || writes[0].Verb != "pr" || !strings.HasSuffix(writes[0].Body, "\n"+trailer) {
		t.Fatalf("writes = %+v", writes)
	}
	branch := workdirs.Branch(landed)
	if writes[0].Branch != branch {
		t.Fatalf("the pr is from %q, not %q", writes[0].Branch, branch)
	}
	if msg := git(t, bare, "log", "-1", "--format=%B", branch); msg != "prove it builds\n\n"+trailer {
		t.Fatalf("the pushed commit says %q", msg)
	}

	// The pr on the wire, as any window reads the job.
	snap, err := g.floor.Load()
	if err != nil {
		t.Fatal(err)
	}
	var wired *factory.PullRequest
	for _, x := range snap.Items {
		if x.ID == id {
			wired = x.PullRequest
		}
	}
	if wired == nil || wired.Number != 7 || wired.URL != it.PullRequest.URL {
		t.Fatalf("the wire's pr = %+v", wired)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := g.record.Since(context.Background(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		last := events[len(events)-1]
		if last.Kind == record.Posted && last.Handle == "octocat" && last.Detail == "pr · "+it.PullRequest.URL {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the record holds no post: %+v", events)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
