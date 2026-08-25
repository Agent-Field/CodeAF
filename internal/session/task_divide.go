package session

// DIVISION: a worker saying "this is wider than one pair of hands".
//
// A task is groomed before anybody has opened the material. The brief says
// "bring the eleven adapters up to the new interface" and nobody yet knows
// whether that is an afternoon or a fortnight, because the thing that decides
// it — how much of the eleven is mechanical and how much is not — is only
// visible from inside. propose_task already lets a node hand out parts it can
// name up front (task.go's fan-out law). What was missing is the other moment:
// the worker halfway through, holding the material, that can now see the work
// is wider than the envelope it was given.
//
// So a worker gets one more verb. `divide_work` names the parts it has found
// and the evidence that revealed them, and the parts become session workers
// under it — the SAME nesting road propose_task admits into, because a second
// spawning road would be a second set of rules about worktrees, cancellation,
// spend and reports, and the person would have two kinds of child to learn.
//
// ── THE TWO GATES, AND THEY ARE THE ONES THAT WERE MEASURED ──
//
// Division is not free. Every part pays for a worktree, an audit and a wait,
// and the swarm bench measured what that costs when it buys nothing: on the
// eight-task corpus, dividing work that was never wide lost outright, and
// dividing work that was wide won by 1.15× to 1.95× of wall clock
// (bench/swarm/AB-REPORT.md). Two gates came out of it, and both of them are
// asked here, in this order:
//
//   - THE EVIDENCE GATE. Does what the worker found actually enumerate enough
//     independent items for the parts to beat one worker doing them in order?
//     internal/splitgate is that question, and it is the same function and the
//     same floor the resident's planner and leaves are gated by — one decision,
//     asked in three places, because two implementations of it would become two
//     answers. It reads the EVIDENCE and not the briefs: what is being weighed
//     is what the worker saw, not how convincingly it wrote the parts up.
//   - THE CAPACITY GATE. Is anybody free to pick the parts up? A division into
//     five parts on a machine where every lane is held is five worktrees, five
//     audits and five waits buying exactly no wall clock, because the parts
//     queue behind the lanes that are already full. THE LAW IS: DO NOT DIVIDE
//     WHAT NOBODY IS FREE TO PICK UP.
//
// A REFUSED DIVISION IS NOT A FAILURE. The worker reads the answer and carries
// on as one worker, which is what it was doing a moment ago — nothing is
// cancelled, nothing is spent, and the node journals, spends and lands exactly
// as it would have if it had never asked. That is the whole bargain that lets
// the road be on by default: the gate refuses small work for free.
//
// ── AND THEN ONE CALL THAT READS THE PLAN ──
//
// The two gates measure whether a division is WORTH IT, and neither of them
// reads the parts: the evidence gate weighs what the worker saw, and the
// capacity gate counts free lanes. But the parts are the thing that actually
// gets built. A part's brief is that worker's WHOLE WORLD — it never sees the
// conversation and cannot ask anybody anything — and every one of them was
// written by whichever model the parent task happens to be running on, which on
// a cheap crew is the cheap one.
//
// So a division that has passed both gates is put ONCE to the tier that thinks
// ([roles.RoleDivision]), which reads the evidence, the parent's own brief and
// all of the parts TOGETHER — the one thing the worker writing them could not do
// — and answers with those parts approved, amended, merged, or refused as not a
// division at all.
//
// IT FAILS OPEN. A reviewer that errors, that times out, or that answers
// something the contract does not allow admits the ORIGINAL parts unchanged. The
// division has already earned its way past two gates that were measured, and a
// flaky second opinion must not be able to turn the road off — the honest
// failure of a second opinion is not having one. That is the OPPOSITE posture
// from the route judge's confirm (route_judge.go), which fails closed, and the
// difference is what each of them stands in front of: the confirm holds back
// work that nothing has vouched for yet, and this one only improves work two
// measured gates already passed.
//
// ONE CALL AND NO REPAIR TURN, for the reason the route judge states about
// itself: this is a judgement nobody asked for, and a second call to rescue the
// first would be spending somebody's money to argue with it.
//
// ── HOW MUCH THINKING A PART IS DONE WITH ──
//
// The parts of one division are not all the same kind of work. Eleven adapters
// to move onto a new interface are eleven jobs whose failure mode is NOT BEING
// DONE YET, which anybody can see; the twelfth part — work out why the fixture
// races — has a failure mode of QUIET WRONGNESS, which a cheap model does not
// fail loudly at. So a part carries a `grade`, and it is the dividing worker's
// own reading: `mechanical` — the default, and what a part that says nothing is
// — keeps the parent task's model, and `careful` resolves through
// [roles.RoleCareful], which is the high tier.
//
// IT IS A GRADE AND NOT A MODEL NAME, and no file on this road writes a model id
// anywhere. The field says what KIND of work the part is and the registry
// answers with a model, so an install that has configured no tiers mints every
// part on the parent's own model exactly as it did before the field existed
// (internal/roles' ladder floors on the session's own model).
//
// ── WHAT THIS IS NOT ──
//
// The resident's `request_split` ENDS the leaf's run and lets the parts replace
// it (internal/exec's linear.go). This does not, and the difference is the
// session engine's own law rather than a shortcut: a v3 task's parent stays and
// coordinates. It holds its node open, every part's report re-enters it as the
// report lands, and it makes one deliverable and one branch out of them
// ([runTaskChild]'s tail loop). A parent that vanished into its parts would
// leave five branches racing for the person's, which is the arrangement
// task.go's nesting law exists to prevent.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/splitgate"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The division's two roles are registered here, beside the calls that make them
// (internal/roles states the open-registry law). They are a PAIR and are worth
// reading as one: the review is one answer that shapes every part, so it is on
// the tier whose whole definition is that; a careful part is many turns of
// ordinary work done well, so it is on the tier beside the auditor and the
// shaper, and never on the mastermind's.
func init() {
	roles.Register(roles.RoleDivision, roles.TierMastermind)
	roles.Register(roles.RoleCareful, roles.TierHigh)
}

