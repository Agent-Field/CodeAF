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
	// record is the step record whole: its tool, its step and its exit.
	record delegate.StepRecord
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
func (h *recordingHost) Stage(stage delegate.StageRecord) {
	h.add(hostRecord{kind: "stage", stage: stage.Stage + "/" + stage.Status})
}
func (h *recordingHost) Step(step delegate.StepRecord) {
	h.add(hostRecord{kind: "step", step: step.Command, record: step})
}
func (h *recordingHost) Terminal(end delegate.Ending) {
	h.add(hostRecord{kind: "terminal", ending: end})
}

// Records names the run's record folder as codeaf's own child host does: from
// CODEAF_RECORDS, which codeaf sets on the line of every run it carries.
func (h *recordingHost) Records() string { return os.Getenv(delegate.EnvRecords) }

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
// usage.cost in the last. It plays one scripted conversation — run the tests,
// write the feature, write the checklist, submit, and stop — and keeps every request it
// was sent.
type modelAPIServer struct {
	mu       sync.Mutex
	requests []seenRequest
	// hold, when set, blocks each request until the caller gives up, and says
	// so on the channel first.
	hold chan struct{}
	// script, when set, is the model's side of the conversation in place of
	// [scriptedReply].
	script func(call int) string
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
	reply := scriptedReply
	if s.script != nil {
		reply = s.script
	}
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
	_, _ = io.WriteString(w, reply(call))
}

func (s *modelAPIServer) seen() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.requests...)
}

