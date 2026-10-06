package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// fakeAPI is the run's model API played by a script: each call is answered by
// the next reply, and every body it was sent is kept.
type fakeAPI struct {
	mu      sync.Mutex
	replies []func(body wireRequest) (int, any)
	bodies  []wireRequest
	auth    []string
}

func (f *fakeAPI) serve(t *testing.T) *Client { return f.serveWithHeader(t, "", "") }

// serveWithHeader is serve with one header on every answer that is not a 200.
func (f *fakeAPI) serveWithHeader(t *testing.T, header, value string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body wireRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("the client sent a body that does not parse: %v", err)
		}
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		if len(f.replies) == 0 {
			f.mu.Unlock()
			t.Errorf("an unscripted call reached the API: %s", raw)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reply := f.replies[0]
		f.replies = f.replies[1:]
		f.mu.Unlock()
		status, payload := reply(body)
		if header != "" && status != 200 {
			w.Header().Set(header, value)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(delegate.ModelAPI{BaseURL: server.URL + "/v1", Token: "tok"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	client.pause = func(int) time.Duration { return time.Millisecond }
	client.held = func() time.Duration { return time.Millisecond }
	return client
}

func (f *fakeAPI) then(reply func(body wireRequest) (int, any)) *fakeAPI {
	f.replies = append(f.replies, reply)
	return f
}

func answer(text string, cost float64) func(wireRequest) (int, any) {
	return func(wireRequest) (int, any) {
		return 200, ai.Response{Choices: []ai.Choice{{Message: textMessage("assistant", text)}}, Usage: &ai.Usage{Cost: &cost}}
	}
}

func callTool(name, args string) func(wireRequest) (int, any) {
	return func(wireRequest) (int, any) {
		cost := 0.01
		return 200, ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: args}}}}}},
			Usage: &ai.Usage{Cost: &cost}}
	}
}

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/views.py", "import os\n\ndef run(cmd):\n    return os.popen(cmd).read()\n")
	write("app/models.py", "class User:\n    password = ''\n")
	write("node_modules/lib/index.js", "os.popen('x')\n")
	write("README.md", "# demo\n")
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

var findingSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"file": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer"}},
	"required":             []any{"file", "line"},
	"additionalProperties": false,
}

// A SESSION READS, THEN ANSWERS IN ITS SCHEMA. The model's tool call is run
// against the repository, its result goes back on the same thread, and the
// answer that meets the schema is the session's answer, priced by every turn.
func TestASessionReadsTheRepositoryAndAnswersInItsSchema(t *testing.T) {
	root := repo(t)
	api := (&fakeAPI{}).
		then(callTool("grep", `{"pattern":"os\\.popen"}`)).
		then(func(body wireRequest) (int, any) {
			tool := body.Messages[len(body.Messages)-1]
			if tool.Role != "tool" || !strings.Contains(tool.Content[0].Text, "app/views.py:4:") {
				t.Errorf("the search result handed back = %+v", tool)
			}
			if strings.Contains(tool.Content[0].Text, "node_modules") {
				t.Errorf("a dependency folder was searched: %s", tool.Content[0].Text)
			}
			return answer(`{"file":"app/views.py","line":4}`, 0.02)(body)
		})
	client := api.serve(t)
	result, err := RunSession(t.Context(), client, SessionOrder{Policy: testPolicy, MaxTurns: 50, Model: "vendor/model", Thread: "hunt-1", Root: root,
		Prompt: "find the command injection", Schema: findingSchema})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != "" || string(result.JSON) != `{"file":"app/views.py","line":4}` {
		t.Fatalf("the session answered %+v", result)
	}
	if result.Turns != 2 || result.Tools != 1 || result.CostUSD < 0.0299 || result.CostUSD > 0.0301 {
		t.Fatalf("turns %d, tools %d, cost %.4f", result.Turns, result.Tools, result.CostUSD)
	}
	for _, body := range api.bodies {
		if body.PromptCacheKey != "hunt-1" || body.Model != "vendor/model" {
			t.Fatalf("a call went out on thread %q for %q", body.PromptCacheKey, body.Model)
		}
	}
	if api.auth[0] != "Bearer tok" {
		t.Fatalf("the call carried %q", api.auth[0])
	}
	if !strings.Contains(api.bodies[0].Messages[0].Content[0].Text, `"required":["file","line"]`) {
		t.Fatalf("the system message does not carry the schema: %s", api.bodies[0].Messages[0].Content[0].Text)
	}
}

