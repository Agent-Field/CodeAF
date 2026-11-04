//go:build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

const hardCapDay = .02
const hardCapRequest = .02

// TestLiveDayCapStopsFourCrews uses the same public engine door as
// internal/session/hard_cap_measure_test.go. Ordinary chat cannot start an
// adaptive run; v3OpenSession wires the real agent, then RunOrchestrate starts it.
// CODEAF_HARD_CAP_FAKE=1 exercises only the concurrent recorder locally.
func TestLiveDayCapStopsFourCrews(t *testing.T) {
	if os.Getenv("CODEAF_HARD_CAP_FAKE") == "1" {
		hardCapFakeRecorder(t)
		return
	}
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skip("set CODEAF_LIVE_FACTORY=1 to authorize paid calls")
	}
	if liveFactoryKey == "" {
		t.Fatal("no OpenRouter credential configured")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyKnowledgeModel: liveFactoryModel, config.KeyMemoryEnabled: "off",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	t.Setenv("CODEAF_MODEL_POOL", "off")
	for _, key := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(key, "")
	}
	keys := config.NewAppCredentials()
	t.Cleanup(config.UseAppCredentials(keys))
	keys.Set(modelsource.DefaultID, liveFactoryKey)
	if err := config.WriteDailyBudgetUSD(profile, hardCapDay); err != nil {
		t.Fatal(err)
	}
	input, output := hardCapPrices(t, ctx)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	trace := &hardCapTrace{ctx: ctx, upstream: config.DefaultBaseURL, input: input, output: output}
	recorder.server.Config.Handler = trace.handler(recorder)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)

	crews := 0
	var agents []*session.Agent
	var runIDs []string
	// Installed before opening sessions so failed setup also prints all receipts.
	defer func() {
		cancel()
		for _, a := range agents {
			_ = a.Close()
			a.SettleWrites()
		}
		trace.wait(t)
		hardCapPairs(t, root)
		trace.report(t, recorder, started, crews, liveFactoryKey)
	}()
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	defer proc.closeAll()
	for i := 0; i < 4; i++ {
		workspace := filepath.Join(root, fmt.Sprintf("crew-%d", i))
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		launch, err := openV3Launch(proc, v3Options{Model: liveFactoryModel, OneModel: true, Workspace: workspace})
		if err != nil {
			t.Fatal(err)
		}
		assertFactoryPin(t, launch.Config, liveFactoryModel)
		cfg := launch.Config
		cfg.SessionFile = filepath.Join(root, fmt.Sprintf("crew-%d.jsonl", i))
		cfg.DeterministicNames = true
		cfg.RolesSource = func(key string) (string, bool) {
			if key == "models.fallbacks" || key == "models.roles" {
				return "", true
			}
			return liveFactoryModel, true
		}
		cfg.ProfileDir = profile
		cfg.ModelFallbacks = nil
		cfg.NearestModels = nil
		cfg.ModelPrice = func(string) (float64, float64, bool) { return input, output, true }
		a, err := v3OpenSession(cfg)
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, a)
	}
	// Start all four owners before waiting for any planner or worker response.
	type begun struct {
		i   int
		id  string
		err error
	}
	starts := make(chan begun, 4)
	gate := make(chan struct{})
	for i, a := range agents {
		go func(i int, a *session.Agent) {
			<-gate
			goal := fmt.Sprintf("Create 500 distinct small Markdown files in this workspace, named crew-%d-000.md through crew-%d-499.md. Each file needs 100 original words about a different software engineering topic. Plan exactly one worker with this whole goal. That worker must write exactly ONE file per model reply, using the write tool, then continue with the next file. Do not batch files, use shell loops, delegate, or stop early. All paths are writable. Keep working until all 500 exist.", i, i)
			id, err := a.RunOrchestrate(ctx, goal, liveFactoryModel, 5)
			starts <- begun{i, id, err}
		}(i, a)
	}
	close(gate)
	runIDs = make([]string, 4)
	for range 4 {
		select {
		case result := <-starts:
			if result.err != nil {
				t.Fatalf("start crew %d: %v", result.i, result.err)
			}
			runIDs[result.i] = result.id
			crews++
		case <-ctx.Done():
			t.Fatal("150s limit: waiting for four crew admissions")
		}
	}
	hardCapWait(t, ctx, "daily cap crossing and all four chat notices", func() bool {
		trace.mu.Lock()
		crossed := !trace.crossed.IsZero()
		trace.mu.Unlock()
		if !crossed {
			return false
		}
		for _, a := range agents {
			found := false
			for _, e := range a.Transcript() {
				found = found || strings.Contains(e.Text, "daily limit") && strings.Contains(e.Text, "/budget day")
			}
			if !found {
				return false
			}
		}
		return true
	})
	hardCapWait(t, ctx, "crew calls to stop", func() bool { trace.mu.Lock(); defer trace.mu.Unlock(); return trace.active == 0 })
	trace.mu.Lock()
	crossed, peak := trace.crossed, trace.peak
	for _, r := range trace.rows {
		if r.Admitted && r.Begin.After(crossed.Add(2*time.Second)) {
			t.Errorf("request %d admitted more than 2s after cap crossing", r.Seq)
		}
	}
	trace.mu.Unlock()
	if peak < 2 {
		t.Error("crew requests never overlapped")
	}
	for i, a := range agents {
		snap, ok := a.OrchestrateSnapshot(runIDs[i])
		if !ok || len(snap.Nodes) == 0 {
			t.Errorf("crew %d never started a real worker", i)
		}
	}
	before := trace.count()
	events, err := agents[0].Submit(ctx, "Reply only STILL BLOCKED. Do not use tools.")
	refused := session.IsDailyLimit(err)
	if err == nil {
		for _, e := range hardCapDrain(t, ctx, events, "refused submit") {
			refused = refused || session.IsDailyLimit(e.Err)
		}
	}
	if !refused || trace.count() != before {
		t.Fatal("later submit was not refused without a model call")
	}
	if err := config.WriteDailyBudgetUSD(profile, .40); err != nil {
		t.Fatal(err)
	}
	trace.mu.Lock()
	trace.raised = true
	trace.mu.Unlock()
	events, err = agents[0].Submit(ctx, "The previous work is cancelled. Do not continue it or use tools. Reply with exactly CAP RAISED.")
	if err != nil {
		t.Fatalf("submit after raise: %v", err)
	}
	var answer strings.Builder
	for _, e := range hardCapDrain(t, ctx, events, "one answer after raising /budget day") {
		if e.Kind == session.EventTextDelta {
			answer.WriteString(e.Text)
		}
		if e.Kind == session.EventError {
			t.Errorf("resume: %v", e.Err)
		}
	}
	if !strings.Contains(answer.String(), "CAP RAISED") {
		t.Error("chat did not answer after raising daily cap")
	}
	hardCapWait(t, ctx, "resumed call accounting", func() bool { trace.mu.Lock(); defer trace.mu.Unlock(); return trace.active == 0 })
	if trace.count() != before+1 {
		t.Errorf("resume made %d calls, want one", trace.count()-before)
	}
}

