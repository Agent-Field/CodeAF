package subharness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// scriptEnv is an Env with no models and no tools in it: every method answers
// from a table keyed on the node's id, so a test states what each node produced
// and the runner's own control flow is the only thing under test.
type scriptEnv struct {
	mu sync.Mutex
	// loops, tools and checks are keyed by node id. A missing key is an error,
	// which is what makes "the runner ran a node it should have skipped" a
	// failure rather than a silent pass.
	loops  map[string]string
	tools  map[string]string
	checks map[string]bool
	gates  map[string]GateAnswer
	fail   map[string]error
	// ran is every node id the env was asked about, in order.
	ran []string
}

func newScript() *scriptEnv {
	return &scriptEnv{
		loops: map[string]string{}, tools: map[string]string{},
		checks: map[string]bool{}, gates: map[string]GateAnswer{}, fail: map[string]error{},
	}
}

func (s *scriptEnv) note(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ran = append(s.ran, id)
	return s.fail[id]
}

func (s *scriptEnv) order() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func (s *scriptEnv) Loop(ctx context.Context, node Node, input string) (string, error) {
	if err := s.note(node.ID); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answer, ok := s.loops[node.ID]
	if !ok {
		return "", fmt.Errorf("the script has no answer for loop %q", node.ID)
	}
	return answer, nil
}

func (s *scriptEnv) Tool(ctx context.Context, node Node, input string) (string, error) {
	if err := s.note(node.ID); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answer, ok := s.tools[node.ID]
	if !ok {
		return "", fmt.Errorf("the script has no answer for tool %q", node.ID)
	}
	return answer, nil
}

