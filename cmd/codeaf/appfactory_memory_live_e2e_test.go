//go:build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLiveMemoryStageMeasure(t *testing.T) {
	liveMemoryStageMeasure(t, false, true)
}

func TestLiveMemoryPauseMeasure(t *testing.T) {
	liveMemoryStageMeasure(t, true, true)
}

func TestLiveMemoryOffMakesNoCalls(t *testing.T) {
	liveMemoryStageMeasure(t, false, false)
}

func liveMemoryStageMeasure(t *testing.T, pause, memoryOn bool) {
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
	values := map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyKnowledgeModel: liveFactoryModel,
	}
	if memoryOn {
		values[config.KeyMemoryEnabled] = config.MemoryOn
	}
	profile := v3Profile(t, values)
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	// NO KEY IN THE ENVIRONMENT: the only road to one is the app's memory.
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
	recorder.budget, recorder.maxCalls = 1, 250
	memoryReady := make(chan struct{})
	memoryResponse := sync.OnceFunc(func() { close(memoryReady) })
	var receiptMu sync.Mutex
	receipts := map[string]float64{}
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
		response, err := http.DefaultTransport.RoundTrip(copy)
		if err == nil && r.Method == http.MethodGet && strings.HasSuffix(u.Path, "/generation") {
			raw, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(raw))
			var receipt struct {
				Data struct {
					Cost *float64 `json:"total_cost"`
				} `json:"data"`
			}
			if readErr == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Data.Cost != nil {
				receiptMu.Lock()
				receipts[u.Query().Get("id")] = *receipt.Data.Cost
				receiptMu.Unlock()
			}
		}
		if err == nil && r.Method == http.MethodPost && r.GetBody != nil {
			body, bodyErr := r.GetBody()
			if bodyErr == nil {
				var request struct {
					Messages []struct{ Content string } `json:"messages"`
				}
				_ = json.NewDecoder(body).Decode(&request)
				body.Close()
				if len(request.Messages) > 1 {
					var payload map[string]json.RawMessage
					if json.Unmarshal([]byte(request.Messages[1].Content), &payload) == nil && payload["messages"] != nil {
						response.Body = &memoryGenerationBody{ReadCloser: response.Body, ready: memoryResponse}
					}
				}
			}
		}
		return response, err
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
	if memoryOn {
		if err := knowledge.Open(profile).StartBuild(); err != nil {
			t.Fatal(err)
		}
	}

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
	// WHOSE JOBS THEY ARE comes from the factory's GitHub link; none is made.
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
		Title: "write the greeting", Repo: "acme/api", Tier: factory.TierOwner, Cap: 1, Model: liveFactoryModel,
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, On: true, Until: "done", Ask: "Plan one change: write greet/HELLO.txt containing hello factory and a newline. Do not delegate. Call stage_result with done true and output describing this single change. Do nothing else."},
			{Name: "write", Kind: factory.StageChat, On: true, Until: "done",
				Ask: "Create the file greet/HELLO.txt (under the current directory) with the write tool, its content exactly `" + want + "` and a trailing newline. Then call stage_result with done true, findings 0 and a one-line output. Do nothing else."},
			{Name: "test", Kind: factory.StageCheck, On: true, Ask: "grep -qx '" + want + "' greet/HELLO.txt"},
			{Name: "review", Kind: factory.StageChat, On: true, Until: "clean", Ask: "Read greet/HELLO.txt. Check it contains exactly hello factory and a newline. Do not delegate or change files. Call stage_result with done true, findings 0 and output describing what you checked."},
			{Name: "proof", Kind: factory.StageChat, On: true, Until: "proven", Ask: "Read greet/HELLO.txt. Call stage_result with done true and a claims array containing one claim: text greeting is correct, ok true, evidence greet/HELLO.txt, medium file. Do not delegate or change files."},
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

	// BEFORE ANY START: the job waits in Backlog, and no model was called.
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

	// THE EXPLICIT RUN, THEN THE EXPLICIT START: the two doors a person uses.
	var run appwire.FactoryLocalStatus
	w.must("factory.local_run", map[string]any{"caller": "live"}, &run)
	if !run.Runner {
		t.Fatalf("run: %+v", run)
	}
	var acted appwire.FactoryJobActed
	w.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &acted)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var it factory.Item
	var pausedAt time.Time
	for {
		it, _ = floor.Get(id)
		ready := false
		select {
		case <-memoryReady:
			ready = true
		default:
		}
		if pause && ready && pausedAt.IsZero() && memoryCallsAfter(t, callPath, time.Time{}).active > 0 {
			var current api.Job
			w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &current)
			w.must("factory.job_hold", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": current.Version}}, &appwire.FactoryJobActed{})
			pausedAt = time.Now()
			memoryAwaitReceipts(t, callPath)
			before := memoryCallsAfter(t, callPath, time.Time{}).started
			time.Sleep(5 * time.Second)
			after := memoryCallsAfter(t, callPath, time.Time{}).started
			t.Logf("PAUSE memory_calls_before=%d after=%d new_calls_after_pause=%d hold_seconds=5", before, after, memoryCallsAfter(t, callPath, pausedAt).started)
			if after != before || memoryCallsAfter(t, callPath, pausedAt).started != 0 {
				t.Error("paused job admitted memory")
			}
			break
		}
		if it.State == factory.StateLanded || it.Failure != nil || it.Question != "" || it.State == factory.StateDismissed || ctx.Err() != nil {
			break
		}
		recorder.mu.Lock()
		over := recorder.cost >= 1
		recorder.mu.Unlock()
		if over {
			stopJob()
			cancel()
			t.Fatal("live run reached $1; no further calls authorized")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ctx.Err() != nil {
		stopJob()
	}
	if pause && pausedAt.IsZero() {
		t.Error("job ended before a running memory call could be paused")
	}
	if !pause && it.State != factory.StateLanded {
		t.Errorf("job state=%s question=%q failure=%+v", it.State, it.Question, it.Failure)
	}
	if gh.count() != 0 {
		t.Errorf("GitHub requests: %d", gh.count())
	}
	// Let the real app's final debounced brief enter the measured call log.
	t.Logf("allowing %s for the final automatic brief", factory.BriefDebounce+time.Second)
	time.Sleep(factory.BriefDebounce + time.Second)
	memoryAwaitQuiet(t, callPath)
	memoryAwaitReceipts(t, callPath)
	it, err = floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	var header api.Job
	w.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &header)
	receiptMu.Lock()
	lateUSD := 0.0
	for _, cost := range receipts {
		lateUSD += cost
	}
	receiptMu.Unlock()
	memoryMeasureTable(t, callPath, it, header.Cost, lateUSD, memoryOn)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("models requested=%v answered=%v provider_total=%.8f problems=%v", recorder.requests, recorder.replies, recorder.cost, recorder.problems)
	if len(recorder.problems) > 0 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Error("live model policy failed")
	}
}

