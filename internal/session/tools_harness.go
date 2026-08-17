package session

// The harness tool: how a shape of work gets built, approved, kept, and run
// again.
//
// A conversation can already do a piece of work once. What it could not do is
// keep HOW it did it. The person who spent twenty minutes teaching a model to
// triage a flaky test — read the log, rerun the test twenty times, decide, patch,
// re-check, ask me before landing — had to teach it again tomorrow, because the
// only record of the shape was a transcript nobody re-reads. internal/subharness
// is that shape as a durable, versioned, named thing; this file is how a
// conversation reaches it.
//
// ── THE FOUR VERBS, AND WHY APPROVAL SITS BETWEEN THEM ──
//
//	preview   the model writes a program; the person is shown a CARD
//	register  the person approves; the entry is written and the version bumps
//	run       the person approves; the program executes and a trace is written
//	show/list/runs/kinds   reading, which nobody has to approve
//
// PREVIEW AND REGISTER ARE TWO CALLS ON PURPOSE. A model that could register in
// one call would be a model that writes into a person's registry on its own
// judgement, and the card is the only moment anybody actually reads what it
// wrote. Register asks — through the same consent lane every dangerous tool call
// goes through (consent.go) — and the card is the question's own text.
//
// ── THE GATE INSIDE A RUN IS THE SAME GATE ──
//
// A human.gate node stops the run and asks. It does NOT invent a mechanism: it
// emits EventConsentRequest, blocks the tool call, and is answered by
// [Agent.ResolveConsent] like everything else — so every surface that can
// already draw a consent card can already answer a harness gate, and a surface
// with nobody watching refuses rather than hanging (the law consent.go states).
//
// What it adds is the THIRD answer. A person watching their own program go
// slightly wrong wants neither "yes" nor "no" but "stop, I will take it from
// here" — so a gate that declares escalate:true offers intervene, answered
// through [Agent.ResolveHarnessGate], and the run ends as intervened with their
// words as its outcome. A surface with two keys still works: yes approves, no
// declines, and nothing is lost but the third door.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// harnessRunCap bounds one whole run's wall time, whatever its nodes ask for. It
// is a backstop and not a budget: every loop is bounded in the file and every
// agent.loop is bounded in turns, so a run that reaches this has found a way to
// be slow that the file's own numbers did not describe.
const harnessRunCap = 30 * time.Minute

const harnessDescription = "Build, keep, and re-run a SHAPE of work — a sub-harness: a named, versioned program of steps (a worker's turn, a tool call, a branch, a bounded loop, a check, a pause to ask the person) that can be run again by name. Use it when this conversation has worked out HOW to do something that will be done again — triage a flaky test, review a diff, sweep a directory, chase a release — and the method is worth keeping. Do NOT use it for one-off work (do that work), or to record what you just did (that is a note). THE FLOW IS: `kinds` to see the vocabulary, then `preview` with a program you wrote to show the person a card, then `register` once they like it, then `run` by name. preview and register are separate calls because the card is what the person approves; register and run both ASK them and can be declined. Every run writes a trace of the path it actually took, kept with the harness."

const harnessSchemaJSON = `{"type":"object","properties":{` +
	`"action":{"type":"string","description":"kinds (the node vocabulary and the ladders), list (what is registered), show (one harness as a card), preview (validate a program and show its card, changing nothing), register (save it — asks the person), run (execute one by name — asks the person), runs (its history), retire (remove the entry, keeping the history)","enum":["kinds","list","show","preview","register","run","runs","retire"]},` +
	`"name":{"type":"string","description":"The harness this action is about, for show, run, runs and retire"},` +
	`"harness":{"type":"string","description":"For preview and register: the WHOLE entry as a JSON object written out as text — {\"name\":…,\"description\":…,\"tools\":[…],\"verify\":…,\"dynamism\":…,\"cap\":…,\"program\":[…]}. Call action=kinds first; it prints the exact shape, every node kind's fields, and the two ladders. Do not set a version: the registry owns it"},` +
	`"input":{"type":"string","description":"For run: what this run is about — the failing test, the diff, the question. It is what the first step reads"},` +
	`"version":{"type":"number","description":"For run and show: a pinned version, as an integer. Leave it out for whatever is registered now"}` +
	`},"required":["action"],"additionalProperties":false}`