// divideDescription is what the worker reads. THE FLOOR AND THE FAN CAP ARE
// INTERPOLATED for taskSchemaJSON's stated reason: a number a model reasons
// with must be the number the code enforces.
var divideDescription = "Hand the parts of THIS work out when the material turns out to be wider than one worker's share — you have opened it, and there are many independent items in front of you rather than the one job the brief described. Each part becomes a worker of its own under this task, in its own copy of the repository, and you stay: their reports come back to you and you make one deliverable out of them. USE IT ONLY FOR GENUINE WIDTH. The parts must be independent — nothing half-finished passing between them, no shared file being edited by two of them — and there must be enough of them to be worth it: this is refused unless your evidence names at least " + strconv.Itoa(splitgate.Floor) + " separate items, because below that a worker doing them in order is faster than paying for a copy of the repository, a check and a wait per part. Sequential work is never divided. Up to " + strconv.Itoa(taskFanLimit) + " parts. `evidence` is what you actually saw that revealed the width — the listing, the count, the search result — and it is what the decision is made on, so state the items and how many. Grade each part for the way it could go wrong: leave `grade` out for ordinary work, and set it to `" + gradeCareful + "` for a part that could look finished and be quietly wrong. If the answer is no, simply carry on with the work in your own hands; nothing is cancelled and nothing is lost."

// divideSchemaJSON is the wire schema. It is deliberately the SAME vocabulary
// the resident's `request_split` uses — parts, each with a title, a summary and
// a brief, plus the evidence that revealed them — so one idea has one shape
// across both products. What it adds is `acceptance` per part, because a
// session worker is finished by an auditor against a done-condition
// (task_audit.go) and a part admitted without one would be judged against
// nothing.
var divideSchemaJSON = `{"type":"object","properties":{` +
	`"evidence":{"type":"string","description":"WHAT YOU SAW that says this work is wider than one worker: the listing, the count, the search result, the names of the items. State how many there are and what they are. THE DECISION IS MADE ON THIS TEXT — a summary that says \"there is a lot here\" names nothing and is refused; \"the adapters directory holds 11 files, each with its own interface to move\" is the evidence"},` +
	`"parts":{"type":"array","minItems":2,"maxItems":` + strconv.Itoa(taskFanLimit) + `,"description":"The parts, each of which one worker could take from start to finished knowing nothing of what the others produced","items":{"type":"object","properties":` +
	`{"title":{"type":"string","description":"WHAT THIS PART IS CALLED, in ` + strconv.Itoa(TaskNameWords) + ` words or fewer — the role or the slice, never the instructions. It is read in a narrow column beside its siblings: \"the eleven adapters\", \"the invoice writer\". A name taken off the front of the brief names every part after how its instructions began"},` +
	`"summary":{"type":"string","description":"One or two lines: what this part does, and to what"},` +
	`"brief":{"type":"string","description":"THIS PART'S WHOLE WORLD. It never sees your conversation and cannot ask you anything: name the files and symbols, the conventions, what you have already learned about this material, and what NOT to touch because another part owns it"},` +
	`"acceptance":{"type":"string","description":"DONE WHEN — the observable done-condition somebody else could check without taking this part's word for it"},` +
	`"grade":{"type":"string","enum":["` + gradeMechanical + `","` + gradeCareful + `"],"description":"HOW THIS PART CAN GO WRONG, which is what decides how much thinking is spent on it. \"` + gradeMechanical + `\" is the default and is what most parts are: the failure mode is simply NOT BEING DONE YET — eleven files moved onto a new interface, nine sections written from material already gathered — and anybody looking at the result can see whether it happened. \"` + gradeCareful + `\" is a part whose failure mode is SUBTLE WRONGNESS: a design decision, a tricky piece of debugging, a judgement about somebody else's code, where the work can look finished and be quietly wrong. Grade for the failure mode and never for size or importance — a long dull part is ` + gradeMechanical + `, and a short part that has to be RIGHT is ` + gradeCareful + `"}},` +
	`"required":["title","summary","brief","acceptance"],"additionalProperties":false}}` +
	`},"required":["evidence","parts"],"additionalProperties":false}`

// divideArguments is the wire form.
type divideArguments struct {
	Evidence string       `json:"evidence"`
	Parts    []dividePart `json:"parts"`
}

type dividePart struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
	// Grade is how this part can go wrong, and it is the ONE field a part may
	// leave out. See the file header: a grade is a kind of work, never a model,
	// and an absent one is [gradeMechanical].
	Grade string `json:"grade,omitempty"`
}

// The two grades. They are spelled once, here, because four readers need them:
// the schema a model writes against, the parser that settles an absent one, the
// review's brief, and the mint that resolves a model from one (design-law §ONE
// SOURCE OF TRUTH).
const (
	gradeMechanical = "mechanical"
	gradeCareful    = "careful"
)

