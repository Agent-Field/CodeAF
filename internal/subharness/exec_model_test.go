package subharness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// modelServer is a provider endpoint with a script behind it: each reply is
// chosen by the first phrase that appears in the request, so a test says "when
// you are asked to check something, say PASS" without knowing the prompt's
// wording. Every request it saw is kept, which is how the prompt-building half
// of the bridge is asserted.
type modelServer struct {
	mu     sync.Mutex
	seen   []string
	bodies []map[string]any
	rules  []modelRule
	used   []int
	http   *httptest.Server
	// bill is the accounting every reply carries when a test sets it, which is
	// what a provider that reports usage looks like from here. Nil is a provider
	// that says nothing, which is what the rest of this file's tests want: they
	// are about what the bridge ASKED, and a run's price is its own subject
	// (usage.go).
	bill *ai.Usage
}

type modelRule struct {
	when, say  string
	tool, args string
	toolCalls  int
}

func newModelServer(t *testing.T, rules ...modelRule) *modelServer {
	t.Helper()
	server := &modelServer{rules: rules, used: make([]int, len(rules))}
	server.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(raw)
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(encoded, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var page strings.Builder
		for _, message := range body.Messages {
			page.WriteString(string(message.Content))
			page.WriteString("\n")
		}
		asked := page.String()

		server.mu.Lock()
		server.seen = append(server.seen, asked)
		server.bodies = append(server.bodies, raw)
		answer := "said nothing in particular"
		var callTool, callArgs string
		for at, rule := range server.rules {
			if strings.Contains(asked, rule.when) {
				answer = rule.say
				if _, offered := raw["tools"]; offered && rule.tool != "" && server.used[at] < rule.toolCalls {
					callTool, callArgs = rule.tool, rule.args
					server.used[at]++
				}
				break
			}
		}
		server.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		message := map[string]any{"role": "assistant", "content": answer}
		if callTool != "" {
			message["content"] = nil
			message["tool_calls"] = []any{map[string]any{
				"id": "call-1", "type": "function",
				"function": map[string]any{"name": callTool, "arguments": callArgs},
			}}
		}
		payload := map[string]any{
			"model": "test/model",
			"choices": []any{map[string]any{
				"index":   0,
				"message": message,
			}},
		}
		server.mu.Lock()
		if server.bill != nil {
			payload["usage"] = server.bill
		}
		server.mu.Unlock()
		reply, _ := json.Marshal(payload)
		_, _ = w.Write(reply)
	}))
	t.Cleanup(server.http.Close)
	return server
}

func (s *modelServer) client(t *testing.T) *provider.Client {
	t.Helper()
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: s.http.URL, Model: "test/model",
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return client
}

// asked reports whether any request carried this phrase.
func (s *modelServer) asked(phrase string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, request := range s.seen {
		if strings.Contains(request, phrase) {
			return true
		}
	}
	return false
}

func (s *modelServer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func testTool(name string) ai.ToolDefinition {
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: "test " + name,
		Parameters: map[string]any{"type": "object"},
	}}
}

func (s *modelServer) offered(at int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at >= len(s.bodies) {
		return nil
	}
	raw, _ := s.bodies[at]["tools"].([]any)
	var names []string
	for _, entry := range raw {
		tool, _ := entry.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		names = append(names, name)
	}
	return names
}

// TestModelExecAgentLoopRunsTools pins the whole bridge: the structured call
// reaches the dispatcher, its answer returns in the transcript, and only the
// model's final words become the node's output.
func TestModelExecAgentLoopRunsTools(t *testing.T) {
	server := newModelServer(t, modelRule{
		when: "inspect it", say: "the suite is green", tool: "bash",
		args: `{"command":"go test ./..."}`, toolCalls: 1,
	})
	h := Harness{Id: Id{Name: "worker", Version: 1}, Whitelist: []string{"bash"}, Verify: Verify{Ladder: VerifyAccept}}
	var called string
	exec := ModelExec(server.client(t), ModelExecOpts{Harness: h, Toolbelt: Toolbelt{
		Defs: []ai.ToolDefinition{testTool("bash")},
		Call: func(_ context.Context, name string, args map[string]any) (string, error) {
			called = fmt.Sprintf("%s %v", name, args["command"])
			return "PASS from the tool", nil
		},
	}})
	result, err := exec(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "inspect it", "tools": "bash",
	}})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if called != "bash go test ./..." {
		t.Fatalf("dispatch got %q", called)
	}
	if result.Out != "the suite is green" || !server.asked("PASS from the tool") {
		t.Fatalf("output %q, tool result reached provider %v", result.Out, server.asked("PASS from the tool"))
	}
}