// AN ANSWER THAT MISSES ITS SCHEMA IS ASKED FOR AGAIN, twice at most and with
// no tools, and the problem is named so the model can fix it.
func TestAnAnswerThatMissesItsSchemaIsAskedForAgainTwice(t *testing.T) {
	root := repo(t)
	api := (&fakeAPI{}).
		then(answer("I found it in views.py", 0)).
		then(func(body wireRequest) (int, any) {
			last := body.Messages[len(body.Messages)-1].Content[0].Text
			if !strings.Contains(last, "holds no JSON object") || len(body.Tools) != 0 {
				t.Errorf("the second ask = %q with %d tools", last, len(body.Tools))
			}
			return answer("```json\n{\"file\":\"app/views.py\"}\n```", 0)(body)
		}).
		then(func(body wireRequest) (int, any) {
			if last := body.Messages[len(body.Messages)-1].Content[0].Text; !strings.Contains(last, "line") {
				t.Errorf("the third ask does not name the missing field: %q", last)
			}
			return answer(`Here: {"file":"app/views.py","line":4} done`, 0)(body)
		})
	result, err := RunSession(t.Context(), api.serve(t), SessionOrder{Policy: testPolicy, MaxTurns: 50, Root: root, Prompt: "find it", Schema: findingSchema})
	if err != nil || result.Failed != "" || string(result.JSON) != `{"file":"app/views.py","line":4}` {
		t.Fatalf("result %+v, err %v", result, err)
	}
	stubborn := (&fakeAPI{}).then(answer("no", 0)).then(answer("no", 0)).then(answer("no", 0))
	result, err = RunSession(t.Context(), stubborn.serve(t), SessionOrder{Policy: testPolicy, MaxTurns: 50, Root: root, Prompt: "find it", Schema: findingSchema})
	if err != nil || result.Failed == "" || result.Turns != 3 {
		t.Fatalf("a model that never met the schema gave %+v, %v", result, err)
	}
}

// THE CEILING ENDS EVERY SESSION AT ONCE. The first 402 is the answer to every
// call after it, without asking a server that already gave it.
func TestTheCeilingIsHeardOnceAndAnsweredToEveryLaterCall(t *testing.T) {
	api := (&fakeAPI{}).then(func(wireRequest) (int, any) {
		return 402, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "the run's dollar ceiling of $1.00 is reached"}}
	})
	client := api.serve(t)
	_, err := client.Complete(t.Context(), Request{Messages: []ai.Message{textMessage("user", "hi")}})
	if !errors.Is(err, ErrCeiling) {
		t.Fatalf("a 402 answered %v", err)
	}
	if _, again := client.Complete(t.Context(), Request{Messages: []ai.Message{textMessage("user", "hi")}}); !errors.Is(again, ErrCeiling) {
		t.Fatalf("the call after the ceiling answered %v", again)
	}
	if len(api.bodies) != 1 {
		t.Fatalf("the API was asked %d times", len(api.bodies))
	}
}

