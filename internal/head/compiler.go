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
//
// The goal rule carries acceptance in the user's own terms because this is the
// only prompt every job passes through: a "task" or "lookup" scale ask never
// reaches the planner, so a success criterion written here is the only one a
// single-leaf build will ever be held to. Written as the builder's evidence it
// licences the exact failure it was meant to catch — every part checked, the
// thing itself never used.
const compilerSystemPrompt = `You are the intent compiler for an asynchronous task graph. Apply ASSUME-AND-DECLARE.

Turn the user's verbatim instruction and the current graph context into a complete execution brief. Return exactly one JSON object with this shape and no text outside it:
{"goal":"...","title":"...","scale":"lookup|task|project","contract":"","builds_on":["<job id>"],"assumptions":["..."],"question":"","question_options":[{"label":"...","value":"..."}],"trial_of":0}

Rules:
- State a clear goal that names the final deliverable, what success means, and the evidence standard that will prove it. Write success from the seat of whoever will use the result: what they will do with it the first time, and what they must observe for it to count as working. Parts of it behaving in a test harness is the builder's evidence, never theirs, and a goal that settles for it buys work that passes its own checks and fails the first real use.
- Name the job in "title": 3 to 5 words, no quotes and no closing punctuation, judged by one test — someone who asked for this work yesterday must recognise it at a glance among unrelated jobs. Prefer the distinctive noun over the generic verb: "Mahabharata nighttime podcast" beats "Create audio content", "Org-wide star count" beats "Gather repository data". Never use the words task, job, request, or goal. It is a NAME, not a summary and not a restatement; a name that needs the goal to be understood has failed.
- Match the shaping to the ask. Something the person will come back to and use repeatedly earns a beat on whether its shape fits that use, named in the goal; a one-shot artefact earns none — say which of the two this is and let the work be exactly as small as it is.
- Write the goal as commander's intent: the end-state and why it matters, never one fixed method. Workers will hit obstacles no one can foresee; a goal that names the outcome lets them substitute means and still land it, while a goal that prescribes a method dies with that method.
- No instruction compiles to impossible. When the ask looks blocked or out of reach, name what actually makes it hard — access, tooling, scale, uncertainty — and reshape around that by safe means: substitute an available source or route for an unavailable one, split the achievable core from the blocked remainder and name both in the goal, or reach the target by approximation first and refinement after. Every such reshaping is declared in assumptions like any other default.
- Fill every missing decision with a practical default: scope, audience, format, quality bar, evidence, timing, tools, and constraints whenever the user did not settle them.
- List every default you supplied in assumptions, and write each one as a decision that changes what the workers will do. Two kinds qualify. One is an ambiguity you settled with a concrete choice — which branch, which base, which source, which format — stated as the choice itself rather than as the fact that a choice was made. The other is a commitment about method or evidence the work will be held to: what must be run, checked, reviewed or matched before the deliverable is handed over. Never restate the request; what the user already asked for is not something you decided. Never record a fact that alters nothing — if a line vanished and no worker would do anything differently, it was never a decision. Assumptions are revisable receipts, not hidden guesses, and they travel with the work as standing orders, so write each one as something a worker could follow or fail.
- Fill gaps with defaults, with two exceptions that go in "question" (empty otherwise). First: a gap both high-consequence and hard to reverse — spending real money externally, deleting or overwriting something that exists, sending or publishing on the user's behalf, or a wrong guess that would waste most of the budget — asked as ONE crisp casual question stating your best-guess default so the user can simply say yes. Second: referent ambiguity — the instruction points at earlier work and MORE THAN ONE prior job plausibly matches. Guessing the referent wastes the whole job and reads as not listening; ask which one, listing the candidates as numbered options identified by the user's own words from each job. A single plausible match is not ambiguity. Never ask about reversible preferences, and never leave placeholders such as TBD or unknown.
- When the answers are enumerable, put them in question_options in the order they should be shown. Options never prevent a free-text answer. Use [] when the question has no useful choices.
- The trial-shaping rule fires only on the explicit notebook flag "an unsettled pair applies here: fact #N". When that flag appears, set trial_of to N and shape the goal so a small, cheap trial of both named approaches runs first and the bulk of the work follows whichever proves out. When no such flag appears, set trial_of to 0. Never infer a trial from ordinary prose.
- The user's words are the authority. Do not narrow or replace them with an inferred request.
- End goal with a line beginning "Verbatim request:" followed by the user's instruction exactly as supplied.
- The graph context lists earlier jobs with their ids, what was asked, and their results. When the instruction continues, improves, or refers to earlier work, name those job ids in builds_on AND restate in the goal the concrete starting points from their results — file paths, names, findings — so the work never starts blind. When the instruction stands alone, builds_on is [].
- A job still running is earlier work too. When this instruction concerns something a live job is changing right now — the same repository, the same document, the same deliverable — name that job in builds_on so this work follows it. Two jobs editing one thing at the same time do not merely duplicate effort; each is working against a state the other is moving, and the result belongs to neither.
- When a live job in the context already covers what is being asked for — not adjacent to it, not a step towards it, but the same deliverable produced by the same work — the honest brief is the one that waits for it rather than a second copy of it. Say so in the goal: name that job, state that the work it is already doing is what was asked for, and shape this brief around what would still be missing when it lands. A duplicate is paid for twice and answers once.
- When the graph context lists attached documents, name them in the goal as required inputs. They become workspace files for workers, which read them with read_document; do not assume the conversational model receives a file content part.
- For project scale, make the parallel structure explicit in the goal: name the parts if they are known, or state that the first step enumerates them and each then proceeds independently. Downstream planning fans out exactly what the goal names; a vague goal collapses into needlessly serial work.
- Judge scale by the structure of the work, never by its topic. Ask two questions. First: does the job enumerate — does doing it mean repeating the same operation over a set of items, sources, or sections that do not depend on each other? Second: does it stratify — does it separate into stages with different working modes, such as gathering, verifying, and synthesizing, where intermediate outputs feed a final deliverable? If either answer is yes, the scale is "project": independent parts are parallel structure, and parallel structure is the point even when one worker could grind through serially. If both answers are no and the job still requires acting — producing, transforming, fetching-then-shaping — it is "task": one worker, one thread of attention, end to end. If the whole job is retrieving or computing a single thing, where the answer is itself the deliverable, it is "lookup".
- One boundary overrides both of those questions: work whose essence is judging or understanding a single artifact as a whole — reviewing a change, auditing an agreement, weighing a body of evidence to reach one verdict — is "task" at any length. The sections of one thing under judgment are not independent items: what the judgment exists to catch lives in the cross-references between them, and a reader split into parts can never see a number in one section contradict a claim in another. Enumeration means repeating an operation over items that stand alone, never dividing one act of comprehension.
- For "task" scale only, also write "contract": the working method this one worker is held to, in two to four sentences from the seat of the person who asked — what done means, what will be run or checked as evidence before handover, and that the whole finished thing is written out in the worker's own final message. For "lookup" and "project" leave contract empty: a lookup's method is to answer, and a project's parts each get their own method later.

Be precise enough for downstream planning, but do not design the task graph yourself.`