func hardCapDrain(t *testing.T, ctx context.Context, events <-chan session.Event, waiting string) []session.Event {
	t.Helper()
	var out []session.Event
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-ctx.Done():
			t.Fatalf("150s limit: waiting for %s", waiting)
		}
	}
}
func hardCapWait(t *testing.T, ctx context.Context, waiting string, done func() bool) {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !done() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatalf("150s limit: waiting for %s", waiting)
		}
	}
}

func hardCapPrices(t *testing.T, ctx context.Context) (float64, float64) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, config.DefaultBaseURL+"/models", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("cannot verify provider prices")
	}
	defer resp.Body.Close()
	var catalog struct {
		Data []struct {
			ID      string                              `json:"id"`
			Pricing struct{ Prompt, Completion string } `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil || resp.StatusCode != 200 {
		t.Fatal("cannot decode provider prices")
	}
	for _, m := range catalog.Data {
		if m.ID == liveFactoryModel {
			in, e1 := strconv.ParseFloat(m.Pricing.Prompt, 64)
			out, e2 := strconv.ParseFloat(m.Pricing.Completion, 64)
			if e1 == nil && e2 == nil && in > 0 && out > 0 && !math.IsInf(in+out, 0) && in <= 1e-7 && out <= 1e-6 {
				return in, out
			}
		}
	}
	t.Fatal("required model absent or price exceeds request cost bound")
	return 0, 0
}

type hardCapRow struct {
	WorkerCrew        int
	Seq               int
	Begin             time.Time
	Took              time.Duration
	Status, Bytes     int
	Usage             bool
	Finish            string
	Cost, Total       float64
	Early, Admitted   bool
	CutBefore, CutMid bool
	AfterRaise        bool
	Generation        string
}
type hardCapTrace struct {
	mu            sync.Mutex
	ctx           context.Context
	upstream      string
	input, output float64
	rows          []*hardCapRow
	active, peak  int
	handlers      int
	reserved      float64
	crossed       time.Time
	raised        bool
}

func (s *hardCapTrace) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.rows) }

// Locks protect counters only. No lock is held over a provider request. Each
// admitted call reserves its entire cost bound until final usage is known;
// missing usage retains that reservation, including calls cut before an id.
func (s *hardCapTrace) handler(recorder *liveRecorder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/generation") {
			out, _ := http.NewRequestWithContext(s.ctx, http.MethodGet, s.upstream+"/generation?"+req.URL.RawQuery, nil)
			out.Header.Set("Authorization", req.Header.Get("Authorization"))
			resp, err := recorder.client.Do(out)
			if err != nil {
				http.Error(w, "billing lookup ended", 502)
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
			return
		}
		row := &hardCapRow{Begin: time.Now(), WorkerCrew: -1}
		s.mu.Lock()
		s.handlers++
		row.Seq = len(s.rows) + 1
		s.rows = append(s.rows, row)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.handlers--
			row.Took = time.Since(row.Begin)
			if !row.Early && req.Context().Err() != nil {
				row.Early = true
				row.CutBefore = row.Bytes == 0
				row.CutMid = row.Bytes > 0 && row.Finish == ""
			}
			if row.Admitted {
				s.active--
			}
			s.mu.Unlock()
		}()
		raw, err := io.ReadAll(io.LimitReader(req.Body, 200001))
		var body map[string]json.RawMessage
		var model string
		_ = json.Unmarshal(raw, &body)
		_ = json.Unmarshal(body["model"], &model)
		s.mu.Lock()
		recorder.mu.Lock()
		recorder.calls++
		recorder.requests[model]++
		reject := ""
		switch {
		case err != nil || len(raw) > 200000 || body == nil:
			reject = "request exceeds cost bound"
		case req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions"):
			reject = "only completions allowed"
		case model != liveFactoryModel || body["models"] != nil:
			reject = "wrong model or fallback list"
			recorder.problems = append(recorder.problems, reject)
		case recorder.cost+s.reserved+hardCapRequest > liveFactoryBudget:
			reject = "hard spend stop"
		case s.ctx.Err() != nil:
			reject = "150s limit"
		}
		if reject != "" {
			row.Status = 400
			row.Total = recorder.cost
			recorder.mu.Unlock()
			s.mu.Unlock()
			http.Error(w, reject, 400)
			return
		}
		// One byte per input token is conservative. Lower an existing output bound;
		// never raise one. Both completion token spellings are honored.
		maxOutput := int(math.Floor((hardCapRequest - float64(len(raw))*s.input) / s.output))
		if maxOutput < 1 {
			row.Status = 400
			recorder.mu.Unlock()
			s.mu.Unlock()
			http.Error(w, "request input exceeds cost bound", 400)
			return
		}
		maxOutput = min(maxOutput, 2048)
		for _, key := range []string{"max_tokens", "max_completion_tokens"} {
			var n int
			if json.Unmarshal(body[key], &n) == nil && n > 0 {
				maxOutput = min(maxOutput, n)
			}
		}
		body["max_tokens"] = json.RawMessage(strconv.Itoa(maxOutput))
		delete(body, "max_completion_tokens")
		raw, _ = json.Marshal(body)
		if len(body["tools"]) > 2 {
			for i := range 4 {
				if bytes.Contains(raw, []byte(fmt.Sprintf("crew-%d-", i))) {
					row.WorkerCrew = i
					break
				}
			}
		}
		row.AfterRaise = s.raised
		row.Admitted = true
		s.reserved += hardCapRequest
		s.active++
		s.peak = max(s.peak, s.active)
		recorder.mu.Unlock()
		s.mu.Unlock()
		// Preserve the caller's cancellation at the real provider boundary.
		// Missing final usage keeps its reservation; /generation prices the cut.
		forwardCtx, stop := context.WithCancel(req.Context())
		stopDeadline := context.AfterFunc(s.ctx, stop)
		defer stopDeadline()
		defer stop()
		out, _ := http.NewRequestWithContext(forwardCtx, http.MethodPost, s.upstream+"/chat/completions", bytes.NewReader(raw))
		out.Header.Set("Authorization", req.Header.Get("Authorization"))
		out.Header.Set("Content-Type", "application/json")
		out.Header.Set("Accept-Encoding", "identity")
		resp, err := recorder.client.Do(out)
		if err != nil {
			row.Status = 502
			http.Error(w, "upstream request ended", 502)
			return
		}
		defer resp.Body.Close()
		row.Status = resp.StatusCode
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		consume := func(data []byte) {
			var chunk struct {
				ID, Model string
				Usage     *struct{ Cost float64 }
				Choices   []struct {
					Finish *string `json:"finish_reason"`
				}
			}
			if json.Unmarshal(data, &chunk) != nil {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			recorder.mu.Lock()
			defer recorder.mu.Unlock()
			if chunk.ID != "" {
				row.Generation = chunk.ID
			}
			if chunk.Model != "" {
				recorder.replies[chunk.Model]++
				if chunk.Model != liveFactoryModel {
					recorder.problems = append(recorder.problems, "wrong response model: "+chunk.Model)
				}
			}
			for _, c := range chunk.Choices {
				if c.Finish != nil {
					row.Finish = *c.Finish
				}
			}
			if chunk.Usage != nil {
				recorder.cost += max(0, chunk.Usage.Cost-row.Cost)
				row.Cost = max(row.Cost, chunk.Usage.Cost)
				row.Usage = true
				if s.crossed.IsZero() && recorder.cost >= hardCapDay {
					s.crossed = time.Now()
				}
			}
			row.Total = recorder.cost
		}
		forward := func(data []byte) {
			// Classify the cut at the caller boundary; billing is queried later.
			if !row.Early && req.Context().Err() != nil {
				row.Early = true
				row.CutBefore = row.Bytes == 0
				row.CutMid = row.Bytes > 0
			}
			previous := row.Bytes
			row.Bytes += len(data)
			if _, err := w.Write(data); err != nil && !row.Early {
				row.Early = true
				row.CutBefore = previous == 0
				row.CutMid = previous > 0
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				if len(line) > 0 {
					if data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); ok {
						consume(bytes.TrimSpace(data))
					}
					forward(line)
				}
				if err != nil {
					break
				}
			}
		} else {
			all, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			consume(all)
			forward(all)
		}
		s.mu.Lock()
		if row.Usage {
			s.reserved -= hardCapRequest
		}
		s.mu.Unlock()
	})
}

func (s *hardCapTrace) wait(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		active := s.handlers
		s.mu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Error("waiting for recorder handlers after cancellation")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hardCapPairs(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, session.UsageLedgerName) {
			return nil
		}
		record := session.ReadTranscript(path)
		for _, e := range append(record.Earlier, record.Entries...) {
			if e.Role == "tool" && !e.Answered {
				t.Errorf("unpaired tool call in %s: %s", path, e.CallID)
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("read transcripts: %v", err)
	}
}

// Receipts are retried only for eventual billing visibility, never completions.
func (s *hardCapTrace) report(t *testing.T, r *liveRecorder, started time.Time, crews int, key string) {
	t.Helper()
	s.mu.Lock()
	rows := append([]*hardCapRow(nil), s.rows...)
	peak := s.peak
	s.mu.Unlock()
	receiptCtx, cancel := context.WithDeadline(context.Background(), started.Add(150*time.Second))
	defer cancel()
	providerTotal, cutBefore, cutMid := 0., 0, 0
	seen := map[string]bool{}
	paidCrews := map[int]bool{}
	for _, row := range rows {
		t.Logf("seq=%d begin=%s took=%s status=%d bytes=%d usage=%t finish=%q cost=%.9f recorded_total=%.9f caller_left_early=%t generation=%q", row.Seq, row.Begin.Format(time.RFC3339Nano), row.Took, row.Status, row.Bytes, row.Usage, row.Finish, row.Cost, row.Total, row.Early, row.Generation)
		if row.Admitted && row.CutBefore {
			cutBefore++
		}
		if row.Admitted && row.CutMid {
			cutMid++
		}
		if row.Admitted && !row.AfterRaise && !s.crossed.IsZero() && row.Begin.After(s.crossed.Add(2*time.Second)) {
			t.Errorf("request %d admitted after the 2s stop bound", row.Seq)
		}
		if row.Generation == "" || seen[row.Generation] {
			continue
		}
		seen[row.Generation] = true
		billed, found := hardCapReceipt(receiptCtx, s.upstream, key, row.Generation)
		providerTotal += billed
		if billed > 0 && row.WorkerCrew >= 0 {
			paidCrews[row.WorkerCrew] = true
		}
		t.Logf("generation=%q billed=%.9f found=%t cut_mid_stream=%t", row.Generation, billed, found, row.CutMid)
		if !found {
			t.Errorf("required provider billing receipt unavailable for generation %q", row.Generation)
		}
	}
	if crews == 4 && len(paidCrews) != 4 {
		t.Errorf("paid worker evidence from %d/4 crews", len(paidCrews))
	}
	if !session.FlushUsage() {
		t.Error("ledger flush failed")
	}
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Errorf("ledger read: %v", err)
	}
	ledger := session.SpendToday(lines, time.Now())
	r.mu.Lock()
	recorded := r.cost
	problems := append([]string(nil), r.problems...)
	r.mu.Unlock()
	if recorded > liveFactoryBudget || providerTotal > liveFactoryBudget {
		t.Error("spending exceeded $0.50 emergency limit")
	}
	if len(problems) > 0 {
		t.Errorf("model validation: %v", problems)
	}
	if ledger > providerTotal+1e-9 {
		t.Errorf("ledger %.9f exceeds provider %.9f", ledger, providerTotal)
	}
	t.Logf("cap=%.9f ledger_day=%.9f recorder_usage=%.9f provider_billed=%.9f cut_before_reply=%d cut_mid_stream=%d peak_concurrent=%d crews_started=%d past_cap=%.9f seconds=%.3f provider_minus_ledger=%.9f", hardCapDay, ledger, recorded, providerTotal, cutBefore, cutMid, peak, len(paidCrews), max(0, providerTotal-hardCapDay), time.Since(started).Seconds(), providerTotal-ledger)
}
func hardCapReceipt(ctx context.Context, upstream, key, id string) (float64, bool) {
	for attempt := 0; attempt < 8; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream+"/generation?id="+url.QueryEscape(id), nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var receipt struct {
				Data struct {
					Cost *float64 `json:"total_cost"`
				}
			}
			decode := json.NewDecoder(resp.Body).Decode(&receipt)
			resp.Body.Close()
			if resp.StatusCode == 200 && decode == nil && receipt.Data.Cost != nil {
				return *receipt.Data.Cost, true
			}
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-time.After(250 * time.Millisecond):
		}
	}
	return 0, false
}

func hardCapFakeRecorder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{}, 4)
	release := make(chan struct{})
	cut := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/generation" {
			fmt.Fprint(w, `{"data":{"total_cost":0.003}}`)
			return
		}
		var body struct {
			MaxTokens int  `json:"max_tokens"`
			Cut       bool `json:"local_cut"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.MaxTokens > 128 {
			t.Errorf("recorder raised max_tokens to %d", body.MaxTokens)
		}
		if body.Cut {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"local-cut\",\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", liveFactoryModel)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(cut)
			return
		}
		ready <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"local\",\"model\":%q,\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0.006}}\n\ndata: [DONE]\n\n", liveFactoryModel)
	}))
	defer fake.Close()
	r := newLiveRecorder(t, fake.URL)
	trace := &hardCapTrace{ctx: ctx, upstream: fake.URL, input: 1e-7, output: 1e-6}
	r.server.Config.Handler = trace.handler(r)
	done := make(chan error, 4)
	for range 4 {
		go func() {
			resp, err := http.Post(r.server.URL+"/chat/completions", "application/json", strings.NewReader(`{"model":"`+liveFactoryModel+`","max_tokens":128}`))
			if err == nil {
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			done <- err
		}()
	}
	for range 4 {
		select {
		case <-ready:
		case <-ctx.Done():
			close(release)
			t.Fatal("local requests did not overlap")
		}
	}
	close(release)
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	trace.wait(t)
	if trace.peak != 4 || trace.crossed.IsZero() || math.Abs(r.cost-.024) > 1e-9 {
		t.Fatalf("fake accounting peak=%d crossed=%t cost=%f", trace.peak, !trace.crossed.IsZero(), r.cost)
	}
	cutCtx, stopCut := context.WithCancel(ctx)
	request, _ := http.NewRequestWithContext(cutCtx, http.MethodPost, r.server.URL+"/chat/completions", strings.NewReader(`{"model":"`+liveFactoryModel+`","max_tokens":128,"local_cut":true}`))
	partial, err := http.DefaultClient.Do(request)
	if err != nil {
		stopCut()
		t.Fatal(err)
	}
	_, err = bufio.NewReader(partial.Body).ReadString('\n')
	stopCut()
	partial.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-cut:
	case <-ctx.Done():
		t.Fatal("caller cancellation did not reach local provider")
	}
	trace.wait(t)
	last := trace.rows[len(trace.rows)-1]
	if !last.Early || !last.CutMid || last.Generation != "local-cut" || last.Usage {
		t.Fatalf("cut stream evidence: %+v", last)
	}
	if billed, found := hardCapReceipt(ctx, fake.URL, "local-only", last.Generation); !found || billed != .003 {
		t.Fatalf("local cut-stream billing receipt: found=%t cost=%f", found, billed)
	}
	// Saturating the reservation refuses further requests at the recorder.
	trace.mu.Lock()
	trace.reserved = .50
	trace.mu.Unlock()
	resp, err := http.Post(r.server.URL+"/chat/completions", "application/json", strings.NewReader(`{"model":"`+liveFactoryModel+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Error("hard stop admitted request")
	}
	trace.wait(t)
	for _, row := range trace.rows {
		t.Logf("local seq=%d begin=%s took=%s status=%d bytes=%d usage=%t finish=%q cost=%.9f recorded_total=%.9f caller_left_early=%t generation=%q", row.Seq, row.Begin.Format(time.RFC3339Nano), row.Took, row.Status, row.Bytes, row.Usage, row.Finish, row.Cost, row.Total, row.Early, row.Generation)
	}
	t.Log("local recorder: four overlapping calls, usage crossing, cancellation and emergency refusal verified; real crews and provider billing remain unrun")
}
