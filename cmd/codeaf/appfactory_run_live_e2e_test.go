//go:build e2e

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LIVE LOCAL RUN. The app's own door, end to end, on one real model:
// factory.local_start serves this machine's factory, a job is in its Backlog,
// factory.local_run turns the engine's runner on, factory.job_start starts the
// job, and a real worker leases it, a real chat stage writes a file with the
// real `write` tool in a worktree, a check reads it, and the job stops at its
// approve gate. NO LAUNCH STUB: nothing here calls the seam's Launch directly.
//
// ONE MODEL, ALL ROLES. The runner's parent config is the one-model launch
// ([assertFactoryPin] walks every role it resolves), and every request goes
// through [liveRecorder], which refuses any request whose `model` is not
// [liveFactoryModel] or that carries a fallback list, and records the model
// each response names. Any other model, fallback or refusal fails the test.
//
// THE KEY IS THE APP'S: it is read once at start (as the product reads one),
// then lives only in the app's credential memory ([config.AppCredentials]),
// the process environment carrying none. It is never printed or written; the
// profile and app home are scanned for it afterwards.
//
// NO GITHUB: the factory's GitHub client is a transport that refuses every
// request and counts it, there is no token, and the approve gate is where this
// stops, so nothing is published.
//
// Opt-in (CODEAF_LIVE_FACTORY=1), a few cents.

// refusingGitHub counts every request and answers none.
type refusingGitHub struct {
	mu   sync.Mutex
	hits int
}

func (g *refusingGitHub) client() *http.Client {
	return &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		g.mu.Lock()
		g.hits++
		g.mu.Unlock()
		return nil, errors.New("the live local lane makes no GitHub request")
	})}
}

func (g *refusingGitHub) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hits
}

