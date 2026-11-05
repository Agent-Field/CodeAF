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
	"net/http/httptest"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

const fourCrewGoal = "Create exactly 20 small Markdown files in this workspace, named note-01.md through note-20.md. Each must contain exactly one short line: hello crew. Plan exactly one worker to write all 20 files with the write tool. Do not delegate, use shell commands, or create any other files. Finish when all 20 files exist."

// TestLiveFourCrewsWallTime compares identical work through the real v3 crew
// door. Fake mode replaces only the provider with a local chat-completions
// server; workers still have to execute write calls and bank their usage.
func TestLiveFourCrewsWallTime(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skip("set CODEAF_LIVE_FACTORY=1; add CODEAF_FOURCREW_FAKE=1 for offline execution")
	}
	fake := os.Getenv("CODEAF_FOURCREW_FAKE") == "1"
	key, upstream := liveFactoryKey, config.DefaultBaseURL
	if fake {
		key = "fourcrew-local-only"
		server := httptest.NewServer(http.HandlerFunc(fourCrewFakeCompletion))
		defer server.Close()
		upstream = server.URL
	} else if key == "" {
		t.Fatal("no OpenRouter credential configured")
	}
	// The timing proxy separates provider time from engine time: it sums
	// each upstream call's duration and records how many overlapped at most.
	timing := newFourCrewTiming(upstream)
	defer timing.server.Close()
	upstream = timing.server.URL
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyKnowledgeModel: liveFactoryModel, config.KeyMemoryEnabled: "off",
		"tools.approvalMode": "allow",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	t.Setenv("CODEAF_MODEL_POOL", "off")
	for _, name := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(name, "")
	}
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, key)
	recorder := newLiveRecorder(t, upstream)
	// Twenty individual writes per crew must fit without the factory lane's
	// smaller call cap changing the workload. Its $0.50 emergency stop remains.
	recorder.maxCalls = 200
	// A run under a shared spend approval passes its remaining share; the
	// recorder refuses the next call once spend reaches it.
	if stop := os.Getenv("CODEAF_FOURCREW_STOP_USD"); stop != "" {
		v, err := strconv.ParseFloat(stop, 64)
		if err != nil || v <= 0 || v > liveFactoryBudget {
			t.Fatalf("CODEAF_FOURCREW_STOP_USD %q must be above 0 and at most %.2f", stop, liveFactoryBudget)
		}
		recorder.budget = v
	}
	defer recorder.server.Close()
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	defer proc.closeAll()
	var agents []*session.Agent
	defer func() {
		for _, a := range agents {
			_ = a.Close()
			a.SettleWrites()
		}
	}()
	workspaces := make([]string, 4)
	for i := range 4 {
		workspaces[i] = filepath.Join(root, fmt.Sprintf("crew-%d", i+1))
		if err := os.MkdirAll(workspaces[i], 0700); err != nil {
			t.Fatal(err)
		}
		launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: workspaces[i]})
		if err != nil {
			t.Fatal(err)
		}
		cfg := launch.Config
		cfg.SessionFile = filepath.Join(root, fmt.Sprintf("crew-%d.jsonl", i+1))
		cfg.DeterministicNames = true
		cfg.ProfileDir = profile
		cfg.ModelFallbacks = nil
		cfg.NearestModels = nil
		cfg.RolesSource = func(key string) (string, bool) {
			if key == "models.fallbacks" || key == "models.roles" {
				return "", true
			}
			return liveFactoryModel, true
		}
		assertFactoryPin(t, cfg, liveFactoryModel)
		a, err := v3OpenSession(cfg)
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	type admission struct {
		crew int
		id   string
		err  error
	}
	starts := make(chan admission, 4)
	gate := make(chan struct{})
	var firstStart sync.Once
	var started time.Time
	for i, a := range agents {
		go func() {
			<-gate
			firstStart.Do(func() { started = time.Now() })
			id, err := a.RunOrchestrate(ctx, fourCrewGoal, liveFactoryModel, liveFactoryBudget)
			starts <- admission{i, id, err}
		}()
	}
	close(gate)
	ids := make([]string, 4)
	for range 4 {
		select {
		case result := <-starts:
			if result.err != nil {
				t.Fatalf("crew %d admission: %v", result.crew+1, result.err)
			}
			ids[result.crew] = result.id
		case <-ctx.Done():
			t.Fatal("timed out admitting crews")
		}
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		allDone := true
		for i, a := range agents {
			snap, ok := a.OrchestrateSnapshot(ids[i])
			allDone = allDone && ok && snap.Done
		}
		if allDone {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("three-minute limit waiting for four crews to finish")
		}
	}
	wall := time.Since(started)
	// Close joins accounting owners before flushing and reading the ledger;
	// close/flush latency is deliberately outside the crew wall time.
	for _, a := range agents {
		if err := a.Close(); err != nil {
			t.Errorf("close crew: %v", err)
		}
		a.SettleWrites()
	}
	// Join both forwarding handlers before reading their totals. A canceled
	// trailing call can outlive crew completion and otherwise escape the sample.
	recorder.server.Close()
	timing.server.Close()
	counts := make([]int, 4)
	for i, workspace := range workspaces {
		for n := 1; n <= 20; n++ {
			data, err := os.ReadFile(filepath.Join(workspace, fmt.Sprintf("note-%02d.md", n)))
			if err == nil && strings.TrimSpace(string(data)) == "hello crew" {
				counts[i]++
			}
		}
		if counts[i] < 20 {
			t.Errorf("crew %d wrote %d/20 expected Markdown files with the required line", i+1, counts[i])
		}
	}
	recorder.mu.Lock()
	calls, cost := recorder.calls, recorder.cost
	seen := map[string]bool{}
	for model := range recorder.requests {
		seen[model] = true
	}
	for model := range recorder.replies {
		seen[model] = true
	}
	recorder.mu.Unlock()
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	t.Logf("FOURCREW wall_ms=%d calls=%d cost_usd=%.9f models=%s files=%v", wall.Milliseconds(), calls, cost, strings.Join(models, ","), counts)
	assertLiveRecorder(t, recorder)
	if !session.FlushUsage() {
		t.Fatal("usage ledger flush did not finish")
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledgerUSD float64
	for _, row := range rows {
		ledgerUSD += row.USD
		if row.Model != liveFactoryModel {
			t.Errorf("ledger row %s recorded model %q, want %s", row.ID, row.Model, liveFactoryModel)
		}
	}
	t.Logf("FOURCREW ledger_usd=%.9f recorder_usd=%.9f", ledgerUSD, cost)
	var inTok, outTok int64
	for _, row := range rows {
		inTok += int64(row.Input)
		outTok += int64(row.Output)
	}
	timing.mu.Lock()
	var reqSum int
	for _, n := range timing.reqBytes {
		reqSum += n
	}
	t.Logf("FOURCREW input_tokens=%d output_tokens=%d request_bytes_sum=%d request_bytes_each=%v", inTok, outTok, reqSum, timing.reqBytes)
	timing.mu.Unlock()
	t.Logf("FOURCREW provider_ms_sum=%d provider_calls=%d max_in_flight=%d", timing.total().Milliseconds(), timing.count(), timing.peak())
	if timing.count() != calls {
		t.Errorf("timing proxy captured %d/%d calls", timing.count(), calls)
	}
	timing.report(t)
	// Each ledger row may round at six decimal places.
	if math.Abs(ledgerUSD-cost) > float64(len(rows))*0.0000005+1e-9 {
		t.Errorf("usage ledger $%.9f differs from recorder $%.9f (%d rows)", ledgerUSD, cost, len(rows))
	}
}

