package head

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// compilerSystemPrompt applies assume-and-declare at the boundary between a
// user's durable intent and planning. Ambiguity becomes a visible, revisable
// receipt instead of a synchronous question that stalls the graph.
const compilerSystemPrompt = `You are the intent compiler for an asynchronous task graph. Apply ASSUME-AND-DECLARE.

Turn the user's verbatim instruction and the current graph context into a complete execution brief. Return exactly one JSON object with this shape and no text outside it:
{"goal":"...","deliverable":"...","budget":"...","scale":"lookup|task|project","builds_on":["<job id>"],"assumptions":["..."],"question":"","question_options":[{"label":"...","value":"..."}],"trial_of":0}

Rules:
- State a clear goal that names the final deliverable, what success means, and the evidence standard that will prove it.
- Write the goal as commander's intent: the end-state and why it matters, never one fixed method. Workers will hit obstacles no one can foresee; a goal that names the outcome lets them substitute means and still land it, while a goal that prescribes a method dies with that method.
- No instruction compiles to impossible. When the ask looks blocked or out of reach, name what actually makes it hard — access, tooling, scale, uncertainty — and reshape around that by safe means: substitute an available source or route for an unavailable one, split the achievable core from the blocked remainder and name both in the goal, or reach the target by approximation first and refinement after. Every such reshaping is declared in assumptions like any other default.
- Name that same concrete final deliverable separately in deliverable.
- Give a sensible free-text budget, including a currency amount when cost is otherwise unspecified.
- When measured execution costs appear in the context, ground the budget in them. A budget contradicted by the system's own measured history is a guess wearing numbers.
- Fill every missing decision with a practical default: scope, audience, format, quality bar, evidence, timing, tools, and constraints whenever the user did not settle them.
- List every default you supplied explicitly in assumptions. Assumptions are revisable receipts, not hidden guesses.
- Fill gaps with defaults, with two exceptions that go in "question" (empty otherwise). First: a gap both high-consequence and hard to reverse — spending real money externally, deleting or overwriting something that exists, sending or publishing on the user's behalf, or a wrong guess that would waste most of the budget — asked as ONE crisp casual question stating your best-guess default so the user can simply say yes. Second: referent ambiguity — the instruction points at earlier work and MORE THAN ONE prior job plausibly matches. Guessing the referent wastes the whole job and reads as not listening; ask which one, listing the candidates as numbered options identified by the user's own words from each job. A single plausible match is not ambiguity. Never ask about reversible preferences, and never leave placeholders such as TBD or unknown.
- When the answers are enumerable, put them in question_options in the order they should be shown. Options never prevent a free-text answer. Use [] when the question has no useful choices.
- The trial-shaping rule fires only on the explicit notebook flag "an unsettled pair applies here: fact #N". When that flag appears, set trial_of to N and shape the goal so a small, cheap trial of both named approaches runs first and the bulk of the work follows whichever proves out. When no such flag appears, set trial_of to 0. Never infer a trial from ordinary prose.
- The user's words are the authority. Do not narrow or replace them with an inferred request.
- End goal with a line beginning "Verbatim request:" followed by the user's instruction exactly as supplied.
- The graph context lists earlier jobs with their ids, what was asked, and their results. When the instruction continues, improves, or refers to earlier work, name those job ids in builds_on AND restate in the goal the concrete starting points from their results — file paths, names, findings — so the work never starts blind. When the instruction stands alone, builds_on is [].
- For project scale, make the parallel structure explicit in the goal: name the parts if they are known, or state that the first step enumerates them and each then proceeds independently. Downstream planning fans out exactly what the goal names; a vague goal collapses into needlessly serial work.
- Judge scale by the structure of the work, never by its topic. Ask two questions. First: does the job enumerate — does doing it mean repeating the same operation over a set of items, sources, or sections that do not depend on each other? Second: does it stratify — does it separate into stages with different working modes, such as gathering, verifying, and synthesizing, where intermediate outputs feed a final deliverable? If either answer is yes, the scale is "project": independent parts are parallel structure, and parallel structure is the point even when one worker could grind through serially. If both answers are no and the job still requires acting — producing, transforming, fetching-then-shaping — it is "task": one worker, one thread of attention, end to end. If the whole job is retrieving or computing a single thing, where the answer is itself the deliverable, it is "lookup".

Be precise enough for downstream planning, but do not design the task graph yourself.`

// Brief is the complete, assumption-bearing intent handed to planning. Budget
// stays free text because the graph does not impose a money type on callers.
type Brief struct {
	Goal        string   `json:"goal"`
	Assumptions []string `json:"assumptions"`
	Deliverable string   `json:"deliverable"`
	Budget      string   `json:"budget"`

	// Scale is the compiler's honest judgement of shape: "lookup" (one fact,
	// one step), "task" (one worker end to end), or "project" (parallel parts
	// worth a planning pass). Downstream decides what to do with it; an
	// unrecognised value degrades to task.
	Scale string `json:"scale"`

	// BuildsOn names earlier jobs this instruction continues or improves.
	// The reconciler turns each into a real dependency edge, so the prior
	// result flows to the new workers as an input digest instead of being
	// rediscovered or guessed at.
	BuildsOn []string `json:"builds_on"`

	// Question is the one gap too consequential to guess, when one exists.
	// Empty is the overwhelmingly common, correct value: asking is reserved
	// for irreversible or expensive mistakes, never for preferences.
	Question string `json:"question"`

	// TrialOf is the fact sequence of the retrieved unsettled pair that this
	// brief deliberately compares. Zero means no experiment was shaped.
	TrialOf int64 `json:"trial_of"`

	// QuestionOptions is the generic selectable askback surface. Charter is set
	// only by the temporal compiler; both omit cleanly for ordinary work.
	QuestionOptions []store.QuestionOption `json:"question_options,omitempty"`
	Charter         *store.CharterSpec     `json:"charter,omitempty"`
}

