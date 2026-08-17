package subharness

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE MODEL BRIDGE: the one [Env] that is neither a test script nor a session.
//
// exec.go says what every kind MEANS and leaves the doing to an Env. A session
// implements that Env over the belt it already has — its loop, its tools, its
// consent lane — and that is the implementation a person in a conversation
// wants. This file is the other one: an Env with nothing behind it but a
// provider client and two closures, so that a harness can be run by anything
// that has a model, with no session in the process at all.
//
// It is deliberately the SMALL implementation, and the smallness is the design:
//
//   - AN agent.loop IS ONE COMPLETION, not a loop. A real loop needs tool
//     schemas, a belt, and a transcript, and the moment this file grew one
//     there would be two session loops in the binary — the thing the whole
//     sub-harness design exists to avoid (registry.go: the library-in-binary).
//     So the node's brief and what its predecessors produced go up as one
//     request, and `max_turns` is stated to the worker as the effort it was
//     budgeted rather than spent as rounds. A harness that needs the real loop
//     is run by the session, which has one.
//   - EVERY JUDGEMENT IS A SEPARATE, TINY CALL. A verify asks one question and
//     wants one word back; a free-text condition asks one question and wants
//     yes or no. Neither is given the run's whole history to reason over — they
//     are given the trail, clipped, which is the evidence a person reading the
//     saved run would have.
//   - NOTHING HERE DECIDES SHAPE. The branch law, the loop's rounds, the gate's
//     three answers and the dynamism budget are all exec.go's and run.go's,
//     unchanged. This file answers questions; it never picks a successor, which
//     is why a program run through this bridge leaves the same trace it would
//     leave anywhere else.
//   - A CALL IS A WHOLE RUN, on a bridge derived from this one. A harness that
//     names another does not get it inlined: the child is walked by [Run] with
//     its own bridge, its own whitelist and its own trace, and what comes back is
//     a summary. That is the same smallness — there is one way to run a harness,
//     and nesting reuses it rather than growing a second one (see [modelEnv.call]
//     for the three refusals that keep the recursion finite).
//
// ── WHAT A GATE MEANS WITH NOBODY THERE ──
//
// [ModelExecOpts.Ask] nil is a surface with no one to ask, and a human.gate
// under it AUTO-APPROVES and says so in the trail. That is the same bargain
// consent.go keeps for a headless run: the honest answers are "refuse" and "I
// said in advance this may run", and a caller wiring this bridge into an
// unattended runner has already said the second by handing over no Ask at all.
// A caller that has somebody to ask hands one over, and then the gate's three
// answers are real — with the caveat that a DECLINE ends a plain [Run] as a
// failure, because Run has only an error to end on. A caller that wants the
// declined and intervened statuses spelled out drives the same environment
// through a [Runner] instead.

// modelClip bounds one earlier output where it is quoted into a prompt. It is
// generous — a step's output is the evidence the next step works from — and it
// exists so that a harness whose second node produced a megabyte does not send
// that megabyte to every node after it.
const modelClip = 8000

// callClip bounds what a child's own answer contributes to the row its caller
// leaves. It is far tighter than modelClip because the parent's trail is a
// SUMMARY of the call — the child's whole run is saved in the child's own
// history, and a caller's row that carried it twice would make a two-deep run
// read as the same evidence pasted twice.
const callClip = 1200

// modelMaxTurns is what an agent.loop that declares no max_turns is told it has
// when the caller named no default either.
const modelMaxTurns = 8

// autoGateNote is what the trail says about a gate nobody was there to answer.
const autoGateNote = "nobody was there to ask, so it carried on"

