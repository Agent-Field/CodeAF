package session

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"sync"
)

// answeringFetch is a back end that fails the way a network does: by URL, with
// the literal words in one answer and a deadline in the other.
type answeringFetch struct {
	mu   sync.Mutex
	urls []string
}

func (*answeringFetch) Name() string { return "answering-fetch" }

func (f *answeringFetch) Fetch(_ context.Context, url string) (string, error) {
	f.mu.Lock()
	f.urls = append(f.urls, url)
	f.mu.Unlock()
	if strings.HasSuffix(url, "/slow") {
		return "", context.DeadlineExceeded
	}
	return "", errors.New(upstreamLiteral)
}

func (f *answeringFetch) fetched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

// rejectionCall is one scripted call and what the typed fact must say of it.
type rejectionCall struct {
	id, tool, args string
	rejected       bool
	// text is the EXACT result the model reads, pinned so the marker can be
	// shown to leave every byte of the old handler result where it was.
	text string
}

const (
	rejectedJSONText   = "Invalid arguments: the arguments are not valid JSON — send one JSON object"
	upstreamLiteral    = "Invalid arguments: upstream says 503"
	bashMissingCommand = `Invalid arguments: command is required — send a JSON object with a non-empty "command" string`
)

// rejectionTurn runs ONE real turn through the real dispatch: a scripted
// provider issues each call in order, and the tools are the belt's own.
func rejectionTurn(t *testing.T, calls []rejectionCall) (*Agent, string, []Event, *answeringFetch, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	fetcher := &answeringFetch{}
	var steps []step
	for _, call := range calls {
		call := call
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(call.id, call.tool, call.args), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	agent, workspace := newTestAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) {
		config.SessionFile = path
		config.SearchFetcher = fetcher
	})
	events := collect(t, mustSubmit(t, agent, "exercise the calls"))
	return agent, path, events, fetcher, workspace
}