// careful reports whether this part is done by the high tier rather than by the
// model its parent task is on. ANYTHING THAT IS NOT THE WORD IS MECHANICAL —
// absent, blank, or a word nobody taught the model — because the cheap answer
// is the safe one to be wrong with, and refusing a whole division over a
// misspelt grade would fail the road shut on a detail nobody would think to
// look at.
func (p dividePart) careful() bool {
	return strings.EqualFold(strings.TrimSpace(p.Grade), gradeCareful)
}

// grade is the word this part carries as the review is shown it — the settled
// one, so a reviewer never reads a blank where the code reads "mechanical".
func (p dividePart) grade() string {
	if p.careful() {
		return gradeCareful
	}
	return gradeMechanical
}

// tidy trims one part and settles its grade to one of the two words. It is one
// function because the tool's own arguments and the reviewer's answer are the
// SAME shape read in two places, and two spellings of "what a part is" is how
// the reviewer comes to be allowed something the worker is not.
func (p dividePart) tidy() dividePart {
	p.Title = strings.TrimSpace(p.Title)
	p.Summary = strings.TrimSpace(p.Summary)
	p.Brief = strings.TrimSpace(p.Brief)
	p.Acceptance = strings.TrimSpace(p.Acceptance)
	p.Grade = p.grade()
	return p
}

// whole reports whether a part has everything a part must have. The tool's own
// parser says WHICH field is missing, because the worker can fix that and ask
// again; this is for the reviewer's answer, which nobody is going to repair.
func (p dividePart) whole() bool {
	return p.Title != "" && p.Summary != "" && p.Brief != "" && p.Acceptance != ""
}

// divideTools is the belt's division family: one verb, and only on a worker the
// road has been armed for.
//
// ABSENT AND NOT REFUSING, which is the law every conditional family on this
// belt is built on (tools.go). A worker whose task was never armed does not
// have the verb, so its prompt, its schema and its whole run are byte-identical
// to what they were before this file existed — which is the honest way to say
// "narrow work costs nothing", rather than putting a verb in front of a model
// that will be told no every time it reaches for it.
func (a *Agent) divideTools() []bare.Tool {
	if !a.mayDivide() {
		return nil
	}
	return []bare.Tool{{
		Name:        "divide_work",
		Description: divideDescription,
		Schema:      json.RawMessage(divideSchemaJSON),
		Execute:     a.divideWork,
	}}
}

// mayDivide says whether this agent may hand its work out in parts. Three
// things must all hold, and each rules out a different mistake: the road is on
// at all (Config.Divide), this agent is a worker that can have children
// ([Config.mayFanOut] — which is what keeps the verb off the floor of the tree,
// off an auditor and off a repair round), and THIS task was armed for division
// when it was admitted (see [Agent.armDivision]).
func (a *Agent) mayDivide() bool { return a.config.mayDivide() }

// mayDivide is the same question asked of a CONFIG, before there is an agent to
// ask, exactly as [Config.mayFanOut] is: [renderSystem] decides whether to tell
// this worker how to divide at construction, and the belt and the prompt must
// not disagree about whether it can.
func (c Config) mayDivide() bool {
	if !c.Divide || !c.mayFanOut() {
		return false
	}
	node := c.tasker.node(c.taskID)
	return node != nil && node.dividing()
}

