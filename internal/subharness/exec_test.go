package subharness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// scriptEnv is an Env with no models and no tools in it: every method answers
// from a table keyed on the node's id, so a test states what each node produced
// and the runner's own control flow is the only thing under test.
type scriptEnv struct {
	loops  map[string]string
	tools  map[string]string
	checks map[string]bool
	gates  map[string]GateAnswer
	conds  map[string]bool
	fail   map[string]error
	// ran is every node id the env was asked about, in order.
	ran []string
}

func newEnv() *scriptEnv {
	return &scriptEnv{
		loops: map[string]string{}, tools: map[string]string{}, checks: map[string]bool{},
		gates: map[string]GateAnswer{}, conds: map[string]bool{}, fail: map[string]error{},
	}
}

func (s *scriptEnv) note(id string) error {
	s.ran = append(s.ran, id)
	return s.fail[id]
}

func (s *scriptEnv) Loop(ctx context.Context, node Node, input string) (string, error) {
	if err := s.note(node.Id); err != nil {
		return "", err
	}
	answer, ok := s.loops[node.Id]
	if !ok {
		return "", fmt.Errorf("the script has no answer for loop %q", node.Id)
	}
	return answer, nil
}

func (s *scriptEnv) Tool(ctx context.Context, node Node, input string) (string, error) {
	if err := s.note(node.Id); err != nil {
		return "", err
	}
	answer, ok := s.tools[node.Id]
	if !ok {
		return "", fmt.Errorf("the script has no answer for tool %q", node.Id)
	}
	return answer, nil
}

func (s *scriptEnv) Gate(ctx context.Context, node Node, state State) (GateAnswer, error) {
	if err := s.note(node.Id); err != nil {
		return GateAnswer{}, err
	}
	answer, ok := s.gates[node.Id]
	if !ok {
		return GateAnswer{Approved: true}, nil
	}
	return answer, nil
}

func (s *scriptEnv) Check(ctx context.Context, node Node, state State) (bool, string, error) {
	if err := s.note(node.Id); err != nil {
		return false, "", err
	}
	passed, ok := s.checks[node.Id]
	if !ok {
		return false, "", fmt.Errorf("the script has no answer for check %q", node.Id)
	}
	return passed, "", nil
}

// Cond answers only the conditions the small language could not, which is what
// the runner promises it will ask about (predicate.go). A round key —
// `tries#2` — is how a test says "it holds on the second pass".
func (s *scriptEnv) Cond(ctx context.Context, node Node, condition string, state State) (bool, error) {
	round := 1
	for _, id := range s.ran {
		if id == node.Id {
			round++
		}
	}
	s.ran = append(s.ran, node.Id)
	if held, ok := s.conds[fmt.Sprintf("%s#%d", node.Id, round)]; ok {
		return held, nil
	}
	return s.conds[node.Id], nil
}

// straight is a program with nothing to decide.
func straight() Harness {
	return Harness{
		Id: Id{Name: "straight", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "look", Kind: KindAgentLoop, Fields: Fields{"brief": "look", "tools": "bash"}},
				{Id: "build", Kind: KindToolCall, Fields: Fields{"tool": "bash", "args": "go build ./..."}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"ladder": VerifyLoop, "check": "go test ./..."}},
			},
			Edges: []Edge{{"look", "build"}, {"build", "check"}},
		},
		Whitelist: []string{"bash"},
		Verify:    Verify{Ladder: VerifyLoop},
	}
}

func envRun(t *testing.T, h Harness, env Env, input string) (Trace, error) {
	t.Helper()
	runner := &Runner{Env: env}
	return runner.Run(context.Background(), h, input)
}

