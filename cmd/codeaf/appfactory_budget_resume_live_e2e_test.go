//go:build e2e

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LIVE RAISED BUDGET. The rig of [TestLiveLocalRunReachesTheApproveGateOnOneModel]
// on one real model, every role pinned to it: an Auto job whose one chat step
// costs more than its budget runs on this machine's runner until the runner
// parks it at its cap ("budget of ... reached"). The person then raises the budget
// to $5 through the factory's raise_budget route (factory.job_set_budget), and
// the job carries on with nobody answering the cap question: the question
// clears, the ledger reads budget_changed then answered (continue), the
// timeline says "budget raised to $5 · carrying on", the same chat writes the
// files the cap left unwritten on further model calls, and the job leaves
// needs-you for its next stop, a fixed approve gate that Auto never answers.
//
// NO GITHUB: the GitHub client refuses and counts every request; the fixed
// gate is where this stops, so nothing is published.
//
// Opt-in (CODEAF_LIVE_FACTORY=1), a few cents.

// liveRaiseCap is the job's first budget, and liveRaiseFiles the files its chat
// step writes one reply at a time. All 18 replies measured about $0.012, so the step
// passes the cap with files still to write and the raise has work to resume.
const (
	liveRaiseCap   = 0.005
	liveRaiseFiles = 18
)

// liveRaiseSpendMost is this lane's whole model spend; the recorder refuses
// any call past it.
const liveRaiseSpendMost = 0.05