type fourCrewSample struct {
	ttfb, end time.Duration
	stream    bool
	canceled  bool
}

// fourCrewTiming forwards to the provider and measures each call.
type fourCrewTiming struct {
	server   *httptest.Server
	mu       sync.Mutex
	sum      time.Duration
	calls    int
	inFlight int
	maxIn    int
	samples  []fourCrewSample
	reqBytes []int
}

func newFourCrewTiming(upstream string) *fourCrewTiming {
	f := &fourCrewTiming{}
	target, _ := url.Parse(upstream)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = target.Host
		// Request size per call, and an optional capture, let two trees'
		// prompts be compared for the same script.
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		f.mu.Lock()
		f.reqBytes = append(f.reqBytes, len(body))
		seq := len(f.reqBytes)
		f.mu.Unlock()
		if dir := os.Getenv("CODEAF_FOURCREW_CAPTURE_DIR"); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("req-%03d.json", seq)), body, 0600)
		}
		f.mu.Lock()
		f.inFlight++
		f.maxIn = max(f.maxIn, f.inFlight)
		f.mu.Unlock()
		began := time.Now()
		var first time.Duration
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{
			GotFirstResponseByte: func() { first = time.Since(began) },
		}))
		// ReverseProxy may panic with ErrAbortHandler when a caller cancels.
		// Keep that call in the accounting even when no complete body arrives.
		defer func() {
			ended := time.Since(began)
			f.mu.Lock()
			defer f.mu.Unlock()
			f.inFlight--
			f.calls++
			f.sum += ended
			f.samples = append(f.samples, fourCrewSample{first, ended, strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream"), r.Context().Err() != nil})
		}()
		proxy.ServeHTTP(w, r)
	}))
	return f
}

func (f *fourCrewTiming) total() time.Duration { f.mu.Lock(); defer f.mu.Unlock(); return f.sum }
func (f *fourCrewTiming) count() int           { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fourCrewTiming) peak() int            { f.mu.Lock(); defer f.mu.Unlock(); return f.maxIn }