func TestModelExecAgentLoopToolOffer(t *testing.T) {
	server := newModelServer(t, modelRule{when: "use one", say: "done"})
	h := Harness{Id: Id{Name: "worker", Version: 1}, Whitelist: []string{"read", "bash"}, Verify: Verify{Ladder: VerifyAccept}}
	exec := ModelExec(server.client(t), ModelExecOpts{Harness: h, Toolbelt: Toolbelt{
		Defs: []ai.ToolDefinition{testTool("read"), testTool("bash")},
		Call: func(context.Context, string, map[string]any) (string, error) { return "ok", nil },
	}})
	if _, err := exec(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "use one", "tools": "bash",
	}}); err != nil {
		t.Fatalf("subset loop: %v", err)
	}
	if got := server.offered(0); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("offered %v, want only bash", got)
	}

	badHarness := h
	badHarness.Whitelist = []string{"read"}
	bad := ModelExec(server.client(t), ModelExecOpts{Harness: badHarness, Toolbelt: Toolbelt{
		Defs: []ai.ToolDefinition{testTool("read"), testTool("bash")},
		Call: func(context.Context, string, map[string]any) (string, error) { return "ok", nil },
	}})
	if _, err := bad(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "use one", "tools": "bash",
	}}); err == nil || !strings.Contains(err.Error(), "whitelist") {
		t.Fatalf("off-whitelist error %v", err)
	}
}

func TestModelExecAgentLoopWithoutToolsIsOneCompletion(t *testing.T) {
	server := newModelServer(t, modelRule{when: "just think", say: "thought"})
	h := Harness{Id: Id{Name: "thinker", Version: 1}, Verify: Verify{Ladder: VerifyAccept}}
	result, err := ModelExec(server.client(t), ModelExecOpts{Harness: h})(context.Background(),
		Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{"brief": "just think"}})
	if err != nil || result.Out != "thought" {
		t.Fatalf("result (%q, %v)", result.Out, err)
	}
	if server.calls() != 1 || len(server.offered(0)) != 0 {
		t.Fatalf("calls %d, offered %v", server.calls(), server.offered(0))
	}
}

func TestModelExecAgentLoopRespectsTurnCeiling(t *testing.T) {
	server := newModelServer(t, modelRule{
		when: "keep looking", say: "final at the ceiling", tool: "read",
		args: `{"path":"notes.md"}`, toolCalls: 99,
	})
	h := Harness{Id: Id{Name: "bounded", Version: 1}, Whitelist: []string{"read"}, Verify: Verify{Ladder: VerifyAccept}}
	called := 0
	result, err := ModelExec(server.client(t), ModelExecOpts{Harness: h, Toolbelt: Toolbelt{
		Defs: []ai.ToolDefinition{testTool("read")},
		Call: func(context.Context, string, map[string]any) (string, error) { called++; return "more", nil },
	}})(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "keep looking", "tools": "read", "max_turns": "2",
	}})
	if err != nil || result.Out != "final at the ceiling" {
		t.Fatalf("result (%q, %v)", result.Out, err)
	}
	if called != 2 || server.calls() != 3 {
		t.Fatalf("dispatched %d times across %d calls, want 2 plus one final call", called, server.calls())
	}
}

func TestModelExecAgentLoopNamedToolsWithoutBelt(t *testing.T) {
	server := newModelServer(t, modelRule{when: "describe it", say: "I would read it"})
	h := Harness{Id: Id{Name: "legacy", Version: 1}, Whitelist: []string{"read"}, Verify: Verify{Ladder: VerifyAccept}}
	result, err := ModelExec(server.client(t), ModelExecOpts{Harness: h})(context.Background(),
		Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{"brief": "describe it", "tools": "read"}})
	if err != nil || result.Out != "I would read it" {
		t.Fatalf("result (%q, %v)", result.Out, err)
	}
	if server.calls() != 1 || len(server.offered(0)) != 0 || !server.asked("You cannot call them here") {
		t.Fatalf("calls %d, offered %v, prompt %v", server.calls(), server.offered(0), server.seen)
	}
}