func TestLiveRaisedBudgetResumesAnAutoJobParkedAtItsCap(t *testing.T) {
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
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)

	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	// THIS LANE'S OWN SPEND CEILING, in front of the recorder's.
	inner := recorder.server.Config.Handler
	recorder.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder.mu.Lock()
		spent := recorder.cost
		recorder.mu.Unlock()
		if spent >= liveRaiseSpendMost {
			recorder.note("the lane's spend ceiling is reached")
			http.Error(w, "the lane's spend ceiling is reached", http.StatusBadRequest)
			return
		}
		inner.ServeHTTP(w, req)
	})
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
	// This wire opens no conversations, and the brief's writer is not what this
	// lane measures: left on, its debounce opens the job's agent mid-run.
	w.backend.briefs = nil
	gh := &refusingGitHub{}
	local := &w.backend.local
	local.githubHTTP = gh.client()
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	local.parent = func() session.Config { return launch.Config }
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}

	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "live"}, &st)
	if !st.Runner {
		t.Fatalf("start left the runner off: %+v", st)
	}
	floor := local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}

	const want = "hello factory"
	id, err := floor.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner,
		Cap: liveRaiseCap, Autonomy: factory.AutonomyAuto,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
				Ask: fmt.Sprintf("Create %d files under the current directory with the write tool: greet/HELLO-01.txt, greet/HELLO-02.txt and so on, each with the content exactly `%s` and a trailing newline. Write exactly one file per reply, in order: never two tool calls in one reply. When every file exists, call stage_result with done true, findings 0 and a one-line output. Do nothing else.", liveRaiseFiles, want)},
			{Name: "verify", Kind: factory.StageCheck, On: true, Ask: fmt.Sprintf("test \"$(cat greet/HELLO-*.txt | grep -cx '%s')\" = %d", want, liveRaiseFiles)},
			// FIXED: Auto never answers it, so the job stops here unpublished.
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true, Fixed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if it, _ := floor.Get(id); !factory.Automatic(it) {
		t.Fatalf("the job is not in Auto: autonomy %q", it.Autonomy)
	}

	var job api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "live"}, &run)
	var acted appwire.FactoryJobActed
	w.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)

	// PARKED AT THE CAP by the runner.
	it := waitItem(t, floor.Get, id, liveFactoryWall, func(it factory.Item) bool {
		return it.QKind == "cap" || it.Failure != nil || it.State == factory.StateLanded || it.State == factory.StateDismissed || (it.Question != "" && it.QKind != "cap")
	})
	parkedQ, parkedState := it.Question, it.State
	var parked api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &parked)
	recorder.mu.Lock()
	callsAtPark, costAtPark := recorder.calls, recorder.cost
	recorder.mu.Unlock()
	path := "unheld"
	if parked.HeldBy != nil {
		path = "held"
	}
	if it.QKind != "cap" {
		summary := liveRaiseSummary(recorder, gh, started, it, parkedQ, path, nil, "")
		writeLiveFactoryLog(t, summary)
		t.Fatalf("the runner did not park the job at its cap: state %s, qkind %q, question %q, failure %+v; summary %v", it.State, it.QKind, it.Question, it.Failure, summary)
	}

	// THE RAISE, through the factory's raise_budget route.
	w.must("factory.job_set_budget", map[string]any{"factory": st.Factory, "job": id, "budget": 5, "expect": map[string]any{"version": parked.Version}}, &acted)

	// IT CARRIES ON: the cap question clears and the job reaches its next stop.
	it = waitItem(t, floor.Get, id, liveFactoryWall, func(it factory.Item) bool {
		return it.Failure != nil || it.State == factory.StateLanded || it.State == factory.StateDismissed || (it.Question != "" && it.QKind != "cap")
	})

	history, err := record.Open(floor, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := history.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledger []string
	for _, e := range events {
		if e.Job == id {
			k := e.Kind
			if e.Detail != "" {
				k += ":" + e.Detail
			}
			ledger = append(ledger, k)
		}
	}
	raised := ""
	if it.Stream != nil {
		for _, l := range it.Stream.Log {
			if strings.HasPrefix(l.Text, "budget raised to ") {
				raised = l.Text
			}
		}
	}
	summary := liveRaiseSummary(recorder, gh, started, it, parkedQ, path, ledger, raised)
	summary["parked_state"] = string(parkedState)
	summary["calls_at_park"] = callsAtPark
	summary["cost_at_park"] = costAtPark
	written, _ := filepath.Glob(filepath.Join(floor.Root(), "work", "*-"+strconv.Itoa(id), "greet", "HELLO-*.txt"))
	summary["files_written"] = len(written)
	writeLiveFactoryLog(t, summary)

	if it.Failure != nil || it.QKind == "cap" || it.State == factory.StateNeedsYou && it.QKind != factory.QKindApprove {
		t.Fatalf("after the raise the job is %s (qkind %q, question %q, failure %+v); summary %v", it.State, it.QKind, it.Question, it.Failure, summary)
	}
	if it.Cap != 5 {
		t.Errorf("cap after the raise = %v, want 5", it.Cap)
	}
	if raised != "budget raised to $5 · carrying on" {
		t.Errorf("timeline line = %q", raised)
	}
	if len(written) != liveRaiseFiles {
		t.Errorf("files in the job's worktree = %d, want %d", len(written), liveRaiseFiles)
	}
	joined := strings.Join(ledger, ",")
	if !strings.Contains(joined, record.BudgetChanged+":$5.00,"+record.Answered+":continue") {
		t.Errorf("ledger %v lacks budget_changed then answered:continue", ledger)
	}
	if n := gh.count(); n != 0 {
		t.Fatalf("%d GitHub requests were attempted", n)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.problems) > 0 {
		t.Fatalf("model policy violations: %v", recorder.problems)
	}
	if recorder.calls <= callsAtPark {
		t.Fatalf("no model call after the raise: %d at the cap, %d in all", callsAtPark, recorder.calls)
	}
	if recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("models: requests %v replies %v, want only %s", recorder.requests, recorder.replies, liveFactoryModel)
	}
	for _, dir := range []string{ambient, appHome, userHome} {
		for p, data := range snapshotTree(t, dir) {
			if strings.Contains(data, liveFactoryKey) {
				t.Fatalf("%s holds the key", p)
			}
		}
	}
}

// waitItem polls the job until done says so or the wall passes.
func waitItem(t *testing.T, get func(int) (factory.Item, error), id int, wall time.Duration, done func(factory.Item) bool) factory.Item {
	t.Helper()
	deadline := time.Now().Add(wall)
	for {
		it, err := get(id)
		if err == nil && (done(it) || time.Now().After(deadline)) {
			return it
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func liveRaiseSummary(recorder *liveRecorder, gh *refusingGitHub, started time.Time, it factory.Item, parked, path string, ledger []string, raised string) map[string]any {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	s := map[string]any{
		"requested_model": liveFactoryModel,
		"request_models":  recorder.requests,
		"response_models": recorder.replies,
		"calls":           recorder.calls,
		"http_statuses":   recorder.statuses,
		"cost_usd":        recorder.cost,
		"problems":        recorder.problems,
		"seconds":         int(time.Since(started).Seconds()),
		"github_requests": gh.count(),
		"parked_at_cap":   parked,
		"path":            path,
		"state":           string(it.State),
		"qkind":           it.QKind,
		"question":        it.Question,
		"cap":             it.Cap,
		"ledger":          ledger,
		"timeline":        raised,
	}
	if it.Stream != nil {
		s["item_spent_usd"] = it.Stream.Spent
	}
	return s
}