// ModelExecOpts is everything the bridge cannot do with a model alone.
type ModelExecOpts struct {
	// Harness is the program being run, and it is REQUIRED. An [Exec] is handed
	// one Node at a time and can see nothing around it, so the shape a node sits
	// in — which harness's whitelist bounds it, which nodes lead into it — has
	// to be closed over here.
	Harness Harness

	// RunTool calls one whitelisted tool with the node's fixed arguments. Nil
	// means this surface has no tools, and a tool.call under it FAILS rather
	// than returning nothing: a program that asked to run the suite and was
	// quietly told "" would go on to verify that "" looks fine.
	RunTool func(ctx context.Context, tool, args string) (string, error)

	// Ask puts a human.gate's question to a person and returns what they typed.
	// Nil is nobody there — see the note at the top of this file.
	Ask func(ctx context.Context, question string) (string, error)

	// MaxTurnsDefault is what an agent.loop that declares no max_turns is told
	// it was budgeted. Zero takes this file's own default.
	MaxTurnsDefault int

	// Store, when set, is where a subharness.call resolves the harness it names
	// and where that child's own trace is saved. Nil means this bridge cannot
	// make calls, and a program that tries says so.
	Store *Store

	// Model is what THIS RUN was asked to think with: the default model for
	// every agent.loop node that did not pin one of its own. Empty is the
	// ordinary run, on the client's model. exec_model_model.go holds the rule
	// and says why a node's own `model` still wins.
	Model string

	// DepthCap bounds subharness.call nesting: how many harnesses deep a call
	// may go below the one this bridge was built for. Zero takes
	// [MaxCallDepth]. It is a caller's knob rather than only the constant
	// because the surface driving the bridge is what knows how much recursion
	// it is willing to pay for — a chat turn and an unattended runner are not
	// the same appetite.
	DepthCap int

	// depth, chain and input are what a derived bridge is handed by the call
	// that derived it, and they are unexported because a caller has no top-level
	// answer for them: the top of a run is depth 0, its own name is the whole
	// chain, and Run opens on no sentence.
	depth int
	chain []Id
	input string
}

// ModelExec turns a provider client into the executor [Run] wants.
//
// The returned Exec is SINGLE-USE per run: it accumulates the outputs the run
// has produced, which is what a later node's brief and a verify's evidence are
// built from. Running two harnesses through one Exec would thread the first
// one's outputs into the second, so a second run takes a second Exec.
func ModelExec(c *provider.Client, opts ModelExecOpts) Exec {
	env := newModelEnv(c, opts)
	runner := &Runner{Env: env}
	// The state the walk threads from node to node. At the top it starts empty,
	// because [Run] takes no opening sentence — the entry node works from its own
	// brief. A DERIVED bridge starts on what its caller had produced, which for a
	// called harness is the only road the parent's work can travel: the child's
	// entry node has no predecessors, so leadingIn falls back to exactly this.
	state := State{Last: opts.input, OK: true}
	return func(ctx context.Context, node Node) (Result, error) {
		if node.Kind == KindSubharnessCall {
			// The call is the bridge's own, not the [Runner]'s: the child runs on
			// a bridge derived from this one, which is what carries the depth, the
			// ancestor chain and the store down to it.
			return env.call(ctx, node, &state)
		}
		result, err := runner.step(ctx, env.harness, &state, withModel(node, opts.Model))
		if err == nil && node.Kind == KindHumanGate && opts.Ask == nil {
			result.Out += " · " + autoGateNote
		}
		return result, err
	}
}

// modelEnv is the [Env] itself: a client, the two closures, and what the run
// has produced so far.
type modelEnv struct {
	client   *provider.Client
	runTool  func(ctx context.Context, tool, args string) (string, error)
	ask      func(ctx context.Context, question string) (string, error)
	maxTurns int
	// harness is the ONE program this bridge runs. A called harness gets its own
	// bridge, which is what makes the child's whitelist — not its caller's — the
	// list its nodes are held to: a triage harness that reads files is callable
	// by one that may only run the suite.
	harness Harness
	// store is where a call resolves a name and where the child's trace lands.
	store *Store
	// depth is how many calls deep this bridge already is; the top is 0.
	depth    int
	depthCap int
	// chain is who is running, outermost first, INCLUDING this harness. It is
	// what a cycle is read off: a call naming something already in it would be a
	// program running inside itself.
	chain []Id
	state *modelState
}

func newModelEnv(c *provider.Client, opts ModelExecOpts) *modelEnv {
	turns := opts.MaxTurnsDefault
	if turns < 1 {
		turns = modelMaxTurns
	}
	deep := opts.DepthCap
	if deep < 1 {
		deep = MaxCallDepth
	}
	h := opts.Harness.Normalize()
	chain := opts.chain
	if len(chain) == 0 {
		chain = []Id{h.Id}
	}
	return &modelEnv{
		client:   c,
		runTool:  opts.RunTool,
		ask:      opts.Ask,
		maxTurns: turns,
		harness:  h,
		store:    opts.Store,
		depth:    opts.depth,
		depthCap: deep,
		chain:    chain,
		state:    &modelState{},
	}
}