// Brief is the complete, assumption-bearing intent handed to planning.
//
// It carried a Deliverable and a Budget until this wave, and neither had a
// reader anywhere in the tree: the goal already names the deliverable, planning
// takes its money from measured history, and resident_build copies eleven fields
// across and dropped exactly these two. What they did have was a validator that
// hard-rejected an empty one with no retry, so an otherwise perfect request came
// back as "I couldn't apply that request: compile request: empty budget" for a
// field nothing would have consumed. A validated field with no consumer is a
// pure failure source, so both are gone from the schema and from validation.
type Brief struct {
	Goal        string   `json:"goal"`
	Assumptions []string `json:"assumptions"`

	// Title is the rail-sized display name for the job, produced by the one
	// call that has already read the whole ask. It used to be a second model
	// round-trip of its own — measured at 222-330 prompt tokens for a 5-token
	// answer, ~0.6s of the critical path before any leaf could start — for a
	// label nothing downstream waits on. Empty is a valid answer and the
	// caller falls back to whatever it named jobs with before.
	Title string `json:"title,omitempty"`

	// Scale is the compiler's honest judgement of shape: "lookup" (one fact,
	// one step), "task" (one worker end to end), or "project" (parallel parts
	// worth a planning pass). Downstream decides what to do with it; an
	// unrecognised value degrades to task.
	Scale string `json:"scale"`

	// Contract is the working method for a task-scale job, written by the one
	// call that has already read the whole ask. It used to be a second
	// structuring round-trip serialized between compile and dispatch — a paid
	// call on the critical path of every single-worker job. Empty is a valid
	// answer and the caller falls back to that separate pass.
	Contract string `json:"contract,omitempty"`

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
	// ServiceIntent is deterministic consent provenance; the provider never
	// gets to infer whether a process may outlive its leaf.
	ServiceIntent bool `json:"service_intent,omitempty"`

	// WorkModel is the model the user named for this job in their own words.
	// Empty is the ordinary case: the surface's current work model serves.
	WorkModel string `json:"work_model,omitempty"`

	// ModelNote is the one calm receipt line about that choice — which model
	// runs the job, or why the name they used did not land.
	ModelNote string `json:"model_note,omitempty"`
}