func TestLiveLocalRunReachesTheApproveGateOnOneModel(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	started := time.Now()

	root := resolvedTempDir(t)
	appHome := filepath.Join(root, "state")
	userHome := filepath.Join(root, "home")
	ambient := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv(home.EnvVar, appHome)
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("CODEAF_PROFILE_DIR", ambient)
	// NO KEY IN THE ENVIRONMENT: the only road to one is the app's memory.
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)

	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)

	g := newRunRig(t)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	assertFactoryPin(t, launch.Config, liveFactoryModel)

	stageSettle = 300 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	w := newFactoryWire(t, testFactoryHub(t))
	gh := &refusingGitHub{}
	local := &w.backend.local
	local.githubHTTP = gh.client()
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	// The conversation config the runner opens its stages from: the one-model
	// launch above, as a terminal's launch hands one to its worker.
	local.parent = func() session.Config { return launch.Config }
	// WHOSE JOBS THEY ARE comes from the factory's GitHub link; none is made.
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}

	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "live"}, &st)
	if st.Runner {
		t.Fatalf("start turned the runner on: %+v", st)
	}
	floor := local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}

	const want = "hello factory"
	id, err := floor.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner, Cap: liveFactoryBudget,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
				Ask: "Create the file HELLO.txt in the current directory with the write tool, its content exactly `" + want + "` and a trailing newline. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: "verify", Kind: factory.StageCheck, On: true, Ask: "grep -qx '" + want + "' HELLO.txt"},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// BEFORE ANY RUN: the job waits in Backlog, no runner, no model call.
	var job api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	if job.State != api.StateBacklog {
		t.Fatalf("the job is %q before any start", job.State)
	}
	if recorder.calls != 0 || local.runner {
		t.Fatalf("something ran before the explicit run: calls %d runner %v", recorder.calls, local.runner)
	}

	// THE EXPLICIT RUN, THEN THE EXPLICIT START: the two doors a person uses.
	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "live"}, &run)
	if !run.Runner {
		t.Fatalf("run: %+v", run)
	}
	var acted appwire.FactoryJobActed
	w.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)

	ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
	defer cancel()
	var it factory.Item
	for {
		it, _ = floor.Get(id)
		if it.State == factory.StateLanded || it.Failure != nil || it.Question != "" || it.State == factory.StateDismissed || ctx.Err() != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	var runners api.Runners
	w.must("factory.runners", map[string]any{"factory": st.Factory}, &runners)
	recorder.mu.Lock()
	summary := map[string]any{
		"requested_model":  liveFactoryModel,
		"request_models":   recorder.requests,
		"response_models":  recorder.replies,
		"calls":            recorder.calls,
		"http_statuses":    recorder.statuses,
		"cost_usd":         recorder.cost,
		"state":            string(it.State),
		"problems":         recorder.problems,
		"seconds":          int(time.Since(started).Seconds()),
		"one_model":        true,
		"door":             "factory.local_start, factory.local_run, factory.job_start; no seam Launch",
		"github_requests":  gh.count(),
		"registered_runs":  len(runners.Runners),
		"stopped_at_gate":  it.State == factory.StateLanded,
		"item_has_failure": it.Failure != nil,
	}
	if it.Stream != nil {
		summary["item_spent_usd"] = it.Stream.Spent
	}
	recorder.mu.Unlock()
	writeLiveFactoryLog(t, summary)

	if it.State != factory.StateLanded {
		t.Fatalf("item is %s after %s (failure %+v, question %q); summary %v", it.State, time.Since(started).Round(time.Second), it.Failure, it.Question, summary)
	}
	if len(it.Proof) == 0 {
		t.Fatalf("the landed item carries no proof: %+v", it.Proof)
	}
	for _, p := range it.Proof {
		if !p.OK {
			t.Errorf("a proof claim failed: %+v", p)
		}
	}
	if len(runners.Runners) == 0 {
		t.Error("no runner registered with the factory: the job did not run on the engine's own runner")
	}
	found, _ := filepath.Glob(filepath.Join(floor.Root(), "work", "*-"+strconv.Itoa(id), "HELLO.txt"))
	if len(found) != 1 {
		t.Fatalf("HELLO.txt in the item's worktree: found %v", found)
	}
	if got, err := os.ReadFile(found[0]); err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("HELLO.txt in the item's worktree = %q, %v", got, err)
	}
	if n := gh.count(); n != 0 {
		t.Fatalf("%d GitHub requests were attempted", n)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.calls == 0 {
		t.Fatal("no model call went through the recorder")
	}
	if len(recorder.problems) > 0 {
		t.Fatalf("model policy violations: %v", recorder.problems)
	}
	if recorder.requests[liveFactoryModel] != recorder.calls {
		t.Fatalf("requested models %v do not all equal %s", recorder.requests, liveFactoryModel)
	}
	if len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("recorded response models %v are not exactly {%s}", recorder.replies, liveFactoryModel)
	}
	if recorder.cost > liveFactoryBudget {
		t.Fatalf("cost $%.4f exceeds the $%.2f budget", recorder.cost, liveFactoryBudget)
	}
	readsSurviveAStop(t, local, id)
	// The key never reached a file: not the profile, not the app home.
	for _, dir := range []string{ambient, appHome, userHome} {
		for path, data := range snapshotTree(t, dir) {
			if strings.Contains(data, liveFactoryKey) {
				t.Fatalf("%s holds the key", path)
			}
		}
	}
}

// readsSurviveAStop is a second app that reaches the solo factory by its address
// and holds no credential for it, as after a restart. While the factory's
// service is stopped, queue and job answer unreachable, the word the list
// uses, never signed_out, which would make the desktop drop its kept rows.
// Served again on the same address, the rows come back.
func readsSurviveAStop(t *testing.T, local *appLocalFactory, id int) {
	t.Helper()
	other := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	other.must("factory.attach", map[string]any{"url": local.host.address}, &conn)
	at := map[string]any{"factory": conn.ID, "job": id}
	other.must("factory.job", at, nil)

	stopped := local.server
	if err := stopped.Close(); err != nil {
		t.Fatal(err)
	}
	other.mustFail("factory.queue", at, appwire.CodeUnreachable)
	other.mustFail("factory.job", at, appwire.CodeUnreachable)
	var list appwire.FactoryList
	other.must("factory.list", map[string]any{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryUnreachable {
		t.Fatalf("list while stopped: %+v", list.Factories)
	}

	listener, err := net.Listen("tcp", strings.TrimPrefix(local.host.address, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	again := &http.Server{Handler: stopped.Handler, BaseContext: stopped.BaseContext}
	go func() { _ = again.Serve(listener) }()
	t.Cleanup(func() { again.Close() })
	var q api.Queue
	other.must("factory.queue", at, &q)
	var job api.Job
	other.must("factory.job", at, &job)
	if job.ID != id {
		t.Fatalf("job after restart: %+v", job)
	}
	t.Logf("solo factory stopped: queue and job unreachable; served again: job %d reads", job.ID)
}
