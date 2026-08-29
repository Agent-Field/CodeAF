package head

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
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
{"structure":"enumerates|stratifies|one_judgement|single_act","goal":"...","title":"...","scale":"lookup|task|project","contract":"","parts":["..."],"builds_on":["<job id>"],"assumptions":["..."],"question":"","question_options":[{"label":"...","value":"..."}],"trial_of":0}

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
- The graph context lists earlier jobs with their ids, what was asked, and their results. When the instruction NEEDS an earlier job's result to start — it continues it, improves it, corrects it, or takes its output as input — name those job ids in builds_on AND restate in the goal the concrete starting points from their results — file paths, names, findings — so the work never starts blind. When the instruction stands alone, builds_on is []. Where two earlier results speak to the same quantity, the later one stands: a correction's product replaces its predecessor, and the goal restates the corrected figure, never the one it corrected.
- A job still running is earlier work too. When this instruction concerns something a live job is changing right now — the same repository, the same document, the same deliverable — name that job in builds_on so this work follows it. Two jobs editing one thing at the same time do not merely duplicate effort; each is working against a state the other is moving, and the result belongs to neither.
- builds_on is a WAIT, and that is what it costs. Every id you name is a hard edge: nothing of this job runs until that job has landed, however long it takes and whatever else the workforce has free. So the test is not whether the two are related, it is whether this work can honestly begin before that one finishes. Being on the same subject, arriving in the same breath, or being introduced by a word that merely adds one thing to another is not a dependency. If two workers who never spoke to each other could do both at once and both results would still be right, builds_on is []. When you are unsure, [] is the safe answer: a missing edge costs a worker re-reading a file, and a wrong edge costs the person the whole running time of a job they never asked to wait for.
- When a live job in the context already covers what is being asked for — not adjacent to it, not a step towards it, but the same deliverable produced by the same work — the honest brief is the one that waits for it rather than a second copy of it. Say so in the goal: name that job, state that the work it is already doing is what was asked for, and shape this brief around what would still be missing when it lands. A duplicate is paid for twice and answers once.
- When the graph context lists attached documents, name them in the goal as required inputs. They become workspace files for workers, which read them with read_document; do not assume the conversational model receives a file content part.
- For project scale, make the structure explicit in the goal: name the pieces and the stages if they are known, or state that the first step enumerates them and each then proceeds independently. Downstream planning lays out exactly what the goal names; a vague goal collapses into needlessly serial work.
- Judge scale by the structure of the work, never by its topic. Ask two questions. First: does the job enumerate — does doing it mean repeating the same operation over a set of items, sources, or sections that do not depend on each other? Second: does it stratify — does it separate into stages that work in different modes, where what one stage produces is what the next one works on? Answer both about the WORK and never about what it arrives in: items that end up as sections of one document are items exactly as separate files are, and a stage that feeds another is a stage even when both end up inside the same deliverable. If either answer is yes, the scale is "project". Project does not mean many answers and it does not mean everything at once: it means the MAKING of this has structure that someone must lay out before it starts — pieces, or stages, or an order between them. A chain is structure exactly as a fan is, and it is the case that needs laying out most: an ordering that has to hold is a thing to be planned, not a thing to be remembered halfway through by whoever happens to be holding the work. If both answers are no and the job still requires acting — producing, transforming, fetching-then-shaping — it is "task": one worker, one thread of attention, end to end. If the whole job is retrieving or computing a single thing, where the answer is itself the deliverable, it is "lookup".
- WIDTH IS FREE TO COORDINATE AND NOT FREE IN APPETITE, and that trade is the whole of naming pieces. The workers are AI agents running at once: there is no meeting, no handover, no fatigue and no context-switch, so an extra worker costs almost nothing to organise and the time the person waits is the longest chain, never the total. What is not free is appetite, and appetite is paid in context: every part is briefed on its own and re-pays whatever it must be told before it can start, then works to the size of the brief it was given. So a part described as broadly as the whole job simply does the whole job again — the same answer bought a second time, landing later than one worker would have. Width therefore pays exactly when the brief narrows as it widens: whenever you name more pieces, say more precisely what each returns and where it stops. Name pieces and their bounds together, or name neither. And bound each one to what a single worker can carry to the end in one go: a piece that outruns what one worker can hold is not finished and handed over, it is stopped and resumed, so the chain you were shortening quietly grows a link you did not plan.
- Where the material already enumerates its units, that enumeration is where the pieces come from. When the units to be worked on are named — in the instruction itself, in the landed result of an earlier job this one builds on, or in the sources it points at — a piece takes some of those units and says which, never all of them; one unit each, or an even batch each when they are many. This is the narrowing the rule above demands and it is free to buy, because a piece told about its own units alone is a piece that was never told about the rest, so many pieces cost close to what one pass over everything would have cost while the person waits for one unit's work rather than all of them in turn. The failure this forbids is halving an enumerated set into two pieces that each carry the whole set: both do everything, one answer is paid for twice, and the wait is longer than doing nothing at all. The same reading governs the other direction — units the material does not present as standing apart are not made independent by being separated, and where nothing is enumerated there is no split to read off it.
- Then challenge every ordering before you write it in. Two pieces are ordered only when one consumes what the other produces, or when one changes material the other reads. Being related, being about the same thing, mattering to the same conclusion, or simply being mentioned later is not an order, and an ordering imposed out of habit is paid for in full by the person waiting. Two moves buy width where an ordering looks unavoidable. Where the second piece is only waiting to be TOLD something, decide that thing now and write it into the goal as a standing decision every piece works against: an interface fixed up front is what MAKES the pieces on either side of it independent, and it costs one sentence against a wait that costs a whole job's running time. And where the wait exists only because nobody yet knows which of several routes works, and each route is small beside the job, take them all at once and keep what lands — uncertainty is a reason for width, not a reason to go one at a time. Where you cannot tell whether two pieces are independent, writing them as independent with what each may assume is the cheaper mistake.
- "structure" is the FIRST field in the object, and that is deliberate: it is the reading everything else follows from, and a label chosen first is a label that goes looking for its reasons afterwards. Answer it before you have written a word of the goal. "enumerates" if the first question above answered yes, "stratifies" if the second did, "one_judgement" if the boundary below took it, and "single_act" only when all three genuinely fail. Whichever you write, "scale" must be the answer that reading forces — and if you find yourself writing "single_act" about work you have just described in pieces or in steps, the reading is wrong, not the description.
- Scale is that judgement's answer, not a label chosen first. "project" whenever the making has more than one piece or more than one stage, however those are ordered and however few answers come back at the end. "task" only when it is a single act carried end to end — nothing inside it that another pair of hands could have been handed with a bound, and no stage that must finish before the next may begin. "lookup" when the answer is itself the deliverable. A person who describes their work in steps has already told you it has structure, and compiling it as one undivided act is narrowing their request, not simplifying it. And note what "project" actually buys: it opens a planning pass over the work and commits you to NOTHING about how many workers there will be — laying the structure out is where the appetite trade above is spent and where one worker may still be the answer. So the smallness of a job is never a reason to call it "task"; only its indivisibility is.
- One boundary overrides both of those questions: work whose essence is reaching ONE judgement about a single body of material that ALREADY EXISTS and has to be read as a whole is "task" at any length. The test is whether the answer could be wrong because of something a reader would only catch by seeing two distant places in that material together — a figure in one place contradicting a claim in another. Where that is what the work is FOR, its sections are not independent items and a reader divided into parts can never see the contradiction. This boundary is about READING something whole and never about PRODUCING something whole: where the material has to be made before it can be weighed, the making has structure whatever the weighing later needs, and a conclusion drawn at the end from pieces that were each sound on their own does not make the pieces one act. Enumeration means repeating an operation over items that stand alone, never dividing one act of comprehension.
- For "task" scale only, also write "contract": the working method this one worker is held to, in two to four sentences from the seat of the person who asked — what done means, what will be run or checked as evidence before handover, and that the whole finished thing is written out in the worker's own final message; naming the file it was saved in is not a delivery, and files are named beside that substance rather than in place of it. The method may demand evidence only in the shape the request asked for it: a request to confirm or verify is satisfied by the fact of the result in the worker's own words, and the method never escalates it into reporting raw output or verbatim transcripts the person did not ask to see — every demand you write becomes a requirement they never made. For "lookup" and "project" leave contract empty: a lookup's method is to answer, and a project's parts each get their own method later.
- parts and scale answer different questions and neither settles the other. Scale says whether the MAKING has structure; parts says how many separate ANSWERS come back to the person. When the instruction is a bundle — several requests in one breath that do not feed each other — write each one into "parts" as a complete standalone assignment: everything its worker needs carried inside it (its subject, its inputs, its own success bar from the goal), because that worker sees its part and nothing else. Parts filled means the scale is "project" and they run at the same time. Leave parts [] whenever one thing comes back, however large, and whenever one request's output feeds another — a genuine sequence is not a bundle. Each part must be a DIFFERENT answer: read the parts you have written against each other, and if two of them would return the same thing, they were never two requests, and the honest answer is one part or none. Empty is where this field starts and where it stays unless a part earns its place: a part is added only when it would come back at the same time as the others and return something none of the others would return, and only when what it buys shows up in what the person waits, what the work costs, and how good the answer is. Nothing else adds one — not thoroughness, not the wish to be seen covering the ground, not the size of the ask. One request that happens to cover many units is one answer and belongs in parts [] — dividing the units among workers is the planning pass's job, not this field's, and doing it here instead produces workers who each answer the whole of it. Parts carry no order between them and nothing makes one wait for another, so writing an ordered pair into parts does not express the order, it DELETES it; a sequence stays whole here and is laid out by the planning pass, which is the only place an ordering can actually be recorded. But parts [] says nothing whatever about the scale: one structured deliverable with parts [] is the most ordinary "project" there is, and planning is what divides the making of it.