// A passing failure is sent again; a bad request is not.
func TestOnlyAPassingFailureIsSentAgain(t *testing.T) {
	api := (&fakeAPI{}).
		then(func(wireRequest) (int, any) { return 503, map[string]string{"error": "busy"} }).
		then(answer("ok", 0))
	if response, err := api.serve(t).Complete(t.Context(), Request{Messages: []ai.Message{textMessage("user", "hi")}}); err != nil || response.Text() != "ok" {
		t.Fatalf("a 503 then an answer gave %v, %v", response, err)
	}
	bad := (&fakeAPI{}).then(func(wireRequest) (int, any) {
		return 400, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "no messages"}}
	})
	if _, err := bad.serve(t).Complete(t.Context(), Request{Messages: []ai.Message{textMessage("user", "hi")}}); err == nil || !strings.Contains(err.Error(), "no messages") {
		t.Fatalf("a 400 gave %v", err)
	}
	if len(bad.bodies) != 1 {
		t.Fatalf("a 400 was sent %d times", len(bad.bodies))
	}
}

// NO TOOL LEADS OUT OF THE REPOSITORY, by name or by link.
func TestNoToolReadsOutsideTheRepository(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "away")); err != nil {
		t.Fatal(err)
	}
	tools := toolbox{root: root, limits: testPolicy.Tools}
	for _, call := range []struct{ name, args string }{
		{"read_file", `{"path":"../secret"}`},
		{"read_file", `{"path":"` + filepath.Join(outside, "secret") + `"}`},
		{"read_file", `{"path":"away/secret"}`},
		{"list_dir", `{"path":"away"}`},
		{"grep", `{"pattern":"key","path":"away"}`},
	} {
		got := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: call.name, Arguments: call.args}})
		if !strings.HasPrefix(got, "error:") || strings.Contains(got, "key\n") {
			t.Errorf("%s %s answered %q", call.name, call.args, got)
		}
	}
	if got := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "read_file", Arguments: `{"path":"app/views.py","offset":4}`}}); !strings.Contains(got, "     4\t    return os.popen(cmd).read()") {
		t.Errorf("a read inside answered %q", got)
	}
	if got := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "glob", Arguments: `{"pattern":"**/*.py"}`}}); got != "app/models.py\napp/views.py\n" {
		t.Errorf("the glob answered %q", got)
	}
	if got := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"PASSWORD","ignore_case":true,"glob":"*.py"}`}}); got != "app/models.py:2:     password = ''\n" {
		t.Errorf("the search answered %q", got)
	}
	if got := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "bash", Arguments: `{}`}}); !strings.Contains(got, "there is no tool") {
		t.Errorf("an unknown tool answered %q", got)
	}
}

// A session that is still reading on its last turn is told to answer and is
// offered no tools.
func TestTheLastTurnIsAnAnswer(t *testing.T) {
	root := repo(t)
	api := (&fakeAPI{}).
		then(callTool("list_dir", `{}`)).
		then(func(body wireRequest) (int, any) {
			if len(body.Tools) != 0 || !strings.Contains(body.Messages[len(body.Messages)-1].Content[0].Text, "Stop reading now") {
				t.Errorf("the last turn offered %d tools", len(body.Tools))
			}
			return answer("plain words", 0)(body)
		})
	result, err := RunSession(context.Background(), api.serve(t), SessionOrder{Policy: testPolicy, Root: root, Prompt: "look", MaxTurns: 2})
	if err != nil || result.Text != "plain words" || result.Failed != "" {
		t.Fatalf("result %+v, err %v", result, err)
	}
}

// THE OLD HARNESS'S CONTRACT HOLDS: an answer that met its schema is decoded
// into the caller's value, a session that ran and gave none is a result with
// IsError and no Go error, and the session is named by the agent that ran it.
func TestHarnessKeepsTheOldHarnesssContract(t *testing.T) {
	root := repo(t)
	var heard []string
	api := (&fakeAPI{}).then(answer(`{"file":"app/views.py","line":4}`, 0.5)).
		then(answer("no", 0)).then(answer("no", 0)).then(answer("no", 0))
	app := mustNew(t, api.serve(t), Config{Root: root, SessionModel: "vendor/work",
		Watch: &Watch{Session: func(label string, _ SessionResult, _ error) { heard = append(heard, label) }}})
	var dest struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	scratch := filepath.Join(t.TempDir(), "secaf-hunt-scan-123456789")
	result, err := app.Harness(t.Context(), "find it", findingSchema, &dest, appxOptions(scratch, root))
	if err != nil || result.IsError || result.Parsed == nil || dest.File != "app/views.py" || dest.Line != 4 || *result.CostUSD != 0.5 {
		t.Fatalf("result %+v, dest %+v, err %v", result, dest, err)
	}
	if api.bodies[0].PromptCacheKey != "hunt-scan-1" || api.bodies[0].Model != "vendor/work" {
		t.Fatalf("the session ran on thread %q, model %q", api.bodies[0].PromptCacheKey, api.bodies[0].Model)
	}
	failed, err := app.Harness(t.Context(), "find it", findingSchema, &dest, appxOptions("/tmp/secaf-prove-tracer-42", root))
	if err != nil || !failed.IsError || failed.Parsed != nil || failed.ErrorMessage == "" {
		t.Fatalf("a session with no answer gave %+v, %v", failed, err)
	}
	labelled := appxOptions(scratch, root)
	labelled.Label = "DataFlowTracer"
	api.then(answer(`{"file":"a","line":1}`, 0))
	if _, err := app.Harness(t.Context(), "trace", findingSchema, &dest, labelled); err != nil {
		t.Fatal(err)
	}
	if strings.Join(heard, ",") != "hunt scan,prove tracer,data flow tracer" {
		t.Fatalf("the sessions were heard as %v", heard)
	}
	if _, err := app.Harness(t.Context(), "x", nil, nil, appxOptions("", t.TempDir())); err == nil {
		t.Fatal("a session over another folder was run")
	}
	if cost, sessions, _ := app.Spent(); sessions != 3 || cost != 0.5 {
		t.Fatalf("spent %.2f over %d sessions", cost, sessions)
	}
}

// A served model that will not take a schema as a response format is asked
// once more with the schema in words.
func TestAStructuredCallFallsBackToTheSchemaInWords(t *testing.T) {
	api := (&fakeAPI{}).
		then(func(body wireRequest) (int, any) {
			if body.ResponseFormat == nil || body.Model != "vendor/light" {
				t.Errorf("the first ask had format %v on %q", body.ResponseFormat, body.Model)
			}
			return 400, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "response_format is not supported"}}
		}).
		then(func(body wireRequest) (int, any) {
			last := body.Messages[len(body.Messages)-1].Content[0].Text
			if body.ResponseFormat != nil || !strings.Contains(last, `"verdict"`) {
				t.Errorf("the second ask had format %v and said %q", body.ResponseFormat, last)
			}
			return answer(`{"verdict":"likely"}`, 0.1)(body)
		})
	app := mustNew(t, api.serve(t), Config{Root: t.TempDir(), AIModel: "vendor/light"})
	response, err := app.AI(t.Context(), "decide", ai.WithSystem("you decide"),
		ai.WithSchema(json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string"}}}`)))
	if err != nil || response.Text() != `{"verdict":"likely"}` {
		t.Fatalf("got %v, %v", response, err)
	}
	if api.bodies[0].Messages[0].Role != "system" {
		t.Fatalf("the system words were not first: %+v", api.bodies[0].Messages)
	}
	if _, err := app.Call(t.Context(), "sec-af.run_verifier", nil); err == nil {
		t.Fatal("the App answered a reasoner call itself")
	}
}