// Compiler converts verbatim user intent into a planning brief without asking
// the user to resolve unspecified details first.
type Compiler struct {
	client       Client
	resolveModel ModelResolver
	oneShot      bool
}

// NewCompiler returns an intent compiler backed by client.
func NewCompiler(client Client) *Compiler {
	return &Compiler{client: client}
}

// WithOneShotErrands says every ask this compiler will ever see arrived on a
// surface that runs exactly one errand and then exits — `aforge do`.
//
// This is the surface stating a fact about itself, not an opinion about the
// work. "Once, not standing" is an option on the ratification card because a
// person may want it; a person who typed `aforge do "<task>"` has already
// chosen it, in the verb, before the compiler read a word. Asking them again
// is asking a question into a process with nobody at the keyboard, and the
// live defect it caused was total: "flag every discrepancy" tripped the
// temporal recognizer's `every <word>` cue, a plain reconciliation of two CSVs
// was drafted as a standing rule with an invented two-minute cadence, and the
// run exited in three seconds having done none of the work it was sent to do.
//
// So the temporal route is not taken here at all, and the ordinary compiler is
// told what surface it is compiling for. Nothing about the judgement of the
// WORK changes; the classification that changes is the one the surface already
// answered.
func (c *Compiler) WithOneShotErrands() *Compiler {
	c.oneShot = true
	return c
}

// oneShotErrandBrief is that fact, in the prompt, for the reasoning half of
// the rail. The deterministic route above is gated structurally; this is the
// same law said to the model, which would otherwise be free to draft a
// standing rule out of an ask that merely sounds recurrent.
const oneShotErrandBrief = "\n\nSurface: this instruction arrived as a single headless errand — one run, " +
	"start to finish, with nobody at a keyboard. It is never a standing rule, a schedule, a watch or a " +
	"recurring routine, however recurrent its wording sounds; compile it as work to be done once, now. " +
	"Never ask a question that only a person could answer: there is no one to answer it."

// WithModelResolver installs the surface's catalog-backed reading of model
// words. Without it the compiler still recognizes them and still says nothing
// wrong: every job simply runs on the default work model.
func (c *Compiler) WithModelResolver(resolve ModelResolver) *Compiler {
	c.resolveModel = resolve
	return c
}