// Compiler converts verbatim user intent into a planning brief without asking
// the user to resolve unspecified details first.
type Compiler struct {
	client Client
}

// NewCompiler returns an intent compiler backed by client.
func NewCompiler(client Client) *Compiler {
	return &Compiler{client: client}
}

// Compile applies assume-and-declare once. The exact instruction is appended
// deterministically to Goal even if a provider ignores that prompt rule, so no
// downstream transformation can silently lose the user's words.
func (c *Compiler) Compile(ctx context.Context, instruction string, graphContext string) (Brief, error) {
	if c == nil || c.client == nil {
		return Brief{}, errors.New("compile intent: nil client")
	}
	if RecognizesStandingIntent(instruction) {
		return c.compileStanding(ctx, instruction, graphContext)
	}
	user := "Current graph context:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction
	response, err := c.client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", compilerSystemPrompt),
		textMessage("user", user),
	}, ai.WithMaxTokens(1000))
	if err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	if response == nil {
		return Brief{}, errors.New("compile intent: provider returned a nil response")
	}

	var brief Brief
	if err := decodeJSONObject(response.Text(), &brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	if question := strings.TrimSpace(brief.Question); question != "" {
		// A question suspends the brief: the rest of the fields are drafts at
		// best, and validating them would reject the ask itself.
		brief.Question = question
		brief.QuestionOptions = normalizeQuestionOptions(brief.QuestionOptions)
		return brief, nil
	}
	brief.QuestionOptions = nil
	if err := validateBrief(brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	brief.Goal = anchorGoal(brief.Goal, instruction)
	brief.Scale = normalizeScale(brief.Scale)
	brief.BuildsOn = normalizeBuildsOn(brief.BuildsOn)
	brief.TrialOf = normalizeTrialOf(graphContext, brief.TrialOf)
	return brief, nil
}

func normalizeTrialOf(graphContext string, selected int64) int64 {
	flagged := unsettledFactSeqs(graphContext)
	for _, seq := range flagged {
		if seq == selected {
			return selected
		}
	}
	if len(flagged) > 0 {
		return flagged[0]
	}
	return 0
}

func unsettledFactSeqs(graphContext string) []int64 {
	seen := make(map[int64]bool)
	var seqs []int64
	for remaining := graphContext; ; {
		index := strings.Index(remaining, store.UnsettledFactFlag)
		if index < 0 {
			break
		}
		remaining = remaining[index+len(store.UnsettledFactFlag):]
		end := 0
		for end < len(remaining) && '0' <= remaining[end] && remaining[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		seq, err := strconv.ParseInt(remaining[:end], 10, 64)
		if err == nil && seq > 0 && !seen[seq] {
			seen[seq] = true
			seqs = append(seqs, seq)
		}
		remaining = remaining[end:]
	}
	return seqs
}

func normalizeBuildsOn(ids []string) []string {
	kept := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		kept = append(kept, id)
	}
	return kept
}

// Scale values the compiler may emit. ScaleTask is also the degradation
// target for anything unrecognised: the safe default shape is one worker.
const (
	ScaleLookup  = "lookup"
	ScaleTask    = "task"
	ScaleProject = "project"
)

func normalizeScale(scale string) string {
	switch strings.ToLower(strings.TrimSpace(scale)) {
	case ScaleLookup:
		return ScaleLookup
	case ScaleProject:
		return ScaleProject
	default:
		return ScaleTask
	}
}

func validateBrief(brief Brief) error {
	if strings.TrimSpace(brief.Goal) == "" {
		return errors.New("empty goal")
	}
	if strings.TrimSpace(brief.Deliverable) == "" {
		return errors.New("empty deliverable")
	}
	if strings.TrimSpace(brief.Budget) == "" {
		return errors.New("empty budget")
	}
	if brief.Assumptions == nil {
		return errors.New("missing assumptions")
	}
	for _, assumption := range brief.Assumptions {
		if strings.TrimSpace(assumption) == "" {
			return errors.New("empty assumption")
		}
	}
	return nil
}

func anchorGoal(goal, instruction string) string {
	goal = strings.TrimSpace(goal)
	// A compiler that followed the prompt already carries the anchor inline;
	// appending a second copy would double the user's words in every receipt.
	if strings.Contains(goal, "Verbatim request:") && strings.Contains(goal, instruction) {
		return goal
	}
	return goal + "\n\nVerbatim request:\n" + instruction
}