// TestModelExecStraight is the whole bridge on the ordinary program: a loop
// that thinks, a tool that runs, and a check that reads what both produced.
func TestModelExecStraight(t *testing.T) {
	server := newModelServer(t,
		modelRule{when: "look at the tree", say: "the parser is in parse.go"},
		modelRule{when: "claim to check", say: "PASS\nthe build ran and the finding is in the trail"},
	)
	var ran []string
	h := straight()
	h.Program.Nodes[0].Fields["brief"] = "look at the tree"
	trace, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{
		Harness: h,
		RunTool: func(_ context.Context, tool, args string) (string, error) {
			ran = append(ran, tool+" "+args)
			return "ok, no errors", nil
		},
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if trace.Status != StatusOK {
		t.Fatalf("status %q, want ok", trace.Status)
	}
	if len(ran) != 1 || ran[0] != "bash go build ./..." {
		t.Fatalf("the tool ran as %v, want one bash with the node's own arguments", ran)
	}
	if server.calls() != 2 {
		t.Fatalf("%d model calls, want one for the loop and one for the check", server.calls())
	}
	// The loop's own answer must reach the check as evidence, which is the
	// whole point of the accumulated outputs.
	if !server.asked("the parser is in parse.go") || !server.asked("ok, no errors") {
		t.Fatalf("the check was not shown the trail: %v", server.seen)
	}
	if !server.asked("loop rung of the verification ladder") {
		t.Fatalf("the harness's ladder did not reach the check: %v", server.seen)
	}
	last := trace.Trail[len(trace.Trail)-1]
	if !strings.Contains(last.Out, "passed") {
		t.Fatalf("the check's row reads %q, want a pass", last.Out)
	}
}

// TestModelExecThreadsPredecessors states the closure-accumulated half: a node
// with two nodes leading into it is shown BOTH of their outputs, named.
func TestModelExecThreadsPredecessors(t *testing.T) {
	server := newModelServer(t,
		modelRule{when: "read the left", say: "left says four"},
		modelRule{when: "read the right", say: "right says nine"},
		modelRule{when: "add them up", say: "thirteen"},
	)
	h := Harness{
		Id: Id{Name: "adder", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "left", Kind: KindAgentLoop, Fields: Fields{"brief": "read the left"}},
				{Id: "right", Kind: KindAgentLoop, Fields: Fields{"brief": "read the right"}},
				{Id: "sum", Kind: KindAgentLoop, Fields: Fields{"brief": "add them up"}},
			},
			Edges: []Edge{{"left", "right"}, {"left", "sum"}, {"right", "sum"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
	}
	trace, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{Harness: h}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// A plain [Run] fills no Out — that is [Runner.Run]'s — so the run's own
	// answer is the last thing the trail holds.
	if last := trace.Trail[len(trace.Trail)-1]; trace.Status != StatusOK || last.Out != "thirteen" {
		t.Fatalf("the run ended %q with %q, want ok and the summing node's answer", trace.Status, last.Out)
	}
	server.mu.Lock()
	last := server.seen[len(server.seen)-1]
	server.mu.Unlock()
	for _, want := range []string{"left says four", "right says nine", "left:", "right:"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the summing node was not shown %q:\n%s", want, last)
		}
	}
}

// TestModelExecFreeTextCondition is the seam predicate.go describes from the
// other side: `ok` never reaches a model, and a sentence always does.
func TestModelExecFreeTextCondition(t *testing.T) {
	server := newModelServer(t,
		modelRule{when: "try again", say: "tried"},
		modelRule{when: "the suite is green", say: "NO\nthree tests are still red"},
	)
	h := Harness{
		Id: Id{Name: "looper", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "work", Kind: KindAgentLoop, Fields: Fields{"brief": "try again"}},
				{Id: "done", Kind: KindLoopUntil, Fields: Fields{"until": "the suite is green", "max_rounds": "3"}},
			},
			Edges: []Edge{{"work", "done"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
		Dyn:    Dyn{Ladder: DynBranch, Cap: 4},
	}
	trace, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{Harness: h}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Three rounds, all answered NO, all asked of the model.
	rounds := 0
	for _, step := range trace.Trail {
		if step.Id == "done" {
			rounds++
		}
	}
	if rounds != 3 {
		t.Fatalf("the loop ran %d rounds, want its full 3", rounds)
	}
	if !server.asked("the suite is green") {
		t.Fatalf("the free-text condition never reached the model")
	}
}

// TestModelExecConditionFailsClosed states the default that keeps the ladder
// honest: an answer nobody can read is NO, never a quiet yes.
func TestModelExecConditionFailsClosed(t *testing.T) {
	for _, said := range []string{"", "it depends, really", "maybe", "I think so?"} {
		if readYes(said) {
			t.Fatalf("%q was read as a yes", said)
		}
	}
	for _, said := range []string{"YES", "yes\nthe suite is green", "Yes."} {
		if !readYes(said) {
			t.Fatalf("%q was not read as a yes", said)
		}
	}
	for _, said := range []string{"", "hard to say", "it looks fine to me"} {
		if passed, _ := readVerdict(said); passed {
			t.Fatalf("%q was read as a pass", said)
		}
	}
	if passed, because := readVerdict("PASS\nthe suite is green"); !passed || because != "the suite is green" {
		t.Fatalf("a pass read as (%v, %q)", passed, because)
	}
}

// TestModelExecGateWithNobodyThere is the headless bargain: the gate approves
// and the trail says why, so a saved run never reads as a person's consent.
func TestModelExecGateWithNobodyThere(t *testing.T) {
	server := newModelServer(t, modelRule{when: "draft it", say: "here is the draft"})
	h := Harness{
		Id: Id{Name: "gated", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "draft", Kind: KindAgentLoop, Fields: Fields{"brief": "draft it"}},
				{Id: "sure", Kind: KindHumanGate, Fields: Fields{"ask": "send it?"}},
			},
			Edges: []Edge{{"draft", "sure"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
	}
	trace, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{Harness: h}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	gate := trace.Trail[len(trace.Trail)-1]
	if !strings.Contains(gate.Out, "approved") || !strings.Contains(gate.Out, autoGateNote) {
		t.Fatalf("the gate's row reads %q, want an approval that says nobody was asked", gate.Out)
	}
}

// TestModelExecGateAnswers is the three answers a person can give, and the one
// rule that matters: a sentence that is neither yes nor no is an intervention.
func TestModelExecGateAnswers(t *testing.T) {
	cases := []struct {
		said string
		want string
		note string
	}{
		{"yes", "approved", ""},
		{"ok, but skip the deploy", "approved", "but skip the deploy"},
		{"no", "declined", ""},
		{"no, the branch is wrong", "declined", "the branch is wrong"},
		{"take over from here", "intervened", "from here"},
		{"intervene", "intervened", ""},
		{"actually let me look at the diff first", "intervened", "actually let me look at the diff first"},
		{"", "intervened", ""},
	}
	for _, c := range cases {
		answer := readGate(c.said)
		if answer.Word() != c.want || strings.TrimSpace(answer.Note) != c.note {
			t.Fatalf("%q read as (%s, %q), want (%s, %q)", c.said, answer.Word(), answer.Note, c.want, c.note)
		}
	}
}

// TestModelExecGateAsks runs a real Ask through the walk, so the declining half
// is exercised where it actually lands rather than only in readGate.
func TestModelExecGateAsks(t *testing.T) {
	server := newModelServer(t, modelRule{when: "draft it", say: "here is the draft"})
	h := Harness{
		Id: Id{Name: "asked", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "draft", Kind: KindAgentLoop, Fields: Fields{"brief": "draft it"}},
				{Id: "sure", Kind: KindHumanGate, Fields: Fields{"ask": "send it?"}},
			},
			Edges: []Edge{{"draft", "sure"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
	}
	var asked string
	_, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{
		Harness: h,
		Ask: func(_ context.Context, question string) (string, error) {
			asked = question
			return "no, not yet", nil
		},
	}))
	if asked != "send it?" {
		t.Fatalf("the person was asked %q, want the node's own question", asked)
	}
	// Through a plain Run a decline can only end as an error, which is the
	// caveat exec_model.go's header states. What matters is that it STOPPED.
	if err == nil {
		t.Fatalf("a declined gate let the run finish")
	}
}

// TestModelExecTools is the tool half: no bridge, off the whitelist, and a
// tool that refused.
func TestModelExecTools(t *testing.T) {
	server := newModelServer(t)
	h := straight()
	build := Node{Id: "build", Kind: KindToolCall, Fields: Fields{"tool": "bash", "args": "go build ./..."}}

	t.Run("no tools on this surface", func(t *testing.T) {
		exec := ModelExec(server.client(t), ModelExecOpts{Harness: h})
		if _, err := exec(context.Background(), build); err == nil ||
			!strings.Contains(err.Error(), "has no tools") {
			t.Fatalf("error %v, want a surface with no tools", err)
		}
	})

	t.Run("off the whitelist", func(t *testing.T) {
		exec := ModelExec(server.client(t), ModelExecOpts{
			Harness: h,
			RunTool: func(context.Context, string, string) (string, error) { return "ran", nil },
		})
		stray := Node{Id: "stray", Kind: KindToolCall, Fields: Fields{"tool": "write", "args": "{}"}}
		if _, err := exec(context.Background(), stray); err == nil ||
			!strings.Contains(err.Error(), "whitelist") {
			t.Fatalf("error %v, want a whitelist refusal", err)
		}
	})

	t.Run("the tool refused", func(t *testing.T) {
		exec := ModelExec(server.client(t), ModelExecOpts{
			Harness: h,
			RunTool: func(context.Context, string, string) (string, error) {
				return "", fmt.Errorf("exit status 2")
			},
		})
		if _, err := exec(context.Background(), build); err == nil ||
			!strings.Contains(err.Error(), "exit status 2") {
			t.Fatalf("error %v, want the tool's own failure", err)
		}
	})
}

// TestModelExecCallNeedsStore states the one kind this bridge refuses outright
// without somewhere to load from.
func TestModelExecCallNeedsStore(t *testing.T) {
	server := newModelServer(t)
	h := straight()
	exec := ModelExec(server.client(t), ModelExecOpts{Harness: h})
	call := Node{Id: "deeper", Kind: KindSubharnessCall, Fields: Fields{"name": "triage"}}
	if _, err := exec(context.Background(), call); err == nil ||
		!strings.Contains(err.Error(), "not in the exec bridge") {
		t.Fatalf("error %v, want the bridge to say it cannot call without a store", err)
	}
}

// TestModelExecCallsChild runs a harness from inside a harness, through a real
// store, and states the two things the wiring buys: the child's OWN whitelist
// bounds its nodes, and its trace lands in its own history.
func TestModelExecCallsChild(t *testing.T) {
	store := At(t.TempDir())
	child := Harness{
		Id: Id{Name: "triage", Desc: "sorts it out", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "sort", Kind: KindToolCall, Fields: Fields{"tool": "read", "args": "notes.md"}},
		}},
		Whitelist: []string{"read"},
		Verify:    Verify{Ladder: VerifyAccept},
	}
	if _, err := store.Save(child); err != nil {
		t.Fatalf("save child: %v", err)
	}
	parent := Harness{
		Id: Id{Name: "outer", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "inner", Kind: KindSubharnessCall, Fields: Fields{"name": "triage"}},
		}},
		// Deliberately NOT read: the parent allows nothing, and the child's
		// node must still run on the child's own list.
		Verify: Verify{Ladder: VerifyAccept},
		Dyn:    Dyn{Ladder: DynRecursive, Cap: 2},
	}
	server := newModelServer(t)
	var ran []string
	trace, err := Run(context.Background(), parent, ModelExec(server.client(t), ModelExecOpts{
		Harness: parent,
		Store:   store,
		RunTool: func(_ context.Context, tool, args string) (string, error) {
			ran = append(ran, tool+" "+args)
			return "the notes say it is fine", nil
		},
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(ran) != 1 || ran[0] != "read notes.md" {
		t.Fatalf("the child's tool ran as %v", ran)
	}
	if len(trace.Trail) != 1 || !strings.Contains(trace.Trail[0].Out, "triage v1 · ok") {
		t.Fatalf("the parent's row reads %v, want the child's pointer and status", trace.Trail)
	}
	runs, err := store.Runs("triage")
	if err != nil || len(runs) != 1 {
		t.Fatalf("the child's history holds %v (%v), want one trace", runs, err)
	}
}

// leafHarness is a child whose only work is one tool.call: a program that needs
// a tool and no model, which is what keeps a nesting test off the network.
func leafHarness(name string) Harness {
	return Harness{
		Id: Id{Name: name, Desc: "reads the notes", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "sort", Kind: KindToolCall, Fields: Fields{"tool": "read", "args": "notes.md"}},
		}},
		Whitelist: []string{"read"},
		Verify:    Verify{Ladder: VerifyAccept},
	}
}