// modelState is the run's own memory: what each node produced, in order, and
// what it has spent on calling other harnesses.
//
// It is a pointer shared by the Env and the Exec closure because an Exec sees
// one node at a time and a brief is built from what came BEFORE — the thing a
// walk that hands over nothing but a Node cannot supply on its own.
type modelState struct {
	// done is every step that produced something, oldest first. A loop that ran
	// three rounds appends three entries, exactly as the trail does, so the
	// evidence a verify reads is the evidence a person reading the saved run
	// would have.
	done []modelStep
	// calls is how many subharness.call nodes this run has actually made, held
	// against the harness's Dyn.Cap. It is THIS run's ledger and not the child's:
	// a called harness spends its own cap on its own calls, which is what keeps a
	// cap a statement about one page rather than about a whole tree.
	calls int
}

type modelStep struct {
	id, kind, out string
}

func (m *modelState) record(node Node, out string) {
	if out = strings.TrimSpace(out); out == "" {
		return
	}
	m.done = append(m.done, modelStep{id: node.Id, kind: node.Kind, out: out})
}

// out is the LAST thing a node produced, which for a looped node is its most
// recent round rather than its first.
func (m *modelState) out(id string) string {
	for at := len(m.done) - 1; at >= 0; at-- {
		if m.done[at].id == id {
			return m.done[at].out
		}
	}
	return ""
}

// ── the kinds ───────────────────────────────────────────────────────────────

// Loop is one completion against the node's brief, oriented by what led into
// it. See the file header for why it is one call and not a loop.
func (m *modelEnv) Loop(ctx context.Context, node Node, input string) (string, error) {
	brief := node.Fields.Get("brief")
	if brief == "" {
		return "", fmt.Errorf("node %q has no brief to work from", node.Id)
	}
	tools, err := m.allowed(node, "tools")
	if err != nil {
		return "", err
	}
	said, err := m.say(ctx, node.Fields.Get("model"), loopSystem, m.loopPrompt(node, brief, tools, input))
	if err != nil {
		return "", err
	}
	if said == "" {
		return "", fmt.Errorf("node %q came back with nothing", node.Id)
	}
	m.state.record(node, said)
	return said, nil
}

// Tool is the whitelisted call, made verbatim. The arguments are the node's own
// — a tool.call is the FIXED half of the library (kinds.go), and a bridge that
// let a model rewrite them would have turned it into an agent.loop.
func (m *modelEnv) Tool(ctx context.Context, node Node, input string) (string, error) {
	tool := node.Fields.Get("tool")
	if tool == "" {
		return "", fmt.Errorf("node %q names no tool", node.Id)
	}
	if !m.harness.Allows(tool) {
		return "", fmt.Errorf("node %q calls %q, which is not on the whitelist", node.Id, tool)
	}
	if m.runTool == nil {
		return "", fmt.Errorf("node %q calls %q and this surface has no tools", node.Id, tool)
	}
	out, err := m.runTool(ctx, tool, node.Fields.Get("args"))
	if err != nil {
		return "", err
	}
	m.state.record(node, out)
	return out, nil
}

// Gate puts the question to whoever is there, and reads their answer with the
// three meanings exec.go's [GateAnswer] describes.
func (m *modelEnv) Gate(ctx context.Context, node Node, state State) (GateAnswer, error) {
	question := node.Fields.Get("ask")
	if question == "" {
		return GateAnswer{}, fmt.Errorf("node %q is a gate with no question", node.Id)
	}
	if m.ask == nil {
		return GateAnswer{Approved: true}, nil
	}
	said, err := m.ask(ctx, question)
	if err != nil {
		return GateAnswer{}, err
	}
	answer := readGate(said)
	m.state.record(node, answer.Word()+detail(answer.Note))
	return answer, nil
}

// Check is the verify rung, asked of the model against the trail.
//
// The RUNG IS IN THE PROMPT, and it is the whole of what makes an `accept` node
// cheap and an `adversarial` one expensive here: there is no separate machinery
// per rung, there is one question asked at the standard the harness declared.
// What comes back is a verdict word and a sentence, and the sentence becomes
// the state the next condition reads (exec.go).
func (m *modelEnv) Check(ctx context.Context, node Node, state State) (bool, string, error) {
	ladder := node.Fields.Get("ladder")
	if ladder == "" {
		ladder = m.harness.Verify.Ladder
	}
	if ladder == "" {
		ladder = VerifyAccept
	}
	claim := node.Fields.Get("check")
	if claim == "" {
		claim = "the work this run was asked for has actually been done"
	}
	said, err := m.say(ctx, "", verifySystem, m.checkPrompt(node, ladder, claim, state))
	if err != nil {
		return false, "", err
	}
	passed, because := readVerdict(said)
	m.state.record(node, verdictWord(passed)+detail(because))
	return passed, because, nil
}

