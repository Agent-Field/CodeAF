//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// Adapted from c34/budget-live-test's raised-budget lane. Run only this test:
// CODEAF_LIVE_FACTORY=1 GOMAXPROCS=2 go test -tags=e2e ./cmd/codeaf
//
//	-run '^TestLiveJobTotalBudgetAcrossRerun$' -count=1 -parallel=1 -p=1 -timeout=12m -v
//
// The existing environment key is kept in credential memory, never in logs.
// First finish eight writes, then set a cap above that paid run and below two
// runs. Start over must stop on job total while its own cost stays below the cap.
// A raise resumes the same run; the job lifetime must equal its usage ledger.
const (
	liveRaiseCap       = 0.02
	liveRaiseFiles     = 8
	liveRaiseSpendMost = 0.30
)

func TestLiveJobTotalBudgetAcrossRerun(t *testing.T) {
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
	ambient := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyKnowledgeModel: liveFactoryModel, config.KeyMemoryEnabled: "off",
	})
	t.Setenv("HOME", userHome)
	t.Setenv(home.EnvVar, appHome)
	t.Setenv(config.ProfileDirEnv, ambient)
	t.Setenv("CODEAF_PROFILE_DIR", ambient)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)

	// Refuse changed prices before authorizing paid work. The request bounds
	// below reserve $0.02, including a conservative byte-per-token input bound.
	response, err := http.Get(config.DefaultBaseURL + "/models")
	if err != nil {
		t.Fatal("cannot verify model prices")
	}
	var catalog struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&catalog)
	response.Body.Close()
	if err != nil {
		t.Fatalf("cannot decode model prices: status=%d content_type=%q error=%v", response.StatusCode, response.Header.Get("Content-Type"), err)
	}
	priced := false
	for _, model := range catalog.Data {
		if model.ID != liveFactoryModel {
			continue
		}
		var input, output float64
		_, e1 := fmt.Sscan(model.Pricing.Prompt, &input)
		_, e2 := fmt.Sscan(model.Pricing.Completion, &output)
		priced = e1 == nil && e2 == nil && input > 0 && input <= 0.0000001 && output > 0 && output <= 0.000001
	}
	if !priced {
		t.Fatal("model price exceeds the hard spend reservation")
	}
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	recorder.budget = liveRaiseSpendMost
	t.Cleanup(func() {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		t.Logf("live attempt: requests=%d request_models=%v response_models=%v spend=%.8f problems=%v", recorder.calls, recorder.requests, recorder.replies, recorder.cost, recorder.problems)
	})
	inner := recorder.server.Config.Handler
	var paid sync.Mutex
	recorder.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			inner.ServeHTTP(w, req)
			return
		}
		paid.Lock()
		defer paid.Unlock()
		recorder.mu.Lock()
		spent, bad := recorder.cost, len(recorder.problems) > 0
		recorder.mu.Unlock()
		// Serial requests have at most $0.02 outstanding. Unknown costs stop work.
		if bad || spent+0.02 >= liveRaiseSpendMost {
			recorder.note("hard spend stop")
			http.Error(w, "hard spend stop", http.StatusBadRequest)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(req.Body, 100001))
		var body map[string]json.RawMessage
		if err != nil || len(raw) > 100000 || json.Unmarshal(raw, &body) != nil {
			recorder.note("request exceeds spend reservation")
			http.Error(w, "request exceeds spend reservation", http.StatusBadRequest)
			return
		}
		body["max_tokens"] = json.RawMessage("1024")
		delete(body, "max_completion_tokens")
		raw, _ = json.Marshal(body)
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		inner.ServeHTTP(w, req)
		recorder.mu.Lock()
		if recorder.cost <= spent {
			recorder.problems = append(recorder.problems, "request has no recorded cost")
		}
		recorder.mu.Unlock()
	})

	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		if r.Method == http.MethodGet && u.Host == recorder.server.Listener.Addr().String() && strings.HasSuffix(u.Path, "/generation") {
			target, err := url.Parse(config.DefaultBaseURL)
			if err != nil {
				return nil, err
			}
			target.Path = strings.TrimRight(target.Path, "/") + "/generation"
			target.RawQuery = u.RawQuery
			copy.URL = target
			copy.Host = target.Host
		}
		if r.Method == http.MethodPost && strings.Contains(u.Host, "openrouter.ai") {
			copy.URL.Scheme = "http"
			copy.URL.Host = recorder.server.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(copy)
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })

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
				Ask: fmt.Sprintf("Create %d files under the current directory with the write tool: greet/HELLO-01.txt, greet/HELLO-02.txt and so on, each with the content exactly `%s` and a trailing newline. Rewrite each file even if it already exists. Write exactly one file per reply, in order: never two tool calls in one reply. When every file exists, call stage_result with done true, findings 0 and a one-line output. Do nothing else.", liveRaiseFiles, want)},
			{Name: "verify", Kind: factory.StageCheck, On: true, Until: factory.UntilGreen, Max: 1, Ask: fmt.Sprintf("test -f %q && test \"$(cat greet/HELLO-*.txt | grep -cx '%s')\" = %d", filepath.Join(root, "allow-check"), want, liveRaiseFiles)},
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

	// Wait for real saved work before taking a second action through the app.
	wait := func(label string, done func(api.Job) bool) api.Job {
		t.Helper()
		deadline := time.Now().Add(liveFactoryWall)
		for {
			recorder.mu.Lock()
			problems := append([]string(nil), recorder.problems...)
			recorder.mu.Unlock()
			if len(problems) > 0 {
				t.Fatalf("%s: recorder policy: %v", label, problems)
			}
			var j api.Job
			w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
			if done(j) {
				return j
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: lifetime=%g cost=%g budget=%g questions=%+v", label, j.LifetimeCost, j.Cost, j.Budget, j.Questions)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	act := func(method string, extra map[string]any) {
		t.Helper()
		var j api.Job
		w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
		params := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": j.Version}}
		for k, v := range extra {
			params[k] = v
		}
		w.must(method, params, &acted)
	}
	first := wait("first run finishes paid work under budget", func(j api.Job) bool {
		return j.State != api.StateRunning && j.Cost > 0 && len(j.Steps) > 0 && j.Steps[0].State == "done"
	})
	if first.Cost >= first.Budget || first.SpendingLimit != nil {
		t.Fatalf("first run exceeded budget: %+v", first)
	}
	// Calibrate to actual provider billing so token/cache price variation cannot
	// turn this into a first-run cap test or a second run that never reaches it.
	budget := first.LifetimeCost * 1.05
	// The public budget picker offers whole-dollar choices. Pin this cents-only
	// fixture through its store, as the original live lane does at creation.
	if err := floor.Update(id, func(it *factory.Item) error { it.Cap = budget; return nil }); err != nil {
		t.Fatal(err)
	}
	// The first check deliberately waits for this fixture marker. Stop that
	// waiting check, then use Start over to create a distinct run. The next
	// check can pass after the paid stage resumes under the raised cap.
	if err := os.WriteFile(filepath.Join(root, "allow-check"), []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	act("factory.job_stop", nil)
	wait("first run is stopped before Start over", func(j api.Job) bool { return j.Failure != nil })
	act("factory.job_run_again", nil)
	parked := wait("second run reaches lifetime cap", func(j api.Job) bool { return j.SpendingLimit != nil })
	if parked.Cost <= 0 || parked.Cost >= budget || parked.LifetimeCost < budget || len(parked.Questions) == 0 {
		t.Fatalf("cap must be from job total, not run cost: %+v", parked)
	}
	question := parked.Questions[0].Text
	if !strings.Contains(question, "job total") {
		t.Fatalf("cap question = %q", question)
	}
	recorder.mu.Lock()
	callsAtPark, costAtPark := recorder.calls, recorder.cost
	recorder.mu.Unlock()
	raised := 5.0
	if raised <= parked.LifetimeCost {
		t.Fatal("raise must exceed the job total")
	}
	act("factory.job_set_budget", map[string]any{"budget": raised})
	final := wait("raise resumes the second run to approval", func(j api.Job) bool {
		return j.SpendingLimit == nil && j.State != api.StateRunning && len(j.Steps) > 0 && j.Steps[0].State == "done"
	})
	if final.LifetimeCost <= parked.LifetimeCost || final.Budget != raised {
		t.Fatalf("raise did not keep counting: %+v", final)
	}
	if !session.FlushUsage() {
		t.Fatal("usage ledger did not flush")
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledgerUSD float64
	var ledgerCalls int
	saved, err := floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	jobName := factory.LedgerName(saved)
	for _, row := range rows {
		if row.Model != liveFactoryModel {
			t.Errorf("ledger model = %q", row.Model)
		}
		if row.FactoryJob == jobName {
			ledgerUSD += row.USD
			ledgerCalls += row.Calls
		}
	}
	recorder.mu.Lock()
	calls, spend := recorder.calls, recorder.cost
	requests, replies := recorder.requests, recorder.replies
	problems := append([]string(nil), recorder.problems...)
	recorder.mu.Unlock()
	writeLiveFactoryLog(t, map[string]any{
		"requests": calls, "request_models": requests, "response_models": replies,
		"spend": spend, "lifetime_cost": final.LifetimeCost, "cost": final.Cost,
		"budget": final.Budget, "ledger_sum": ledgerUSD, "ledger_calls": ledgerCalls,
		"first_cost": first.Cost, "second_cost_at_cap": parked.Cost,
		"lifetime_at_cap": parked.LifetimeCost, "budget_at_cap": budget,
		"question": question, "calls_at_cap": callsAtPark, "spend_at_cap": costAtPark,
		"seconds": int(time.Since(started).Seconds()), "problems": problems,
	})
	if len(problems) > 0 || calls <= callsAtPark || requests[liveFactoryModel] != calls || len(requests) != 1 || len(replies) != 1 || replies[liveFactoryModel] == 0 {
		t.Fatalf("model/request policy: calls=%d requests=%v replies=%v problems=%v", calls, requests, replies, problems)
	}
	if spend >= liveRaiseSpendMost || ledgerUSD <= 0 || math.Abs(final.LifetimeCost-ledgerUSD) > 0.000001 || math.Abs(spend-ledgerUSD) > 0.000001 || ledgerCalls != calls {
		t.Errorf("accounting: spend=%.8f lifetime=%.8f ledger=%.8f requests=%d ledger_calls=%d", spend, final.LifetimeCost, ledgerUSD, calls, ledgerCalls)
	}
	if gh.count() != 0 {
		t.Errorf("GitHub requests = %d", gh.count())
	}
	for _, dir := range []string{ambient, appHome, userHome} {
		for path, data := range snapshotTree(t, dir) {
			if strings.Contains(data, liveFactoryKey) {
				t.Fatalf("credential persisted at %s", path)
			}
		}
	}
}