func TestARunnerWalksAStraightProgramAndSaysItEndedOK(t *testing.T) {
	env := newEnv()
	env.loops["look"] = "the reconciler test"
	env.tools["build"] = "ok"
	env.checks["check"] = true

	trace, err := envRun(t, straight(), env, "the nightly failed")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Status != StatusOK {
		t.Fatalf("the run ended %q, want ok", trace.Status)
	}
	if got := strings.Join(env.ran, ","); got != "look,build,check" {
		t.Fatalf("the env was asked about %s, want look,build,check", got)
	}
	if len(trace.Trail) != 3 {
		t.Fatalf("the trail has %d steps, want 3", len(trace.Trail))
	}
}

// A program that finishes on a check that did not pass has not done what it
// said it would, and saying it ended ok would make the ladder decorative.
func TestARunThatEndsOnAFailedCheckIsAFailure(t *testing.T) {
	env := newEnv()
	env.loops["look"] = "the reconciler test"
	env.tools["build"] = "ok"
	env.checks["check"] = false

	trace, err := envRun(t, straight(), env, "")
	if err == nil {
		t.Fatal("a run that ended on a failed check reported no failure")
	}
	if trace.Status != StatusFailed {
		t.Fatalf("the run ended %q, want failed", trace.Status)
	}
}

func TestANodeThatErrorsEndsTheRunAsFailed(t *testing.T) {
	env := newEnv()
	env.loops["look"] = "seen"
	env.fail["build"] = errors.New("bash: exit 1")

	trace, err := envRun(t, straight(), env, "")
	if err == nil || !strings.Contains(err.Error(), "exit 1") {
		t.Fatalf("the failure did not come back: %v", err)
	}
	if trace.Status != StatusFailed || trace.Err == "" {
		t.Fatalf("the trace does not record the failure: %+v", trace)
	}
	if last := trace.Trail[len(trace.Trail)-1]; last.Id != "build" || last.Err == "" {
		t.Fatalf("the failing node is not the last step: %+v", last)
	}
}

// THE SMALL LANGUAGE IS ANSWERED HERE and everything else is asked of the
// environment. Both are the same call site, so a harness cannot be answered two
// ways (exec.go's [Runner.cond]).
func TestABranchTakesTheFirstArmOnAConditionThisPackageCanDecide(t *testing.T) {
	env := newEnv()
	env.loops["name-it"] = "it looks flaky to me"
	env.tools["rerun"] = "20 runs, 3 failures"
	env.loops["explain"] = "it is not flaky"
	env.conds["tries"] = true
	env.checks["check"] = true

	trace, err := envRun(t, flake(), env, "the nightly failed")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Status != StatusOK {
		t.Fatalf("the run ended %q, want ok", trace.Status)
	}
	for _, id := range env.ran {
		if id == "explain" {
			t.Fatalf("the else arm ran on a condition that held: %v", env.ran)
		}
	}
	if !strings.Contains(trailOut(trace, "pick"), "→ rerun") {
		t.Fatalf("the trail does not say which arm was taken: %q", trailOut(trace, "pick"))
	}
}

func TestABranchTakesTheElseArmWhenTheConditionDoesNotHold(t *testing.T) {
	env := newEnv()
	env.loops["name-it"] = "it is a real failure"
	env.loops["explain"] = "the assertion is wrong"

	trace, err := envRun(t, flake(), env, "")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Status != StatusOK {
		t.Fatalf("the run ended %q, want ok", trace.Status)
	}
	for _, id := range env.ran {
		if id == "rerun" {
			t.Fatalf("the first arm ran on a condition that did not hold: %v", env.ran)
		}
	}
}

// A `when` the small language cannot read is the environment's to judge, and a
// harness written in sentences is the ordinary case (run_test.go's programs are
// all written that way).
func TestAConditionInSentencesIsHandedToTheEnvironment(t *testing.T) {
	h := flake()
	fieldsOf(t, &h, "pick")["when"] = "the test still fails"
	env := newEnv()
	env.loops["name-it"] = "seen"
	env.loops["explain"] = "not flaky"
	env.conds["pick"] = false

	if _, err := envRun(t, h, env, ""); err != nil {
		t.Fatal(err)
	}
	asked := false
	for _, id := range env.ran {
		if id == "pick" {
			asked = true
		}
	}
	if !asked {
		t.Fatalf("the environment was never asked about the sentence: %v", env.ran)
	}
}