type harnessArguments struct {
	Action  string `json:"action"`
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Input   string `json:"input"`
	Version int    `json:"version"`
}

// harnessTools is the belt's harness family — one tool, and only where there is
// both a registry to write into and somebody to approve a write.
//
// A TASK NODE DOES NOT GET IT, for propose_task's own reason (task.go): there is
// nobody in a node's world to show a card to, and both of the verbs that matter
// here are questions. A node that needs a harness's shape should be given the
// work, not the registry.
func (a *Agent) harnessTools() []bare.Tool {
	if a.config.InTask || a.harnessStore() == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        "harness",
		Description: harnessDescription,
		Schema:      json.RawMessage(harnessSchemaJSON),
		Execute:     a.harnessTool,
	}}
}

// harnessStore is this session's registry, or nil when the surface configured
// none. It is built once, lazily, for the reason the state store is: most
// conversations never touch it.
func (a *Agent) harnessStore() *subharness.Store {
	root := strings.TrimSpace(a.config.HarnessDir)
	if root == "" {
		return nil
	}
	a.harnessOnce.Do(func() { a.harnesses = subharness.New(root) })
	return a.harnesses
}

// harnessTool is the whole dispatch. Everything it can answer badly is an
// ordinary tool result rather than a Go error, exactly as the rest of this belt
// answers: a program with a broken condition in it is a call the model can make
// again, and the refusal names the step.
func (a *Agent) harnessTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed harnessArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	store := a.harnessStore()
	if store == nil {
		return "Harnesses are not available in this session.", true, nil
	}
	name := strings.TrimSpace(parsed.Name)

	switch strings.TrimSpace(parsed.Action) {
	case "kinds":
		return harnessVocabulary(), false, nil

	case "list":
		return a.harnessList(store), false, nil

	case "show":
		harness, problem := a.harnessNamed(store, name, parsed.Version)
		if problem != "" {
			return problem, true, nil
		}
		return subharness.Card(harness), false, nil

	case "preview":
		harness, problem := parseHarnessEntry(parsed.Harness)
		if problem != "" {
			return problem, true, nil
		}
		// A preview is deliberately a DRAFT card: version 0, drawn as "draft",
		// so the person reading it in the transcript can see at a glance that
		// nothing has been written yet.
		return "This is a draft — nothing is registered yet. Ask the person, then call register.\n\n" +
			subharness.Card(harness), false, nil

	case "register":
		return a.harnessRegister(ctx, store, parsed.Harness)

	case "run":
		return a.harnessRun(ctx, store, name, parsed.Version, parsed.Input)

	case "runs":
		return a.harnessRuns(store, name)

	case "retire":
		return a.harnessRetire(ctx, store, name)
	}
	return fmt.Sprintf("Invalid arguments: action %q is not one of kinds, list, show, preview, register, run, runs, retire", parsed.Action), true, nil
}

// ── reading ─────────────────────────────────────────────────────────────────

func (a *Agent) harnessList(store *subharness.Store) string {
	rows, err := store.List()
	if err != nil {
		return "Could not read the registry: " + err.Error()
	}
	if len(rows) == 0 {
		return "No harnesses are registered yet. Call action=kinds to see what a program is made of, then preview one."
	}
	var out strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&out, "%s  v%d  %s", row.Name, row.Version, row.Description)
		if last, ok := store.LastRun(row.Name); ok {
			fmt.Fprintf(&out, "  · last run %s (%s)", last.Started.Format(time.RFC3339), last.Status)
		}
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func (a *Agent) harnessRuns(store *subharness.Store, name string) (string, bool, error) {
	if name == "" {
		return "Invalid arguments: runs needs a name", true, nil
	}
	paths, err := store.Runs(name)
	if err != nil {
		return "Could not read the history: " + err.Error(), true, nil
	}
	if len(paths) == 0 {
		return name + " has never run.", false, nil
	}
	var out strings.Builder
	for at, path := range paths {
		if at >= 10 {
			fmt.Fprintf(&out, "… %d older\n", len(paths)-at)
			break
		}
		run, err := store.LoadRun(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&out, "%s  v%d  %s  %s\n",
			run.Started.Format(time.RFC3339), run.Version, run.Status, run.Elapsed().Round(time.Second))
	}
	// The newest run in full, because "what happened last time" is the question
	// somebody asking for a history is usually actually asking.
	if newest, err := store.LoadRun(paths[0]); err == nil {
		out.WriteString("\n")
		out.WriteString(subharness.RunCard(newest))
	}
	return out.String(), false, nil
}