// callerHarness is a program that is nothing but calls, one after another. Its
// whitelist is deliberately empty: a caller lends its child nothing.
func callerHarness(name string, budget int, calls ...string) Harness {
	h := Harness{
		Id:     Id{Name: name, Version: 1},
		Verify: Verify{Ladder: VerifyAccept},
		Dyn:    Dyn{Ladder: DynRecursive, Cap: budget},
	}
	for at, called := range calls {
		id := fmt.Sprintf("call%d", at+1)
		h.Program.Nodes = append(h.Program.Nodes,
			Node{Id: id, Kind: KindSubharnessCall, Fields: Fields{"name": called}})
		if at > 0 {
			h.Program.Edges = append(h.Program.Edges, Edge{fmt.Sprintf("call%d", at), id})
		}
	}
	return h
}

func saveHarness(t *testing.T, store *Store, h Harness) {
	t.Helper()
	if _, err := store.Save(h); err != nil {
		t.Fatalf("save %s: %v", h.Id.Name, err)
	}
}

// readNotes is the one tool the leaves of these tests call, and a record of
// every time it ran.
func readNotes(ran *[]string) func(context.Context, string, string) (string, error) {
	return func(_ context.Context, tool, args string) (string, error) {
		*ran = append(*ran, tool+" "+args)
		return "the notes say it is fine", nil
	}
}