// Tables come from the shipped call log, ledger and stage records. They are
// printed only; no measurement artifact becomes part of the repository.
func memoryMeasureTable(t *testing.T, path string, it factory.Item, headerUSD, receiptUSD float64, memoryOn bool) {
	t.Helper()
	if !session.FlushUsage() {
		t.Error("usage ledger did not flush")
	}
	ledger, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var calls []calllog.Record
	starts := map[string]time.Time{}
	var ledgerUSD, memoryUSD float64
	providerUSD := receiptUSD
	total, memory, afterEnd := 0, 0, 0
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
		at, err := time.Parse(time.RFC3339Nano, row.Time)
		if err != nil {
			t.Fatal(err)
		}
		if row.Phase == "start" {
			if row.Tag == "memory" && it.Stream != nil && !it.Stream.Ended.IsZero() && at.After(it.Stream.Ended) {
				afterEnd++
			}
			starts[row.ID] = at
			continue
		}
		calls = append(calls, row)
		total++
		providerUSD += row.Cost
		if row.Tag == "memory" || row.Tag == "reflex" {
			memory++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, row := range ledger {
		if row.Model != "" && row.Model != liveFactoryModel {
			t.Errorf("ledger model=%s", row.Model)
		}
		ledgerUSD += row.USD
		if row.Role == "memory" {
			memoryUSD += row.USD
		}
	}
	t.Logf("memory calls started after job end=%d", afterEnd)
	if afterEnd != 0 {
		t.Error("memory started after the job ended")
	}
	t.Log("stage turn | memory calls | memory USD | step-end ms | memory-only ms")
	var delays, memoryDelays []int64
	if it.Stream == nil {
		t.Fatal("no stage stream recorded")
	}
	for _, phase := range it.Stream.Phases {
		if phase.Kind == factory.StageGate || phase.Started.IsZero() {
			continue
		}
		var header struct {
			ID string `json:"id"`
		}
		if f, err := os.Open(phase.Chat); err == nil {
			_ = json.NewDecoder(bufio.NewReader(f)).Decode(&header)
			f.Close()
		}
		if phase.Kind == factory.StageChat && header.ID == "" {
			t.Errorf("stage %s has no readable conversation identity", phase.Name)
		}
		count, usd := 0, 0.0
		for _, row := range ledger {
			if row.Session == header.ID && row.Role == "memory" {
				usd += row.USD
			}
		}
		for _, row := range calls {
			if row.Tag != "memory" {
				continue
			}
			at, _ := time.Parse(time.RFC3339Nano, row.Time)
			owned := row.Node == header.ID && header.ID != ""
			if row.Node == "" {
				owned = !at.Before(phase.Started) && !at.After(phase.At)
			}
			if owned {
				count++
			}
		}
		stepEnd := phase.At
		for _, next := range it.Stream.Phases {
			if next.Started.After(phase.Started) && next.Started.Before(stepEnd) {
				stepEnd = next.Started
			}
		}
		workEnd := phase.Started
		if phase.Kind == factory.StageCheck {
			workEnd = stepEnd
		}
		for _, row := range calls {
			at, _ := time.Parse(time.RFC3339Nano, row.Time)
			if row.Tag == "turn" && at.After(workEnd) && !at.After(stepEnd) {
				workEnd = at
			}
		}
		delay := max(int64(0), stepEnd.Sub(workEnd).Milliseconds())
		// Parallel compiler requests overlap. Count their wall-time union in
		// this stage's tail rather than adding their request durations.
		intervals := [][2]time.Time{}
		for _, row := range calls {
			if row.Tag != "memory" || (row.Node != "" && row.Node != header.ID) {
				continue
			}
			end, _ := time.Parse(time.RFC3339Nano, row.Time)
			begin := starts[row.ID]
			if begin.Before(workEnd) {
				begin = workEnd
			}
			if end.After(stepEnd) {
				end = stepEnd
			}
			if end.After(begin) {
				intervals = append(intervals, [2]time.Time{begin, end})
			}
		}
		sort.Slice(intervals, func(i, j int) bool { return intervals[i][0].Before(intervals[j][0]) })
		var memoryMS int64
		var covered time.Time
		for _, interval := range intervals {
			begin, end := interval[0], interval[1]
			if begin.Before(covered) {
				begin = covered
			}
			if end.After(begin) {
				memoryMS += end.Sub(begin).Milliseconds()
				covered = end
			}
		}
		delays = append(delays, delay)
		memoryDelays = append(memoryDelays, memoryMS)
		t.Logf("%s/%d | %d | %.8f | %d | %d", phase.Name, phase.Round, count, usd, delay, memoryMS)
	}
	sort.Slice(delays, func(i, j int) bool { return delays[i] < delays[j] })
	sort.Slice(memoryDelays, func(i, j int) bool { return memoryDelays[i] < memoryDelays[j] })
	if len(delays) > 0 {
		t.Logf("memory-only median_ms=%d max_ms=%d", memoryDelays[len(memoryDelays)/2], memoryDelays[len(memoryDelays)-1])
		t.Logf("step-end median_ms=%d max_ms=%d", delays[len(delays)/2], delays[len(delays)-1])
	}
	t.Logf("TOTAL calls=%d memory_calls=%d memory_USD=%.8f provider_USD=%.8f ledger_USD=%.8f job_header_USD=%.8f", total, memory, memoryUSD, providerUSD, ledgerUSD, headerUSD)
	t.Logf("late_provider_receipts_USD=%.8f", receiptUSD)
	if math.Abs(ledgerUSD-providerUSD) > 0.000001 {
		t.Errorf("ledger lost provider-reported cost: %.8f vs %.8f", ledgerUSD, providerUSD)
	}
	if math.Abs(headerUSD-ledgerUSD) > 0.000001 {
		t.Errorf("job header differs from ledger: %.8f vs %.8f", headerUSD, ledgerUSD)
	}
	if providerUSD >= 1 || ledgerUSD >= 1 || headerUSD >= 1 {
		t.Error("live run reached $1")
	}
	if memoryOn && memory == 0 {
		t.Error("no memory call measured")
	}
	if !memoryOn && memory != 0 {
		t.Errorf("memory off made %d calls", memory)
	}
}

// Receipt recovery is measured after the job, outside every stage-tail interval.
// The live fixture does not turn the two-second user wait into a receipt wait.
// memoryAwaitQuiet waits until the call log has stopped growing, so a brief
// that started just before the debounce ended has finished before the header
// is read.
func memoryAwaitQuiet(t *testing.T, path string) {
	t.Helper()
	const quiet = 8 * time.Second
	size, since := int64(-1), time.Now()
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != size {
			size, since = info.Size(), time.Now()
		}
		if time.Since(since) >= quiet {
			return
		}
	}
	t.Error("the call log kept growing after the job ended")
}