func TestRejectedBeforeRunIsATypedFactAndNeverReadFromText(t *testing.T) {
	// Older runs could learn a command as a cure for rejected arguments; that memory must stay silent.
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	signature, keyed := fixSignature("bash", bashMissingCommand)
	if !keyed {
		t.Fatal("the stale argument-error memory has no signature")
	}
	store := settledFixStore(t, filepath.Join(root, "v3", fixesFileName))
	for range fixMinPairings {
		store.confirm(signature, "printf RECOVERED")
	}
	store.confirmAdvised(signature, "printf RECOVERED")
	if len(store.consult(signature)) == 0 {
		t.Fatal("the stale argument-error memory did not become recallable")
	}
	store.settle()

	calls := []rejectionCall{
		{"c-fetch-json", "web_fetch", `{"url":`, true, rejectedJSONText},
		{"c-fetch-missing", "web_fetch", `{}`, true, "Invalid arguments: url is required"},
		{"c-bash-json", "bash", `{"command":"touch ran.marker"`, true, rejectedJSONText},
		{"c-bash-missing", "bash", `{}`, true, bashMissingCommand},
		// RAN and exited nonzero, and its own output says the words.
		{"c-exit", "bash", `{"command":"printf 'Invalid arguments: from the script\\n'; exit 3"}`, false, ""},
		// RAN and failed in the world: the file is not there.
		{"c-read-missing", "read", `{"path":"no-such-file.txt"}`, false, ""},
		// RAN: the back end answered, and its answer is the literal words.
		{"c-http", "web_fetch", `{"url":"https://example.test/a"}`, false, ""},
		// RAN: the request hit its deadline.
		{"c-deadline", "web_fetch", `{"url":"https://example.test/slow"}`, false, ""},
	}
	agent, path, events, fetcher, workspace := rejectionTurn(t, calls)

	if _, err := os.Stat(filepath.Join(workspace, "ran.marker")); err == nil {
		t.Fatal("a malformed bash call executed its command")
	}
	if got := fetcher.fetched(); len(got) != 2 || got[0] != "https://example.test/a" || got[1] != "https://example.test/slow" {
		t.Fatalf("the fetcher saw %v: a rejected fetch must never reach it, and only the two executed ones may", got)
	}

	failed := map[string]Event{}
	for _, event := range events {
		if event.Kind == EventToolFailed {
			failed[event.CallID] = event
		}
	}
	check := func(label string, rejected func(string) bool, entries []DisplayEntry) {
		for _, call := range calls {
			if call.rejected != rejected(call.id) {
				t.Errorf("%s: %s rejected=%v, want %v", label, call.id, rejected(call.id), call.rejected)
			}
		}
		for _, call := range calls {
			row := toolEntryFor(t, entries, call.id)
			wantInvoked := call.id != "c-bash-json" && call.id != "c-bash-missing"
			if row.ToolResult.Invoked() != wantInvoked {
				t.Errorf("%s: %s invoked=%v want %v", label, call.id, row.ToolResult.Invoked(), wantInvoked)
			}
		}
		for _, call := range calls {
			if call.text == "" {
				continue
			}
			if got := toolEntryFor(t, entries, call.id).Output; got != call.text {
				t.Errorf("%s: %s output bytes moved:\n got %q\nwant %q", label, call.id, got, call.text)
			}
		}
	}
	for _, call := range calls {
		event, ok := failed[call.id]
		if !ok {
			t.Fatalf("%s produced no EventToolFailed (events: %d)", call.id, len(events))
		}
		if event.ToolInvoked != (call.id != "c-bash-json" && call.id != "c-bash-missing") {
			t.Errorf("%s event invoked=%v", call.id, event.ToolInvoked)
		}
		if event.HarnessMade || event.Refused {
			t.Errorf("%s: an argument rejection must stay distinct from HarnessMade/Refused: %+v", call.id, event)
		}
	}
	check("live event", func(id string) bool { return failed[id].RejectedBeforeRun }, agent.Transcript())
	check("live history", func(id string) bool { return toolEntryFor(t, agent.Transcript(), id).ToolResult.RejectedBeforeRun() }, agent.Transcript())
	if !strings.Contains(failed["c-http"].Output, upstreamLiteral) || !strings.Contains(failed["c-exit"].Output, "Invalid arguments: from the script") {
		t.Fatalf("the literal words did not reach the executed results: %q / %q", failed["c-http"].Output, failed["c-exit"].Output)
	}
	if !strings.Contains(failed["c-deadline"].Output, "deadline") {
		t.Fatalf("the deadline failure did not reach the result: %q", failed["c-deadline"].Output)
	}
	if exit := failed["c-exit"].Exit; exit == nil || *exit != 3 {
		t.Fatalf("the executed nonzero exit lost its code: %v", exit)
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened := reopen(t, path).Transcript()
	check("reopened history", func(id string) bool { return toolEntryFor(t, reopened, id).ToolResult.RejectedBeforeRun() }, reopened)
	detached := ReadTranscript(path).Entries
	check("detached reading", func(id string) bool { return toolEntryFor(t, detached, id).ToolResult.RejectedBeforeRun() }, detached)
}

// Rejections live on actual result-message occurrences, not duration/call-ID maps.
func TestRejectedResultsPersistOnTheirOwnMessageAndDoNotInventDuration(t *testing.T) {
	agent, path, _, _, _ := rejectionTurn(t, []rejectionCall{{"bad", "bash", `{"command":"x"`, true, rejectedJSONText}})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(body), "\n") {
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Role == "tool" && entry.ToolCallID == "bad" {
			found = entry.Presentation != nil && entry.Presentation.Audience == "tool" && entry.Presentation.RejectedBeforeRun
		}
		if entry.Type == "took" && entry.Took != nil && entry.Took.CallID == "bad" {
			t.Fatal("pre-dispatch rejection invented a work duration")
		}
	}
	if !found || agent.file.took("bad") != 0 {
		t.Fatal("typed result mark absent or duration invented")
	}
}

func TestRejectedResultsWithReusedIDsStayOccurrenceScopedWithinOneRun(t *testing.T) {
	calls := []rejectionCall{
		{"reuse", "bash", `{"command":"x"`, true, rejectedJSONText},
		{"reuse", "bash", `{"command":"printf ran; exit 3"}`, false, ""},
		{"reuse", "bash", `{"command":"x"`, true, rejectedJSONText},
		{"reuse", "bash", `{"command":"printf 'Invalid arguments: actual script'; exit 4"}`, false, ""},
	}
	agent, path, events, _, _ := rejectionTurn(t, calls)
	check := func(label string, entries []DisplayEntry) {
		t.Helper()
		var flags []bool
		for _, entry := range entries {
			if entry.Role == "tool" && entry.CallID == "reuse" {
				flags = append(flags, entry.ToolResult.RejectedBeforeRun())
			}
		}
		if !reflect.DeepEqual(flags, []bool{true, false, true, false}) {
			t.Fatalf("%s reused-ID flags %v", label, flags)
		}
	}
	check("live", agent.Transcript())
	// Compaction re-journals exact immutable result marks, not provider IDs.
	agent.file.appendCompaction(compactionPass{}, 0, append([]ai.Message(nil), agent.snapshot()[1:]...))
	var flags []bool
	for _, event := range events {
		if event.Kind == EventToolFailed && event.CallID == "reuse" {
			flags = append(flags, event.RejectedBeforeRun)
		}
	}
	if !reflect.DeepEqual(flags, []bool{true, false, true, false}) {
		t.Fatalf("events %v", flags)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check("reopened", reopen(t, path).Transcript())
	check("detached", ReadTranscript(path).Entries)
}

func TestRejectedResultsWithDuplicateIDsInOneBatchRemainUnclassified(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "same", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{}`}},
		{ID: "same", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{}`}},
	}
	result := ai.Message{Role: "tool", ToolCallID: "same", Content: []ai.ContentPart{{Type: "text", Text: rejectedJSONText}}}
	presentation := &presentationIndex{}
	presentation.remember(result, &messagePresentation{Audience: "tool", RejectedBeforeRun: true})
	entries := shapeEntries([]ai.Message{{Role: "assistant", ToolCalls: calls}, result}, &sessionFile{presentation: presentation})
	var count int
	for _, entry := range entries {
		if entry.Role == "tool" && entry.CallID == "same" {
			count++
			if entry.ToolResult.RejectedBeforeRun() {
				t.Fatal("ambiguous provider ID lent a rejection to a call")
			}
		}
	}
	if count != 2 {
		t.Fatalf("lost original calls: %d", count)
	}
}