// report measures TTFB at the upstream response's first byte (including headers).
// End includes forwarding the body to the recorder; it is not model token TTFT.
func (f *fourCrewTiming) report(t *testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var firsts, reads []time.Duration
	var firstSum, readSum time.Duration
	for i, sample := range f.samples {
		t.Logf("FOURCREW call=%d stream=%t canceled=%t ttfb_us=%d end_us=%d read_us=%d", i+1, sample.stream, sample.canceled, sample.ttfb.Microseconds(), sample.end.Microseconds(), (sample.end - sample.ttfb).Microseconds())
		if sample.stream {
			firsts = append(firsts, sample.ttfb)
			reads = append(reads, sample.end-sample.ttfb)
			firstSum += sample.ttfb
			readSum += sample.end - sample.ttfb
		}
	}
	t.Logf("FOURCREW stream_calls=%d ttfb_us_sum=%d ttfb_us_median=%d read_us_sum=%d read_us_median=%d", len(firsts), firstSum.Microseconds(), fourCrewMedian(firsts).Microseconds(), readSum.Microseconds(), fourCrewMedian(reads).Microseconds())
}

func fourCrewMedian(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	n := len(values)
	if n%2 == 0 {
		return values[n/2-1]/2 + values[n/2]/2
	}
	return values[n/2]
}

// fourCrewDeltas splits the real reply across frames, preserving assembled output.
func fourCrewDeltas(delta map[string]any, chunks int) []map[string]any {
	out := make([]map[string]any, chunks)
	if content, ok := delta["content"].(string); ok {
		for i := range chunks {
			out[i] = map[string]any{"content": content[len(content)*i/chunks : len(content)*(i+1)/chunks]}
		}
		return out
	}
	calls := delta["tool_calls"].([]any)
	for i := range chunks {
		var fragments []any
		for _, raw := range calls {
			call := raw.(map[string]any)
			function := call["function"].(map[string]any)
			args := function["arguments"].(string)
			part := map[string]any{"arguments": args[len(args)*i/chunks : len(args)*(i+1)/chunks]}
			fragment := map[string]any{"index": call["index"], "function": part}
			if i == 0 {
				fragment["id"] = call["id"]
				fragment["type"] = call["type"]
				part["name"] = function["name"]
			}
			fragments = append(fragments, fragment)
		}
		out[i] = map[string]any{"tool_calls": fragments}
	}
	return out
}

func fourCrewFakeCompletion(w http.ResponseWriter, r *http.Request) {
	// A fixed provider delay makes engine overhead comparable across trees.
	if ms, err := strconv.Atoi(os.Getenv("CODEAF_FOURCREW_FAKE_DELAY_MS")); err == nil && ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "local chat completions only", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid completion request", http.StatusBadRequest)
		return
	}
	planner, opening, answered, write := false, false, false, false
	for _, m := range req.Messages {
		planner = planner || m.Role == "system" && strings.Contains(string(m.Content), "You plan an adaptive run.")
		opening = opening || strings.Contains(string(m.Content), "nothing yet; this is the opening call")
		answered = answered || m.Role == "tool"
	}
	for _, tool := range req.Tools {
		write = write || tool.Function.Name == "write"
	}
	delta := map[string]any{"content": "All 20 files are written."}
	finish := "stop"
	switch {
	case planner && opening:
		plan, _ := json.Marshal(map[string]any{"add": []any{map[string]any{"id": "notes", "title": "Write notes", "goal": fourCrewGoal, "write_scope": []string{"."}}}})
		delta["content"] = string(plan)
	case planner:
		delta["content"] = `{"done":{"brief":"Report that notes are complete."}}`
	case write && !answered:
		var calls []any
		for n := 1; n <= 20; n++ {
			args, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("note-%02d.md", n), "content": "hello crew\n"})
			calls = append(calls, map[string]any{"index": n - 1, "id": fmt.Sprintf("note-%02d", n), "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)}})
		}
		delta = map[string]any{"tool_calls": calls}
		finish = "tool_calls"
	}
	// A nonzero provider cost makes a missing ledger row observable.
	usage := map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "cost": 0.001234567}
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks, _ := strconv.Atoi(os.Getenv("CODEAF_FOURCREW_FAKE_CHUNKS"))
		chunks = max(1, chunks)
		delay, _ := strconv.Atoi(os.Getenv("CODEAF_FOURCREW_FAKE_CHUNK_MS"))
		for _, part := range fourCrewDeltas(delta, chunks) {
			chunk, _ := json.Marshal(map[string]any{"id": "fourcrew-local", "model": liveFactoryModel, "choices": []any{map[string]any{"index": 0, "delta": part, "finish_reason": nil}}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			if delay > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(time.Duration(delay) * time.Millisecond):
				}
			}
		}
		end, _ := json.Marshal(map[string]any{"id": "fourcrew-local", "model": liveFactoryModel, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, "usage": usage})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", end)
		return
	}
	delta["role"] = "assistant"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "fourcrew-local", "model": liveFactoryModel, "choices": []any{map[string]any{"index": 0, "message": delta, "finish_reason": finish}}, "usage": usage})
}

// A canceled trailing call still belongs in the proxy's count. ReverseProxy
// aborts its handler on a broken stream instead of returning normally.
func TestFourCrewTimingIncludesCanceledStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	timing := newFourCrewTiming(upstream.URL)
	defer timing.server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, timing.server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var first [1]byte
	if _, err := io.ReadFull(response.Body, first[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	response.Body.Close()
	timing.server.Close()
	if got := timing.count(); got != 1 {
		t.Fatalf("proxy captured %d calls, want the canceled call counted once", got)
	}
}