// harnessNamed resolves one entry, at a version or at whatever is current.
func (a *Agent) harnessNamed(store *subharness.Store, name string, version int) (subharness.Harness, string) {
	if name == "" {
		return subharness.Harness{}, "Invalid arguments: name is required"
	}
	harness, err := store.LoadVersion(name, version)
	if err != nil {
		return subharness.Harness{}, "No harness called " + name + ": " + err.Error()
	}
	return harness, ""
}

// parseHarnessEntry reads the entry the model wrote and refuses it in the words
// a builder can act on.
//
// THE VERSION IS DROPPED HERE, whatever the model put in it. The registry owns
// that number (subharness's store.go) and a builder that could name its own
// would be a builder that could name one twice.
func parseHarnessEntry(text string) (subharness.Harness, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return subharness.Harness{}, "Invalid arguments: harness is required — the whole entry as a JSON object. Call action=kinds for the shape"
	}
	harness, err := subharness.Decode([]byte(text))
	if err != nil {
		return harness, "That is not a harness object: " + err.Error()
	}
	harness.Version = 0
	subharness.Clamp(&harness)
	if err := subharness.Validate(&harness); err != nil {
		return harness, "That program will not do: " + err.Error()
	}
	return harness, ""
}

// ── writing, which asks ─────────────────────────────────────────────────────

func (a *Agent) harnessRegister(ctx context.Context, store *subharness.Store, text string) (string, bool, error) {
	harness, problem := parseHarnessEntry(text)
	if problem != "" {
		return problem, true, nil
	}
	// What version this WOULD become, so the question the person answers names
	// the thing that is about to happen rather than a shape with no number.
	next := 1
	if existing, err := store.Load(harness.Name); err == nil {
		next = existing.Version + 1
	}
	verb := "register"
	if next > 1 {
		verb = fmt.Sprintf("update to v%d", next)
	}
	answer, err := a.askAboutHarness(ctx, "register", harness.Name,
		fmt.Sprintf("%s %s", verb, harness.Name), subharness.Card(harness), false)
	if err != nil {
		return "the turn ended before this was approved", true, nil
	}
	if !answer.Approved {
		if note := strings.TrimSpace(answer.Note); note != "" {
			return "the person did not register this: " + note, false, nil
		}
		return "the person did not register this", false, nil
	}
	saved, err := store.Save(harness)
	if err != nil {
		return "Could not register it: " + err.Error(), true, nil
	}
	return fmt.Sprintf("registered %s at v%d — run it with action=run, name=%s",
		saved.Name, saved.Version, saved.Name), false, nil
}

func (a *Agent) harnessRetire(ctx context.Context, store *subharness.Store, name string) (string, bool, error) {
	harness, problem := a.harnessNamed(store, name, 0)
	if problem != "" {
		return problem, true, nil
	}
	answer, err := a.askAboutHarness(ctx, "retire", harness.Name,
		"retire "+harness.Name, subharness.Card(harness), false)
	if err != nil {
		return "the turn ended before this was answered", true, nil
	}
	if !answer.Approved {
		return "the person kept " + harness.Name, false, nil
	}
	if err := store.Remove(harness.Name); err != nil {
		return "Could not retire it: " + err.Error(), true, nil
	}
	// The runs stay, and saying so is the difference between a retirement and a
	// deletion somebody thought they had asked for.
	return fmt.Sprintf("retired %s — its %s history is still under %s",
		harness.Name, harness.Name, store.RunDir(harness.Name)), false, nil
}

