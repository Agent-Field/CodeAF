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
	if opts.Store != nil {
		// The child's pages come from the store, its trace goes back to the
		// store, and the loader is wrapped so that a called harness's own
		// whitelist — not its caller's — is what bounds its nodes.
		runner.Loader = scopedLoader{inner: opts.Store, state: env.state}
		runner.Saver = opts.Store
	}
	// The state the walk threads from node to node. It starts empty because
	// [Run] takes no opening sentence — the entry node works from its own brief,
	// and a caller with something to say goes through [Runner], which does.
	state := State{OK: true}
	return func(ctx context.Context, node Node) (Result, error) {
		if node.Kind == KindSubharnessCall && opts.Store == nil {
			return Result{}, errors.New("subharness.call is not in the exec bridge without a store")
		}
		result, err := runner.step(ctx, env.harness(node), &state, withModel(node, opts.Model))
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
	state    *modelState
}

func newModelEnv(c *provider.Client, opts ModelExecOpts) *modelEnv {
	turns := opts.MaxTurnsDefault
	if turns < 1 {
		turns = modelMaxTurns
	}
	return &modelEnv{
		client:   c,
		runTool:  opts.RunTool,
		ask:      opts.Ask,
		maxTurns: turns,
		state:    &modelState{scope: []Harness{opts.Harness.Normalize()}},
	}
}

// modelState is the run's own memory: what each node produced, in order, and
// which harnesses the run is inside.
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
	// scope is the harness stack, top first: the program being run, then every
	// harness a subharness.call has loaded. A node is bounded by the one it
	// belongs to, not by whichever was outermost.
	scope []Harness
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

// harness is the program a node belongs to. The stack is searched from the top
// so that a child harness's node beats a parent's node of the same id, which is
// the only way two programs in one run can collide.
func (m *modelState) harness(node Node) Harness {
	for at := len(m.scope) - 1; at >= 0; at-- {
		if found, ok := m.scope[at].Program.Node(node.Id); ok && found.Kind == node.Kind {
			return m.scope[at]
		}
	}
	if len(m.scope) > 0 {
		return m.scope[0]
	}
	return Harness{}
}

func (m *modelEnv) harness(node Node) Harness { return m.state.harness(node) }

// scopedLoader is [Loader] with a memory: every harness a call resolves is
// pushed onto the run's scope, so the child's nodes are bounded by the CHILD's
// whitelist. Without it a called harness would be held to its caller's tools,
// which is a rule nobody wrote and which fails the ordinary case — a triage
// harness that reads files being called by one that only runs the suite.
type scopedLoader struct {
	inner Loader
	state *modelState
}

func (s scopedLoader) Load(name string, version int) (Harness, error) {
	child, err := s.inner.Load(name, version)
	if err != nil {
		return Harness{}, err
	}
	s.state.scope = append(s.state.scope, child.Normalize())
	return child, nil
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
	if !m.harness(node).Allows(tool) {
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
		ladder = m.harness(node).Verify.Ladder
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
	for _, id := range m.harness(node).Program.Predecessors(node.Id) {
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
	h := m.harness(node)
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

// clipModel shortens one output for a prompt, keeping the head and the tail —
// a command's output says what it did at the top and whether it worked at the
// bottom, and a middle-out clip is the one that keeps both.
func clipModel(text string) string {
	if len(text) <= modelClip {
		return text
	}
	head := modelClip / 2
	tail := modelClip - head
	return text[:head] + "\n…\n" + text[len(text)-tail:]
}
