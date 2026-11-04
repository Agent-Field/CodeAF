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
	// The first run's budget leaves room for a model that wanders and misses the
	// cache a few times, so only the second run measures the cap.
	liveRaiseCap       = 0.05
	liveRaiseFiles     = 8
	liveRaiseSpendMost = 0.30
	// The whole check normally takes under 30 seconds. Its own limit sits below
	// a three-minute go test timeout, so a job that never moves is reported with
	// what it was waiting on instead of a goroutine dump.
	liveRaiseWall = 150 * time.Second
	// A person presses try again at most this many times before the check fails.
	liveRaiseTryAgain = 3
)

// liveCallTrace says, for one forwarded request, what the provider answered and
// whether the caller left early.
type liveCallTrace struct {
	http.ResponseWriter
	status, bytes    int
	id, finish, tail string
	sawCost          bool
}

func (w *liveCallTrace) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *liveCallTrace) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *liveCallTrace) Write(p []byte) (int, error) {
	w.bytes += len(p)
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(p)), "data:"))
	var m struct {
		ID      string `json:"id"`
		Choices []struct {
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(line), &m) == nil {
		if m.ID != "" {
			w.id = m.ID
		}
		for _, c := range m.Choices {
			if c.Finish != "" {
				w.finish = c.Finish
			}
		}
		if m.Usage != nil {
			w.sawCost = true
		}
	} else if w.status >= 400 {
		w.tail = line
	}
	return w.ResponseWriter.Write(p)
}

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
	// Every paid request's outcome, printed at the end whichever way the test
	// goes: a request without a cost is only explainable from these.
	var traced []string
	var uncosted []string
	// capNow is the job's budget as this test last set it. costedTotals is the
	// recorded total after each priced call; cutAtCap counts calls the budget
	// watcher cancelled before any reply, which have no cost to record.
	capNow := float64(liveRaiseCap)
	var costedTotals []float64
	cutAtCap := 0
	t.Cleanup(func() {
		for _, line := range traced {
			t.Log("call: " + line)
		}
		for _, id := range uncosted {
			t.Logf("billed for uncosted %s: %s", id, liveGenerationCost(id))
		}
	})
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
		var shape struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
			Stream   bool              `json:"stream"`
		}
		_ = json.Unmarshal(raw, &shape)
		begin := time.Since(started)
		trace := &liveCallTrace{ResponseWriter: w, status: http.StatusOK}
		inner.ServeHTTP(trace, req)
		left := context.Cause(req.Context())
		recorder.mu.Lock()
		delta := recorder.cost - spent
		traced = append(traced, fmt.Sprintf("n=%d begin=%s took=%s msgs=%d tools=%d stream=%t status=%d bytes=%d usage=%t finish=%q cost=%.8f total=%.8f left=%v id=%s tail=%.200q",
			recorder.calls, begin.Round(time.Millisecond), (time.Since(started)-begin).Round(time.Millisecond), len(shape.Messages), len(shape.Tools), shape.Stream,
			trace.status, trace.bytes, trace.sawCost, trace.finish, delta, recorder.cost, left, trace.id, trace.tail))
		switch {
		case recorder.cost > spent:
			costedTotals = append(costedTotals, recorder.cost)
		case left != nil && trace.bytes == 0 && spent >= capNow:
			// The call before this one crossed the cap after this one was admitted.
			// The running step's stop cancels it before the provider answers.
			cutAtCap++
		default:
			recorder.problems = append(recorder.problems, "request has no recorded cost")
			uncosted = append(uncosted, trace.id)
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
	// A model can end its replies without reporting until the step runs out of
	// attempts. The job then asks, as it should, and a person presses try again.
	// A press never hides a stuck job: each one needs the job to have asked, in
	// a later round, with a later paid call than the press before. A job asking
	// in the same round is left waiting, and fails on this check's limit.
	triedAgain, pressedRound, pressedCalls := 0, -1, 0
	// Wait for real saved work before taking a second action through the app.
	wait := func(label string, done func(api.Job) bool) api.Job {
		t.Helper()
		deadline := started.Add(liveRaiseWall)
		seen := ""
		for {
			recorder.mu.Lock()
			problems := append([]string(nil), recorder.problems...)
			recorder.mu.Unlock()
			if len(problems) > 0 {
				t.Fatalf("%s: recorder policy: %v", label, problems)
			}
			var j api.Job
			w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
			if now := liveJobShape(j); now != seen {
				seen = now
				t.Logf("job at %s (%s): %s", time.Since(started).Round(time.Millisecond), label, now)
			}
			if done(j) {
				return j
			}
			if j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsOutOfAttempts && len(j.Steps) > 0 && j.Steps[0].Round > pressedRound && triedAgain < liveRaiseTryAgain {
				recorder.mu.Lock()
				calls := recorder.calls
				recorder.mu.Unlock()
				if j.Steps[0].State != "waiting" || j.NeedsYou.Question == nil || j.NeedsYou.Question.Text == "" {
					t.Fatalf("%s: out of attempts without asking: %s", label, liveJobShape(j))
				}
				if triedAgain > 0 && calls <= pressedCalls {
					t.Fatalf("%s: try again made no paid call: %d then %d: %s", label, pressedCalls, calls, liveJobShape(j))
				}
				triedAgain++
				pressedRound, pressedCalls = j.Steps[0].Round, calls
				t.Logf("try again %d at round %d after %d calls: %s", triedAgain, pressedRound, calls, j.NeedsYou.Question.Text)
				act("factory.job_step_rerun", map[string]any{"step": "write"})
				continue
			}
			if time.Now().After(deadline) {
				saved, _ := floor.Get(id)
				t.Fatalf("%s: still waiting after %s: %s; saved state=%q question=%q log=%s", label, liveRaiseWall, seen, saved.State, saved.Question, liveJobLogTail(saved))
			}
			time.Sleep(250 * time.Millisecond)
		}
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
	recorder.mu.Lock()
	capNow = budget
	recorder.mu.Unlock()
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
	// The overshoot is the one priced call that crossed: no call was paid for
	// after the recorded total reached the budget.
	for i, total := range costedTotals {
		if total >= budget && i != len(costedTotals)-1 {
			t.Errorf("%d priced calls after the total reached the budget: totals=%v budget=%g", len(costedTotals)-1-i, costedTotals, budget)
			break
		}
	}
	recorder.mu.Unlock()
	raised := 5.0
	if raised <= parked.LifetimeCost {
		t.Fatal("raise must exceed the job total")
	}
	act("factory.job_set_budget", map[string]any{"budget": raised})
	final := wait("raise resumes the second run to approval", func(j api.Job) bool {
		return j.SpendingLimit == nil && j.State != api.StateRunning && len(j.Steps) > 0 && j.Steps[0].State == "done"
	})
	if triedAgain > 0 {
		recorder.mu.Lock()
		called := recorder.calls
		recorder.mu.Unlock()
		if called <= pressedCalls || final.Steps[0].Round <= pressedRound {
			t.Fatalf("the last try again made no progress: calls %d then %d, round %d then %d", pressedCalls, called, pressedRound, final.Steps[0].Round)
		}
	}
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
	calls, spend, cut := recorder.calls, recorder.cost, cutAtCap
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
		"cut_at_cap": cut, "tried_again": triedAgain,
	})
	if len(problems) > 0 || calls <= callsAtPark || requests[liveFactoryModel] != calls || len(requests) != 1 || len(replies) != 1 || replies[liveFactoryModel] == 0 {
		t.Fatalf("model/request policy: calls=%d requests=%v replies=%v problems=%v", calls, requests, replies, problems)
	}
	if spend >= liveRaiseSpendMost || ledgerUSD <= 0 || math.Abs(final.LifetimeCost-ledgerUSD) > 0.000001 || math.Abs(spend-ledgerUSD) > 0.000001 || ledgerCalls != calls-cut {
		t.Errorf("accounting: spend=%.8f lifetime=%.8f ledger=%.8f requests=%d cut=%d ledger_calls=%d", spend, final.LifetimeCost, ledgerUSD, calls, cut, ledgerCalls)
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

// liveJobShape is what a person's page knows about a job, in one line.
func liveJobShape(j api.Job) string {
	steps := make([]string, 0, len(j.Steps))
	for _, s := range j.Steps {
		steps = append(steps, s.Name+"="+s.State)
	}
	questions := make([]string, 0, len(j.Questions))
	for _, q := range j.Questions {
		questions = append(questions, q.Text)
	}
	return fmt.Sprintf("state=%s lifetime=%.8f cost=%.8f budget=%.8f limit=%t steps=%v questions=%q", j.State, j.LifetimeCost, j.Cost, j.Budget, j.SpendingLimit != nil, steps, questions)
}

func liveJobLogTail(it factory.Item) string {
	if it.Stream == nil {
		return "no run"
	}
	raw, _ := json.Marshal(it.Stream.Log)
	const most = 3000
	if len(raw) > most {
		raw = raw[len(raw)-most:]
	}
	return string(raw)
}

// liveGenerationCost asks the provider what it billed for a generation.
func liveGenerationCost(id string) string {
	if id == "" {
		return "no generation id was seen"
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodGet, config.DefaultBaseURL+"/generation?id="+url.QueryEscape(id), nil)
		if err != nil {
			return "bad request"
		}
		req.Header.Set("Authorization", "Bearer "+liveFactoryKey)
		response, err := (&http.Client{Transport: http.DefaultTransport, Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return "lookup failed"
		}
		var answer struct {
			Data struct {
				TotalCost float64 `json:"total_cost"`
				Finish    string  `json:"finish_reason"`
				Cancelled bool    `json:"cancelled"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&answer)
		response.Body.Close()
		if response.StatusCode == http.StatusOK && decodeErr == nil {
			return fmt.Sprintf("total_cost=%.8f finish=%q cancelled=%t", answer.Data.TotalCost, answer.Data.Finish, answer.Data.Cancelled)
		}
		if attempt >= 6 {
			return fmt.Sprintf("lookup status %d", response.StatusCode)
		}
		time.Sleep(2 * time.Second)
	}
}