// harnessRun asks, executes, records, and answers with the path the run took.
func (a *Agent) harnessRun(ctx context.Context, store *subharness.Store, name string, version int, input string) (string, bool, error) {
	harness, problem := a.harnessNamed(store, name, version)
	if problem != "" {
		return problem, true, nil
	}
	answer, err := a.askAboutHarness(ctx, "run", harness.Name,
		fmt.Sprintf("run %s v%d", harness.Name, harness.Version), subharness.Card(harness), false)
	if err != nil {
		return "the turn ended before this was approved", true, nil
	}
	if !answer.Approved {
		if note := strings.TrimSpace(answer.Note); note != "" {
			return "the person did not run it: " + note, false, nil
		}
		return "the person did not run it", false, nil
	}
	if redirect := strings.TrimSpace(answer.Note); redirect != "" {
		// Approval with words is a redirect, the shape a task proposal already
		// has (task.go): their sentence rides into the first step rather than
		// being merged into the model's.
		input = strings.TrimRight(input, "\n") + "\n\nThe person starting this run says: " + redirect
	}

	// The run outlives no turn: it is a tool call, and an interrupt must end it.
	// The cap above that is the backstop, not the budget.
	runCtx, cancel := context.WithTimeout(ctx, harnessRunCap)
	defer cancel()

	env := &harnessEnv{agent: a, harness: harness}
	a.markHarnessRunning(harness.Name, true)
	defer a.markHarnessRunning(harness.Name, false)

	runner := &subharness.Runner{Env: env, Loader: store, Saver: store}
	run, runErr := runner.Run(runCtx, harness, input)
	path, saveErr := store.SaveRun(run)

	card := subharness.RunCard(run)
	if saveErr == nil {
		card += "\n\ntrace · " + path
	}
	// A FAILED RUN IS NOT A FAILED TOOL CALL, which is why runErr is read and
	// not returned. The harness ran, it recorded the path it took, and the
	// model's next move is to read that trace; a red error row would hide the
	// one artifact worth reading and put a fault on a tool that worked.
	_ = runErr
	return card, false, nil
}

// ── the question ────────────────────────────────────────────────────────────

// harnessAnswer is one person's answer about a harness.
type harnessAnswer struct {
	Approved  bool
	Intervene bool
	Note      string
}

// askAboutHarness puts one question — register this, run this, or a gate inside
// a run — to the person, through the consent lane every other question uses.
//
// The CARD is the question. The lane carries the tool's name, a gloss, the
// arguments and the policy's phrasing; here the gloss is the one line ("run
// triage-flake v2") and the arguments are the whole card, which is what a
// surface expands to show. Nothing new is drawn and nothing new is journaled:
// consent is events only, for the reason consent.go states.
//
// escalate says this question offers the third answer. It rides in the phrasing
// as well as in the resolve, so a surface with two keys still says something
// true.
func (a *Agent) askAboutHarness(ctx context.Context, verb, name, gloss, card string, escalate bool) (harnessAnswer, error) {
	a.mu.Lock()
	hub := a.hub
	watched := a.config.AskConsent && hub != nil
	a.mu.Unlock()
	if !watched {
		// Nobody is watching, so nothing may be written or run on somebody's
		// behalf. It is the same answer approve() gives an unwatched prompt, for
		// the same reason: blocking would hang, and allowing would make a
		// question mean nothing wherever the surface is not a terminal.
		return harnessAnswer{}, nil
	}
	rule := fmt.Sprintf("harness %s — %s", verb, name)
	if escalate {
		rule += " · you can also take it over (intervene)"
	}
	answer, err := a.askAnswer(ctx, hub, ai.ToolCall{
		Function: ai.ToolCallFunction{Name: "harness", Arguments: card},
	}, approval.Decision{Action: approval.ActionPrompt, Rule: rule}, false, escalate)
	if err != nil {
		return harnessAnswer{}, err
	}
	return harnessAnswer{
		Approved:  answer.allow,
		Intervene: answer.gate == GateIntervene,
		Note:      answer.note,
	}, nil
}

// GateChoice is one answer to a harness's human.gate. It is the third option a
// bool cannot carry, and it is the recovery question's own shape (recovery.go)
// for a different question.
type GateChoice string

const (
	// GateApprove lets the run carry on. A note with it is a redirect.
	GateApprove GateChoice = "approve"
	// GateDecline ends the run. It is not a failure.
	GateDecline GateChoice = "decline"
	// GateIntervene ends the run and hands the work to the person: the trace
	// stops where it stands and their words are its outcome.
	GateIntervene GateChoice = "intervene"
)