// Cond answers the conditions the small language refused, and ONLY those —
// [Runner.cond] decides which those are, and everything predicate.go can settle
// never reaches a model at all.
func (m *modelEnv) Cond(ctx context.Context, node Node, condition string, state State) (bool, error) {
	said, err := m.say(ctx, "", condSystem, m.condPrompt(node, condition, state))
	if err != nil {
		return false, err
	}
	return readYes(said), nil
}

// ── calling another harness ─────────────────────────────────────────────────

// call runs the harness a subharness.call names, here, on a bridge derived from
// this one.
//
// THE CHILD IS A RUN, NOT A SUBROUTINE. It gets its own bridge — so its own
// whitelist bounds its nodes and its own outputs are the evidence its checks
// read — its own walk, and its own trace in its own history, which is where
// anybody asking "how does triage behave" will look. What comes back into the
// parent's trail is the pointer, the status and what the child finally said:
// enough to read the parent on its own, with the child's file for the detail.
//
// Three refusals stand between a program and infinite recursion, and they are
// three different mistakes:
//
//   - DEPTH is a fan of harnesses that each thought they were the top one. It is
//     bounded by [ModelExecOpts.DepthCap], counted in bridges rather than nodes.
//   - A CYCLE is a program reaching itself, however long the way round. The
//     ancestor chain is carried down and printed back, because "a calls b calls
//     a" is the one message that makes the mistake obvious.
//   - THE BUDGET is the harness's own Dyn.Cap. A call is a decision to run work
//     the page did not spell out, which is exactly what the dynamism ladder
//     charges for, so each one spends a unit of the CALLING harness's cap.
func (m *modelEnv) call(ctx context.Context, node Node, state *State) (Result, error) {
	name := node.Fields.Get("name")
	if name == "" {
		return Result{}, fmt.Errorf("node %q is a call that names no harness", node.Id)
	}
	if m.store == nil {
		return Result{}, fmt.Errorf("node %q calls %q, and subharness.call is not in the exec bridge without a store",
			node.Id, name)
	}
	if m.depth+1 > m.depthCap {
		return Result{}, fmt.Errorf("node %q calls %q, which would run %d harnesses deep, past the depth cap of %d: %s",
			node.Id, name, m.depth+1, m.depthCap, chainWords(m.chain, name))
	}
	// The chain is read by NAME and not by pointer: a harness calling another
	// version of something already running is still a program inside itself, and
	// the version it pinned is not a reason to let the recursion stand.
	if inChain(m.chain, name) {
		return Result{}, fmt.Errorf("node %q calls %q, which is already running: %s",
			node.Id, name, chainWords(m.chain, name))
	}
	child, err := m.store.Load(name, node.Fields.Int("version", 0))
	if err != nil {
		// Named and missing is the ordinary mistake — a page renamed, a harness
		// never saved on this machine — so the error says which node asked for
		// what before it says what the registry answered.
		return Result{}, fmt.Errorf("node %q calls %q, which this registry does not have: %w", node.Id, name, err)
	}
	// Spent AFTER the load, so a call that never happened costs nothing. The
	// ledger is this bridge's own: run.go spends the same cap on the loop rounds a
	// program did not already contain, and the two are counted apart because one
	// is the walk's decision and the other is this file's.
	if budget := m.harness.Dyn.Cap; budget > 0 {
		if m.state.calls >= budget {
			return Result{}, fmt.Errorf("node %q calls %q, and %s has spent its dynamism cap of %d calls",
				node.Id, name, m.harness.Id.Name, budget)
		}
		m.state.calls++
	}

	trace, childErr := Run(ctx, child, ModelExec(m.client, ModelExecOpts{
		Harness:         child,
		RunTool:         m.runTool,
		Ask:             m.ask,
		MaxTurnsDefault: m.maxTurns,
		Store:           m.store,
		DepthCap:        m.depthCap,
		depth:           m.depth + 1,
		chain:           append(append([]Id{}, m.chain...), child.Id),
		input:           state.Last,
	}))
	// A person who answered the child's gate is not the child failing, and the
	// history it leaves must not say it was — the same reading [Runner.Run] gives
	// its own trace, applied to a walk that only had an error to end on.
	var ended *stop
	if errors.As(childErr, &ended) {
		trace.Status, trace.Err, childErr = ended.status, "", nil
		if said := strings.TrimSpace(ended.note); said != "" {
			trace.Out = said
		}
	}

	head := fmt.Sprintf("%s v%d · %s · %d steps", child.Id.Name, child.Id.Version, trace.status(), len(trace.Trail))
	if trace.Id.Name != "" {
		if path, err := m.store.SaveRun(trace); err == nil {
			head += " · " + path
		}
	}
	if childErr != nil {
		state.OK = false
		return Result{}, fmt.Errorf("%s: %w", head, childErr)
	}
	said := traceOut(trace)
	out := head
	if said != "" {
		out += "\n" + clip(said, callClip)
	}
	*state = State{Last: said, OK: true}
	// A CHILD THAT WAS DECLINED STOPS THE PARENT. The person said no to work this
	// program asked for; carrying on as if they had not is the one reading of that
	// answer nobody meant.
	if ended != nil {
		return Result{Out: out}, &stop{status: ended.status, note: said}
	}
	return Result{Out: out}, nil
}