func memoryAwaitReceipts(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(provider.ReceiptWait)
	for {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		pending := map[string]bool{}
		unpriced := false
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			var row calllog.Record
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if row.Phase == "start" {
				pending[row.ID] = true
				continue
			}
			delete(pending, row.ID)
			if row.Cost == 0 && (row.Error != "" || row.Ended != "") {
				unpriced = true
			}
		}
		scanErr := scanner.Err()
		file.Close()
		if scanErr != nil {
			t.Fatal(scanErr)
		}
		if len(pending) == 0 {
			if unpriced {
				t.Logf("waiting %s for interrupted-call receipts after the job ended", provider.ReceiptWait+time.Second)
				time.Sleep(provider.ReceiptWait + time.Second)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%d model calls still lack terminal records after job end", len(pending))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type memoryCallWindow struct{ started, active int }

func memoryCallsAfter(t *testing.T, path string, since time.Time) memoryCallWindow {
	t.Helper()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return memoryCallWindow{}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := memoryCallWindow{}
	pending := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var row calllog.Record
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, row.Time)
		if row.Phase == "start" && row.Tag == "memory" && at.After(since) {
			count.started++
			pending[row.ID] = true
		}
		if row.Phase != "start" {
			delete(pending, row.ID)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	count.active = len(pending)
	return count
}

// Wait for a generation identity so cancellation exercises paid receipt recovery.
type memoryGenerationBody struct {
	io.ReadCloser
	seen  []byte
	ready func()
}

func (b *memoryGenerationBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.ready != nil {
		b.seen = append(b.seen, p[:n]...)
		if bytes.Contains(b.seen, []byte("gen-")) {
			b.ready()
			b.ready, b.seen = nil, nil
		}
	}
	return n, err
}