func TestToolDoorRefusalPersistsDistinctFromArgumentRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("denied", "bash", `{"command":"touch denied.marker"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) {
		config.SessionFile = path
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow, Tools: map[string]approval.Action{"bash": approval.ActionDeny}}
	})
	events := collect(t, mustSubmit(t, agent, "try the command"))
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok || !failed.Refused || !failed.HarnessMade || failed.RejectedBeforeRun {
		t.Fatalf("door facts: %+v", failed)
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied.marker")); !os.IsNotExist(err) {
		t.Fatal("denied command executed")
	}
	check := func(label string, entries []DisplayEntry) {
		t.Helper()
		row := toolEntryFor(t, entries, "denied")
		if !row.ToolResult.Refused() || !row.ToolResult.HarnessMade() || row.ToolResult.RejectedBeforeRun() || row.ToolResult.Invoked() {
			t.Fatalf("%s: %+v", label, row)
		}
	}
	check("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check("reopened", reopen(t, path).Transcript())
	check("detached", ReadTranscript(path).Entries)
}

func TestRejectedResultsWithReusedIDsStayOccurrenceScopedAcrossTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	provider := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "bash", `{"command":`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first finished"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "bash", `{"command":"printf second; exit 3"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second finished"), nil },
	}}
	agent, _ := newTestAgent(t, provider, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "first"))
	collect(t, mustSubmit(t, agent, "second"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]DisplayEntry{agent.Transcript(), reopen(t, path).Transcript(), ReadTranscript(path).Entries} {
		var flags []bool
		for _, entry := range entries {
			if entry.Role == "tool" && entry.CallID == "same" {
				flags = append(flags, entry.ToolResult.RejectedBeforeRun())
			}
		}
		if !reflect.DeepEqual(flags, []bool{true, false}) {
			t.Fatalf("cross-turn IDs %v", flags)
		}
	}
}

// THE HELPER ANSWERS THE SAME BYTES AND IS SAFE WITHOUT A MARK.
func TestRejectToolArgumentsKeepsTheTextAndIsSafeBare(t *testing.T) {
	if got := rejectToolArguments(context.Background(), "Invalid arguments: x"); got != "Invalid arguments: x" {
		t.Fatalf("text moved: %q", got)
	}
	//lint:ignore SA1012 a nil context is exactly the sessionless case under test
	if got := rejectToolArguments(nil, "y"); got != "y" { //nolint:staticcheck
		t.Fatalf("nil context changed the text: %q", got)
	}
	ctx, mark := withToolRejection(context.Background())
	if mark.RejectedBeforeRun() {
		t.Fatal("a fresh mark was already set")
	}
	rejectToolArguments(ctx, "z")
	if !mark.RejectedBeforeRun() {
		t.Fatal("the mark was not set")
	}
	_, other := withToolRejection(ctx)
	if other.RejectedBeforeRun() {
		t.Fatal("a nested dispatch shared its parent's mark")
	}
}

// THE SKILLS NOTICE CARRIES ITS KIND AT THE PRODUCER (the memory notice is
// pinned in memory_test.go beside the unrecorded mark it already asserts).
func TestTypedNoticesNameTheirKindAtTheProducer(t *testing.T) {
	skills := turnSkillsNotice([]string{"review", "deploy"})
	if skills.NoticeKind != NoticeKindSkills || len(skills.Skills) != 2 {
		t.Fatalf("skills notice: %+v", skills)
	}
}