// ResolveHarnessGate answers one harness question with all three options and the
// person's own words.
//
// A surface with two keys uses [Agent.ResolveConsent] and loses only the third
// door — yes approves, no declines. An id nobody is waiting on is ignored,
// exactly as every other resolve on this lane ignores a late answer.
func (a *Agent) ResolveHarnessGate(id uint64, choice GateChoice, note string) {
	switch choice {
	case GateApprove, GateDecline, GateIntervene:
	default:
		// The narrowest reading, for the reason an unknown consent scope takes
		// it: a typo must never widen what happens, and declining changes
		// nothing.
		choice = GateDecline
	}
	a.deliverConsent(id, consentAnswer{
		allow: choice == GateApprove,
		gate:  choice,
		note:  strings.TrimSpace(note),
	})
}

// ── live state, for a surface that draws it ─────────────────────────────────

// markHarnessRunning records that a run is in flight, so a surface can draw a
// strip for it. It is a counter rather than a flag because a subharness.call
// runs inside a run and both are the same harness's business.
func (a *Agent) markHarnessRunning(name string, running bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if running {
		a.harnessDepth++
		a.harnessName = name
		return
	}
	if a.harnessDepth > 0 {
		a.harnessDepth--
	}
	if a.harnessDepth == 0 {
		a.harnessName = ""
	}
}

// RunningHarness is the harness this session is executing, and false when it is
// executing none. It is the strip's whole data source (internal/tui3's
// harness.go): one name, polled, never an event — a run is minutes long and a
// second event lane for it would be a second ordering rule.
func (a *Agent) RunningHarness() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.harnessName, a.harnessDepth > 0
}

// ── the environment: what makes the nodes do anything ───────────────────────

// harnessEnv is [subharness.Env] over this session's own machinery: the same
// provider client, the same belt, the same approval policy, the same consent
// lane. NOTHING HERE IS A SECOND ENGINE — a harness leaf and a conversation
// reach for identical hands, which is what makes a shape that worked in the
// conversation work when it is run by name.
type harnessEnv struct {
	agent   *Agent
	harness subharness.Harness
}

// Loop runs one agent.loop node: a bounded tool loop with its own two-message
// context, on the whitelist this node was given.
//
// IT IS NOT THIS CONVERSATION. The node gets its brief and the previous step's
// output and nothing else, for the task contract's reason (task.go): a step
// whose behaviour depended on what was said in the room half an hour ago is a
// step that behaves differently every time the harness is run.
func (e *harnessEnv) Loop(ctx context.Context, node subharness.Node, input string) (string, error) {
	tools := e.agent.harnessBelt(node.Tools)
	definitions, err := toolDefinitions(tools)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(node.Model)
	if model == "" {
		model = e.agent.Model()
	}
	messages := []ai.Message{
		textMessage("system", harnessNodeSystem(e.harness, node)),
		textMessage("user", harnessNodeBrief(node, input)),
	}
	turns := node.MaxTurns
	if turns <= 0 {
		turns = subharness.DefaultTurns
	}
	for turn := 0; turn < turns; turn++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// WithoutStream: this is a step of a program, not somebody talking. Its
		// text is the node's result, and typing it into the room would put a
		// second voice in the transcript.
		response, err := e.agent.client.CompleteWithMessages(provider.WithoutStream(ctx), messages,
			ai.WithModel(model), ai.WithTools(definitions))
		if err != nil {
			return "", err
		}
		if response == nil {
			return "", fmt.Errorf("the model returned nothing")
		}
		// The person pays for it and no turn asked for it, which is exactly what
		// the auxiliary bucket is (loop.go).
		e.agent.addAuxiliaryUsage(response)

		calls := response.ToolCalls()
		if len(calls) == 0 {
			return strings.TrimSpace(response.Text()), nil
		}
		messages = append(messages, ai.Message{
			Role: "assistant", Content: assistantContent(response), ToolCalls: calls,
		})
		// Through the ordinary batch, so every call passes the approval gate and
		// a dangerous one still reaches the person (loop.go's runTools).
		results := e.agent.runTools(ctx, calls, e.agent.currentHub())
		for index, call := range calls {
			text := ""
			if index < len(results) {
				text = results[index].text
			}
			messages = append(messages, ai.Message{
				Role: "tool", ToolCallID: call.ID,
				Content: []ai.ContentPart{{Type: "text", Text: text}},
			})
		}
	}
	return "", fmt.Errorf("ran %d turns without finishing", turns)
}