// Compile applies assume-and-declare once. The exact instruction is appended
// deterministically to Goal even if a provider ignores that prompt rule, so no
// downstream transformation can silently lose the user's words.
func (c *Compiler) Compile(ctx context.Context, instruction string, graphContext string) (Brief, error) {
	if c == nil || c.client == nil {
		return Brief{}, errors.New("compile intent: nil client")
	}
	if !c.oneShot && RecognizesStandingIntent(instruction) {
		return c.compileStanding(ctx, instruction, graphContext)
	}
	serviceIntent := RecognizesServiceIntent(instruction)
	words, wanted := RecognizeModelWords(instruction)
	var choice WorkModelChoice
	if wanted && c.resolveModel != nil {
		choice = c.resolveModel(words)
		if len(choice.Candidates) > 1 {
			if !words.Answered {
				// A model word that could mean several models is referent
				// ambiguity like any other: one choose question, no work spliced.
				return Brief{
					Question:        modelChoiceQuestion(choice.Requested),
					QuestionOptions: modelChoiceOptions(choice.Candidates),
					ServiceIntent:   serviceIntent,
				}, nil
			}
			// This question was already asked and answered. Asking it again is
			// a loop the user cannot leave, so the best candidate takes the job
			// and the receipt carries the way to switch.
			choice = settleAnsweredAmbiguity(choice)
		}
	}
	user := "Current graph context:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction +
		settledQuestionBrief(instruction) + c.surfaceBrief()
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
		brief.ServiceIntent = serviceIntent
		return brief, nil
	}
	brief.QuestionOptions = nil
	if err := validateBrief(brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	brief.Goal = anchorQualityWords(anchorGoal(brief.Goal, instruction), instruction)
	brief.Scale = normalizeScale(brief.Scale)
	brief.Title = normalizeTitle(brief.Title)
	brief.BuildsOn = normalizeBuildsOn(brief.BuildsOn)
	brief.TrialOf = normalizeTrialOf(graphContext, brief.TrialOf)
	brief.ServiceIntent = serviceIntent
	brief.WorkModel = strings.TrimSpace(choice.Model)
	if wanted {
		brief.ModelNote = modelReceiptNote(words, choice)
	}
	return brief, nil
}

// surfaceBrief is what the surface knows about itself and the model cannot
// guess. Empty for a chat window, which is every other caller: a conversation
// has a mouth and may be asked anything.
func (c *Compiler) surfaceBrief() string {
	if c == nil || !c.oneShot {
		return ""
	}
	return oneShotErrandBrief
}

// settledQuestionBrief declares the answers this ask already carries. The
// deterministic recognizers are gated structurally; this is the same law for
// the reasoning half of the rail, which would otherwise be free to put a
// settled question back to the user.
func settledQuestionBrief(instruction string) string {
	answers := answeredCompilerQuestions(instruction)
	if len(answers) == 0 {
		return ""
	}
	settled := "\n\nAlready answered by the user — these are settled, never ask them again:"
	for _, answer := range answers {
		settled += "\n- " + answer
	}
	return settled
}

// RecognizesServiceIntent marks asks whose requested end-state is a running
// thing the user can continue to open or use. Ordinary "run tests" work is
// deliberately excluded.
func RecognizesServiceIntent(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	if strings.HasPrefix(lower, "serve ") || strings.Contains(lower, " please serve ") {
		return true
	}
	for _, phrase := range []string{
		"keep it running", "keep this running", "keep the server running",
		"so i can open it", "so i can see it", "run the app", "serve the app",
		"start the app", "launch the app", "start the server", "start dev server",
		"start the dev server", "start vite", "start preview", "run the server",
		"run the site", "serve locally", "run locally",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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

// normalizeTitle takes the model's name at its word and only strips what a
// display surface cannot use: surrounding quotes, trailing punctuation, and a
// length no rail could show. A title that comes back empty or unusable is not
// an error — the naming pass is a nicety and the caller has a fallback.
func normalizeTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'")
	title = strings.TrimRight(title, " .!?:;,")
	if strings.Contains(title, "\n") {
		title = strings.TrimSpace(strings.SplitN(title, "\n", 2)[0])
	}
	return title
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