// EVERY HANDLER RETURN THAT OPENS WITH THE ARGUMENT-REFUSAL WORDS IS MARKED.
// The words are how a model reads the bad news; the typed mark is how a surface
// knows it, and a return added next month that forgets the mark would draw an
// unrun call as a failure. Parser helpers that return (value, problem) are
// marked by their callers and are not return statements of this shape.
func TestEveryHandlerArgumentRefusalIsMarked(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	opens := func(e ast.Expr) bool {
		for {
			b, ok := e.(*ast.BinaryExpr)
			if !ok {
				break
			}
			e = b.X
		}
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name == "invalidArgumentsPrefix"
		case *ast.BasicLit:
			return strings.HasPrefix(strings.Trim(v.Value, "`\""), "Invalid arguments")
		case *ast.CallExpr:
			if len(v.Args) > 0 {
				if lit, ok := v.Args[0].(*ast.BasicLit); ok {
					return strings.HasPrefix(strings.Trim(lit.Value, "`\""), "Invalid arguments")
				}
			}
		}
		return false
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) < 2 || !opens(ret.Results[0]) {
				return true
			}
			if id, ok := ret.Results[1].(*ast.Ident); ok && id.Name == "true" {
				t.Errorf("%s:%d returns an argument refusal without rejectToolArguments", name, fset.Position(ret.Pos()).Line)
			}
			return true
		})
	}
}

func TestToolInvocationDoesNotClaimAQuickTaskGateRanItsHandler(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	node.graph.mu.Lock()
	node.spec.quick.undone = true
	node.graph.mu.Unlock()
	call := appWriteCall("must-not-exist", "no")
	observed, invoked := observeToolInvocation(appBareTool(t, dir, "write"))
	text, bad, err := agent.executeTaskTool(context.Background(), observed, call, json.RawMessage(call.Function.Arguments))
	if err != nil || !bad || text != "this quick task has ended" || invoked.Load() {
		t.Fatalf("gate: %q bad=%v err=%v invoked=%v", text, bad, err, invoked.Load())
	}
	// The production dispatcher must publish the same fact, not just the wrapper.
	agent.config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	result := agent.executeTool(context.Background(), agent.newEpisode(), nil, call, argsText(call))
	if result.invoked || !result.isError {
		t.Fatalf("dispatcher falsely claimed invocation: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "must-not-exist")); !os.IsNotExist(err) {
		t.Fatal("blocked handler wrote a file")
	}
}

func TestStateSaveFailuresAreNotArgumentRejections(t *testing.T) {
	agent, _ := stateAgent(t)
	store := agent.state()
	store.path = t.TempDir() // A directory cannot be atomically replaced by the JSON state file.
	var track, commit bare.Tool
	for _, tool := range agent.stateTools() {
		if tool.Name == "track" {
			track = tool
		}
		if tool.Name == "commit" {
			commit = tool
		}
	}
	for _, tc := range []struct {
		tool   bare.Tool
		args   string
		reject bool
	}{
		{track, `{"text":"","kind":"progress","evidence":"read"}`, true},
		{track, `{"text":"saw work","kind":"progress","evidence":"read"}`, false},
		{commit, `{"id":"absent"}`, true},
		{commit, `{"id":"p1"}`, false},
	} {
		ctx, mark := withToolRejection(context.Background())
		text, bad, err := tc.tool.Execute(ctx, json.RawMessage(tc.args))
		if err != nil || !bad || mark.RejectedBeforeRun() != tc.reject {
			t.Fatalf("%s: rejected=%v want%v bad=%v err=%v text=%q", tc.tool.Name, mark.RejectedBeforeRun(), tc.reject, bad, err, text)
		}
	}
	records := store.snapshot()
	if len(records) != 1 || records[0].Status != statusDone {
		t.Fatalf("save failure hid actual state mutation: %+v", records)
	}
}

func TestInvalidVideoScoreLevelRefusesBeforeProbingAnyFile(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ctx, mark := withToolRejection(context.Background())
	zero := float64(0)
	text, bad, err := agent.scoreVideo(ctx, editVideoArguments{Video: "not-a-real-video", Audio: "not-a-real-audio", Level: &zero})
	if err != nil || !bad || !mark.RejectedBeforeRun() || !strings.Contains(text, "level") {
		t.Fatalf("late validation: %q bad=%v err=%v", text, bad, err)
	}
}

func TestBashEnvelopeHarnessFactsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	calls := []ai.ToolCall{{ID: "envelope", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{}`}}}
	agent.record(ai.Message{Role: "assistant", ToolCalls: calls})
	agent.rejectBashEnvelope(newEventHub(), calls, "held envelope")
	check := func(entries []DisplayEntry) {
		t.Helper()
		row := toolEntryFor(t, entries, "envelope")
		if !row.ToolResult.HarnessMade() || row.ToolResult.Invoked() || row.ToolResult.RejectedBeforeRun() || row.ToolResult.Refused() {
			t.Fatalf("envelope facts: %+v", row)
		}
	}
	check(agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check(ReadTranscript(path).Entries)
}