// Tool calls one tool with the node's fixed arguments.
//
// THE WHITELIST IS CHECKED HERE TOO, not only at parse time. The entry on disk
// could have been edited by hand since it was approved, and the whitelist is the
// one sentence on the card a person actually reads to decide what this thing can
// reach.
func (e *harnessEnv) Tool(ctx context.Context, node subharness.Node, input string) (string, error) {
	if !harnessAllows(e.harness, node.Tool) {
		return "", fmt.Errorf("%q is not on this harness's whitelist", node.Tool)
	}
	args := node.Args
	if args == nil {
		args = map[string]any{}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	call := ai.ToolCall{
		ID:       fmt.Sprintf("harness-%s", node.ID),
		Function: ai.ToolCallFunction{Name: node.Tool, Arguments: string(encoded)},
	}
	results := e.agent.runTools(ctx, []ai.ToolCall{call}, e.agent.currentHub())
	if len(results) == 0 {
		return "", fmt.Errorf("the tool returned nothing")
	}
	if results[0].isError {
		return "", fmt.Errorf("%s", results[0].text)
	}
	return results[0].text, nil
}

// Gate stops the run and asks.
func (e *harnessEnv) Gate(ctx context.Context, node subharness.Node, state subharness.State) (subharness.GateAnswer, error) {
	card := node.Prompt
	if last := strings.TrimSpace(state.Last); last != "" {
		card += "\n\nwhat the run has so far:\n" + capOutput(last)
	}
	answer, err := e.agent.askAboutHarness(ctx, "gate", e.harness.Name,
		harnessGateGloss(e.harness, node), card, node.Escalate)
	if err != nil {
		return subharness.GateAnswer{}, err
	}
	return subharness.GateAnswer{
		Approved: answer.Approved,
		// A surface that answered "intervene" on a gate that did not offer it is
		// answering a question it was not asked; the run reads it as a decline,
		// which is the narrower of the two.
		Intervene: answer.Intervene && node.Escalate,
		Note:      answer.Note,
	}, nil
}

// Check runs one verify node.
//
// TWO RUNGS OF THE LADDER ARE MECHANISMS AND THE REST ARE QUESTIONS, and that is
// the honest split rather than a gap:
//
//   - accept passes. It is the name for no verification (subharness.go).
//   - human asks the person, through the same gate every other question uses.
//   - everything else runs the check. A check that looks like a command is run
//     as one through the belt's bash — pass is exit zero — and a check that
//     looks like a question is put to a model with nothing but the material and
//     the question, answering PASS or FAIL on its first line.
//
// The second of those is what makes rederive and adversarial reachable at all
// without a second worker pool: the rung decides how the question is PUT, and
// the ladder's meaning is carried in the prompt rather than in nine code paths.
func (e *harnessEnv) Check(ctx context.Context, node subharness.Node, state subharness.State) (bool, string, error) {
	rung := node.Rung
	if rung == "" {
		rung = subharness.RungAccept
	}
	switch rung {
	case subharness.RungAccept:
		return true, "", nil

	case subharness.RungHuman:
		answer, err := e.agent.askAboutHarness(ctx, "verify", e.harness.Name,
			"verify · "+harnessFirstLine(node.Check), state.Last, false)
		if err != nil {
			return false, "", err
		}
		return answer.Approved, answer.Note, nil
	}

	if harnessLooksLikeCommand(node.Check) {
		if !harnessAllows(e.harness, "bash") {
			return false, "", fmt.Errorf("this check runs a command but bash is not on the whitelist")
		}
		output, err := e.Tool(ctx, subharness.Node{
			ID: node.ID, Tool: "bash", Args: map[string]any{"command": node.Check},
		}, state.Last)
		if err != nil {
			// A non-zero exit is a check that FAILED, not a check that could not
			// run — and the difference is the whole reason Check returns two
			// values and an error rather than one bool.
			return false, err.Error(), nil
		}
		return true, output, nil
	}

	verdict, err := e.judge(ctx, rung, node.Check, state.Last)
	if err != nil {
		return false, "", err
	}
	return verdict.passed, verdict.said, nil
}

type harnessVerdict struct {
	passed bool
	said   string
}

// judge puts the check to a model as a question, in the words its rung means.
func (e *harnessEnv) judge(ctx context.Context, rung subharness.Rung, check, material string) (harnessVerdict, error) {
	response, err := e.agent.client.CompleteWithMessages(provider.WithoutStream(ctx),
		[]ai.Message{
			textMessage("system", harnessJudgePrompt(rung)),
			textMessage("user", "The check:\n"+check+"\n\nThe work to check:\n"+material),
		}, ai.WithModel(e.agent.Model()))
	if err != nil {
		return harnessVerdict{}, err
	}
	if response == nil {
		return harnessVerdict{}, fmt.Errorf("the judge returned nothing")
	}
	e.agent.addAuxiliaryUsage(response)
	text := strings.TrimSpace(response.Text())
	first := strings.ToUpper(strings.TrimSpace(harnessFirstLine(text)))
	// PASS has to be said out loud. Anything else — a hedge, an empty answer, a
	// model that wrote an essay — is a fail, because a verifier that defaults to
	// yes is not a verifier.
	return harnessVerdict{passed: strings.HasPrefix(first, "PASS"), said: text}, nil
}

// ── the words a node is given ───────────────────────────────────────────────

func harnessNodeSystem(harness subharness.Harness, node subharness.Node) string {
	var out strings.Builder
	out.WriteString("You are one step of a sub-harness — a saved shape of work that is run by name.\n\n")
	fmt.Fprintf(&out, "The harness is %s v%d", harness.Name, harness.Version)
	if harness.Description != "" {
		out.WriteString(": " + harness.Description)
	}
	out.WriteString(".\n")
	fmt.Fprintf(&out, "This step is %q.\n\n", node.ID)
	out.WriteString("Do this step and nothing else. You cannot ask anybody anything — " +
		"if something is missing, say so in your answer and stop. " +
		"Your ANSWER is what the next step reads, so end with the result itself " +
		"rather than with a description of what you did.\n")
	if len(node.Tools) > 0 {
		out.WriteString("\nYou have: " + strings.Join(node.Tools, ", ") + ".\n")
	} else {
		out.WriteString("\nYou have no tools on this step: answer from what you were given.\n")
	}
	return out.String()
}

func harnessNodeBrief(node subharness.Node, input string) string {
	brief := strings.TrimSpace(node.Prompt)
	if input = strings.TrimSpace(input); input != "" {
		brief += "\n\nWhat the run has so far:\n" + input
	}
	return brief
}

// harnessJudgePrompt is one rung of the ladder, written out as the instruction
// it means. The ladder is the ARGUMENT (subharness.go) and this is the only
// place its rungs become words.
func harnessJudgePrompt(rung subharness.Rung) string {
	head := "You are checking somebody else's work. Answer with PASS or FAIL on the first line, " +
		"then one or two lines saying why. Say PASS only if you are sure.\n\n"
	switch rung {
	case subharness.RungSchema:
		return head + "Check the SHAPE only: is what was asked for present, in the form it was asked for? " +
			"Do not judge whether it is correct."
	case subharness.RungInvariants:
		return head + "Check the properties that must hold of ANY correct answer to this, " +
			"whatever the answer happens to be."
	case subharness.RungLoop:
		return head + "Check whether the stated condition actually holds of this work, on the evidence in it."
	case subharness.RungReport:
		return head + "Check whether the work ACCOUNTS for itself: could somebody else audit this " +
			"from what is written here, or are the claims unsupported?"
	case subharness.RungRederive:
		return head + "Work the answer out again yourself, independently, and then compare. " +
			"FAIL if you reach a different answer."
	case subharness.RungAdversarial:
		return head + "Try to REFUTE this work. Look for the case that breaks it, the assumption that " +
			"does not hold, the step that does not follow. FAIL if you find one. Default to FAIL if unsure."
	}
	return head
}

// harnessGateGloss is the one line a gate's consent card leads with.
func harnessGateGloss(harness subharness.Harness, node subharness.Node) string {
	line := harness.Name + " · " + harnessFirstLine(node.Prompt)
	if node.Escalate {
		line += " · or take it over"
	}
	return line
}

// harnessVocabulary is action=kinds: the whole language a program is written in,
// generated from the registry so it cannot drift from what will be accepted.
func harnessVocabulary() string {
	var out strings.Builder
	out.WriteString("A harness entry is a JSON object:\n\n")
	out.WriteString(`{"name":"triage-flake","description":"chase a flaky test to a fix",` +
		`"tools":["read","grep","bash"],"verify":"loop","dynamism":"branch","cap":3,"program":[…]}` + "\n\n")
	out.WriteString("NODE KINDS — every step is {\"id\":…,\"kind\":…} plus that kind's own fields:\n")
	for _, doc := range subharness.Kinds() {
		fmt.Fprintf(&out, "  %-16s %-46s needs dynamism %s\n", doc.Kind, doc.Blurb, doc.Needs)
	}
	out.WriteString("\nFIELDS BY KIND:\n" +
		"  agent.loop      prompt (required), tools (subset of the harness's), model, max_turns\n" +
		"  tool.call       tool (required, on the whitelist), args {}\n" +
		"  branch          cases [{when, steps[]}], else []\n" +
		"  loop.until      until (condition), max (rounds), steps []\n" +
		"  parallel.split  lanes [{name, steps[]}], join: all | first\n" +
		"  human.gate      prompt (required), escalate: true to offer 'I'll take it from here'\n" +
		"  verify          rung, check (a shell command, or a question for a model to judge)\n" +
		"  subharness.call call (a registered name), call_version\n" +
		"  trigger         on: hosted | idle | watch | source.command, spec — first step only\n")
	out.WriteString("\nCONDITIONS (branch when, loop until) ask about the step before them:\n" +
		"  always · never · ok · failed · empty · nonempty · " +
		"contains <text> · equals <text> · matches <regexp>\n")
	out.WriteString("\nTHE VERIFY LADDER (the harness's own rung is the ceiling for its verify nodes):\n  ")
	for at, rung := range subharness.Rungs() {
		if at > 0 {
			out.WriteString(" < ")
		}
		out.WriteString(string(rung))
	}
	out.WriteString("\n\nTHE DYNAMISM LADDER (how much shape the program may grow; cap is its integer bound):\n  ")
	for at, rung := range subharness.Dynamisms() {
		if at > 0 {
			out.WriteString(" < ")
		}
		out.WriteString(string(rung))
	}
	fmt.Fprintf(&out, "\n\nBOUNDS: at most %d steps, nested %d deep, %d lanes to a split, "+
		"%d rounds to a loop, %d turns to a worker. The registry sets the version, not you.\n",
		subharness.MaxNodes, subharness.MaxDepth, subharness.MaxLanes, subharness.MaxRounds, subharness.MaxTurns)
	return out.String()
}

// ── small helpers ───────────────────────────────────────────────────────────

// harnessBelt is this session's belt narrowed to what a node was given. The
// intersection is taken against the whitelist as well as the node's list, so a
// node cannot reach past the harness even if the entry on disk was edited by
// hand after it was approved.
func (a *Agent) harnessBelt(wanted []string) []bare.Tool {
	var out []bare.Tool
	for _, tool := range a.beltTools() {
		for _, want := range wanted {
			if strings.TrimSpace(want) == tool.Name {
				out = append(out, tool)
				break
			}
		}
	}
	return out
}

// currentHub is the in-flight turn's fan-out, read under the lock that writes
// it. A harness runs inside a tool call, so there is always a turn; nil is the
// honest answer for a run started from somewhere else, and every question this
// package asks handles it (see [Agent.askAboutHarness]).
func (a *Agent) currentHub() *eventHub {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hub
}

func harnessAllows(harness subharness.Harness, tool string) bool {
	tool = strings.TrimSpace(tool)
	for _, listed := range harness.Tools {
		if strings.TrimSpace(listed) == tool {
			return true
		}
	}
	return false
}

// harnessLooksLikeCommand decides how a check is run: as a command, or as a
// question put to a model.
//
// The rule is deliberately crude and stated rather than clever — a check is a
// command when it is ONE line that starts with a word that is plausibly a
// program and holds no sentence punctuation. A verifier that guessed subtly
// would be a verifier that ran a sentence in a shell.
func harnessLooksLikeCommand(check string) bool {
	check = strings.TrimSpace(check)
	if check == "" || strings.Contains(check, "\n") {
		return false
	}
	if strings.ContainsAny(check, "?") {
		return false
	}
	fields := strings.Fields(check)
	if len(fields) == 0 {
		return false
	}
	head := fields[0]
	for _, char := range head {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9', char == '.', char == '/', char == '_', char == '-':
		default:
			return false
		}
	}
	// A sentence's first word is a word; a command's first word is a program.
	// The distinction that survives is punctuation elsewhere in the line.
	return !strings.ContainsAny(check, ",;")
}

func harnessFirstLine(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return text
}