// TestModelExecCallsTwoDeep is the nesting itself: top calls mid, mid calls
// leaf, and each level leaves its own trace while the caller's row keeps the
// pointer, the status and what the child finally said.
func TestModelExecCallsTwoDeep(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, leafHarness("leaf"))
	saveHarness(t, store, callerHarness("mid", 2, "leaf"))
	top := callerHarness("top", 2, "mid")

	server := newModelServer(t)
	var ran []string
	trace, err := Run(context.Background(), top, ModelExec(server.client(t), ModelExecOpts{
		Harness: top,
		Store:   store,
		RunTool: readNotes(&ran),
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if server.calls() != 0 {
		t.Fatalf("%d model calls, want none — every node in this tree is a call or a tool", server.calls())
	}
	if len(ran) != 1 || ran[0] != "read notes.md" {
		t.Fatalf("the deepest tool ran as %v, want one read on the child's own whitelist", ran)
	}
	if len(trace.Trail) != 1 {
		t.Fatalf("the top's trail is %v, want one call row", trace.Trail)
	}
	row := trace.Trail[0].Out
	for _, want := range []string{"mid v1 · ok", "leaf v1 · ok", "the notes say it is fine"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the top's row does not carry %q:\n%s", want, row)
		}
	}
	// Each level's run is saved in its OWN history, which is where somebody
	// asking how leaf behaves would look.
	for _, name := range []string{"mid", "leaf"} {
		runs, err := store.Runs(name)
		if err != nil || len(runs) != 1 {
			t.Fatalf("%s's history holds %v (%v), want one trace", name, runs, err)
		}
	}
}

// TestModelExecCallDepthCap states the bound: a call that would run deeper than
// the cap refuses, and says how deep it was and what the cap was.
func TestModelExecCallDepthCap(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, leafHarness("leaf"))
	saveHarness(t, store, callerHarness("mid", 2, "leaf"))
	top := callerHarness("top", 2, "mid")

	server := newModelServer(t)
	var ran []string
	_, err := Run(context.Background(), top, ModelExec(server.client(t), ModelExecOpts{
		Harness:  top,
		Store:    store,
		DepthCap: 1,
		RunTool:  readNotes(&ran),
	}))
	if err == nil {
		t.Fatalf("a call past the depth cap was allowed")
	}
	for _, want := range []string{"depth cap of 1", "top v1 → mid v1 → leaf", "mid v1 · failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal reads %q, want it to carry %q", err, want)
		}
	}
	if len(ran) != 0 {
		t.Fatalf("the leaf's tool ran %v under a cap that should have stopped its harness", ran)
	}
}