// dividing reports whether this node was armed for division at admission.
func (n *TaskNode) dividing() bool {
	if n == nil {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.divide
}

// armDivision decides, ONCE, whether one piece of work is allowed to discover
// that it is wide. It is asked at admission by [TaskGraph.admit], which is the
// one door every task in this package comes through whoever opened it.
//
// THREE SIGNALS, AND ALL THREE ARE FREE — no model call is made here, because
// this question is asked of every task that is ever admitted. Each is one
// reader of the work saying it looks wider than one pair of hands, and they
// cover the three doors work comes through: the typed command, the model's own
// belt, and the text of the brief itself.
//
//   - THE JUDGE ALREADY SAID SO. `/task <brief>` asks a sizing judge whether
//     the work parallelizes (task_person.go's [Agent.judgeDecomposable]) and
//     then starts ONE WORKER whatever the answer was — a yes arms that worker
//     rather than opening a planner, and the surface says so in one dim line
//     (internal/tui3's taskcommand.go). THAT IS THE POINT OF THE WAVE, and it
//     is now the shape wide work takes by default: the upfront choice is gone
//     because a single worker that turns out to be holding six jobs can say so
//     from the material instead of from the brief.
//   - THE PROPOSER SAID SO IN ITS OWN WORDS. propose_task carries a `wide`
//     argument ([taskSpec.wide], task.go), and it is the model's half of the
//     same flip: a chat model that judged the work broad is answering the
//     question the sizing judge is asked at the typed door, with the whole
//     conversation in front of it rather than one sentence. THAT JUDGEMENT IS
//     THE YES. It starts one worker and arms it — never a planner — which is
//     what stops "this is a broad multi-source sweep" from being a reflex that
//     reaches past this road entirely.
//   - THE WORK'S OWN TEXT ENUMERATES ENOUGH ITEMS. This is
//     cmd/aforge/cooperative.go's plan-time gate asked of a task instead of a
//     plan: a brief that already names eleven adapter FILES is a brief that may
//     divide. It is what arms the road for work nobody ran the judge over — a
//     proposal the chat model groomed, `/task solo`, a person whose standing
//     answer is `single`. THE NOUN IS LOAD-BEARING AND A COUNT ALONE IS NOT
//     ENOUGH: internal/splitgate reads a number only where it stands beside one
//     of the eighteen item-nouns it knows, so "eleven adapters" counts ZERO and
//     "eleven adapter files" counts eleven. This signal therefore arms far less
//     work than its wording suggests, which is why it is the last of the three
//     and never the one a door should lean on by itself.
//
// Anything else is not armed, and a task that is not armed is byte-identical to
// a task from before this wave.
func (a *Agent) armDivision(spec taskSpec) bool {
	if a == nil || !a.config.Divide {
		return false
	}
	// ONLY ORDINARY WORK MAY DIVIDE, AND THE KIND IS ASKED BEFORE ANY OF THE
	// THREE SIGNALS ARE. A harness DESIGN is two model calls producing one JSON
	// page, in a room, with no worktree and nothing to hand out
	// (harness_task.go); a saved program's RUN has its steps written down
	// already (subharness_run.go). Neither has parts, and neither has a worker's
	// belt to put the verb on.
	//
	// IT MATTERS BECAUSE THE THIRD SIGNAL READS TEXT. A design goal saying "a
	// harness that checks the 8 endpoint files" enumerates enough items for
	// [splitgate.WorthIt], and a design thread satisfies mayFanOut — so without
	// this line the page writer would be handed divide_work the moment the road
	// was switched on, and it spawns workers in worktrees. The kind is settled
	// at admission and never changes (task_contract.go's [TaskKind]).
	if spec.kind() != "" {
		return false
	}
	if spec.wide {
		return true
	}
	if a.judgedDivisible(spec.request) || a.judgedDivisible(spec.brief) {
		return true
	}
	return enumeratesWidth(spec.title, spec.brief, spec.acceptance)
}

// enumeratesWidth is [Agent.armDivision]'s THIRD SIGNAL on its own: does this
// text already name enough separate items to be worth handing out?
//
// IT IS A FUNCTION AND NOT A LINE INSIDE armDivision BECAUSE UNATTENDED WORK HAS
// ONLY THIS SIGNAL. A standing order's firing is armed here too (standing_run.go),
// and it has neither of the other two: nobody typed `/task` for the sizing judge
// to answer, and no chat model wrote `wide` on a proposal — a firing's brief was
// compiled from the person's own sentence months ago and has been sitting in a
// file since. Two spellings of "what counts as wide text" would be two floors,
// and the day they disagreed the honest one would be whichever the reader in
// front of you did not use (design-law §ONE SOURCE OF TRUTH).
//
// The pieces are joined with newlines because that is how [splitgate.WorthIt]
// reads a plan: a number and its noun must stand together, and gluing a title
// onto the front of a brief invents adjacencies neither of them wrote.
func enumeratesWidth(pieces ...string) bool {
	return splitgate.WorthIt(strings.Join(pieces, "\n"))
}

// rememberDivisible banks a yes from the sizing judge against the exact text it
// was asked about. It is one entry, not a map: the judge is asked immediately
// before the work is started, by one command, and a bank that grew for the life
// of the session would be remembering answers about work that was never begun.
func (a *Agent) rememberDivisible(brief string) {
	a.mu.Lock()
	a.divisibleAsk = strings.TrimSpace(brief)
	a.mu.Unlock()
}

// judgedDivisible reports whether the sizing judge's last yes was about this
// text. The comparison is exact on the trimmed text rather than fuzzy, because
// a near-miss here would arm a road for work nobody judged.
func (a *Agent) judgedDivisible(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.divisibleAsk != "" && a.divisibleAsk == text
}

// ── the verb ────────────────────────────────────────────────────────────────

// divideWork is the tool's whole life: read the request, put it to the two
// gates, and — if both say yes — admit the parts on the nesting road.
//
// EVERY ANSWER IS AN ORDINARY TOOL RESULT and never a Go error, the way every
// other tool on this belt answers. A refusal is something the worker acts on by
// carrying on, and an error would end its turn over a question it was entitled
// to ask.
func (a *Agent) divideWork(ctx context.Context, args json.RawMessage) (string, bool, error) {
	parsed, problem := parseDivideArguments(args)
	if problem != "" {
		return problem, true, nil
	}
	graph := a.graph()
	parent := a.config.taskID

	// GATE ONE: THE EVIDENCE. What the worker SAW has to name enough separate
	// items for the parts to beat one worker doing them in order
	// (internal/splitgate carries the floor and the measurement behind it).
	if splitgate.Armed() && !splitgate.WorthIt(parsed.Evidence) {
		return divisionTooNarrow(parsed.Evidence), false, nil
	}
	// GATE TWO: THE FREE HANDS, AND IT IS THE LANES ONLY.
	//
	// Parts that can only queue behind lanes that are already full would still be
	// done one at a time, and paying for a copy of the repository apiece buys
	// nothing — that is the argument, and it is about the person's own
	// task.parallel cap and nothing else.
	//
	// THE MACHINE'S OWN HOLD IS NOT A REASON TO REFUSE A DIVISION, and this is
	// where the two stopped being one question. A busy machine is temporary and
	// the engine already knows how to wait for it: [TaskGraph.runFrontier] holds
	// a queued node on exactly this reading, announces it as `machine busy`, and
	// [TaskGraph.armPoll] lifts it by itself five seconds later. Work admitted
	// through propose_task has always been treated that way. A division that
	// refused instead made the DEFAULT ROAD FOR WIDE WORK fail closed on a
	// condition under which the exception merely queues — and it left the road
	// depending on a model remembering to ask again, which is the whole thing
	// armPoll exists so that nothing has to do.
	//
	// So the parts are admitted, the frontier holds them, and the receipt says
	// so ([divisionDone]).
	if graph.freeHands() <= 0 {
		return divisionNoLane(len(parsed.Parts), graph.laneLimit()), false, nil
	}

	// AND THEN THE PLAN IS READ, ONCE, BY THE TIER THAT THINKS. It comes after
	// both gates because it is the only step here that costs money: a division
	// the evidence does not support or that nobody is free to pick up is refused
	// for free, exactly as it always was, and a worker whose work is not wide
	// never reaches this line. What comes back is the parts as the reviewer
	// wants them — approved, sharpened, or merged — or a refusal. It FAILS OPEN;
	// see the file header for why, and [Agent.reviewDivision] for the whole of
	// what it can and cannot change.
	//
	// IT IS ALSO BEFORE THE SLOTS ARE CLAIMED, which is not an accident: a
	// review that merges five parts into three must be counted against the fan
	// cap as three, and a claim taken for a part the reviewer then merged away
	// would be a hand held for nobody.
	parts, refusal := a.reviewDivision(ctx, graph.node(parent), parsed)
	if refusal != "" {
		return refusal, false, nil
	}
	parsed.Parts = parts

	// THE SLOTS ARE TAKEN FOR THE WHOLE DIVISION BEFORE ANY OF IT IS ADMITTED.
	// A division is ONE decision: three parts admitted and a fourth refused by
	// the fan cap would leave the worker holding a shape nobody chose, so the
	// cap is met before anything exists rather than halfway through
	// ([TaskGraph.claimChild] states why the claim and not the count).
	taken := 0
	defer func() {
		for ; taken > 0; taken-- {
			graph.releaseChild(parent)
		}
	}()
	for range parsed.Parts {
		if refusal := graph.claimChild(parent); refusal != "" {
			return refusal, false, nil
		}
		taken++
	}

	// WHOSE WORK THIS IS, and what the person actually asked for. Both are the
	// nesting seam's own answers (task.go's proposeTask takes exactly these
	// three lines): the parts are registered under this node, one level deeper,
	// owned by this agent — so their worktrees branch off this one's and come
	// home into it — and each of them opens on the sentence the person typed,
	// inherited through [Agent.taskRequest] because there is nobody in a
	// worktree to type a new one.
	//
	// THE MODEL, AND THE OTHER MODEL. A part inherits the parent task's model,
	// which is what every child on this road has always done — same worker, same
	// job, somewhere quieter. What is new is that a part GRADED CAREFUL is minted
	// on the high tier instead ([roles.RoleCareful]), because the parts of one
	// division are not one kind of work: see the file header.
	//
	// BOTH ARE RESOLVED ONCE, ABOVE THE LOOP. The ladder reads settings and the
	// live conversation model, and a division whose third part resolved
	// differently from its first because something moved underneath it would be
	// a family nobody could account for afterwards.
	model := a.resolveTaskModel("").model
	careful := a.carefulModel(model)
	request := a.taskRequest()
	ids := make([]uint64, 0, len(parsed.Parts))
	titles := make([]string, 0, len(parsed.Parts))
	for _, part := range parsed.Parts {
		partModel := model
		if part.careful() {
			partModel = careful
		}
		spec := taskSpec{
			title: part.Title,
			// A MODEL WROTE THIS TITLE as a name, so the namer leaves it alone
			// (taskname.go).
			named:      true,
			summary:    part.Summary,
			request:    request,
			brief:      part.Brief,
			acceptance: part.Acceptance,
			model:      partModel,
			parent:     parent,
			depth:      a.config.taskDepth + 1,
			owner:      a,
		}
		id := graph.reserve()
		graph.admit(id, spec)
		taken--
		ids = append(ids, id)
		titles = append(titles, part.Title)
	}
	return divisionDone(ids, titles, graph.machineBusy()), false, nil
}

// parseDivideArguments reads one call and says, in plain words, what is wrong
// with it.
func parseDivideArguments(args json.RawMessage) (divideArguments, string) {
	var parsed divideArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return parsed, "Invalid arguments: " + err.Error()
	}
	parsed.Evidence = strings.TrimSpace(parsed.Evidence)
	if parsed.Evidence == "" {
		return parsed, "Invalid arguments: evidence is required — say what you actually saw that shows this work is wider than one worker, and how many separate items there are"
	}
	for i := range parsed.Parts {
		parsed.Parts[i] = parsed.Parts[i].tidy()
	}
	if len(parsed.Parts) < 2 {
		return parsed, "Invalid arguments: a division needs at least 2 parts — one part is the work you are already doing"
	}
	if len(parsed.Parts) > taskFanLimit {
		return parsed, fmt.Sprintf("Invalid arguments: %d parts is more than the %d one piece of work may be split into. Name the biggest %d and keep the rest in your own hands", len(parsed.Parts), taskFanLimit, taskFanLimit)
	}
	for i, part := range parsed.Parts {
		for _, missing := range []struct{ value, field string }{
			{part.Title, "title"},
			{part.Summary, "summary"},
			{part.Brief, "brief"},
			{part.Acceptance, "acceptance"},
		} {
			if missing.value == "" {
				return parsed, fmt.Sprintf("Invalid arguments: part %d has no %s, and every part needs one", i+1, missing.field)
			}
		}
	}
	return parsed, ""
}