Be precise enough for downstream planning, but do not design the task graph yourself.`

// PartList tolerates the shapes models actually send when they list the ask's
// separate requests. The strict form is an array of strings; live models were
// measured wrapping each part in an object instead, and a declaration field
// must never be able to fail the compile — the worst legal outcome of a
// malformed parts array is no declaration, which is exactly what the field
// meant before it existed.
type PartList []string

func (p *PartList) UnmarshalJSON(data []byte) error {
	var plain []string
	if err := json.Unmarshal(data, &plain); err == nil {
		*p = plain
		return nil
	}
	var wrapped []map[string]any
	if err := json.Unmarshal(data, &wrapped); err == nil {
		parts := make(PartList, 0, len(wrapped))
		for _, item := range wrapped {
			// Whatever the key, the part is the longest string in the object:
			// a wrapper key like "part" or "text" carries the assignment, and
			// any id or index beside it is shorter.
			best := ""
			for _, value := range item {
				if text, ok := value.(string); ok && len(text) > len(best) {
					best = text
				}
			}
			if strings.TrimSpace(best) != "" {
				parts = append(parts, best)
			}
		}
		*p = parts
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		if strings.TrimSpace(one) != "" {
			*p = PartList{one}
		}
		return nil
	}
	*p = nil
	return nil
}

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
	// one step), "task" (one worker end to end), or "project" (structured work
	// worth a planning pass). Downstream decides what to do with it; an
	// unrecognised value degrades to task.
	Scale string `json:"scale"`

	// Structure is the structural reading scale is supposed to follow from,
	// asked for in its own field so that it is ANSWERED before the label is
	// chosen rather than reconstructed to justify one.
	//
	// The prompt already said "scale is that judgement's answer, not a label
	// chosen first", and measured live that is exactly what went wrong: on an
	// ask that both enumerates and stratifies ("a short note on each one first,
	// and then one comparison table that uses all three notes") the compiler
	// returned "task" three times out of three — and the SAME model, on the
	// same prompt and the same sentence, returned "project" both times it was
	// additionally asked to quote the rule it was following. The judgement was
	// available; nothing made the model reach it before naming a label. So the
	// reading is a field, and reconcileScale below makes the label follow it.
	Structure string `json:"structure,omitempty"`

	// Contract is the working method for a task-scale job, written by the one
	// call that has already read the whole ask. It used to be a second
	// structuring round-trip serialized between compile and dispatch — a paid
	// call on the critical path of every single-worker job. Empty is a valid
	// answer and the caller falls back to that separate pass.
	Contract string `json:"contract,omitempty"`

	// Parts is the compiler's reading of how many separate answers the ask
	// wants: several requests in one breath that do not feed each other, each
	// written as a complete standalone assignment. The reading is made here,
	// where the whole ask was read; what shape the work then takes is decided
	// downstream by the planner, which is handed these words verbatim as
	// evidence and nothing more. There was once a route that took this field
	// as a layout instead, and its whole failure was that a layout cannot say
	// a request waits. Empty means one thing comes back.
	Parts PartList `json:"parts,omitempty"`

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

	// Subharness is the specialist worker the compiler judged this whole job
	// to be for. Empty is the default worker and is the answer for nearly every
	// job — including every job in a process where no specialist is registered,
	// because then the field is never mentioned to the model at all. A name
	// that reaches no registered worker is dropped here rather than carried:
	// mis-selection degrades to the baseline, it never fails a compile.
	Subharness string `json:"subharness,omitempty"`
}

// Compiler converts verbatim user intent into a planning brief without asking
// the user to resolve unspecified details first.
type Compiler struct {
	client       Client
	resolveModel ModelResolver
	oneShot      bool
	menu         func() string
	knownWorker  func(string) bool
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

// WithSubharnessMenu supplies the workers this process actually has, following
// the WithSelfKnowledge precedent: nil, and a menu that comes back empty,
// preserve the compiler prompt exactly — which is the whole additive law, since
// a process with one worker has nothing to choose between.
//
// It takes two functions rather than one because a menu is a claim and a name
// coming back is a claim to check. The text teaches the choice; known settles
// whether the answer reached anything real. Both are the registry's to answer,
// and this package is not the registry's — that is the seam.
func (c *Compiler) WithSubharnessMenu(menu func() string, known func(string) bool) *Compiler {
	c.menu = menu
	c.knownWorker = known
	return c
}

// subharnessBrief is the menu as it appears in the prompt, or nothing at all.
// The field is named to the model here rather than in the JSON shape line at
// the top of the prompt, because that line is the prompt every process has and
// adding an unusable field to it would break the byte-identical baseline for a
// choice that does not exist.
//
// It rides the user message rather than the system one. Position by volatility,
// not by semantic category: this reads as law — here are the workers, here is
// how to choose between them — and law belongs with the law. But the menu
// carries each specialist's measured line, and those are run counts and a
// four-decimal average cost that move every time a leaf of that worker
// finishes. Sent as part of the system message it rewrote, mid-session, the one
// string in the whole compile that could have been identical from job to job.
func (c *Compiler) subharnessBrief() string {
	if c == nil || c.menu == nil {
		return ""
	}
	menu := strings.TrimSpace(c.menu())
	if menu == "" {
		return ""
	}
	return "\n\n" + menu + "\nReturn the choice as \"subharness\":\"<name>\" in the same JSON object. " +
		"Omit it, or leave it empty, for the default worker."
}

// normalizeSubharness drops a name that reaches no registered worker. It is
// degradation rather than validation: a compile is the cheapest call in the job
// and the only one whose loss forfeits everything after it, so a hallucinated
// worker costs the job its specialist and nothing else.
func (c *Compiler) normalizeSubharness(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || c == nil || c.knownWorker == nil || !c.knownWorker(name) {
		return ""
	}
	return name
}

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
	// Assembled stable-first, and the menu is last for the same reason the head's
	// spend line is: it is the fastest-moving thing said here, so it sits where
	// there is nothing left behind it to invalidate.
	user := "Current graph context:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction +
		settledQuestionBrief(instruction) + c.surfaceBrief() + c.subharnessBrief()
	// The compile reply carries the goal with the verbatim ask inside it, the
	// title, the task method, and any separate requests — all in one JSON object,
	// which is exactly why a completion cap sized for the goal alone became a
	// guillotine: two GAIA questions long enough to echo hit 1000 tokens to
	// the digit, the JSON was cut mid-structure, and both questions were lost
	// whole for $0.0003 each. The cap breathes with the ask now, and a reply
	// that still comes back without a complete object gets exactly one more
	// try at double room — a compile is the cheapest call in the job and the
	// only one whose loss forfeits everything after it.
	compileTokens := compileReplyTokens(instruction)
	// What this call is FOR, for the model-call log (provider.WithCallTag). It
	// is set here rather than at each of the two sends below because the retry
	// is the same call asked twice, and a reader chasing a lost compile wants
	// both rows under one word. There is no node to name: the compile happens
	// before the graph it will produce exists.
	ctx = provider.WithCallTag(ctx, "compile")
	// The system message is the constant and nothing else, for every process
	// this compiler runs in: measured content that moves within a session is
	// added below, in the user message, never here.
	// THE REPLY IS ASKED FOR AS JSON ON THE WIRE, NOT ONLY IN THE PROMPT.
	// Every planning call sends its shape as a response format; this one asked
	// in prose alone, and a model handed an issue written in Markdown answered
	// in Markdown — twice, once per attempt — and the whole job was forfeit at
	// three seconds for $0.0007. JSON mode rather than a strict schema, because
	// the shape here is the prompt's to extend: the subharness menu, the
	// charter, the model note are fields the prompt adds when they apply, and
	// a strict schema would have to know every one of them. An endpoint that
	// refuses the format has it taken off by the provider's own ladder.
	response, err := c.client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", compilerSystemPrompt),
		textMessage("user", user),
	}, ai.WithMaxTokens(compileTokens), ai.WithJSONMode())
	if err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	if response == nil {
		return Brief{}, errors.New("compile intent: provider returned a nil response")
	}

	var brief Brief
	if err := decodeJSONObject(response.Text(), &brief); err != nil {
		retry, retryErr := c.client.CompleteWithMessages(ctx, []ai.Message{
			textMessage("system", compilerSystemPrompt),
			textMessage("user", user),
		}, ai.WithMaxTokens(compileTokens*2), ai.WithJSONMode())
		// The error a person reads is the SECOND attempt's, because that is the
		// one that decided the outcome: reporting the first here told an
		// operator "empty response" when the retry had failed some other way.
		if retryErr != nil {
			return Brief{}, fmt.Errorf("compile intent: %w (first attempt: %v)", retryErr, err)
		}
		if retry == nil {
			return Brief{}, fmt.Errorf("compile intent: provider returned a nil response on retry (first attempt: %v)", err)
		}
		if retryErr := decodeJSONObject(retry.Text(), &brief); retryErr != nil {
			return Brief{}, fmt.Errorf("compile intent: %w (first attempt: %v)", retryErr, err)
		}
		response = retry
	}
	if question := strings.TrimSpace(brief.Question); question != "" {
		// A question suspends the brief: the rest of the fields are drafts at
		// best, and validating them would reject the ask itself.
		brief.Question = question
		brief.QuestionOptions = normalizeQuestionOptions(brief.QuestionOptions)
		brief.ServiceIntent = serviceIntent
		brief.Subharness = c.normalizeSubharness(brief.Subharness)
		return brief, nil
	}
	brief.QuestionOptions = nil
	if err := validateBrief(&brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	brief.Goal = anchorQualityWords(anchorGoal(brief.Goal, instruction), instruction)
	brief.Scale = reconcileScale(brief.Structure, normalizeScale(brief.Scale))
	brief.Title = normalizeTitle(brief.Title)
	brief.BuildsOn = normalizeBuildsOn(brief.BuildsOn)
	brief.TrialOf = normalizeTrialOf(graphContext, brief.TrialOf)
	brief.ServiceIntent = serviceIntent
	brief.Subharness = c.normalizeSubharness(brief.Subharness)
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

// compileReplyTokens sizes the compile reply's room by the ask it must echo:
// the goal restates the instruction verbatim, so the floor plus the ask's own
// bulk (tokens ≈ bytes/3, doubled for the goal's framing around it) is the
// least that cannot be cut mid-structure.
//
// The floor was 1000, sized for a reply, and on a reasoning model that is not
// what the number buys. max_tokens is the whole completion budget and the
// thinking is spent out of it first, so a short sentence naming a lot of
// separable work gets a budget it cannot even think inside: measured live on
// `~deepseek/deepseek-v4-flash-latest`, three ordinary multi-part asks came
// back `finish_reason:"length"`, `content:null`, with completion_tokens equal
// to the cap TO THE DIGIT — 1106 of 1106, 1100 of 1100, 1124 of 1124 — and the
// retry at double the room did the same. Nothing was truncated, because nothing
// was ever written; the whole budget went on reasoning nobody sees, the compile
// failed, and the compile is the one call whose loss forfeits the job.
//
// The bug was the quantity, not the size. A reply's length tracks how many
// pieces the WORK has, and the ask's own length says nothing about that: "six
// things:" is nine characters. Since a cap is a ceiling and not a purchase — a
// call that stops early is billed for what it wrote — the floor is now sized to
// the widest brief this prompt can legitimately produce rather than to the
// narrowest, and short asks pay nothing for the headroom they do not use.
// Measured successful compiles landed between 827 and 5565 completion tokens.
func compileReplyTokens(instruction string) int {
	const floor = 8000
	echo := 2 * len(instruction) / 3
	return floor + echo
}

// Scale values the compiler may emit. ScaleTask is also the degradation
// target for anything unrecognised: the safe default shape is one worker.
const (
	ScaleLookup  = "lookup"
	ScaleTask    = "task"
	ScaleProject = "project"
)

// Structure values, the structural reading scale must follow from.
const (
	StructureEnumerates  = "enumerates"
	StructureStratifies  = "stratifies"
	StructureOneJudgment = "one_judgement"
	StructureSingleAct   = "single_act"
)

// reconcileScale makes the label follow the reading, which is the one half of
// this decision that does not need a model. The prompt states the implication
// outright — a job that enumerates or stratifies is "project" — so a brief that
// answers "enumerates" and then writes "task" has contradicted itself, and 5.23
// says the deterministic half stays deterministic rather than being asked for
// twice and believed the second time.
//
// It only ever WIDENS, and the asymmetry is deliberate. "project" buys a
// planning pass over the work and commits nothing about how many workers run:
// the pass may still lay the job out as one leaf, which is the answer the
// narrow label was reaching for anyway. So a wrong widening costs one cheap
// call, while a wrong narrowing costs the structure of the job outright — there
// is no later pass that can rediscover the stages nobody was asked to lay out.
// A "lookup" is left alone: a reading that says the answer IS the deliverable
// is not describing work with parts, and promoting it would buy a plan for
// something with nothing to plan.
func reconcileScale(structure, scale string) string {
	if scale != ScaleTask {
		return scale
	}
	switch strings.ToLower(strings.TrimSpace(structure)) {
	case StructureEnumerates, StructureStratifies:
		return ScaleProject
	}
	return scale
}

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

// validateBrief rejects only what nothing downstream could work from: a goal
// with no words in it. Assumptions are OPTIONAL BY MEANING — a brief that made
// none is a complete brief — so a model that leaves the list out or writes a
// blank entry is tidied, never refused. It used to be refused: deepseek-flash
// answered a seven-tool ask with a faultless brief and no "assumptions" key,
// and the whole run ended at 58 seconds with "compile intent: missing
// assumptions", the same shape as the "empty budget" failure the Brief comment
// above records. Every reader of the list (the receipt, the working-decisions
// anchor) already treats nil and empty alike.
func validateBrief(brief *Brief) error {
	if strings.TrimSpace(brief.Goal) == "" {
		return errors.New("empty goal")
	}
	kept := brief.Assumptions[:0]
	for _, assumption := range brief.Assumptions {
		if assumption = strings.TrimSpace(assumption); assumption != "" {
			kept = append(kept, assumption)
		}
	}
	brief.Assumptions = kept
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
