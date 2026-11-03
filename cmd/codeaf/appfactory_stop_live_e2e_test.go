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
 "sync/atomic"
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
	"github.com/Agent-Field/codeaf/internal/teams"
)

// Adapted from appfactory_budget_resume_live_e2e_test.go in c52's budget lane.
// Run only this test, with an existing environment credential (never log it):
//
//	CODEAF_LIVE_FACTORY=1 GOMAXPROCS=2 go test -tags=e2e ./cmd/codeaf \
//	  -run '^TestLiveQuestionPendingStopIsStop$' -count=1 -parallel=1 -p=1 -timeout=12m -v
//
// All roles and auxiliary requests pass the same model/spend recorder. Serial
// requests reserve $0.02 before forwarding; the $0.10 cap is stricter than the
// separate $0.30 emergency ceiling. GitHub is refused and counted.
const liveStopSpendMost = 0.10

func TestLiveQuestionPendingStopIsStop(t *testing.T) {
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
	t.Setenv("CODEAF_MODEL_POOL", "off")
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
	recorder.budget = liveStopSpendMost
 var stopAt atomic.Int64
 var cancelledRequests int
 var highestCost float64
	t.Cleanup(func() {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		t.Logf("live attempt: requests=%d request_models=%v response_models=%v spend=%.8f cancelled_requests=%d conservative_spend=%.8f problems=%v", recorder.calls, recorder.requests, recorder.replies, recorder.cost, cancelledRequests, recorder.cost+float64(cancelledRequests)*highestCost, recorder.problems)
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
 reserved := spent + float64(cancelledRequests)*highestCost
		recorder.mu.Unlock()
		// Reserve each unpriced cancellation at the highest observed call cost.
		if bad || reserved+max(0.02, highestCost) >= liveStopSpendMost {
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
        var cancelledAt atomic.Int64
        cancelled := make(chan struct{})
        stopWatching := context.AfterFunc(req.Context(), func() {
            cancelledAt.Store(time.Now().UnixNano())
            close(cancelled)
        })
        defer stopWatching()
        inner.ServeHTTP(w, req)
        if req.Context().Err() != nil { <-cancelled }
        recorder.mu.Lock()
		if recorder.cost <= spent {
			at := stopAt.Load()
            if at != 0 && cancelledAt.Load() >= at {
                cancelledRequests++
            } else {
                recorder.problems = append(recorder.problems, "request has no recorded cost")
            }
		} else {
 highestCost = max(highestCost, recorder.cost-spent)
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
	// Brief generation is outside this Stop lane; manager conversations stay real.
	w.backend.briefs = nil
	freshEngineProcess(t)
	freshEngineFactoryStore(t)
	w.backend.open = appOpenInProcess
	w.backend.emit = w.server
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

	// The Test step buys a real ask before Stop. The fixture marker is outside
	// its checkout and is exposed only after the person's explicit rerun.
	marker := filepath.Join(root, "person-rerun")
	id, err := floor.Add(context.Background(), factory.Item{
		Title: "Stop a question-pending Test", Repo: "acme/api", Tier: factory.TierOwner,
		Cap: liveStopSpendMost, Autonomy: factory.AutonomyAuto, Model: liveFactoryModel,
		Stages: []factory.Stage{
			{Name: "test", Kind: factory.StageChat, On: true, Until: factory.UntilDone,
				Ask: fmt.Sprintf("First use read to check %s. If it exists, call stage_result with done true, findings 0, output 'person rerun completed', and do nothing else. If it does not exist, call ask with head 'Which private test phrase does the person authorize?', reason 'Only the person knows their private phrase; no file or recipe contains it. The manager must ask_person, never invent an answer.', two options 'Provide phrase' and 'Keep waiting', and blocking turn true. Wait for the answer. Do not guess, investigate or change files.", marker)},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true, Fixed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func() api.Job {
		t.Helper()
		var j api.Job
		w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
		return j
	}
	act := func(method string, extra map[string]any) {
		t.Helper()
		j := read()
		params := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": j.Version}}
		for k, v := range extra {
			params[k] = v
		}
		w.must(method, params, &appwire.FactoryJobActed{})
	}
	policy := func() {
		t.Helper()
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		if len(recorder.problems) != 0 || recorder.cost+float64(cancelledRequests)*highestCost >= liveStopSpendMost || recorder.cost >= 0.30 {
			t.Fatalf("recorder policy: spend=%.8f problems=%v", recorder.cost, recorder.problems)
		}
	}
	wait := func(label string, done func(api.Job) bool) api.Job {
		t.Helper()
		deadline := time.Now().Add(liveFactoryWall)
		for {
			policy()
			j := read()
			if done(j) {
				return j
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: state=%s questions=%+v failure=%+v", label, j.State, j.Questions, j.Failure)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	ledger := func() (float64, int) {
		t.Helper()
		if !session.FlushUsage() {
			t.Fatal("usage ledger did not flush")
		}
		rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var cost float64
		var calls int
		for _, row := range rows {
			if row.Model != liveFactoryModel {
				t.Fatalf("ledger model=%q", row.Model)
			}
			cost += row.USD
			calls += row.Calls
		}
		return cost, calls
	}
	var running appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "live"}, &running)
	act("factory.job_start", nil)
	pending := wait("real Test ask reaches person", func(j api.Job) bool { return j.State == api.StateNeedsYou && len(j.Questions) > 0 })
	saved, err := floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Asking == nil || saved.Asking.Stage != "test" {
		t.Fatalf("Test did not ask: %+v", saved.Asking)
	}
	question := saved.Asking.Question
	profile := w.backend.localProfile()
	room, err := factoryItemTeam(profile, saved)
	if err != nil {
		t.Fatal(err)
	}
	member := filepath.Join(root, "delayed-test.jsonl")
	if err := os.WriteFile(member, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(profile, func(f *teams.File) error {
		return f.AddMember(room, teams.Member{Key: member, File: member, Handle: "test", Word: "test"})
	}); err != nil {
		t.Fatal(err)
	}
	// Finish accounting before Stop so this run needs no in-flight exception.
	time.Sleep(2 * time.Second)
	beforeUSD, beforeCalls := ledger()
	recorder.mu.Lock()
	if math.Abs(beforeUSD-recorder.cost) > 0.000001 || beforeCalls != recorder.calls {
		t.Errorf("pre-Stop ledger mismatch: ledger=%.8f/%d proxy=%.8f/%d", beforeUSD, beforeCalls, recorder.cost, recorder.calls)
	}
	recorder.mu.Unlock()
	if t.Failed() {
		t.FailNow()
	}
	stopAt.Store(time.Now().UnixNano())
	act("factory.job_stop", nil)
	stoppedAt := time.Now()
	baseline := read()
	history, err := record.Open(floor, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertStopped := func(label string) api.Job {
		t.Helper()
		policy()
		j := read()
		cur, err := w.backend.local.host.floor.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Failure == nil || j.Failure.Code != factory.CodeStopped || !factory.StoppedByPerson(cur) || j.State == api.StateQueued || j.State == api.StateRunning || cur.PullRequest != nil || gh.count() != 0 {
			t.Fatalf("%s: Stop fault: state=%s failure=%+v PR=%+v github=%d", label, j.State, j.Failure, cur.PullRequest, gh.count())
		}
		if j.Desk != nil && j.Desk.Run != nil && baseline.Desk != nil && baseline.Desk.Run != nil && !j.Desk.Run.Started.Equal(baseline.Desk.Run.Started) {
			t.Fatalf("%s: a new run started", label)
		}
		events, err := history.Since(context.Background(), stoppedAt)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Job == id && (e.Kind == record.Started || e.Kind == record.AutoStarted || e.Kind == record.PRReady || e.Kind == record.Posted || e.Kind == record.StepRerun) {
				t.Fatalf("%s: post-Stop event %+v", label, e)
			}
		}
		return j
	}
	assertStopped("immediately")
	var chats appwire.FactoryJobChats
	w.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": id}, &chats)
	for _, e := range []teams.Entry{
		{Kind: teams.KindQuestion, From: "test", To: teams.ToManager, Text: question},
		{Kind: teams.KindEvent, From: "test", To: teams.ToManager, State: teams.StateAsking, Text: "asks: " + question},
	} {
		if err := teams.AppendTraffic(profile, room, e); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(95 * time.Second)
	for time.Now().Before(deadline) {
		assertStopped("queued question wait")
		usd, calls := ledger()
		if usd != beforeUSD || calls != beforeCalls {
			t.Fatalf("automatic spend after Stop: before=%.8f/%d after=%.8f/%d", beforeUSD, beforeCalls, usd, calls)
		}
		time.Sleep(time.Second)
	}
	traffic, err := teams.ReadTraffic(profile, room, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	ack := false
	for _, e := range traffic {
		if strings.Contains(e.Text, "could not wake") && strings.Contains(e.Text, "stopped") {
			ack = true
		}
	}
	if !ack {
 for _, e := range traffic { t.Logf("queued traffic: kind=%s from=%s to=%s state=%s text=%q", e.Kind, e.From, e.To, e.State, e.Text) }
		t.Fatal("queued question had no stopped-manager acknowledgement")
	}
	afterWait := assertStopped("after 95 seconds")
	afterUSD, _ := ledger()
	t.Logf("STOP_WAIT: before=%.8f after=%.8f state=%s no_new_run=true PR=nil github=%d in_flight=0 question=%q", beforeUSD, afterUSD, afterWait.State, gh.count(), pending.Questions[0].Text)

	var turn appwire.TurnResult
	w.must("agents.send", map[string]any{"id": chats.Agent, "text": "Why is this job waiting? Explain what I must do next; do not start any work."}, &turn)
	wait("person manager chat answers", func(api.Job) bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, line := range w.lines {
			var ev struct {
				Event string           `json:"event"`
				Data  appwire.TurnDone `json:"data"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == appwire.EventTurnDone && ev.Data.Agent == chats.Agent && ev.Data.Turn == turn.Turn {
				if !ev.Data.OK {
					t.Fatalf("manager chat failed: %+v", ev.Data)
				}
				return true
			}
		}
		return false
	})
	var page appwire.EntriesResult
	w.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
	var words strings.Builder
	for _, e := range page.Entries {
		if e.Kind == "answer" && e.Turn == turn.Turn {
			words.WriteString(e.Text)
			words.WriteByte('\n')
		}
	}
	managerWords := words.String()
	if !strings.Contains(managerWords, "this job is stopped") || !strings.Contains(managerWords, "a person presses Start") {
		t.Fatalf("manager words=%q", managerWords)
	}
	assertStopped("person chat")
	chatUSD, _ := ledger()
	t.Logf("MANAGER: words=%q ledger=%.8f", managerWords, chatUSD)

	// Close the app backend and engine, then reopen only persisted profile,
	// registry, factory store and transcript data in a new engine lifecycle.
	hub := w.backend.factoryHub()
	dir := hub.dir()
	w.backend.Close()
	freshEngineProcess(t)
	freshEngineFactoryStore(t)
	again := newAppFactoryHub()
	again.dir = func() string { return dir }
	w = newFactoryWire(t, again)
	w.backend.open = appOpenInProcess
	w.backend.emit = w.server
	w.backend.briefs = nil
	w.backend.local.githubHTTP = gh.client()
	w.backend.local.rules = local.rules
	w.backend.local.parent = func() session.Config { return launch.Config }
	w.backend.local.seamHook = local.seamHook
	assertStopped("engine restart")
	w.must("factory.local_run", map[string]any{"caller": "live"}, &running)
	w.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": id}, &chats)
	for i := 0; i < 5; i++ {
		assertStopped("restart watch")
		time.Sleep(time.Second)
	}
	restarted := assertStopped("after restart")
	restartUSD, restartCalls := ledger()
	chatCalls := 0
	recorder.mu.Lock()
	chatCalls = recorder.calls
	recorder.mu.Unlock()
	if restartUSD != chatUSD || restartCalls != chatCalls {
		t.Fatalf("restart spend: before=%.8f after=%.8f ledger_calls=%d proxy_calls=%d", chatUSD, restartUSD, restartCalls, chatCalls)
	}
	t.Logf("RESTART: state=%s no_new_run=true PR=nil github=%d ledger=%.8f", restarted.State, gh.count(), restartUSD)
	if err := os.WriteFile(marker, []byte("person authorized rerun\n"), 0600); err != nil {
		t.Fatal(err)
	}
	act("factory.job_step_rerun", map[string]any{"step": "test"})
	final := wait("person rerun executes paid Test", func(j api.Job) bool {
		return j.State != api.StateRunning && len(j.Steps) > 0 && j.Steps[0].State == "done"
	})
	finalUSD, finalCalls := ledger()
	recorder.mu.Lock()
	calls, spend := recorder.calls, recorder.cost
	requests, replies := recorder.requests, recorder.replies
	recorder.mu.Unlock()
	if finalUSD <= restartUSD || finalCalls <= restartCalls || math.Abs(finalUSD-spend) > 0.000001 || finalCalls != calls {
		t.Fatalf("rerun accounting: ledger=%.8f/%d proxy=%.8f/%d before=%.8f/%d", finalUSD, finalCalls, spend, calls, restartUSD, restartCalls)
	}
	if len(requests) != 1 || requests[liveFactoryModel] != calls || len(replies) != 1 || replies[liveFactoryModel] == 0 {
		t.Fatalf("model policy requests=%v replies=%v", requests, replies)
	}
	policy()
	cur, err := w.backend.local.host.floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if factory.StoppedByPerson(cur) || cur.PullRequest != nil || gh.count() != 0 {
		t.Fatalf("person rerun: stopped=%v PR=%+v github=%d", factory.StoppedByPerson(cur), cur.PullRequest, gh.count())
	}
	t.Logf("RESULT: requests=%d models=%v response_models=%v before_stop=%.8f after_stop=%.8f after_person_chat=%.8f after_person_start=%.8f person_start_delta=%.8f final_state=%s seconds=%d", calls, requests, replies, beforeUSD, afterUSD, chatUSD, finalUSD, finalUSD-restartUSD, final.State, int(time.Since(started).Seconds()))
	for _, dir := range []string{ambient, appHome, userHome} {
		for path, data := range snapshotTree(t, dir) {
			if strings.Contains(data, liveFactoryKey) {
				t.Fatalf("credential persisted at %s", path)
			}
		}
	}
}
