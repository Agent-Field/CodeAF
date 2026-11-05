//go:build e2e

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLiveBriefHeaderMatchesLedger(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}

	root := resolvedTempDir(t)
	appHome := filepath.Join(root, "state")
	userHome := filepath.Join(root, "home")
	callPath := filepath.Join(t.TempDir(), "logs", "calls.jsonl")
	t.Setenv(calllog.EnvVar, callPath)
	t.Setenv(calllog.BodiesEnvVar, "")
	t.Setenv("HOME", userHome)
	t.Setenv(home.EnvVar, appHome)
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyKnowledgeModel: liveFactoryModel, config.KeyMemoryEnabled: "off",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	// The key is kept only in the app credential store.
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_MODEL_POOL", "off")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)

	recorder := newLiveRecorder(t, config.DefaultBaseURL)
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
	w.backend.open = appOpenInProcess
	gh := &refusingGitHub{}
	local := &w.backend.local
	local.githubHTTP = gh.client()
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	// The conversation config the runner opens its stages from: the one-model
	// launch above, as a terminal's launch hands one to its worker.
	local.parent = func() session.Config { return launch.Config }
	// The factory identity is a fixture; no GitHub link is made.
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}

	var st appwire.FactoryLocalStatus
	// factory.local_start turns the runner on itself (ensureLocalRunner); a
	// job still runs only once a person presses Start.
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
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner, Cap: liveFactoryBudget, Model: liveFactoryModel,
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
				Ask: "Create the file greet/HELLO.txt (under the current directory) with the write tool, its content exactly `" + want + "` and a trailing newline. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: "test", Kind: factory.StageCheck, On: true, Ask: "grep -qx '" + want + "' greet/HELLO.txt"},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	stopJob := func() {
		var current api.Job
		w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &current)
		var stopped appwire.FactoryJobActed
		w.must("factory.job_stop", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": current.Version}}, &stopped)
	}

	// Before Start the job waits in Backlog without calling a model.
	var job api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	if job.State != api.StateBacklog {
		t.Fatalf("the job is %q before any start", job.State)
	}
	recorder.mu.Lock()
	early := recorder.calls
	recorder.mu.Unlock()
	if early != 0 {
		t.Fatalf("something ran before the explicit start: calls %d", early)
	}

	// Run and Start are the same two doors a person uses.
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
		recorder.mu.Lock()
		over := recorder.cost >= liveFactoryBudget
		recorder.mu.Unlock()
		if over {
			stopJob()
			cancel()
			t.Fatal("live run reached its spend cap; no further calls authorized")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if ctx.Err() != nil {
		stopJob()
	}
	if it.State != factory.StateLanded {
		t.Errorf("job state=%s question=%q failure=%+v", it.State, it.Question, it.Failure)
	}
	if gh.count() != 0 {
		t.Errorf("GitHub requests: %d", gh.count())
	}
	var brief appwire.FactoryJobBriefWritten
	w.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": id}, &brief)
	t.Logf("brief model=%s cost=%.8f kept=%v", brief.Model, brief.CostUSD, brief.Kept)
	if brief.Model != liveFactoryModel || brief.CostUSD <= 0 {
		t.Error("no priced brief write on the pinned model")
	}
	// Let the real app's final debounced brief enter the measured call log.
	t.Logf("allowing %s for the final automatic brief", factory.BriefDebounce+time.Second)
	time.Sleep(factory.BriefDebounce + time.Second)
	awaitLiveMoneyReceipts(t, callPath)
	var header api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &header)
	briefAssertTotals(t, callPath, header.Cost, recorder)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("models requested=%v answered=%v provider_total=%.8f problems=%v", recorder.requests, recorder.replies, recorder.cost, recorder.problems)
	if len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls || len(recorder.requests) != 1 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Error("live model policy failed")
	}
}

// Memory is disabled so every measured provider dollar must belong to the job.
func briefAssertTotals(t *testing.T, path string, header float64, recorder *liveRecorder) {
	t.Helper()
	if !session.FlushUsage() {
		t.Fatal("usage ledger did not flush")
	}
	ledger, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledgerUSD, memoryUSD float64
	for _, row := range ledger {
		if row.Model != "" && row.Model != liveFactoryModel {
			t.Errorf("ledger model=%s", row.Model)
		}
		ledgerUSD += row.USD
		if row.Role == "memory" {
			memoryUSD += row.USD
			t.Error("memory ledger row with memory off")
		}
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var providerUSD float64
	var briefCalls, memoryCalls int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var row calllog.Record
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Model != "" && row.Model != liveFactoryModel {
			t.Errorf("call log model=%s", row.Model)
		}
		if row.Phase == "start" {
			continue
		}
		if row.Cost == 0 && (row.Error != "" || row.Ended != "") {
			t.Errorf("provider total unresolved for interrupted call %s: late ledger receipts cannot be compared to terminal call-log cost", row.ID)
		}
		providerUSD += row.Cost
		if row.Tag == "memory" {
			memoryCalls++
			t.Error("memory call with memory off")
		}
		if row.Tag == "brief" {
			briefCalls++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	recordedUSD := recorder.cost
	recorder.mu.Unlock()
	t.Logf("TOTAL provider_USD=%.8f recorder_USD=%.8f ledger_USD=%.8f job_header_USD=%.8f memory_calls=%d memory_USD=%.8f brief_calls=%d", providerUSD, recordedUSD, ledgerUSD, header, memoryCalls, memoryUSD, briefCalls)
	if briefCalls == 0 {
		t.Error("no brief call recorded")
	}
	if providerUSD <= 0 || math.Abs(providerUSD-ledgerUSD) > 0.000001 || math.Abs(ledgerUSD-header) > 0.000001 || math.Abs(recordedUSD-providerUSD) > 0.000001 {
		t.Error("provider, ledger and job header differ or have no spend")
	}
	if ledgerUSD >= 1 || providerUSD >= 1 || header >= 1 {
		t.Error("live run reached $1")
	}
}