// scriptedReply is the model's side of the conversation, one reply per call:
// run the tests first, write the feature, write the checklist, submit, stop.
func scriptedReply(call int) string {
	switch call {
	case 1:
		return toolCall(call, "bash", map[string]any{"command": "make test"})
	case 2:
		return toolCall(call, "write", map[string]any{"filePath": "feature.txt", "content": "implemented\n"})
	case 3:
		return toolCall(call, "write", map[string]any{"filePath": ".senior-dev/checklist.md", "content": "- [x] the feature is implemented\n"})
	case 4:
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
	t.Setenv(app.StateDirEnv, "")
	t.Setenv(delegate.EnvRecords, "")
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
	if !slices.Equal(records[0].stages, app.Stages) || len(records[0].stages) != 15 {
		t.Fatalf("hello names %v, want the run's fifteen stages %v", records[0].stages, app.Stages)
	}
	var steps, terminals int
	named := map[string]bool{}
	for at, record := range records {
		switch record.kind {
		case "hello":
			if at != 0 {
				t.Fatalf("a second hello at record %d", at)
			}
		case "step":
			steps++
			// EVERY STEP NAMES ITS TOOL AND THE STEP OF THE PROCESS IT SERVED.
			if record.record.Tool == "" || !slices.Contains(app.Steps, record.record.Step) {
				t.Fatalf("step %q says tool %q and step %q, want a tool and one of %v", record.step, record.record.Tool, record.record.Step, app.Steps)
			}
			named[record.record.Step] = true
			// A COMMAND SAYS HOW IT EXITED, from the shell tool's own record of it.
			if record.record.Tool == "bash" && record.record.Exit == nil {
				t.Fatalf("the command step %q carries no exit code", record.step)
			}
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
	// The scripted model runs the tests, writes the feature, then its checklist,
	// then submits; senior-dev then runs the project's own build and tests itself.
	for _, want := range []string{app.StepExplore, app.StepImplement, app.StepChecklist, app.StepSubmit, app.StepVerify} {
		if !named[want] {
			t.Errorf("no step was named %q: %v", want, named)
		}
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
	if len(requests) < 5 {
		t.Fatalf("%d model requests, want the scripted five", len(requests))
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

// --state-dir REACHES THE RUN, AND WINS OVER SENIOR_DEV_STATE_DIR. A whole run
// keeps its session database and its conversation in the directory the flag
// names; the variable's directory and the folder's .senior-dev hold none of
// them, and the brief is still written where the model is told to read it.
func TestTheStateDirFlagKeepsTheStoreWhereItSays(t *testing.T) {
	workspace, _ := hermeticRun(t)
	stateDir, variable, record := filepath.Join(t.TempDir(), "run-1"), t.TempDir(), t.TempDir()
	t.Setenv(app.StateDirEnv, variable)
	t.Setenv(delegate.EnvRecords, record)
	api := httptest.NewServer(&modelAPIServer{})
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	err := runBody(t, context.Background(), host, "--state-dir", stateDir,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature.")
	if err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}

	records := host.snapshot()
	if ending := records[len(records)-1].ending; ending.Status != delegate.StatusPass {
		t.Fatalf("ending = %+v, want pass", ending)
	}
	for _, name := range []string{"senior-dev.db", "storage", "projection.lock"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); err != nil {
			t.Errorf("%s is not in the --state-dir: %v", name, err)
		}
	}
	if entries, _ := os.ReadDir(variable); len(entries) != 0 {
		t.Errorf("%s's directory holds %d entries although --state-dir named another", app.StateDirEnv, len(entries))
	}
	if entries, _ := os.ReadDir(record); len(entries) != 0 {
		t.Errorf("the record folder holds %d entries although --state-dir named another directory", len(entries))
	}
	for _, name := range []string{"senior-dev.db", "storage", "projection.lock"} {
		if _, err := os.Lstat(filepath.Join(workspace, ".senior-dev", name)); err == nil {
			t.Errorf("the folder's .senior-dev holds %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, ".senior-dev", "spec.md")); err != nil {
		t.Errorf("the brief is not where the model reads it: %v", err)
	}
}

// A RUN CODEAF CARRIES KEEPS ITS STORE IN ITS RECORD FOLDER. With no
// --state-dir and no SENIOR_DEV_STATE_DIR, the session database, the records
// and the lock go in a directory of the run's own in the folder codeaf named
// on CODEAF_RECORDS, and the folder's .senior-dev holds only what the model
// works from.
func TestACarriedRunKeepsItsStoreInTheRecordFolder(t *testing.T) {
	workspace, _ := hermeticRun(t)
	record := t.TempDir()
	t.Setenv(delegate.EnvRecords, record)
	api := httptest.NewServer(&modelAPIServer{})
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	if err := runBody(t, context.Background(), host,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature."); err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}
	records := host.snapshot()
	if ending := records[len(records)-1].ending; ending.Status != delegate.StatusPass {
		t.Fatalf("ending = %+v, want pass", ending)
	}
	for _, name := range []string{"senior-dev.db", "storage", "projection.lock"} {
		if _, err := os.Stat(filepath.Join(record, "store", name)); err != nil {
			t.Errorf("%s is not in the record folder's store: %v", name, err)
		}
	}
	assertNotesOnly(t, workspace)
}

// A LAUNCH REFUSED BEFORE ITS FIRST CALL LEAVES NO STORE BEHIND. Where the
// store goes is decided early, but its directory is made only when the store
// opens, after every refusal, so the next launch of the task gets store and
// not store.1 beside an empty one.
func TestARefusedLaunchLeavesNoStoreInTheRecordFolder(t *testing.T) {
	workspace, _ := hermeticRun(t)
	record := t.TempDir()
	t.Setenv(delegate.EnvRecords, record)
	api := httptest.NewServer(&modelAPIServer{})
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	_ = runBody(t, context.Background(), host,
		"--asked", "--high", "openrouter/nobody/knows-this-model", "--", "Add", "the", "feature.")
	records := host.snapshot()
	if ending := records[len(records)-1].ending; ending.Status != delegate.StatusCrashed {
		t.Fatalf("ending = %+v, want the launch refused", ending)
	}
	if entries, _ := os.ReadDir(record); len(entries) != 0 {
		t.Errorf("a refused launch left %d entries in the record folder", len(entries))
	}
}

// SENIOR_DEV_STATE_DIR WINS OVER THE RECORD FOLDER, as --state-dir does: a
// directory a person named is where the store goes, and the record folder
// gets none.
func TestTheStateDirVariableWinsOverTheRecordFolder(t *testing.T) {
	workspace, _ := hermeticRun(t)
	record, variable := t.TempDir(), filepath.Join(t.TempDir(), "named")
	t.Setenv(delegate.EnvRecords, record)
	t.Setenv(app.StateDirEnv, variable)
	api := httptest.NewServer(&modelAPIServer{})
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	if err := runBody(t, context.Background(), host,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature."); err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(variable, "senior-dev.db")); err != nil {
		t.Errorf("the store is not where %s named: %v", app.StateDirEnv, err)
	}
	if entries, _ := os.ReadDir(record); len(entries) != 0 {
		t.Errorf("the record folder holds %d entries although %s named another directory", len(entries), app.StateDirEnv)
	}
}

// A RUN NOBODY CARRIED KEEPS ITS STORE IN THE FOLDER'S .senior-dev, as before:
// with no record folder there is nowhere else that is the run's own.
func TestARunWithNoRecordFolderKeepsItsStoreInTheFolder(t *testing.T) {
	workspace, _ := hermeticRun(t)
	api := httptest.NewServer(&modelAPIServer{})
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	if err := runBody(t, context.Background(), host,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature."); err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}
	for _, name := range []string{"senior-dev.db", "storage", "projection.lock"} {
		if _, err := os.Stat(filepath.Join(workspace, ".senior-dev", name)); err != nil {
			t.Errorf("%s is not in the folder's .senior-dev: %v", name, err)
		}
	}
}

// A .senior-dev THE WORK REMOVES MID-RUN IS WRITTEN BACK, AND THE RUN HANDS IN.
// The model's own command empties it, as a benchmark's restore script and
// OpenSSL's `make clean` did; the store is in the record folder and untouched,
// the brief and the checklist are written back as the run last read them, and
// the submit that reads the checklist next is accepted.
func TestANotesFolderTheWorkRemovesIsWrittenBack(t *testing.T) {
	workspace, _ := hermeticRun(t)
	record := t.TempDir()
	t.Setenv(delegate.EnvRecords, record)
	server := &modelAPIServer{script: func(call int) string {
		switch call {
		case 4:
			return toolCall(call, "bash", map[string]any{"command": "rm -rf .senior-dev"})
		case 5:
			return toolCall(call, "submit", map[string]any{
				"reason": "feature.txt now holds the feature", "evidence": "make test exits 0",
				"checklist_satisfied": true,
			})
		}
		return scriptedReply(call)
	}}
	api := httptest.NewServer(server)
	t.Cleanup(api.Close)
	host := &recordingHost{
		workspace: workspace,
		api:       delegate.ModelAPI{BaseURL: api.URL + "/v1", Token: runToken},
	}

	if err := runBody(t, context.Background(), host,
		"--high", "openrouter/fixture/vendor-model", "--", "Add", "the", "feature."); err != nil {
		t.Fatalf("the body answered an error: %v", err)
	}
	records := host.snapshot()
	if ending := records[len(records)-1].ending; ending.Status != delegate.StatusPass {
		t.Fatalf("ending = %+v, want pass", ending)
	}
	if !slices.ContainsFunc(records, func(record hostRecord) bool {
		return record.kind == "stage" && record.stage == "implement/notes-rewritten"
	}) {
		t.Error("no implement/notes-rewritten record says a note file was written back")
	}
	for name, want := range map[string]string{
		"spec.md":      "Add the feature.",
		"checklist.md": "- [x] the feature is implemented\n",
	} {
		if data, err := os.ReadFile(filepath.Join(workspace, ".senior-dev", name)); err != nil || string(data) != want {
			t.Errorf(".senior-dev/%s = %q, %v; want %q written back", name, data, err, want)
		}
	}
	messages, _ := filepath.Glob(filepath.Join(record, "store", "storage", "message", "*", "*.json"))
	if len(messages) < 5 {
		t.Errorf("the store in the record folder holds %d messages; the conversation should be whole", len(messages))
	}
	assertNotesOnly(t, workspace)
}

// assertNotesOnly fails the test when the folder's .senior-dev holds anything
// but the files the model works from: no database, no records, no lock.
func assertNotesOnly(t *testing.T, workspace string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(workspace, ".senior-dev"))
	if err != nil {
		t.Fatalf("the folder's .senior-dev: %v", err)
	}
	allowed := map[string]bool{
		"spec.md": true, "checklist.md": true, "pinned.txt": true, "steering.md": true,
		"tool-output": true, "cmake-build": true,
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Errorf("the folder's .senior-dev holds %s, which is not one of the model's files", entry.Name())
		}
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
