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
)

// modelServer is a provider endpoint with a script behind it: each reply is
// chosen by the first phrase that appears in the request, so a test says "when
// you are asked to check something, say PASS" without knowing the prompt's
// wording. Every request it saw is kept, which is how the prompt-building half
// of the bridge is asserted.
type modelServer struct {
	mu    sync.Mutex
	seen  []string
	rules []modelRule
	http  *httptest.Server
}

type modelRule struct {
	when, say string
}

func newModelServer(t *testing.T, rules ...modelRule) *modelServer {
	t.Helper()
	server := &modelServer{rules: rules}
	server.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
		answer := "said nothing in particular"
		for _, rule := range server.rules {
			if strings.Contains(asked, rule.when) {
				answer = rule.say
				break
			}
		}
		server.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		reply, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"index":   0,
				"message": map[string]any{"role": "assistant", "content": answer},
			}},
		})
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