// The loop is bounded by the file and by the budget (run.go); what this asks is
// that the condition is the environment's answer, round by round — a loop whose
// `until` is a sentence, which is the ordinary case.
func TestALoopRunsRoundsUntilTheEnvironmentSaysItHolds(t *testing.T) {
	h := flake()
	fieldsOf(t, &h, "tries")["until"] = "the suite is green"
	env := newEnv()
	env.loops["name-it"] = "it looks flaky to me"
	env.tools["rerun"] = "20 runs, 3 failures"
	env.conds["tries#1"] = false
	env.conds["tries#2"] = true
	env.checks["check"] = true

	trace, err := envRun(t, h, env, "")
	if err != nil {
		t.Fatal(err)
	}
	rounds := 0
	for _, step := range trace.Trail {
		if step.Id == "tries" {
			rounds++
		}
	}
	if rounds != 2 {
		t.Fatalf("the loop ran %d rounds, want 2", rounds)
	}
	if trace.Spent != 1 {
		t.Fatalf("a second round spent %d of the budget, want 1", trace.Spent)
	}
}

// A DECLINE IS NOT A FAULT. The gate did exactly what it is for.
func TestAGateThatIsDeclinedEndsTheRunWithoutAFailure(t *testing.T) {
	env := newEnv()
	env.loops["name-it"] = "it looks flaky to me"
	env.tools["rerun"] = "3 failures"
	env.conds["tries"] = true
	env.checks["check"] = true
	env.gates["land"] = GateAnswer{Note: "not this week"}

	trace, err := envRun(t, flake(), env, "")
	if err != nil {
		t.Fatalf("a decline came back as a failure: %v", err)
	}
	if trace.Status != StatusDeclined {
		t.Fatalf("the run ended %q, want declined", trace.Status)
	}
	if trace.Out != "not this week" {
		t.Fatalf("the run's output is %q, want what the person said", trace.Out)
	}
	if trace.Err != "" {
		t.Fatalf("a decline recorded an error: %q", trace.Err)
	}
}

// THE THIRD ANSWER. The person is taking it from here.
func TestAGateCanBeTakenOver(t *testing.T) {
	env := newEnv()
	env.loops["name-it"] = "it looks flaky to me"
	env.tools["rerun"] = "3 failures"
	env.conds["tries"] = true
	env.checks["check"] = true
	env.gates["land"] = GateAnswer{Intervene: true, Note: "I'll land it by hand"}

	trace, err := envRun(t, flake(), env, "")
	if err != nil {
		t.Fatalf("an escalation came back as a failure: %v", err)
	}
	if trace.Status != StatusIntervened {
		t.Fatalf("the run ended %q, want intervened", trace.Status)
	}
	if trace.Out != "I'll land it by hand" {
		t.Fatalf("the run's output is %q, want the words they typed", trace.Out)
	}
}

// An approval with words attached is a redirect: the sentence becomes what the
// next node reads.
func TestAnApprovalWithWordsRedirectsWhatComesNext(t *testing.T) {
	h := Harness{
		Id: Id{Name: "gated", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "ask-first", Kind: KindHumanGate, Fields: Fields{"ask": "go on?"}},
				{Id: "work", Kind: KindAgentLoop, Fields: Fields{"brief": "do it"}},
			},
			Edges: []Edge{{"ask-first", "work"}},
		},
	}
	env := newEnv()
	env.gates["ask-first"] = GateAnswer{Approved: true, Note: "start with the reconciler"}
	env.loops["work"] = "done"

	seen := ""
	relay := &relayEnv{scriptEnv: env, onLoop: func(input string) { seen = input }}
	if _, err := envRun(t, h, relay, "the nightly failed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "start with the reconciler") {
		t.Fatalf("the redirect never reached the next node: %q", seen)
	}
}

// ── calls ───────────────────────────────────────────────────────────────────

type mapLoader map[string]Harness