// carefulModel is the model a part graded [gradeCareful] is minted on: the
// roles ladder's answer for [roles.RoleCareful], with the parent task's own
// model as the floor.
//
// THE FLOOR IS WHAT MAKES THE FIELD FREE. An install that has configured no
// tiers — which is every install until somebody opens the settings sheet —
// resolves this to the model it was handed, so a careful part is minted exactly
// where a mechanical one is and the grade costs nothing but a word on the wire
// (internal/roles' ladder floors on the session default). A failure to resolve
// is the same answer for the same reason: nothing here may invent a model id.
func (a *Agent) carefulModel(fallback string) string {
	model, err := roles.Resolve(roles.Source(a.config.RolesSource), roles.RoleCareful, fallback)
	if err != nil || strings.TrimSpace(model) == "" {
		return fallback
	}
	return model
}

// ── the plan, read once by the tier that thinks ─────────────────────────────

const (
	// divideReviewTokens is the reviewer's budget, and it is large because the
	// answer is the PARTS THEMSELVES: up to [taskFanLimit] briefs written out in
	// full. A cap that truncated the last part's brief would hand a worker half
	// a world.
	divideReviewTokens = 4000
	divideReviewTemp   = 0
	// divideReviewPatience bounds the wait. The tier's own bound is ten minutes
	// (roles.Patience) and that is the right figure for a planner nobody is
	// waiting on; here a worker is mid-turn with its own steps ticking and its
	// parts not yet existing, so this one is nearer. It is deliberately generous
	// against the alternative — a division that never happened is worse than a
	// division that started three minutes late — and it is bounded rather than
	// absent because failing open means waiting longer buys nothing at all.
	divideReviewPatience = 3 * time.Minute
	// divideReviewBriefBytes and divideReviewEvidenceBytes bound the two things
	// the reviewer is shown that it cannot amend. THE PARTS THEMSELVES ARE NEVER
	// CLIPPED: they are what is being reviewed, and a reviewer amending a brief
	// whose ending it never saw would be writing over work it could not read.
	divideReviewBriefBytes    = 6000
	divideReviewEvidenceBytes = 4000
	// divideRefusalBytes bounds the one line of the reviewer's own that reaches
	// the worker's receipt. It is a sentence and not a paragraph: what the
	// worker does next is carry on, and a refusal that argued its case at length
	// would be a page of reading in front of an answer that is already made.
	divideRefusalBytes = 200
)