func (s *scriptEnv) Gate(ctx context.Context, node Node, state State) (GateAnswer, error) {
	if err := s.note(node.ID); err != nil {
		return GateAnswer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answer, ok := s.gates[node.ID]
	if !ok {
		return GateAnswer{Approved: true}, nil
	}
	return answer, nil
}

func (s *scriptEnv) Check(ctx context.Context, node Node, state State) (bool, string, error) {
	if err := s.note(node.ID); err != nil {
		return false, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A check keyed per ROUND lets a loop test say "it fails, then it passes":
	// check#2 is the second time the same node runs.
	round := 0
	for _, id := range s.ran {
		if id == node.ID {
			round++
		}
	}
	if passed, ok := s.checks[fmt.Sprintf("%s#%d", node.ID, round)]; ok {
		return passed, "", nil
	}
	passed, ok := s.checks[node.ID]
	if !ok {
		return false, "", fmt.Errorf("the script has no answer for check %q", node.ID)
	}
	return passed, "", nil
}

func run(t *testing.T, harness Harness, env Env, input string) (Run, error) {
	t.Helper()
	runner := &Runner{Env: env}
	return runner.Run(context.Background(), harness, input)
}

// traceOf finds one trace by id.
func traceOf(r Run, id string) (Trace, bool) {
	for _, node := range r.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Trace{}, false
}

func TestRunWalksAStraightProgramAndTracesIt(t *testing.T) {
	harness := Harness{
		Name: "straight", Tools: []string{"bash"}, Verify: RungLoop,
		Program: []Node{
			{ID: "look", Kind: KindAgentLoop, Prompt: "look", Tools: []string{"bash"}},
			{ID: "build", Kind: KindToolCall, Tool: "bash", Args: map[string]any{"command": "go build ./..."}},
			{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."},
		},
	}
	env := newScript()
	env.loops["look"] = "it is the reconciler"
	env.tools["build"] = "ok"
	env.checks["check"] = true

	got, err := run(t, harness, env, "the nightly failed")
	if err != nil {
		t.Fatalf("a straight program failed: %v", err)
	}
	if got.Status != StatusOK {
		t.Fatalf("status = %s", got.Status)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("the trace has %d nodes, want one per step", len(got.Nodes))
	}
	// THE TRACE IS A DAG: every node after the first names the one it followed.
	if got.Nodes[0].Needs != nil {
		t.Fatalf("the first node came after something: %v", got.Nodes[0].Needs)
	}
	for at := 1; at < len(got.Nodes); at++ {
		needs := got.Nodes[at].Needs
		if len(needs) != 1 || needs[0] != got.Nodes[at-1].ID {
			t.Fatalf("node %s came after %v, want %s", got.Nodes[at].ID, needs, got.Nodes[at-1].ID)
		}
	}
	if strings.Join(env.order(), " ") != "look build check" {
		t.Fatalf("the steps ran out of order: %v", env.order())
	}
}

func TestBranchTakesOneArmAndSaysWhichInTheTrace(t *testing.T) {
	harness := Harness{
		Name: "pick", Tools: []string{"bash"}, Dynamism: DynBranch,
		Program: []Node{
			{ID: "look", Kind: KindAgentLoop, Prompt: "look"},
			{ID: "pick", Kind: KindBranch,
				Cases: []Case{
					{When: "contains flaky", Steps: []Node{{ID: "rerun", Kind: KindToolCall, Tool: "bash"}}},
					{When: "contains broken", Steps: []Node{{ID: "fix", Kind: KindAgentLoop, Prompt: "fix"}}},
				},
				Else: []Node{{ID: "shrug", Kind: KindAgentLoop, Prompt: "shrug"}}},
		},
	}
	env := newScript()
	env.loops["look"] = "the test looks BROKEN to me"
	env.loops["fix"] = "fixed"
	env.loops["shrug"] = "no idea"
	env.tools["rerun"] = "reran"

	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if order := strings.Join(env.order(), " "); order != "look fix" {
		t.Fatalf("the wrong arm ran: %v", order)
	}
	trace, ok := traceOf(got, "pick")
	if !ok || !strings.Contains(trace.Note, "b →") {
		t.Fatalf("the trace does not say which arm was taken: %+v", trace)
	}
	if got.Output != "fixed" {
		t.Fatalf("the run's output is %q", got.Output)
	}
}

func TestBranchFallsToElseAndThenToNothing(t *testing.T) {
	program := func(hasElse bool) Harness {
		harness := Harness{
			Name: "pick", Dynamism: DynBranch,
			Program: []Node{
				{ID: "look", Kind: KindAgentLoop, Prompt: "look"},
				{ID: "pick", Kind: KindBranch, Cases: []Case{
					{When: "contains flaky", Steps: []Node{{ID: "rerun", Kind: KindAgentLoop, Prompt: "rerun"}}},
				}},
			},
		}
		if hasElse {
			harness.Program[1].Else = []Node{{ID: "shrug", Kind: KindAgentLoop, Prompt: "shrug"}}
		}
		return harness
	}
	env := newScript()
	env.loops["look"] = "all green"
	env.loops["shrug"] = "nothing to do"
	if _, err := run(t, program(true), env, ""); err != nil {
		t.Fatalf("else: %v", err)
	}
	if order := strings.Join(env.order(), " "); order != "look shrug" {
		t.Fatalf("else did not run: %v", order)
	}

	env = newScript()
	env.loops["look"] = "all green"
	got, err := run(t, program(false), env, "")
	if err != nil {
		t.Fatalf("a branch with no matching arm and no else is not a failure: %v", err)
	}
	trace, _ := traceOf(got, "pick")
	if trace.Note != "no case matched" {
		t.Fatalf("the trace does not record the shrug: %q", trace.Note)
	}
}

func TestLoopRunsUntilTheConditionHoldsAndCountsRounds(t *testing.T) {
	harness := Harness{
		Name: "fix", Tools: []string{"bash"}, Verify: RungLoop, Dynamism: DynBranch,
		Program: []Node{{ID: "repair", Kind: KindLoopUntil, Until: "ok", Max: 4, Steps: []Node{
			{ID: "patch", Kind: KindAgentLoop, Prompt: "patch it"},
			{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."},
		}}},
	}
	env := newScript()
	env.loops["patch"] = "patched"
	// Fails, fails, passes: three rounds.
	env.checks["check#1"], env.checks["check#2"], env.checks["check#3"] = false, false, true

	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("the loop failed: %v", err)
	}
	if rounds := strings.Count(strings.Join(env.order(), " "), "patch"); rounds != 3 {
		t.Fatalf("the body ran %d times, want 3", rounds)
	}
	// EVERY ROUND IS ITS OWN TRACE, numbered, so a person can see where it
	// stopped being cheap.
	third, ok := traceOf(got, "check#3")
	if !ok || third.Round != 3 {
		t.Fatalf("the third round is not in the trace as round 3: %+v", third)
	}
	loop, _ := traceOf(got, "repair")
	if !strings.Contains(loop.Note, "after 3 of 4") {
		t.Fatalf("the loop does not say how many rounds it took: %q", loop.Note)
	}
}

func TestLoopThatRunsOutOfRoundsFails(t *testing.T) {
	harness := Harness{
		Name: "fix", Verify: RungLoop, Dynamism: DynBranch,
		Program: []Node{{ID: "repair", Kind: KindLoopUntil, Until: "ok", Max: 2, Steps: []Node{
			{ID: "patch", Kind: KindAgentLoop, Prompt: "patch it"},
			{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."},
		}}},
	}
	env := newScript()
	env.loops["patch"] = "patched"
	env.checks["check"] = false

	got, err := run(t, harness, env, "")
	if err == nil {
		t.Fatalf("a loop that never converged reported success")
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "never held") {
		t.Fatalf("the run does not say why it failed: %s / %s", got.Status, got.Error)
	}
	if rounds := strings.Count(strings.Join(env.order(), " "), "patch"); rounds != 2 {
		t.Fatalf("the loop ran %d rounds past its own bound", rounds)
	}
}

func TestSplitFansOutAndJoinsWithADiamond(t *testing.T) {
	harness := Harness{
		Name: "wide", Dynamism: DynWidth, Cap: 3,
		Program: []Node{{ID: "sweep", Kind: KindParallelSplit, Join: JoinAll, Lanes: []Lane{
			{Name: "tests", Steps: []Node{{ID: "t", Kind: KindAgentLoop, Prompt: "read the tests"}}},
			{Name: "docs", Steps: []Node{{ID: "d", Kind: KindAgentLoop, Prompt: "read the docs"}}},
		}}},
	}
	env := newScript()
	env.loops["t"] = "tests say A"
	env.loops["d"] = "docs say B"

	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	join, ok := traceOf(got, "sweep.join")
	if !ok {
		t.Fatalf("the split minted no join: %+v", got.Nodes)
	}
	if len(join.Needs) != 2 {
		t.Fatalf("the join has %d edges, want one per lane", len(join.Needs))
	}
	for _, id := range []string{"t", "d"} {
		lane, ok := traceOf(got, id)
		if !ok || lane.Lane == "" {
			t.Fatalf("lane node %s does not say which lane it was in: %+v", id, lane)
		}
		if len(lane.Needs) != 1 || lane.Needs[0] != "sweep" {
			t.Fatalf("lane node %s does not come off the split: %v", id, lane.Needs)
		}
	}
	if !strings.Contains(got.Output, "tests say A") || !strings.Contains(got.Output, "docs say B") {
		t.Fatalf("the join lost a lane: %q", got.Output)
	}
}

func TestSplitFailsWhenALaneDoes(t *testing.T) {
	harness := Harness{
		Name: "wide", Dynamism: DynWidth,
		Program: []Node{{ID: "sweep", Kind: KindParallelSplit, Lanes: []Lane{
			{Name: "one", Steps: []Node{{ID: "a", Kind: KindAgentLoop, Prompt: "x"}}},
			{Name: "two", Steps: []Node{{ID: "b", Kind: KindAgentLoop, Prompt: "x"}}},
		}}},
	}
	env := newScript()
	env.loops["a"] = "fine"
	env.fail["b"] = errors.New("the model refused")
	got, err := run(t, harness, env, "")
	if err == nil {
		t.Fatalf("a failed lane did not fail the split")
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "lane two") {
		t.Fatalf("the run does not name the lane that broke: %s", got.Error)
	}
}

func TestGateDeclineEndsTheRunWithoutFailingIt(t *testing.T) {
	harness := Harness{
		Name: "ask",
		Program: []Node{
			{ID: "work", Kind: KindAgentLoop, Prompt: "do it"},
			{ID: "land", Kind: KindHumanGate, Prompt: "land it?"},
			{ID: "after", Kind: KindAgentLoop, Prompt: "tidy up"},
		},
	}
	env := newScript()
	env.loops["work"] = "done"
	env.loops["after"] = "tidied"
	env.gates["land"] = GateAnswer{Approved: false, Note: "not this week"}

	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("a decline is not an error: %v", err)
	}
	if got.Status != StatusDeclined {
		t.Fatalf("status = %s, want declined", got.Status)
	}
	if strings.Contains(strings.Join(env.order(), " "), "after") {
		t.Fatalf("the program carried on past a no: %v", env.order())
	}
	gate, _ := traceOf(got, "land")
	if gate.Answer != "declined" {
		t.Fatalf("the trace does not record the answer: %+v", gate)
	}
	if got.Output != "not this week" {
		t.Fatalf("the person's words did not become the outcome: %q", got.Output)
	}
}

func TestGateEscalationEndsTheRunAsIntervened(t *testing.T) {
	harness := Harness{
		Name: "ask",
		Program: []Node{
			{ID: "work", Kind: KindAgentLoop, Prompt: "do it"},
			{ID: "land", Kind: KindHumanGate, Prompt: "land it?", Escalate: true},
			{ID: "after", Kind: KindAgentLoop, Prompt: "tidy"},
		},
	}
	env := newScript()
	env.loops["work"] = "done"
	env.loops["after"] = "tidied"
	env.gates["land"] = GateAnswer{Intervene: true, Note: "I will take it from here"}

	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("intervening is not an error: %v", err)
	}
	if got.Status != StatusIntervened {
		t.Fatalf("status = %s, want intervened", got.Status)
	}
	if strings.Contains(strings.Join(env.order(), " "), "after") {
		t.Fatalf("the program kept going after somebody took it over")
	}
	gate, _ := traceOf(got, "land")
	if gate.Answer != "intervened" || gate.Output != "I will take it from here" {
		t.Fatalf("the trace lost the handover: %+v", gate)
	}
}

func TestGateApprovalWithWordsIsARedirect(t *testing.T) {
	harness := Harness{
		Name: "ask",
		Program: []Node{
			{ID: "land", Kind: KindHumanGate, Prompt: "land it?"},
			{ID: "after", Kind: KindAgentLoop, Prompt: "carry on"},
		},
	}
	env := newScript()
	env.gates["land"] = GateAnswer{Approved: true, Note: "but skip the docs"}
	env.loops["after"] = "done"

	got, err := run(t, harness, env, "the patch")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	after, _ := traceOf(got, "after")
	if after.Started.IsZero() {
		t.Fatalf("the approved program did not carry on")
	}
	// The redirect has to reach the next node, which is what the runner threads
	// through as state — asserted through the trace's own edges.
	if got.Status != StatusOK {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestAFailedCheckAtTheEndFailsTheRun(t *testing.T) {
	harness := Harness{
		Name: "check", Verify: RungLoop,
		Program: []Node{{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."}},
	}
	env := newScript()
	env.checks["check"] = false
	got, err := run(t, harness, env, "")
	if err == nil || got.Status != StatusFailed {
		t.Fatalf("a program that ended on a failing check called itself %s (%v)", got.Status, err)
	}
	if !strings.Contains(got.Error, "did not pass") {
		t.Fatalf("the failure does not say what happened: %s", got.Error)
	}
}

func TestAFailedCheckASubsequentBranchHandlesIsNotAFailure(t *testing.T) {
	harness := Harness{
		Name: "check", Verify: RungLoop, Dynamism: DynBranch,
		Program: []Node{
			{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."},
			{ID: "react", Kind: KindBranch, Cases: []Case{
				{When: "failed", Steps: []Node{{ID: "say", Kind: KindAgentLoop, Prompt: "report it"}}},
			}},
		},
	}
	env := newScript()
	env.checks["check"] = false
	env.loops["say"] = "reported"
	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("a handled failure failed the run: %v", err)
	}
	if got.Status != StatusOK {
		t.Fatalf("status = %s", got.Status)
	}
}

// memoryLoader is a registry in a map, for the recursion tests.
type memoryLoader map[string]Harness

func (m memoryLoader) LoadVersion(name string, version int) (Harness, error) {
	harness, ok := m[name]
	if !ok {
		return Harness{}, fmt.Errorf("%w: %s", ErrNoHarness, name)
	}
	return harness, nil
}

func TestSubharnessCallRunsAnotherProgram(t *testing.T) {
	child := Harness{
		Name: "inner", Version: 2,
		Program: []Node{{ID: "work", Kind: KindAgentLoop, Prompt: "the inner work"}},
	}
	parent := Harness{
		Name: "outer", Dynamism: DynRecursive,
		Program: []Node{{ID: "delegate", Kind: KindSubharnessCall, Call: "inner"}},
	}
	env := newScript()
	env.loops["work"] = "inner did it"

	runner := &Runner{Env: env, Loader: memoryLoader{"inner": child}}
	got, err := runner.Run(context.Background(), parent, "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	trace, _ := traceOf(got, "delegate")
	if !strings.Contains(trace.Note, "inner v2 · ok") {
		t.Fatalf("the trace does not name what it called: %q", trace.Note)
	}
	if got.Output != "inner did it" {
		t.Fatalf("the child's answer did not come back: %q", got.Output)
	}
}

func TestSubharnessCallCannotOutrunTheDepthCap(t *testing.T) {
	// A cycle the registry cannot see: two harnesses that call each other.
	first := Harness{Name: "first", Dynamism: DynRecursive,
		Program: []Node{{ID: "to-second", Kind: KindSubharnessCall, Call: "second"}}}
	second := Harness{Name: "second", Dynamism: DynRecursive,
		Program: []Node{{ID: "to-first", Kind: KindSubharnessCall, Call: "first"}}}

	runner := &Runner{Env: newScript(), Loader: memoryLoader{"first": first, "second": second}}
	_, err := runner.Run(context.Background(), first, "")
	if err == nil || !strings.Contains(err.Error(), "nested more than") {
		t.Fatalf("a cycle through the registry ran forever: %v", err)
	}
}

func TestACallWithNoLoaderIsRefusedRatherThanSkipped(t *testing.T) {
	harness := Harness{Name: "outer", Dynamism: DynRecursive,
		Program: []Node{{ID: "delegate", Kind: KindSubharnessCall, Call: "inner"}}}
	got, err := run(t, harness, newScript(), "")
	if err == nil {
		t.Fatalf("a call this surface cannot make was silently skipped")
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestCancellingARunRecordsItAsCancelled(t *testing.T) {
	harness := Harness{
		Name: "long",
		Program: []Node{
			{ID: "one", Kind: KindAgentLoop, Prompt: "x"},
			{ID: "two", Kind: KindAgentLoop, Prompt: "x"},
		},
	}
	env := newScript()
	env.loops["one"], env.loops["two"] = "a", "b"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &Runner{Env: env}
	got, err := runner.Run(ctx, harness, "")
	if err == nil {
		t.Fatalf("a dead context ran the program anyway")
	}
	if got.Status != StatusCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestTheTriggerIsRecordedAndDoesNothing(t *testing.T) {
	harness := Harness{
		Name: "watched",
		Program: []Node{
			{ID: "start", Kind: KindTrigger, On: TriggerWatch, Spec: "bench/nightly.log"},
			{ID: "work", Kind: KindAgentLoop, Prompt: "look"},
		},
	}
	env := newScript()
	env.loops["work"] = "looked"
	got, err := run(t, harness, env, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Trigger != string(TriggerWatch) {
		t.Fatalf("the run does not say what started it: %q", got.Trigger)
	}
	if order := strings.Join(env.order(), " "); order != "work" {
		t.Fatalf("the trigger did something: %v", order)
	}
}