// TestModelExecCallCycle is a program reaching itself: ping calls pong, pong
// calls ping, and the refusal prints who was running so the loop is readable.
func TestModelExecCallCycle(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, callerHarness("ping", 2, "pong"))
	saveHarness(t, store, callerHarness("pong", 2, "ping"))
	ping, err := store.Load("ping", 0)
	if err != nil {
		t.Fatalf("load ping: %v", err)
	}

	server := newModelServer(t)
	_, err = Run(context.Background(), ping, ModelExec(server.client(t), ModelExecOpts{
		Harness: ping,
		Store:   store,
	}))
	if err == nil {
		t.Fatalf("a cycle ran")
	}
	for _, want := range []string{"already running", "ping v1 → pong v1 → ping"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal reads %q, want it to carry %q", err, want)
		}
	}
}

// TestModelExecCallSpendsDyn is the budget: a call is a decision, so each one
// spends a unit of the CALLING harness's cap and the one past it refuses.
func TestModelExecCallSpendsDyn(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, leafHarness("leaf"))
	spender := callerHarness("spender", 1, "leaf", "leaf")

	server := newModelServer(t)
	var ran []string
	_, err := Run(context.Background(), spender, ModelExec(server.client(t), ModelExecOpts{
		Harness: spender,
		Store:   store,
		RunTool: readNotes(&ran),
	}))
	if err == nil {
		t.Fatalf("a second call ran on a cap of one")
	}
	for _, want := range []string{"spender", "dynamism cap of 1 calls"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal reads %q, want it to carry %q", err, want)
		}
	}
	// The FIRST call is paid for and happens; only the second is refused.
	if len(ran) != 1 {
		t.Fatalf("the leaf's tool ran %v, want the one call the cap paid for", ran)
	}
	runs, err := store.Runs("leaf")
	if err != nil || len(runs) != 1 {
		t.Fatalf("leaf's history holds %v (%v), want the one run that was afforded", runs, err)
	}
}