// traceOut is what a child finally said: the run's own output when it carried
// one, and otherwise the last step that produced anything — which is what a
// plain [Run] leaves, since filling Out is [Runner.Run]'s job.
func traceOut(t Trace) string {
	if out := strings.TrimSpace(t.Out); out != "" {
		return out
	}
	for at := len(t.Trail) - 1; at >= 0; at-- {
		if out := strings.TrimSpace(t.Trail[at].Out); out != "" {
			return out
		}
	}
	return ""
}

// inChain reports whether a name is already running above this call.
func inChain(chain []Id, name string) bool {
	for _, id := range chain {
		if id.Name == name {
			return true
		}
	}
	return false
}

// chainWords writes who is running as one line, with the name that would not be
// allowed on the end: `outer v1 → triage v1 → outer`.
func chainWords(chain []Id, name string) string {
	parts := make([]string, 0, len(chain)+1)
	for _, id := range chain {
		parts = append(parts, fmt.Sprintf("%s v%d", id.Name, id.Version))
	}
	return strings.Join(append(parts, name), " → ")
}

// ── the prompts ─────────────────────────────────────────────────────────────

const loopSystem = "You are one step of a saved procedure. Do the step you are given, using what " +
	"the steps before you produced, and answer with the RESULT — the finding, the draft, the " +
	"decision — and nothing about being an assistant. Steps after you read your answer and " +
	"nothing else, so anything they will need has to be in it."

const verifySystem = "You are a check inside a saved procedure. Answer with PASS or FAIL on the " +
	"first line and one sentence saying why on the second. Judge the claim against the evidence " +
	"you are shown and against nothing else: evidence that does not settle the claim is a FAIL, " +
	"because the point of a check is to say when a run has not shown its work."

const condSystem = "You are answering one question about a procedure that is running. Answer YES " +
	"or NO on the first line and nothing else. If what you were shown does not settle it, answer NO."

// loopPrompt is the brief, the tools it was given, and what led into it.
func (m *modelEnv) loopPrompt(node Node, brief, tools, input string) string {
	var page strings.Builder
	page.WriteString("Step: " + node.Id + "\n\n" + brief + "\n")
	if tools != "" {
		// Named, not handed over: this bridge makes one call and cannot run a
		// tool mid-answer. Saying which tools the step was given is what lets a
		// worker answer "run the suite and read it" with the command it would
		// have run, which the tool.call after it can then actually make.
		page.WriteString("\nTools this step was given: " + tools +
			". You cannot call them here — say what you would run and what you would look for.\n")
	}
	page.WriteString(fmt.Sprintf("\nEffort budgeted: %d turns' worth, in ONE answer.\n",
		node.Fields.Int("max_turns", m.maxTurns)))
	if came := m.leadingIn(node, input); came != "" {
		page.WriteString("\nWhat the steps before this one produced:\n" + came)
	}
	return page.String()
}