func appxOptions(cwd, project string) appx.HarnessOptions {
	return appx.HarnessOptions{Cwd: cwd, ProjectDir: project}
}

// A CALL THE CEILING HOLDS BEHIND CALLS IN FLIGHT WAITS ITS TURN, however many
// times it is held, and is not a failure; the ceiling itself is.
func TestAHeldCallWaitsItsTurn(t *testing.T) {
	held := func(wireRequest) (int, any) {
		return 429, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "the run's dollar ceiling of $5.00 is held by calls in flight"}}
	}
	api := &fakeAPI{}
	for range 6 {
		api.then(held)
	}
	api.then(answer("ok", 0))
	client := api.serveWithHeader(t, modelapi.HeldHeader, "ceiling")
	response, err := client.Complete(t.Context(), Request{Messages: []ai.Message{textMessage("user", "hi")}})
	if err != nil || response.Text() != "ok" || len(api.bodies) != 7 {
		t.Fatalf("a held call answered %v, %v after %d asks", response, err, len(api.bodies))
	}
}

// Every tool's parameters are a JSON Schema a strict server takes: an object
// whose `required` is a list, empty included.
func TestEveryToolSchemaIsStrictlyValid(t *testing.T) {
	for _, tool := range (toolbox{limits: testPolicy.Tools}).definitions() {
		encoded, _ := json.Marshal(tool.Function.Parameters)
		var shape struct {
			Type     string          `json:"type"`
			Required json.RawMessage `json:"required"`
		}
		_ = json.Unmarshal(encoded, &shape)
		if shape.Type != "object" || !strings.HasPrefix(string(shape.Required), "[") {
			t.Errorf("%s's parameters are %s", tool.Function.Name, encoded)
		}
		if _, err := compileSchema(tool.Function.Parameters); err != nil {
			t.Errorf("%s's parameters do not compile: %v", tool.Function.Name, err)
		}
	}
}