// TestModelExecCallDeclined is the gate one level down: a person said no to work
// the parent asked for, so the parent stops — and the child's own history says
// declined rather than failed, because nothing went wrong.
func TestModelExecCallDeclined(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, Harness{
		Id: Id{Name: "asker", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "sure", Kind: KindHumanGate, Fields: Fields{"ask": "send it?"}},
		}},
		Verify: Verify{Ladder: VerifyAccept},
	})
	outer := callerHarness("outer", 1, "asker")

	server := newModelServer(t)
	_, err := Run(context.Background(), outer, ModelExec(server.client(t), ModelExecOpts{
		Harness: outer,
		Store:   store,
		Ask:     func(context.Context, string) (string, error) { return "no, not yet", nil },
	}))
	// Through a plain Run the parent can only end on an error, which is the caveat
	// this file's header states. What matters is that it STOPPED and said why.
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("the parent ended with %v, want a stop that says declined", err)
	}
	runs, err := store.Runs("asker")
	if err != nil || len(runs) != 1 {
		t.Fatalf("asker's history holds %v (%v), want one trace", runs, err)
	}
	trace, err := store.LoadRun(runs[0])
	if err != nil {
		t.Fatalf("load the child's trace: %v", err)
	}
	if trace.Status != StatusDeclined || trace.Err != "" {
		t.Fatalf("the child's trace says (%q, %q), want declined and no failure", trace.Status, trace.Err)
	}
	if trace.Out != "not yet" {
		t.Fatalf("the child's out is %q, want what the person typed", trace.Out)
	}
}

// TestModelExecClips keeps a megabyte out of every prompt after the one that
// produced it, and keeps both ends of what it clips.
func TestModelExecClips(t *testing.T) {
	long := strings.Repeat("a", modelClip/2) + "MIDDLE" + strings.Repeat("z", modelClip)
	clipped := clipModel(long)
	if len(clipped) > modelClip+8 {
		t.Fatalf("clipped to %d bytes, want about %d", len(clipped), modelClip)
	}
	if !strings.HasPrefix(clipped, "aaaa") || !strings.HasSuffix(clipped, "zzzz") {
		t.Fatalf("the clip lost an end: %.40q…%.40q", clipped, clipped[len(clipped)-40:])
	}
	if strings.Contains(clipped, "MIDDLE") {
		t.Fatalf("a clip that kept the middle did not clip")
	}
	if short := clipModel("nothing to clip"); short != "nothing to clip" {
		t.Fatalf("a short output was changed to %q", short)
	}
}

// TestModelExecNoModel is the one refusal that is not about a harness: a bridge
// built with no client cannot pretend a node ran.
func TestModelExecNoModel(t *testing.T) {
	h := straight()
	exec := ModelExec(nil, ModelExecOpts{Harness: h})
	node := Node{Id: "look", Kind: KindAgentLoop, Fields: Fields{"brief": "look"}}
	if _, err := exec(context.Background(), node); err == nil ||
		!strings.Contains(err.Error(), "no model") {
		t.Fatalf("error %v, want a bridge with no model to say so", err)
	}
}