// leadingIn is the closure-accumulated half: every predecessor's output, named
// by the node that produced it, in program order.
//
// It falls back to the state the walk threaded — the last step's output — for
// the node that has no predecessors, which is the entry, and for a program
// whose predecessors all produced nothing.
func (m *modelEnv) leadingIn(node Node, input string) string {
	var parts []string
	for _, id := range m.harness.Program.Predecessors(node.Id) {
		if out := m.state.out(id); out != "" {
			parts = append(parts, id+":\n"+clipModel(out))
		}
	}
	if len(parts) == 0 {
		if input = strings.TrimSpace(input); input != "" {
			return clipModel(input)
		}
		return ""
	}
	return strings.Join(parts, "\n\n")
}

func (m *modelEnv) checkPrompt(node Node, ladder, claim string, state State) string {
	var page strings.Builder
	page.WriteString("The claim to check, at the " + ladder + " rung of the verification ladder:\n" +
		claim + "\n\n" + ladderSays(ladder) + "\n")
	if !state.OK {
		page.WriteString("\nThe step before this one did not succeed.\n")
	}
	page.WriteString("\nWhat the run has produced so far:\n" + m.trail())
	return page.String()
}

func (m *modelEnv) condPrompt(node Node, condition string, state State) string {
	var page strings.Builder
	page.WriteString("The question, written into the procedure at step " + node.Id + ":\n" +
		condition + "\n")
	if !state.OK {
		page.WriteString("\nThe step before this one did not succeed.\n")
	}
	page.WriteString("\nWhat the run has produced so far:\n" + m.trail())
	return page.String()
}

// trail is the run's evidence, oldest first. Every step is clipped, because a
// judgement is made on what a step SAID and a prompt that carried three whole
// artifacts would push the claim itself out of the model's attention.
func (m *modelEnv) trail() string {
	if len(m.state.done) == 0 {
		return "(nothing yet — this is the first step)"
	}
	var page strings.Builder
	for at, step := range m.state.done {
		if at > 0 {
			page.WriteString("\n\n")
		}
		fmt.Fprintf(&page, "%d. %s (%s):\n%s", at+1, step.id, step.kind, clipModel(step.out))
	}
	return page.String()
}

// ladderSays is one sentence per rung: what checking AT that rung means. It is
// a table here rather than a field on the ladder because the words are a
// prompt, and a prompt is this file's business and not the registry's.
func ladderSays(ladder string) string {
	switch ladder {
	case VerifyAccept:
		return "At this rung, take the work at its word: it passes unless it plainly contradicts itself."
	case VerifySchema:
		return "At this rung, check the SHAPE: the right fields, the right kinds of value, nothing missing."
	case VerifyInvariants:
		return "At this rung, check the rules that must hold whatever the answer is, and name any that do not."
	case VerifyLoop:
		return "At this rung, the work must show it was actually run — output, a result, a reading — not a claim that it would work."
	case VerifyReport:
		return "At this rung, the work must be reported well enough that somebody else could act on it without asking a question."
	case VerifyRederive:
		return "At this rung, derive the answer yourself from the evidence and pass only if you land on the same one."
	case VerifyAdversarial:
		return "At this rung, try to BREAK the claim: look for the case, the input or the reading that makes it false, and pass only if you cannot find one."
	case VerifyHuman:
		return "At this rung, only a person can pass this. Say what a person would have to see to be satisfied, and fail unless the evidence already shows it."
	}
	return "Check the claim against the evidence."
}

// ── reading what came back ──────────────────────────────────────────────────

// The gate's vocabulary. The FIRST WORD decides, and everything after it is the
// note — so "yes, but skip the deploy" is an approval carrying a redirect, and
// "no, the branch is wrong" is a refusal carrying its reason.
var (
	gateYes = []string{"yes", "y", "ok", "okay", "approve", "approved", "go", "run", "continue", "proceed"}
	gateNo  = []string{"no", "n", "stop", "decline", "declined", "cancel", "abort", "reject"}
	gateOwn = []string{"intervene", "takeover", "mine"}
)

// readGate reads a person's answer at a gate.
//
// AN ANSWER THAT IS NEITHER YES NOR NO IS AN INTERVENTION, never a quiet
// approval. Somebody who types a sentence at a gate is saying something the two
// buttons could not, and the one reading of that nobody meant is "carry on" —
// so the run stops where it stands and their words become its output, which is
// exactly what [GateAnswer]'s third answer is for.
func readGate(said string) GateAnswer {
	said = strings.TrimSpace(said)
	if said == "" {
		// An empty answer is the surface saying nothing came back, not a person
		// saying yes.
		return GateAnswer{Intervene: true}
	}
	first, rest := splitCondition(said)
	first = strings.Trim(first, ".,!:;\"'")
	switch {
	case wordIn(gateYes, first):
		return GateAnswer{Approved: true, Note: rest}
	case wordIn(gateNo, first):
		return GateAnswer{Note: rest}
	case wordIn(gateOwn, first):
		return GateAnswer{Intervene: true, Note: rest}
	case first == "take" && strings.HasPrefix(strings.ToLower(rest), "over"):
		return GateAnswer{Intervene: true, Note: strings.TrimSpace(rest[len("over"):])}
	}
	return GateAnswer{Intervene: true, Note: said}
}