// A label already in words keeps them; an identifier is split into words; an
// ordinary opening capital is lowered and an acronym kept.
func TestSessionLabelsReadAsAPersonWouldSayThem(t *testing.T) {
	for _, tc := range []struct{ label, cwd, want string }{
		{"DataFlowTracer", "", "data flow tracer"},
		{"Data flow mapper", "", "data flow mapper"},
		{"DoS hunter · scan", "", "DoS hunter · scan"},
		{"auth hunter · src/UserView.py:12", "", "auth hunter · src/UserView.py:12"},
		{"", "/tmp/secaf-remediation-123", "remediation"},
		{"", "/repo", "agent"},
	} {
		if got := sessionLabel(tc.label, tc.cwd); got != tc.want {
			t.Errorf("%q (%q) reads %q, want %q", tc.label, tc.cwd, got, tc.want)
		}
	}
}

// THE SHARED LOOP SAYS THE PROGRAM'S OWN WORK. It was sec's alone and told
// every agent it was one of a security audit; a review's reviewers read the
// code for the wrong thing when told that.
func TestASessionIsToldTheWorkItsProgramNames(t *testing.T) {
	audit := sessionSystem(SessionOrder{Root: "/repo", Work: "a security audit"})
	if !strings.HasPrefix(audit, "You are one agent of a security audit of the repository at /repo. ") {
		t.Fatalf("sec's sessions are told %q", firstLineOf(audit))
	}
	review := sessionSystem(SessionOrder{Root: "/repo", Work: "a code review"})
	if !strings.HasPrefix(review, "You are one agent of a code review of the repository at /repo. ") || strings.Contains(review, "security") {
		t.Fatalf("a review's sessions are told %q", firstLineOf(review))
	}
	if plain := sessionSystem(SessionOrder{Root: "/repo"}); !strings.HasPrefix(plain, "You are one agent working on the repository at /repo. ") {
		t.Fatalf("a session with no work named is told %q", firstLineOf(plain))
	}
}