// divideReviewBrief is what the reviewer is told. THE FAN CAP IS INTERPOLATED
// for the reason [divideDescription] states: a number a model reasons with must
// be the number the code enforces.
//
// IT TEACHES ONE THING THE WORKER COULD NOT KNOW: what the parts look like side
// by side. The worker wrote them one after another out of material it was
// holding; overlaps, a part that is really a second stage, and a brief that
// assumes what its own author knew are all invisible from there and obvious
// from here.
var divideReviewBrief = `A worker part-way through a piece of work has decided it is wider than one pair of hands, and has written the parts it wants to hand out. You read the whole division ONCE and answer for it.

Each part becomes a worker of its own, in its own copy of the repository. It never sees this conversation, it cannot ask anybody anything, and its brief is the only thing it will ever know about why it exists. Whatever you leave in a brief is that worker's whole world.

READ THE PARTS TOGETHER, WHICH IS THE ONE THING THEIR AUTHOR COULD NOT DO:

  - two parts that would edit the same file, or whose scopes overlap. Fix the boundary in both briefs, or merge them into one part.
  - a part that cannot start until another has finished. That is a stage and not a part: merge it into the part it waits on.
  - a brief that assumes what its author knew. Name the files, the symbols, the conventions, and what NOT to touch because another part owns it.
  - a done-condition somebody else could not check without taking the part's own word for it.

Answer with ONE JSON object and nothing else — no prose, no code fence:

  {"parts": [{"title": "...", "summary": "...", "brief": "...", "acceptance": "...", "grade": "` + gradeMechanical + `"}, ...]}

Those are the parts that will exist, in the shape you give them. Repeat a part unchanged to approve it, rewrite its brief to sharpen it, give one part where there were two to merge them. Write every field on every part, the grade included — a part you leave a field off is a part that loses it. At least 2 parts and at most ` + strconv.Itoa(taskFanLimit) + `.

  grade  "` + gradeCareful + `" for a part whose failure mode is subtle wrongness — a design decision, tricky debugging, a judgement about somebody else's code — and "` + gradeMechanical + `" for a part whose failure mode is simply not being done yet. It decides how much thinking that part is done with, so it is worth being right about in both directions.

REFUSE ONLY WHEN THIS IS NOT A DIVISION AT ALL — the parts are stages of one procedure, or there is really one job here:

  {"refuse": true, "why": "one line, in a person's own words"}

Refusing throws nothing away: the worker carries on with the work in its own hands. It is not the answer to a division you would have written differently — amend that one instead.`

// divideReview is the reviewer's whole vocabulary: the parts as it wants them,
// or a refusal. An empty one is neither, and is read as "no answer" — which
// admits the original parts (see [Agent.reviewDivision]).
type divideReview struct {
	Refuse bool         `json:"refuse"`
	Why    string       `json:"why"`
	Parts  []dividePart `json:"parts"`
}