// readVerdict reads a check's answer: the word, then the reason.
//
// ANYTHING THAT IS NOT A PASS IS A FAIL. A check whose answer cannot be read is
// a check that did not pass, because the alternative — treating an unreadable
// verdict as a pass — is a verify ladder that gets weaker the worse the model
// answers.
func readVerdict(said string) (bool, string) {
	said = strings.TrimSpace(said)
	if said == "" {
		return false, "the check came back with nothing"
	}
	head, rest := headAndRest(said)
	word := strings.Trim(strings.ToLower(head), ".,!:;*\"'")
	switch word {
	case "pass", "passed", "passes", "ok", "yes", "true":
		return true, rest
	case "fail", "failed", "fails", "no", "false":
		return false, rest
	}
	return false, said
}

// readYes reads a yes-or-no answer, and NO is the default for the same reason a
// verdict's is: a condition nobody could read must not silently take an arm.
func readYes(said string) bool {
	head, _ := headAndRest(strings.TrimSpace(said))
	switch strings.Trim(strings.ToLower(head), ".,!:;*\"'") {
	case "yes", "y", "true", "pass", "passed":
		return true
	}
	return false
}

// headAndRest splits an answer into its first word and everything after, with
// the first LINE break treated as a space — the two-line shape the verify and
// condition prompts ask for.
func headAndRest(said string) (string, string) {
	said = strings.TrimSpace(said)
	at := strings.IndexFunc(said, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if at < 0 {
		return said, ""
	}
	return said[:at], strings.TrimSpace(said[at+1:])
}

func wordIn(list []string, word string) bool {
	for _, item := range list {
		if item == word {
			return true
		}
	}
	return false
}

func verdictWord(passed bool) string {
	if passed {
		return "passed"
	}
	return "did not pass"
}

func detail(note string) string {
	if note = strings.TrimSpace(note); note != "" {
		return " · " + note
	}
	return ""
}

// ── the call ────────────────────────────────────────────────────────────────

// allowed reads a comma-separated tool field and holds it to the harness's
// whitelist. Validate already refused a page that names a tool the harness does
// not allow; this is the same law at the moment it would be USED, which is
// where it has to hold for a program that reached this node down a road nobody
// validated.
func (m *modelEnv) allowed(node Node, field string) (string, error) {
	names := splitList(node.Fields.Get(field))
	if len(names) == 0 {
		return "", nil
	}
	h := m.harness
	for _, name := range names {
		if !h.Allows(name) {
			return "", fmt.Errorf("node %q hands out %q, which is not on the whitelist", node.Id, name)
		}
	}
	return strings.Join(names, ", "), nil
}

// say is the one outbound call in this file. It never streams: nobody is
// watching a harness node arrive token by token, and the deltas would be
// events on a turn that is not this run's.
func (m *modelEnv) say(ctx context.Context, model, system, user string) (string, error) {
	if m.client == nil {
		return "", errors.New("this surface has no model to think with")
	}
	var options []ai.Option
	// An empty model is the client's own, which is the session's model: a node
	// that names none should ride whatever the person is talking to, not a
	// second default this file invented.
	if model = strings.TrimSpace(model); model != "" {
		options = append(options, ai.WithModel(model))
	}
	response, err := m.client.CompleteWithMessages(provider.WithoutStream(ctx), []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: system}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: user}}},
	}, options...)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the model answered with nothing at all")
	}
	return strings.TrimSpace(response.Text()), nil
}

// clipModel shortens one output for a prompt.
func clipModel(text string) string { return clip(text, modelClip) }

// clip keeps the head and the tail — a command's output says what it did at the
// top and whether it worked at the bottom, and a middle-out clip is the one that
// keeps both.
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	head := limit / 2
	tail := limit - head
	return text[:head] + "\n…\n" + text[len(text)-tail:]
}
