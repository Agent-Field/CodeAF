//go:build !windows

package seniordev

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
)

// hostRecord is one record a run wrote, in the order it wrote them.
type hostRecord struct {
	kind   string
	stages []string
	stage  string
	step   string
	ending delegate.Ending
}

// recordingHost is codeaf as a run meets it: a workspace, ceilings, a model
// API, and the four records, kept in order.
type recordingHost struct {
	mu        sync.Mutex
	workspace string
	ceilings  delegate.Ceilings
	api       delegate.ModelAPI
	records   []hostRecord
}

func (h *recordingHost) Workspace() string           { return h.workspace }
func (h *recordingHost) Ceilings() delegate.Ceilings { return h.ceilings }
func (h *recordingHost) Models() delegate.ModelAPI   { return h.api }
func (h *recordingHost) Hello(stages []string)       { h.add(hostRecord{kind: "hello", stages: stages}) }
func (h *recordingHost) Stage(stage, status string) {
	h.add(hostRecord{kind: "stage", stage: stage + "/" + status})
}
func (h *recordingHost) Step(command, _ string) { h.add(hostRecord{kind: "step", step: command}) }
func (h *recordingHost) Terminal(end delegate.Ending) {
	h.add(hostRecord{kind: "terminal", ending: end})
}

func (h *recordingHost) add(r hostRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
}

func (h *recordingHost) snapshot() []hostRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hostRecord(nil), h.records...)
}

// A key and an address senior-dev must never read. Before codeaf carried it,
// senior-dev took both from these two variables; a run that still did would
// send the one to the other.
const (
	keyNobodyMayRead = "sk-or-v1-a-key-senior-dev-must-never-read"
	runToken         = "codeaf-run-token-for-this-run-only"
)

// modelAPIServer answers like codeaf's model API: OpenRouter's streamed
// chat-completions shape, a keepalive comment before the first chunk, and
// usage.cost in the last. It plays one scripted conversation — write the
// feature, write the checklist, submit, and stop — and keeps every request it
// was sent.
type modelAPIServer struct {
	mu       sync.Mutex
	requests []seenRequest
	// hold, when set, blocks each request until the caller gives up, and says
	// so on the channel first.
	hold chan struct{}
}

type seenRequest struct {
	path          string
	authorization string
	affinity      string
	referer       string
	title         string
	body          map[string]any
	raw           string
}

func (s *modelAPIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.requests = append(s.requests, seenRequest{
		path:          r.URL.Path,
		authorization: r.Header.Get("Authorization"),
		affinity:      r.Header.Get("x-session-affinity"),
		referer:       r.Header.Get("HTTP-Referer"),
		title:         r.Header.Get("X-Title"),
		body:          body,
		raw:           string(raw) + fmt.Sprint(r.Header),
	})
	call := len(s.requests)
	hold := s.hold
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	// The model API keeps a waiting stream alive with SSE comments.
	_, _ = io.WriteString(w, ": keepalive\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	if hold != nil {
		select {
		case hold <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return
	}
	_, _ = io.WriteString(w, scriptedReply(call))
}

func (s *modelAPIServer) seen() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.requests...)
}