func firstLineOf(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// ONE AGENT'S FAILED SESSION IS NOT THE RUN'S. A model that keeps answering
// with no choices fails that session as a result the program can degrade
// around; the ceiling still ends the run as an error.
func TestAnEmptyReplyFailsOnlyItsSessionAndTheCeilingEndsTheRun(t *testing.T) {
	root := repo(t)
	empty := func(wireRequest) (int, any) { return 200, ai.Response{} }
	api := (&fakeAPI{}).then(empty).then(empty).then(empty)
	app := mustNew(t, api.serve(t), Config{Root: root})
	var dest struct {
		File string `json:"file"`
	}
	failed, err := app.Harness(t.Context(), "find it", findingSchema, &dest, appxOptions("/tmp/secaf-hunt-scan-1", root))
	if err != nil || failed == nil || !failed.IsError || !strings.Contains(failed.ErrorMessage, "no choices") {
		t.Fatalf("an empty reply gave %+v, %v; want a failed session and no error", failed, err)
	}
	refused := (&fakeAPI{}).then(func(wireRequest) (int, any) {
		return http.StatusPaymentRequired, map[string]any{"error": map[string]any{"message": "the ceiling is reached"}}
	})
	capped := mustNew(t, refused.serve(t), Config{Root: root})
	if _, err := capped.Harness(t.Context(), "find it", findingSchema, &dest, appxOptions("/tmp/secaf-hunt-scan-2", root)); !errors.Is(err, ErrCeiling) {
		t.Fatalf("the ceiling gave %v; want it to end the run", err)
	}
}

// EACH AGENT IS BOUNDED BY ITS OWN FIGURES, and a figure the program leaves
// at zero is the App's default.
func TestEachAgentRunsUnderItsOwnLimits(t *testing.T) {
	app := mustNew(t, nil, Config{MaxTurns: 50, SessionWall: 30 * time.Minute, Limits: func(opts appx.HarnessOptions) Limits {
		if opts.Label == "scanner" {
			return Limits{Turns: 75}
		}
		return Limits{}
	}})
	if got := app.limitsFor(appx.HarnessOptions{Label: "scanner"}); got.Turns != 75 || got.Wall != 30*time.Minute {
		t.Fatalf("the scanner runs under %+v", got)
	}
	if got := app.limitsFor(appx.HarnessOptions{Label: "other"}); got.Turns != 50 || got.Wall != 30*time.Minute {
		t.Fatalf("an agent with no limits of its own runs under %+v", got)
	}
}

// A LONG LINE SAYS HOW TO REACH THE REST, AND GREP REACHES IT. A context file
// written as one line of JSON was cut at 2,000 bytes with no way to its rest,
// and the agents reading it ran to their turn cap.
func TestALongLineIsCutOnACharacterAndGrepShowsAMatchFarIntoIt(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 1999) + "é" + strings.Repeat("b", 28000) + `"needle":"found it"` + strings.Repeat("c", 500)
	if err := os.WriteFile(filepath.Join(root, "context.json"), []byte(long+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolbox{root: root, limits: testPolicy.Tools}
	read := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "read_file", Arguments: `{"path":"context.json"}`}})
	if !utf8.ValidString(read) || !strings.Contains(read, "é …[line cut: the first 2000 of its 30519 characters; grep for what you need in it") {
		t.Fatalf("the long line read as %q", read[max(0, len(read)-200):])
	}
	found := tools.run(ai.ToolCall{Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"needle"}`}})
	if !strings.HasPrefix(found, "context.json:1: … ") || !strings.Contains(found, `"needle":"found it"`) || len([]rune(found)) > 300 {
		t.Fatalf("grep showed %q", found)
	}
}

// testPolicy is the tests' own figures: a program states every one, and so do
// the tests that stand in for one.
var testPolicy = Policy{
	FollowUps: 2, ContextChars: 400_000,
	AnswerNow: "Stop reading now and give your answer from what you have found, in the form the system message asks for.",
	Tools: ToolLimits{ReadLines: 400, ReadBytes: 48 << 10, ReadLineRunes: 2000, ListEntries: 400, GlobMatches: 300,
		GrepMatches: 200, GrepFileBytes: 2 << 20, GrepLineRunes: 240},
}

// mustNew opens an App for a test, stating every figure the test left unset
// with the tests' own.
func mustNew(t *testing.T, client *Client, config Config) *App {
	t.Helper()
	if config.Sessions == 0 {
		config.Sessions = 8
	}
	if config.Calls == 0 {
		config.Calls = 8
	}
	if config.MaxTurns == 0 {
		config.MaxTurns = 50
	}
	if config.SessionWall == 0 {
		config.SessionWall = 30 * time.Minute
	}
	if config.Policy == (Policy{}) {
		config.Policy = testPolicy
	}
	app, err := New(client, config)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// NOTHING HERE HAS A DEFAULT. A program that leaves a figure unset is refused
// where the App is opened and where a session is run, naming the figure, so
// no program runs on a number another program chose.
func TestEveryFigureIsTheProgramsAndAnUnsetOneIsRefused(t *testing.T) {
	whole := Config{Sessions: 8, Calls: 8, MaxTurns: 50, SessionWall: time.Minute, Policy: testPolicy}
	if _, err := New(nil, whole); err != nil {
		t.Fatalf("a Config with every figure stated was refused: %v", err)
	}
	for name, unset := range map[string]func(*Config){
		"Sessions":                   func(c *Config) { c.Sessions = 0 },
		"Calls":                      func(c *Config) { c.Calls = 0 },
		"MaxTurns":                   func(c *Config) { c.MaxTurns = 0 },
		"SessionWall":                func(c *Config) { c.SessionWall = 0 },
		"Policy.FollowUps":           func(c *Config) { c.Policy.FollowUps = 0 },
		"Policy.ContextChars":        func(c *Config) { c.Policy.ContextChars = 0 },
		"Policy.AnswerNow":           func(c *Config) { c.Policy.AnswerNow = " " },
		"Policy.Tools.ReadLines":     func(c *Config) { c.Policy.Tools.ReadLines = 0 },
		"Policy.Tools.ReadBytes":     func(c *Config) { c.Policy.Tools.ReadBytes = 0 },
		"Policy.Tools.ReadLineRunes": func(c *Config) { c.Policy.Tools.ReadLineRunes = 0 },
		"Policy.Tools.ListEntries":   func(c *Config) { c.Policy.Tools.ListEntries = 0 },
		"Policy.Tools.GlobMatches":   func(c *Config) { c.Policy.Tools.GlobMatches = 0 },
		"Policy.Tools.GrepMatches":   func(c *Config) { c.Policy.Tools.GrepMatches = 0 },
		"Policy.Tools.GrepFileBytes": func(c *Config) { c.Policy.Tools.GrepFileBytes = 0 },
		"Policy.Tools.GrepLineRunes": func(c *Config) { c.Policy.Tools.GrepLineRunes = 0 },
	} {
		config := whole
		unset(&config)
		if _, err := New(nil, config); err == nil || !strings.Contains(err.Error(), "Config."+name+" is not set") {
			t.Errorf("leaving %s unset gave %v", name, err)
		}
	}
	if _, err := RunSession(t.Context(), nil, SessionOrder{Root: t.TempDir(), Prompt: "look", Policy: testPolicy}); err == nil || !strings.Contains(err.Error(), "SessionOrder.MaxTurns is not set") {
		t.Fatalf("a session with no turns of its own ran: %v", err)
	}
	if _, err := RunSession(t.Context(), nil, SessionOrder{Root: t.TempDir(), Prompt: "look", MaxTurns: 3}); err == nil || !strings.Contains(err.Error(), "SessionOrder.Policy.FollowUps is not set") {
		t.Fatalf("a session with no policy ran: %v", err)
	}
	if _, err := NewClient(delegate.ModelAPI{BaseURL: "http://127.0.0.1:1/v1", Token: "tok"}, -1); err == nil {
		t.Fatal("a client with negative retries was opened")
	}
}