// reviewDivision is the one mastermind call a division makes. It answers the
// parts to admit and an empty refusal, or no parts and the sentence the worker
// is told instead.
//
// EVERY FAILURE ADMITS THE ORIGINAL PARTS. No model client, a role that will not
// resolve, a call that errors or times out, a reply that is not JSON, an answer
// with one part or with more than the fan cap, a part missing a field: each of
// those is this call absent rather than a division refused. The file header
// argues why, and it is the same shape the auditor and the namer keep — a second
// opinion that cannot be had is not a verdict.
func (a *Agent) reviewDivision(ctx context.Context, parent *TaskNode, parsed divideArguments) ([]dividePart, string) {
	ctx, cancel := context.WithTimeout(ctx, divideReviewPatience)
	defer cancel()

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	response, reviewer, err := a.callRole(ctx, roles.RoleDivision, model,
		[]ai.Message{
			textMessage("system", divideReviewBrief),
			textMessage("user", divideReviewQuestion(parent, parsed)),
		},
		ai.WithMaxTokens(divideReviewTokens),
		ai.WithTemperature(divideReviewTemp))
	if err != nil || response == nil {
		return parsed.Parts, ""
	}
	// The person pays for it, out of the pocket every auxiliary call comes from,
	// against the model that actually answered.
	a.addAuxiliaryUsage(response, reviewer, 1)

	raw, err := subharness.Salvage(response.Text())
	if err != nil {
		return parsed.Parts, ""
	}
	var review divideReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return parsed.Parts, ""
	}
	if review.Refuse {
		return nil, divisionNotAsWritten(review.Why)
	}
	parts := make([]dividePart, 0, len(review.Parts))
	for _, part := range review.Parts {
		part = part.tidy()
		if !part.whole() {
			// A part with a field missing is an answer nobody can act on, and
			// there is no repair turn here to ask for it back.
			return parsed.Parts, ""
		}
		parts = append(parts, part)
	}
	if len(parts) < 2 || len(parts) > taskFanLimit {
		// A SHAPE THE CONTRACT DOES NOT ALLOW IS NOT A REFUSAL, even where it
		// reads like one. Merging everything into a single part is how a
		// reviewer says "one job" without saying it, and it is exactly the
		// answer the brief asks it to spell as `refuse` — reading it as a
		// refusal here would put a whole road behind a model's phrasing, which
		// is what the fail-open law exists to stop.
		return parsed.Parts, ""
	}
	return parts, ""
}

// divideReviewQuestion is the division as the reviewer reads it: the work it
// came out of, what the worker saw, and every part in full.
func divideReviewQuestion(parent *TaskNode, parsed divideArguments) string {
	var out strings.Builder
	if parent != nil {
		out.WriteString("THE WORK BEING DIVIDED:\n")
		out.WriteString(clip(strings.TrimSpace(parent.instruction()), divideReviewBriefBytes))
		out.WriteString("\n\n")
	}
	out.WriteString("WHAT THE WORKER SAW THAT MADE IT DIVIDE:\n")
	out.WriteString(clip(parsed.Evidence, divideReviewEvidenceBytes))
	out.WriteString("\n\nTHE PARTS IT WANTS TO HAND OUT:")
	for i, part := range parsed.Parts {
		fmt.Fprintf(&out, "\n\n%d. %s\n   what it is: %s\n   grade: %s\n   brief: %s\n   done when: %s",
			i+1, part.Title, part.Summary, part.grade(), part.Brief, part.Acceptance)
	}
	out.WriteString("\n\nAnswer with one JSON object.")
	return out.String()
}

// ── what the worker is told, in words a person could read over its shoulder ──
//
// THE VOCABULARY LAW (CLAUDE.md): nothing a person reads may say gate, quorum,
// starvation or arm. These four sentences are written for the worker AND for
// the journal a person opens afterwards, which is why they say "split into
// three parts" and "waiting for a free hand" and never name the machinery that
// decided.

// divisionTooNarrow is the answer to a division the evidence does not support.
// It says the number back, because the worker's next move depends on whether it
// under-counted what it saw or genuinely has narrow work in front of it.
func divisionTooNarrow(evidence string) string {
	return fmt.Sprintf(
		"not split: what you found names %d separate items, and work is only split at %d or more — below that one worker doing them in order is faster than a copy of the repository, a check and a wait for each part. Carry on with the work in your own hands. If there really are more items than that, say what they are and how many, and ask again.",
		splitgate.Items(evidence), splitgate.Floor)
}

// divisionNoLane is the answer to a division there is no LANE for: this
// session's own task.parallel cap has nothing free beside the worker asking.
//
// IT SAYS WHETHER ASKING AGAIN COULD EVER HELP, because that is the only thing
// the worker's next move turns on and the two cases are genuinely different
// facts. With a cap above one, a lane frees the moment something finishes and
// the work really can be split in a minute. With a cap of exactly one there is
// no second pair of hands in this session at all and there never will be, so
// telling a worker to ask again later would be sending it back to a door that
// is never going to open (the asker's own lane is deliberately not counted —
// see [TaskGraph.freeHands]).
//
// It does NOT say a waiting part costs a copy of the repository, because that is
// not true: a worktree is made when a part STARTS ([Agent.workTaskNode]) and
// never when it is admitted. What the cap is really protecting against is a
// division whose parts would run one at a time anyway, which is a division that
// bought no time and paid three worktrees for it.
func divisionNoLane(parts int, limit int) string {
	if limit == 1 {
		return fmt.Sprintf(
			"not split: this session runs one task at a time, so there is no second pair of hands to give the %d parts to — they would be done one after another exactly as you would do them, and each would cost its own copy of the repository. Carry on with the work in your own hands; asking again will not change this.",
			parts)
	}
	return fmt.Sprintf(
		"not split: every lane is busy right now, so the %d parts would be done one at a time anyway and each would cost its own copy of the repository. Carry on with the work in your own hands; ask again once something finishes, if it is still too wide for one.",
		parts)
}