// scriptedReply is the model's side of the conversation, one reply per call.
func scriptedReply(call int) string {
	switch call {
	case 1:
		return toolCall(call, "write", map[string]any{"filePath": "feature.txt", "content": "implemented\n"})
	case 2:
		return toolCall(call, "write", map[string]any{"filePath": ".senior-dev/checklist.md", "content": "- [x] the feature is implemented\n"})
	case 3:
		return toolCall(call, "submit", map[string]any{
			"reason": "feature.txt now holds the feature", "evidence": "make test exits 0",
			"checklist_satisfied": true,
		})
	default:
		return `data: {"id":"gen-text","choices":[{"delta":{"content":"Done."}}]}` + "\n\n" +
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"cost":0.001,"prompt_tokens":40,"completion_tokens":3,"total_tokens":43}}` + "\n\n" +
			"data: [DONE]\n\n"
	}
}

func toolCall(call int, name string, arguments map[string]any) string {
	encodedArguments, _ := json.Marshal(arguments)
	chunk := map[string]any{
		"id": fmt.Sprintf("gen-%d", call),
		"choices": []any{map[string]any{
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": fmt.Sprintf("call-%d", call), "type": "function",
				"function": map[string]any{"name": name, "arguments": string(encodedArguments)},
			}}},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"cost": 0.002, "prompt_tokens": 30, "completion_tokens": 10, "total_tokens": 40},
	}
	encoded, _ := json.Marshal(chunk)
	return "data: " + string(encoded) + "\n\ndata: [DONE]\n\n"
}

// hermeticRun is the environment a run meets in these tests: nothing of the
// machine's own configuration, a catalog on disk, no fetch, and the two old
// provider variables set to values senior-dev must not read. It answers the
// workspace, a git repository with a build and a test that pass.
func hermeticRun(t *testing.T) (workspace string, trap *atomic.Bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("SENIOR_DEV_CONFIG_DIR", t.TempDir())
	t.Setenv("SENIOR_DEV_CONFIG", "")
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", "")
	t.Setenv("SENIOR_DEV_PERMISSION", "")
	t.Setenv("SENIOR_DEV_NET", "allow")
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "1")
	catalog, err := filepath.Abs(filepath.Join("modelsdev", "testdata", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENIOR_DEV_MODELS_PATH", catalog)
	hit := &atomic.Bool{}
	trapServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(trapServer.Close)
	t.Setenv("OPENROUTER_API_KEY", keyNobodyMayRead)
	t.Setenv("OPENROUTER_BASE_URL", trapServer.URL)

	workspace = t.TempDir()
	files := map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "README.md", "Makefile"},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "base"},
	} {
		command := exec.Command("git", args...)
		command.Dir = workspace
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return workspace, hit
}

// runBody runs the run command's body the way codeaf's command line does:
// bound on a fresh flag set, handed the line after the flags as the brief.
func runBody(t *testing.T, ctx context.Context, host delegate.Host, line ...string) error {
	t.Helper()
	command, ok := Program.Command(Program.Default)
	if !ok {
		t.Fatalf("senior-dev has no %q command", Program.Default)
	}
	fs := flag.NewFlagSet("senior-dev run", flag.ContinueOnError)
	body := command.Bind(fs)
	if err := fs.Parse(line); err != nil {
		t.Fatal(err)
	}
	return body(ctx, host, fs.Args())
}

// THE WHOLE ROAD, AGAINST A MODEL API THAT ANSWERS LIKE CODEAF'S. The run is
// given a host whose model API is a local server, works a scripted task in a
// real repository, and is held to the protocol: hello first with its stages,
// a step per finished tool call, exactly one terminal and nothing after it;
// every model call on the route codeaf spells, carrying the run's token; and
// no read of the key or the address senior-dev used to take from its
// environment.
func TestTheRunCommandWorksATaskThroughTheModelAPIItIsGiven(t *testing.T) {
	workspace, trapHit := hermeticRun(t)
	server := &modelAPIServer{}
	api := httptest.NewServer(server)
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	err := runBody(t, context.Background(), host,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature.")
	if err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}

	records := host.snapshot()
	if len(records) == 0 || records[0].kind != "hello" {
		t.Fatalf("the first record is not hello: %+v", records)
	}
	if !slices.Equal(records[0].stages, app.Stages) || len(records[0].stages) != 13 {
		t.Fatalf("hello names %v, want the run's thirteen stages %v", records[0].stages, app.Stages)
	}
	var steps, terminals int
	for at, record := range records {
		switch record.kind {
		case "hello":
			if at != 0 {
				t.Fatalf("a second hello at record %d", at)
			}
		case "step":
			steps++
		case "terminal":
			terminals++
			if at != len(records)-1 {
				t.Fatalf("records follow the terminal: %+v", records[at:])
			}
		}
	}
	if steps < 1 {
		t.Fatalf("no step records: %+v", records)
	}
	if terminals != 1 {
		t.Fatalf("%d terminal records, want exactly one", terminals)
	}
	stages := map[string]bool{}
	for _, record := range records {
		if record.kind == "stage" {
			stages[strings.SplitN(record.stage, "/", 2)[0]] = true
			if !slices.Contains(app.Stages, strings.SplitN(record.stage, "/", 2)[0]) {
				t.Fatalf("stage %q is not one the hello named", record.stage)
			}
		}
	}
	for _, want := range []string{"bootstrap", "intake", "implement", "submit", "verification", "ship"} {
		if !stages[want] {
			t.Errorf("no %s stage was reported: %v", want, stages)
		}
	}

	ending := records[len(records)-1].ending
	if ending.Status != delegate.StatusPass {
		t.Fatalf("ending = %+v, want pass", ending)
	}
	if ending.Claim != "feature.txt now holds the feature" {
		t.Errorf("claim = %q, want the model's submission reason", ending.Claim)
	}
	if !strings.Contains(ending.Observed, "passed") {
		t.Errorf("observed = %q, want what senior-dev saw its build and tests do", ending.Observed)
	}
	if ending.CostUSD <= 0 {
		t.Errorf("cost = %v, want the calls' own usage.cost summed", ending.CostUSD)
	}
	for _, banned := range []string{"verified", "verdict", "auditor", "refuted"} {
		for _, said := range []string{ending.Message, ending.Claim, ending.Observed, ending.Reason} {
			if strings.Contains(said, banned) {
				t.Errorf("the ending says %q, a word no person reads from codeaf: %q", banned, said)
			}
		}
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "feature.txt")); err != nil || string(content) != "implemented\n" {
		t.Fatalf("the work is not in the tree: %q, %v", content, err)
	}

	requests := server.seen()
	if len(requests) < 4 {
		t.Fatalf("%d model requests, want the scripted four", len(requests))
	}
	route, err := url.Parse(modelapi.ChatURL(host.api.BaseURL))
	if err != nil {
		t.Fatal(err)
	}
	for at, request := range requests {
		if request.path != route.Path {
			t.Errorf("request %d went to %q, want the model API's route %q", at, request.path, route.Path)
		}
		if request.authorization != "Bearer "+runToken {
			t.Errorf("request %d carried Authorization %q, want the run's token", at, request.authorization)
		}
		if strings.Contains(request.raw, keyNobodyMayRead) {
			t.Errorf("request %d carried the provider key from the environment", at)
		}
		if request.affinity == "" {
			t.Errorf("request %d lost its x-session-affinity header", at)
		}
		if request.referer != "" || request.title != "" {
			t.Errorf("request %d carried attribution headers: %q %q", at, request.referer, request.title)
		}
		if request.body["prompt_cache_key"] == nil {
			t.Errorf("request %d lost its prompt_cache_key", at)
		}
		if usage, _ := request.body["usage"].(map[string]any); usage["include"] != true {
			t.Errorf("request %d did not ask for usage: %v", at, request.body["usage"])
		}
		if _, routed := request.body["provider"]; routed {
			t.Errorf("request %d carried a provider-routing block", at)
		}
		if len(request.body["tools"].([]any)) == 0 {
			t.Errorf("request %d carried no tools", at)
		}
	}
	if trapHit.Load() {
		t.Fatal("a request went to OPENROUTER_BASE_URL")
	}
}

// SIGTERM IS codeaf's STOP. The run's context ends while a model call is in
// flight; the run starts nothing new, ships what it has and writes its one
// terminal quickly, saying the work did not finish — not that it crashed.
func TestAStoppedRunEndsWithItsOneTerminalAndTheTruth(t *testing.T) {
	workspace, _ := hermeticRun(t)
	server := &modelAPIServer{hold: make(chan struct{}, 1)}
	api := httptest.NewServer(server)
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- runBody(t, ctx, host, "--high", "openrouter/fixture/vendor-model", "--", "Add the feature.")
	}()
	select {
	case <-server.hold:
	case err := <-done:
		t.Fatalf("the run ended before its first model call: %v, %+v", err, host.snapshot())
	case <-time.After(30 * time.Second):
		t.Fatal("the run never made a model call")
	}
	stopped := time.Now()
	stop()
	select {
	case <-done:
	case <-time.After(delegate.DefaultGrace):
		t.Fatalf("the run outlived the grace a stop gives it")
	}
	if took := time.Since(stopped); took > 10*time.Second {
		t.Errorf("the run took %s to end after the stop", took)
	}
	records := host.snapshot()
	var terminals []delegate.Ending
	for _, record := range records {
		if record.kind == "terminal" {
			terminals = append(terminals, record.ending)
		}
	}
	if len(terminals) != 1 || records[len(records)-1].kind != "terminal" {
		t.Fatalf("terminals = %+v, want exactly one, last", terminals)
	}
	if terminals[0].Status != delegate.StatusFail || !strings.HasPrefix(terminals[0].Message, "stopped before it finished") {
		t.Fatalf("ending = %+v, want the stop said as unfinished work", terminals[0])
	}
	if n := len(server.seen()); n != 1 {
		t.Fatalf("%d model requests, want none after the stop", n)
	}
}