func (m mapLoader) Load(name string, version int) (Harness, error) {
	h, ok := m[name]
	if !ok {
		return Harness{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return h, nil
}

func caller(name string) Harness {
	return Harness{
		Id: Id{Name: name, Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "hand-off", Kind: KindSubharnessCall, Fields: Fields{"name": "child"}},
			},
		},
		Dyn: Dyn{Ladder: DynRecursive, Cap: 1},
	}
}

func TestACallRunsTheOtherHarnessAndCarriesItsOutputOn(t *testing.T) {
	child := straight()
	child.Id.Name = "child"
	env := newEnv()
	env.loops["look"] = "seen"
	env.tools["build"] = "ok"
	env.checks["check"] = true

	runner := &Runner{Env: env, Loader: mapLoader{"child": child}}
	trace, err := runner.Run(context.Background(), caller("parent"), "go")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Status != StatusOK {
		t.Fatalf("the parent ended %q, want ok", trace.Status)
	}
	if got := trailOut(trace, "hand-off"); !strings.Contains(got, "child v1 · ok") {
		t.Fatalf("the parent's trail does not name the child run: %q", got)
	}
}

// A CHILD THAT WAS DECLINED STOPS THE PARENT: the person said no to work this
// program asked for.
func TestAChildThatWasDeclinedStopsTheParent(t *testing.T) {
	child := Harness{
		Id:      Id{Name: "child", Version: 1},
		Program: Program{Nodes: []Node{{Id: "ask-first", Kind: KindHumanGate, Fields: Fields{"ask": "go on?"}}}},
	}
	env := newEnv()
	env.gates["ask-first"] = GateAnswer{Note: "no"}

	runner := &Runner{Env: env, Loader: mapLoader{"child": child}}
	trace, err := runner.Run(context.Background(), caller("parent"), "go")
	if err != nil {
		t.Fatalf("a declined child failed the parent: %v", err)
	}
	if trace.Status != StatusDeclined {
		t.Fatalf("the parent ended %q, want declined", trace.Status)
	}
}

func TestACallWithNoLoaderIsAnErrorRatherThanASilentSkip(t *testing.T) {
	env := newEnv()
	if _, err := envRun(t, caller("parent"), env, "go"); err == nil ||
		!strings.Contains(err.Error(), "cannot reach other harnesses") {
		t.Fatalf("a call with no loader gave %v", err)
	}
}

func TestCallsAreBoundedByDepth(t *testing.T) {
	loop := caller("child")
	env := newEnv()
	runner := &Runner{Env: env, Loader: mapLoader{"child": loop}}
	_, err := runner.Run(context.Background(), caller("parent"), "go")
	if err == nil || !strings.Contains(err.Error(), "nested more than") {
		t.Fatalf("unbounded recursion gave %v", err)
	}
}

func TestARunnerWithNoEnvironmentRefusesToStart(t *testing.T) {
	runner := &Runner{}
	if _, err := runner.Run(context.Background(), straight(), ""); err == nil {
		t.Fatal("a runner with no Env ran anyway")
	}
}

// relayEnv watches what an agent.loop node was handed.
type relayEnv struct {
	*scriptEnv
	onLoop func(string)
}

func (r *relayEnv) Loop(ctx context.Context, node Node, input string) (string, error) {
	if r.onLoop != nil {
		r.onLoop(input)
	}
	return r.scriptEnv.Loop(ctx, node, input)
}

// fieldsOf reaches one node's fields to rewrite them, so a test can say which
// condition it is about without restating the whole program.
func fieldsOf(t *testing.T, h *Harness, id string) Fields {
	t.Helper()
	for at := range h.Program.Nodes {
		if h.Program.Nodes[at].Id == id {
			return h.Program.Nodes[at].Fields
		}
	}
	t.Fatalf("no node %q in the program", id)
	return nil
}

func trailOut(t Trace, id string) string {
	for _, step := range t.Trail {
		if step.Id == id {
			return step.Out
		}
	}
	return ""
}
