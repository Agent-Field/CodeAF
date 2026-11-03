package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
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
	if a.Verb == "comment" {
		return factory.Receipt{URL: "https://github.com/acme/api/issues/12#issuecomment-7"}, nil
	}
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
	id := g.add(t, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, check("printf 'package ledger\\n\\nconst fixed = true\\n' > ledger.go"))
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

	if _, err := g.doors.SignOff(id, false); err == nil || !strings.Contains(err.Error(), "requires a recorded approval") {
		t.Fatalf("an approve with no recorded approval = %v", err)
	}
	if it, _ := g.st.Get(id); it.State != factory.StateLanded || len(src.writes()) != 0 {
		t.Fatalf("an unrecorded approve moved the job: %s %+v", it.State, src.writes())
	}

	event, err := g.record.Append(context.Background(), record.Event{Kind: record.Approved, Job: id, Handle: "octocat", Detail: "proof", Commit: landed.HeadCommit})
	if err != nil {
		t.Fatal(err)
	}
	trailer := "Factory-Approval: " + event.ID + " @octocat " + landed.HeadCommit
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
	if head := git(t, bare, "rev-parse", branch); head != landed.HeadCommit {
		t.Fatalf("pushed %q instead of the approved commit %q", head, landed.HeadCommit)
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
		if last.Kind == record.Posted && last.Handle == "me" && last.Detail == "pr · "+it.PullRequest.URL {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the record holds no post: %+v", events)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Approval through the HTTP door publishes a proof-only job as an issue
// comment. Its receipt survives the worker report, done and floor read.
func TestNoDiffHTTPApprovalPublishesCommentAndFinishes(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "api")
	git(t, "", "init", "-q", "-b", "main", checkout)
	git(t, checkout, "commit", "--allow-empty", "-q", "-m", "first")
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, "", "init", "-q", "--bare", bare)
	git(t, checkout, "remote", "add", "origin", bare)
	git(t, checkout, "push", "-q", "origin", "main")
	git(t, checkout, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	workdirs := run.NewWorkdirs(filepath.Join(t.TempDir(), "work"), run.ExecGit{})
	src := &pullSource{}
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Source = func(string) factory.Source { return src }
		o.Workdir = func(it factory.Item) (string, error) { return workdirs.For(it, checkout) }
		o.Push = func(string, string, ...func() error) error { t.Error("no-diff approval pushed"); return nil }
	})
	id := g.add(t, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, check("true"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Kind, it.Num, it.Repo = factory.KindIssue, 12, "acme/api"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	landed := g.waitFor(t, id, factory.StateLanded)
	if landed.Publish != "comment" {
		t.Fatalf("proof gate publish = %q", landed.Publish)
	}
	seam := g.doors
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
	if err := g.st.Update(id, func(it *factory.Item) error { it.Assignee = "me"; return nil }); err != nil {
		t.Fatal(err)
	}
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil }
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	cfg := serve.Config{Store: g.st, Record: g.record, Now: func() time.Time { return time.Unix(0, clock.Load()) }}
	handler := serve.New(seam, cfg)
	defer handler.Close()
	request := httptest.NewRequest("POST", "/v1/jobs/"+strconv.Itoa(id)+"/approve", strings.NewReader(`{"commit":"`+landed.HeadCommit+`"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var receipt struct{ Event string }
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || response.Code != 200 {
		t.Fatalf("approve = %d %s, error %v", response.Code, response.Body, err)
	}
	clock.Add(int64(factory.ApprovalUndo))
	handler.Close()
	handler = serve.New(seam, cfg)
	defer handler.Close()
	settle := httptest.NewRequest("GET", "/v1/queue", nil)
	settle.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), settle)
	approvals, _ := g.record.Since(context.Background(), time.Time{})
	for _, event := range approvals {
		if event.Kind == record.Approved {
			receipt.Event = event.ID
		}
	}
	if receipt.Event == "" {
		t.Fatal("no approval after Undo window")
	}
	it := g.waitFor(t, id, factory.StateShipped)
	url := "https://github.com/acme/api/issues/12#issuecomment-7"
	if it.PullRequest != nil || it.CommentURL != url || it.Publish != "comment" {
		t.Fatalf("done receipt = %+v", it)
	}
	writes := src.writes()
	trailer := "Factory-Approval: " + receipt.Event + " @me " + landed.HeadCommit
	if len(writes) != 1 || writes[0].Verb != "comment" || !strings.HasSuffix(writes[0].Body, "\n"+trailer) || !strings.Contains(writes[0].Body, "Proof ·") {
		t.Fatalf("writes = %+v", writes)
	}
	if branches := git(t, bare, "branch", "--list", "factory/*"); branches != "" {
		t.Fatalf("no-diff approval pushed %q", branches)
	}
	snap, err := g.floor.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range snap.Items {
		if x.ID == id && (x.State != factory.StateShipped || x.PullRequest != nil || x.CommentURL != url || x.Publish != "comment") {
			t.Fatalf("floor receipt = %+v", x)
		}
	}
	events, err := g.record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == record.Posted && event.Handle == "me" && event.Detail == "comment · "+url {
			return
		}
	}
	t.Fatalf("no comment in the record: %+v", events)
}

// capParked is a job held by the rig's worker whose runner parked it at its
// budget: the first step spends first, the second spends five more. autonomy
// is the job's mode; the rig's own server is the worker-backed desk.
func capParked(t *testing.T, autonomy string, first, budget float64) (rig, int) {
	t.Helper()
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(_ context.Context, j run.Job) (factory.StageResult, error) {
			cost := first
			if j.Stage.Name == "after" {
				cost = 5
			}
			return factory.StageResult{Done: true, Spent: cost}, nil
		})}
	}, "me")
	id := g.add(t, factory.Stage{Name: "charge", Kind: factory.StageCheck, On: true}, factory.Stage{Name: "after", Kind: factory.StageCheck, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error { it.Cap = budget; it.Assignee = "me"; it.Autonomy = autonomy; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.doors.Launch(id); err != nil {
		t.Fatal(err)
	}
	if it := g.waitFor(t, id, factory.StateNeedsYou); it.QKind != "cap" {
		t.Fatalf("job parked on %q (%q), not its budget", it.QKind, it.Question)
	}
	return g, id
}

// raise posts a budget raise to the rig's worker-backed desk.
func (g rig) raise(t *testing.T, id int, budget float64) {
	t.Helper()
	resp, err := http.Post(fmt.Sprintf("%s/v1/jobs/%d/raise_budget", g.base, id), "application/json", strings.NewReader(fmt.Sprintf(`{"budget":%g}`, budget)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("raise %d %s", resp.StatusCode, body)
	}
}

// staysParked fails when the job leaves its cap question within a while.
func (g rig) staysParked(t *testing.T, id int, cap float64) factory.Item {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		it, err := g.st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if it.State != factory.StateNeedsYou || it.QKind != "cap" || it.Cap != cap {
			t.Fatalf("parked job moved: state=%s qkind=%q cap=%v", it.State, it.QKind, it.Cap)
		}
		if time.Now().After(deadline) {
			return it
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// THE DESK'S COPY OF A HELD JOB THAT ITS RUNNER PARKED AT THE BUDGET CARRIES
// THE ASK: the worker's report names the question, and the desk keeps it as
// Asking. A raise must therefore see the cap ask through Asking.
func TestCapParkedHeldJobDeskCopyCarriesTheAsk(t *testing.T) {
	g, id := capParked(t, factory.AutonomyAuto, 2, 2)
	it, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Asking == nil || it.Asking.Question != it.Question {
		t.Fatalf("desk copy asking=%+v question=%q", it.Asking, it.Question)
	}
}

// RAISING THE BUDGET OF AN AUTO JOB ITS RUNNER PARKED AT THE CAP RESUMES IT:
// the raise answers the cap question "continue" through the person's answer
// door, so the ask is taken down, the ledger says who raised it to what and
// that the job carried on, and the worker's runner finishes the job.
func TestRaisedJobBudgetReachesTheAssigneesRunner(t *testing.T) {
	g, id := capParked(t, factory.AutonomyAuto, 2, 2)
	g.raise(t, id, 10)
	it := g.waitFor(t, id, factory.StateLanded)
	if it.Cap != 10 || it.Stream.Spent != 7 || it.Question != "" || it.QKind != "" || it.Asking != nil {
		t.Fatalf("runner budget=%v spent=%v question=%q qkind=%q asking=%+v", it.Cap, it.Stream.Spent, it.Question, it.QKind, it.Asking)
	}
	if logHas(it, "budget raised to $10 · carrying on") == 0 {
		t.Fatalf("timeline does not say the budget was raised: %+v", it.Stream.Log)
	}
	events, err := g.record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		if e.Job == id && (e.Kind == record.BudgetChanged || e.Kind == record.Answered) {
			kinds = append(kinds, e.Kind+":"+e.Detail)
		}
	}
	if want := []string{record.BudgetChanged + ":$10.00", record.Answered + ":" + factory.ChoiceContinue}; strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("ledger %v, want %v", kinds, want)
	}
}

// A raise above spend answers Manual's cap question. The next step still
// needs the person's approval before it runs.
func TestRaisedBudgetResumesManualCapAskThenAsksBeforeNextStep(t *testing.T) {
	g, id := capParked(t, factory.AutonomyManual, 2, 2)
	g.raise(t, id, 10)
	deadline := time.Now().Add(5 * time.Second)
	for {
		it, err := g.st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if it.State == factory.StateNeedsYou && it.QKind == factory.QKindApprove {
			if it.Question != "after is next · continue, or send back the step before?" || it.Cap != 10 || it.Stream.Spent != 2 || it.Stream.Phases[1].Round != 0 {
				t.Fatalf("Manual did not keep its next-step decision: %+v", it)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("raise did not pass the Manual cap ask: state=%s qkind=%q question=%q", it.State, it.QKind, it.Question)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := g.floor.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if it.Cap != 10 || it.Stream.Spent != 7 || it.Question != "" || it.Asking != nil || it.Stream.Phases[1].Round != 1 {
		t.Fatalf("approved Manual step did not finish once: %+v", it)
	}
}

// A cap increase that leaves no room for more spend keeps Manual parked.
func TestRaisedBudgetAtOrBelowSpendLeavesManualJobParked(t *testing.T) {
	for _, spent := range []float64{5, 7} {
		t.Run(fmt.Sprintf("spent_%g", spent), func(t *testing.T) {
			g, id := capParked(t, factory.AutonomyManual, spent, 2)
			g.raise(t, id, 5)
			if it := g.staysParked(t, id, 5); it.Asking == nil || it.Asking.Question != it.Question || it.Stream.Spent != spent {
				t.Fatalf("raise changed the Manual cap ask: %+v", it)
			}
		})
	}
}

// A RAISE THAT DOES NOT CLEAR THE SPEND IS NO ROOM TO CARRY ON.
func TestRaisedBudgetAtTheSpendLeavesAnAutoJobParked(t *testing.T) {
	g, id := capParked(t, factory.AutonomyAuto, 5, 2)
	g.raise(t, id, 5)
	if it := g.staysParked(t, id, 5); it.Asking == nil {
		t.Fatal("a raise to the spend took down the cap question")
	}
}
