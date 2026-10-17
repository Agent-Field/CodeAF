//go:build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/home"
)

// THE LIVE FACTORY STAGE LANE. One bounded run of the real factory runner: a
// chat stage opened through the real stage maker, a real model on OpenRouter
// driving the real `write` and `stage_result` tools in a temporary git
// checkout, a check stage that reads what the model wrote, and the item
// landing at its approve gate. Opt-in (CODEAF_LIVE_FACTORY=1), because it
// spends a few cents.
//
// WHAT IS REAL: the runner, the chat executor, the stage maker, the tools, the
// worktree, the check, the model. WHAT IS NOT: GitHub. The checkout has no
// remote, the profile has no token, and nothing is published; the approve gate
// is where this lane stops, so no post stage or pull request is exercised.
//
// EVERY MODEL CALL GOES THROUGH [liveRecorder], which refuses a request whose
// `model` is not the pinned one (or that carries a `models` fallback list),
// forwards the rest to the real OpenRouter, and checks the model the response
// names. It keeps model identifiers, counts, statuses and cost, and never a
// header or a body.

const (
	liveFactoryOptIn  = "CODEAF_LIVE_FACTORY"
	liveFactoryModel  = "deepseek/deepseek-v4.1-flash"
	liveFactoryBudget = 0.50
	liveFactoryWall   = 3 * time.Minute
	liveFactoryMaxHit = 40
)

// liveFactoryKey is read when the test binary starts, before TestMain clears
// every credential and re-roots the profile: the same reason internal/e2e's
// machineKey is read at init. It is the key a launch on this machine would
// use, and it is never printed or written anywhere.
var liveFactoryKey = strings.TrimSpace(config.APIKeyAt(liveFactoryProfileDir()))

func liveFactoryProfileDir() string {
	if dir := strings.TrimSpace(config.ProfileDir()); dir != "" {
		return dir
	}
	return home.InheritedDir()
}

// liveRecorder is an HTTP forwarding recorder for /chat/completions.
type liveRecorder struct {
	server   *httptest.Server
	mu       sync.Mutex
	calls    int
	requests map[string]int
	replies  map[string]int
	statuses map[int]int
	cost     float64
	problems []string
}

func newLiveRecorder(t *testing.T, upstream string) *liveRecorder {
	t.Helper()
	r := &liveRecorder{requests: map[string]int{}, replies: map[string]int{}, statuses: map[int]int{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			http.Error(w, "the live lane forwards chat completions only", http.StatusServiceUnavailable)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(req.Body, 16<<20))
		if err != nil {
			http.Error(w, "unreadable request", http.StatusBadRequest)
			return
		}
		var body map[string]json.RawMessage
		var asked string
		if err := json.Unmarshal(raw, &body); err == nil {
			_ = json.Unmarshal(body["model"], &asked)
		}
		r.mu.Lock()
		r.calls++
		r.requests[asked]++
		over := ""
		switch {
		case asked != liveFactoryModel:
			over = "request model " + asked + " is not " + liveFactoryModel
		case body["models"] != nil:
			over = "request carries a models fallback list"
		case r.cost >= liveFactoryBudget:
			over = "the budget is spent"
		case r.calls > liveFactoryMaxHit:
			over = "the call cap is reached"
		}
		if over != "" {
			r.problems = append(r.problems, over)
		}
		r.mu.Unlock()
		if over != "" {
			http.Error(w, over, http.StatusBadRequest)
			return
		}
		out, err := http.NewRequestWithContext(req.Context(), http.MethodPost, upstream+"/chat/completions", bytes.NewReader(raw))
		if err != nil {
			http.Error(w, "bad upstream", http.StatusInternalServerError)
			return
		}
		for _, h := range []string{"Authorization", "Content-Type", "Accept", "HTTP-Referer", "X-Title"} {
			if v := req.Header.Get(h); v != "" {
				out.Header.Set(h, v)
			}
		}
		out.Header.Set("Accept-Encoding", "identity")
		resp, err := (&http.Client{}).Do(out)
		if err != nil {
			r.note("upstream unreachable")
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		r.mu.Lock()
		r.statuses[resp.StatusCode]++
		r.mu.Unlock()
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		r.forward(w, resp.Body, strings.Contains(resp.Header.Get("Content-Type"), "event-stream"))
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *liveRecorder) note(problem string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems = append(r.problems, problem)
}

// forward copies the response through, flushing as it goes, and reads the
// model and cost out of it. A stream is read line by line; anything else is
// read whole.
func (r *liveRecorder) forward(w http.ResponseWriter, src io.Reader, stream bool) {
	flusher, _ := w.(http.Flusher)
	if !stream {
		all, _ := io.ReadAll(io.LimitReader(src, 16<<20))
		_, _ = w.Write(all)
		r.read(all)
		return
	}
	reader := bufio.NewReaderSize(src, 64<<10)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
			if rest, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); ok {
				r.read(bytes.TrimSpace(rest))
			}
		}
		if err != nil {
			return
		}
	}
}

// read takes the model a reply names, once per reply, and any cost it reports.
func (r *liveRecorder) read(chunk []byte) {
	var m struct {
		Model string `json:"model"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if json.Unmarshal(chunk, &m) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if m.Model != "" {
		r.replies[m.Model]++
		if m.Model != liveFactoryModel {
			r.problems = append(r.problems, "response model "+m.Model+" is not "+liveFactoryModel)
		}
	}
	r.cost += m.Usage.Cost
}

func TestLiveFactoryStageDrivesARealModelToTheApproveGate(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends a few cents on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads (OPENROUTER_API_KEY, profile api_key); not faking a pass")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("BLOCKED: git is not installed")
	}
	started := time.Now()

	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv(factoryPostureEnv, "")

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
	maker := factoryStageMaker(g.st, g.workspace, "", launch.Config)
	r := buildFactoryRunner(g.st, g.st, g.workspace, "", maker, nil)
	stopAll(t, g.st, r)
	seam := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(r))

	const want = "hello factory"
	id, err := g.st.Add(context.Background(), factory.Item{
		Title: "write the greeting", Repo: "api", Tier: factory.TierOwner, Cap: liveFactoryBudget,
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

	ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
	defer cancel()
	if err := seam.Launch(id); err != nil {
		t.Fatal(err)
	}
	var it factory.Item
	reached := false
	for !reached {
		it, _ = g.st.Get(id)
		switch {
		case it.State == factory.StateLanded:
			reached = true
		case it.Failure != nil || it.Question != "" || it.State == factory.StateDismissed:
			reached = true
		case ctx.Err() != nil:
			reached = true
		default:
			time.Sleep(250 * time.Millisecond)
		}
	}

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
		"github":           "none: no remote, no token, no publish",
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
	got, err := os.ReadFile(filepath.Join(g.st.Root(), "work", "api-"+strconv.Itoa(id), "HELLO.txt"))
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("HELLO.txt in the item's worktree = %q, %v", got, err)
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
}

// writeLiveFactoryLog keeps the summary outside the repository, for the person
// reading it afterwards: identifiers, counts, state and cost, nothing else.
func writeLiveFactoryLog(t *testing.T, summary map[string]any) {
	t.Helper()
	dir := os.Getenv("CODEAF_LIVE_FACTORY_LOGS")
	if dir == "" {
		t.Logf("live factory summary: %v", summary)
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Logf("no log directory: %v", err)
		return
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	name := filepath.Join(dir, "live-factory-"+time.Now().UTC().Format("20060102-150405")+".json")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Logf("summary not written: %v", err)
	}
	t.Logf("live factory summary: %v", summary)
}