// divisionNotAsWritten is the answer to a division the reviewer read as one job
// rather than several.
//
// IT IS THE GATES' OWN ENDING AND A SENTENCE OF ITS OWN. The ending is the one
// thing that must not be new: an ordinary tool result, nothing admitted,
// nothing cancelled, nothing spent, and a worker that carries straight on with
// the work in its own hands — which is what [divisionTooNarrow] and
// [divisionNoLane] both do and what the whole road's bargain rests on. The
// SENTENCE has to be its own, because the other two say why in terms of a count
// and a lane, and telling a worker "what you found names 3 separate items" over
// a plan that was refused for overlapping scopes would send it back to fix a
// number that was never the problem.
//
// It says the reviewer's own line where there is one, because that is the only
// part of this the worker can act on: it is the difference between "this is one
// job" and "parts 2 and 3 are the same file".
func divisionNotAsWritten(why string) string {
	line := clip(firstLine(strings.TrimSpace(why)), divideRefusalBytes)
	if line == "" {
		return "not split: read together, the parts are one job rather than several. Carry on with the work in your own hands; nothing is cancelled and nothing is lost."
	}
	return "not split: read together, the parts are one job rather than several — " +
		line + ". Carry on with the work in your own hands; nothing is cancelled and nothing is lost."
}

// divisionDone is the receipt: what the work was split into, and what happens
// next. It says the parts do not have to be waited for, because they do not —
// this node stays open and every report is put in front of it as it lands
// ([runTaskChild]'s tail loop) — and a worker that sat on a wait would spend its
// whole allowance doing nothing.
//
// AND IT SAYS WHEN THE PARTS ARE NOT STARTING YET. A machine carrying more than
// the load or the memory floor allows holds every new node
// ([TaskGraph.runFrontier]), so the parts are admitted and waiting rather than
// working — and a receipt that said nothing about it would have a worker reading
// "split into three parts" while three cards sat still. It lifts by itself
// ([TaskGraph.armPoll]), which is the half worth saying: there is nothing for
// the worker to do about it and nothing for it to come back and re-ask.
func divisionDone(ids []uint64, titles []string, machineBusy bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "split into %d parts:", len(ids))
	for i, id := range ids {
		fmt.Fprintf(&out, "\n  %d — %s", id, titles[i])
	}
	out.WriteString("\nEach works from its own brief, in its own copy of the repository, and its branch comes home into yours. Keep working — do not wait for them; each report arrives here when it lands, and this work is not finished until you have folded them into one deliverable.")
	if machineBusy {
		out.WriteString("\nThis machine is busy right now, so the parts are waiting for it rather than working. They start themselves as soon as it clears; there is nothing for you to do about that and nothing to come back for.")
	}
	return out.String()
}

// ── how many hands are free ─────────────────────────────────────────────────

// freeHands is how many more workers this session's own cap could START right
// now, not counting the one asking. It is the session engine's answer to the
// resident's slot probe (internal/resident's runner.go), asked of the ceiling a
// person set ([TaskGraph.runFrontier] holds a ready node on the same one, as
// `slot`).
//
// THE ASKER'S OWN LANE IS NOT COUNTED, and that is the whole difference between
// this being a real gate and being a formality. A dividing worker hands its
// lane back the moment it starts waiting on its parts ([TaskGraph.park]), so
// counting it would mean a session capped at one task always had "a free hand"
// — and its parts would then run one at a time while the parent waited, which
// is a division that bought nothing and paid for three worktrees.
//
// NO CAP MEANS HANDS ENOUGH. With task.parallel unset there is no count to
// subtract from, so the answer is the most parts one piece of work could ever
// ask for.
//
// IT ASKS THE LANES AND NOT THE MACHINE, and the two used to be one answer here.
// A hand held by the person's cap and a hand held by a loaded box are different
// facts with different endings: the cap is a standing decision about how much
// this session may run at once, and the machine is a passing condition the
// frontier already waits out and lifts by itself ([TaskGraph.armPoll]). Folding
// the second one into this count made a division fail closed where the same
// reading merely queues a proposal, and made the refusal say "no free hand" over
// a session whose lanes were all empty. The machine is asked separately, by
// [TaskGraph.machineBusy], and it is not a refusal.
func (g *TaskGraph) freeHands() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.limit <= 0 {
		return taskFanLimit
	}
	if free := g.limit - g.running; free > 0 {
		return free
	}
	return 0
}

// machineBusy is the admission governor's own reading: this box is carrying more
// than the load or the memory floor allows, so nothing new starts until it
// clears (task_pressure.go).
//
// Asked before any lock, exactly as [TaskGraph.runFrontier] asks it: it is two
// small file reads behind a one-second cache, and no reading of /proc belongs
// under the lock that everything announcing a node holds.
func (g *TaskGraph) machineBusy() bool {
	return g != nil && g.governor.holds()
}

// laneLimit is the person's own task.parallel cap, or 0 for no cap. It is read
// for one reason: a refusal has to know whether a lane could ever come free
// ([divisionNoLane]), and a cap of exactly one says it cannot.
func (g *TaskGraph) laneLimit() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
}
