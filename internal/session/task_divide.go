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
// ── AND ONE PLACE THE EVIDENCE GATE IS NOT THE LAST WORD ──
//
// The floor was measured on items a counter can see: a number standing beside
// a plural that is not a measure (internal/splitgate). Some work is wide in a
// shape any counter is blind to. Four whole ISSUES in one request are four
// ownable jobs with four done-conditions, and they enumerate NOTHING; so do four
// modules, which the bench measured losing. No amount of teaching a free counter
// over free text tells those two apart, and a floor tuned until it did would be
// a floor tuned to a benchmark's phrasing.
//
// So the floor does not move. What the road
// has instead is a TIEBREAK, and it fires on a disagreement rather than on a
// phrase: the evidence gate refuses on the count AND a model that read this
// request already said the work was broad ([taskSpec.armed] holds which reader
// armed the task, and [TaskNode.armedByJudgement] is the test). Two readers
// contradicting each other is not an answer, so the division is put to the one
// reader that can settle it — the DIVISION REVIEWER below, which was going to
// read these parts anyway, on the merits it already weighs. A floor refusal on
// UNARMED work, or on work the counter itself armed, is untouched: free, final,
// and one reader never gets to disagree with itself at a mastermind's price.
//
// THE CAPACITY GATE IS UNTOUCHED BY ALL OF IT and still binds after the
// reviewer's yes: nothing here can divide work nobody is free to pick up.
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
//
// IT IS WRITTEN FOR DENSITY, for the reason [taskDescription] states about
// itself: this string is marshalled into the tool block in front of every
// request of every turn a divided worker takes, so it says each rule once and
// leaves the teaching to the field it governs — the evidence field says what
// evidence is, and this preamble no longer says it a second time.
var divideDescription = "Hand the parts of THIS work out when the material turns out wider than one worker's share. Each part becomes a worker of its own under this task, in its own copy of the repository, and you stay to make one deliverable out of their reports. ONLY FOR GENUINE WIDTH: the parts must be independent — nothing half-finished passing between them, and no file two of them name, which is refused outright — and this is refused unless your evidence names at least " + strconv.Itoa(splitgate.Floor) + " separate items, below which doing them in order beats paying for a copy of the repository, a check and a wait per part. Sequential work is never divided. Up to " + strconv.Itoa(taskFanLimit) + " parts. Grade each part for the way it could go wrong: leave `grade` out for ordinary work, set it to `" + gradeCareful + "` for a part that could look finished and be quietly wrong. If the answer is no, carry on in your own hands; nothing is cancelled and nothing is lost."

// divideSchemaJSON is the wire schema. It is deliberately the SAME vocabulary
// the resident's `request_split` uses — parts, each with a title, a summary and
// a brief, plus the evidence that revealed them — so one idea has one shape
// across both products. What it adds is `acceptance` per part, because a
// session worker is finished by an auditor against a done-condition
// (task_audit.go) and a part admitted without one would be judged against
// nothing.
//
// ITS FIELD DESCRIPTIONS ARE WRITTEN FOR DENSITY, for the reason
// [divideDescription] states: they ride in front of every request, so each rule
// is stated once and the worked examples that merely restated a rule are gone.
var divideSchemaJSON = `{"type":"object","properties":{` +
	`"evidence":{"type":"string","description":"WHAT YOU SAW that says this work is wider than one worker: the listing, the count, the search result: how many items and what they are. THE DECISION IS MADE ON THIS TEXT — \"there is a lot here\" names nothing and is refused; \"the adapters directory holds 11 files, each with its own interface to move\" is evidence"},` +
	`"parts":{"type":"array","minItems":2,"maxItems":` + strconv.Itoa(taskFanLimit) + `,"description":"The parts, each of which one worker could take from start to finished knowing nothing of what the others produced","items":{"type":"object","properties":` +
	`{"title":{"type":"string","description":"WHAT THIS PART IS CALLED, in ` + strconv.Itoa(TaskNameWords) + ` words or fewer — the role or the slice, never its instructions. It is read in a narrow column beside its siblings: \"the eleven adapters\""},` +
	`"summary":{"type":"string","description":"One or two lines: what this part does, and to what"},` +
	`"brief":{"type":"string","description":"THIS PART'S WHOLE WORLD. It never sees your conversation and cannot ask you anything: name the files and symbols, the conventions, what you have learned about this material, and what NOT to touch because another part owns it"},` +
	`"acceptance":{"type":"string","description":"DONE WHEN — the observable done-condition somebody else could check without taking this part's word for it"},` +
	`"grade":{"type":"string","enum":["` + gradeMechanical + `","` + gradeCareful + `"],"description":"HOW THIS PART CAN GO WRONG, which decides how much thinking it is done with. \"` + gradeMechanical + `\", the default and most parts: the failure mode is NOT BEING DONE YET, visible to anybody looking at the result. \"` + gradeCareful + `\": the failure mode is SUBTLE WRONGNESS — a design decision, tricky debugging, a judgement about somebody else's code — where the work can look finished and be quietly wrong. Grade for the failure mode, never size or importance: a long dull part is ` + gradeMechanical + `, a short part that must be RIGHT is ` + gradeCareful + `"},` +
	expectsSchemaJSON + `},` +
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
	// Grade is how this part can go wrong, and it is one of the two fields a
	// part may leave out. See the file header: a grade is a kind of work, never
	// a model, and an absent one is [gradeMechanical].
	Grade string `json:"grade,omitempty"`
	// Expects is what THIS PART'S brief assumes is already true of the folder
	// its worker will get, checked before that worker spends anything
	// (handoffcontract.go). It is the other optional field, and it is the one
	// the run this road was measured on needed most: the briefs named files and
	// symbols of a world their workers were never given.
	Expects []Expectation `json:"expects,omitempty"`
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
	return n.armedBy() != ""
}

// armedBy is WHICH READER armed this node, or an empty string for work that was
// never armed at all. It is [taskSpec.armed] read under the graph's lock, which
// is the only way anything outside admission may read it.
func (n *TaskNode) armedBy() string {
	if n == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.armed
}

// armedByJudgement reports whether a MODEL READING THE WORK is what armed this
// task, rather than the work's own text being counted.
//
// IT IS THE TIEBREAK'S WHOLE TRIGGER, and the distinction it draws is the reason
// the tiebreak is not simply "armed work gets a second chance". The floor that
// refuses a division is [splitgate.WorthIt] counting items, and [armedCounted]
// is that same function counting the same way over the brief — so a floor
// refusal on work the text armed is ONE READER AGREEING WITH ITSELF, and
// spending a model call to ask whether it meant it would be paying to hear an
// answer twice. [armedWide] and [armedJudged] are a different reader entirely: a
// model read the request and said the work was broad, and a free counter has now
// said it is not. THAT is a disagreement, and it is the only thing worth a call.
func (n *TaskNode) armedByJudgement() bool {
	switch n.armedBy() {
	case armedWide, armedJudged:
		return true
	}
	return false
}

// takeTiebreak reports whether this node may put a below-floor division to the
// reviewer, and takes the right to as it answers: ONCE PER TASK AND NEVER AGAIN.
//
// IT IS BOUNDED BECAUSE THE REFUSAL INVITES A SECOND ASK. [divisionTooNarrow]
// tells the worker to come back if there really are more items than it said, and
// that invitation is right — but on judge-armed work every retry would now reach
// a mastermind, so a worker that kept asking would spend somebody's money arguing
// with a reader that has already read this work. One is the same number the rest
// of this road settles on for the same reason: the review is one call and there
// is no repair turn behind it.
//
// IT IS SPENT BY AN ANSWER AND GIVEN BACK ON SILENCE. The bound exists to stop
// a worker paying to argue with a reader that has already read this work — and
// a reviewer that never answered has read nothing, so treating its timeout as
// the answer turned one slow endpoint into a permanent refusal (a live cell
// did exactly that on 2026-08-28: three minutes of provider silence spent the
// task's one adjudication, and the retry with better evidence met the counter
// alone). So the right is taken here, before the call, and refunded by
// [Agent.divideOnce] when no answer of any kind came back. What keeps the
// refund from becoming an unbounded run of attempts is the ladder itself: an
// adjudication's fall-through rung is the session's own model
// ([Agent.callRole]), which is alive by construction, so a review that cannot
// be had twice running means the session itself has lost its model — and that
// worker has larger problems than its division.
func (n *TaskNode) takeTiebreak() bool {
	if n == nil {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.adjudicated {
		return false
	}
	n.adjudicated = true
	return true
}

// refundTiebreak gives back the adjudication [TaskNode.takeTiebreak] took,
// because the reviewer it was taken for never answered — see that function for
// why silence must not spend it.
func (n *TaskNode) refundTiebreak() {
	if n == nil {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.adjudicated = false
}

// The three words [Agent.armDivision] can answer with, one per signal. They are
// written down as constants because four readers share them: the arming itself,
// the tiebreak that tells a model's reading from the text gate's, the row this
// project's record keeps of what a task was allowed to do, and the tests that
// pin all three.
//
// THEY ARE WORDS A PERSON CAN READ, because one of those readers is a file
// somebody opens (task_index.go). "wide", "judged" and "counted" say who
// decided; "armed", which is what this package calls the act, is machinery and
// stays in the code (CLAUDE.md's vocabulary law).
const (
	// armedWide is a MODEL'S OWN READING OF BREADTH: propose_task's `wide`, the
	// route judge's verdict, the ceiling's measured evidence that one turn
	// outran one pair of hands.
	armedWide = "wide"
	// armedJudged is the sizing judge's yes at the typed `/task` door.
	armedJudged = "judged"
	// armedCounted is the work's OWN TEXT naming enough separate items —
	// [splitgate.WorthIt] over the brief, which is the same counter the evidence
	// gate uses on the evidence.
	armedCounted = "counted"
)

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
//     answer is `single`. THE PILE IS LOAD-BEARING AND A COUNT ALONE IS NOT
//     ENOUGH: internal/splitgate reads a number only where a plural that is
//     not a measure stands with it, so "eleven adapters" counts eleven and
//     "keep it under 250 words" counts zero — the gate knows the closed class
//     of measures rather than an open list of things people have piles of,
//     precisely so a domain nobody anticipated ("34 person-rows") still
//     counts. The counting is free text over free prose all the same, which is
//     why this signal is the last of the three and never the one a door should
//     lean on by itself.
//
// Anything else is not armed, and a task that is not armed is byte-identical to
// a task from before this wave.
//
// IT ANSWERS WHICH SIGNAL AND NOT MERELY WHETHER, and an empty string is no.
// The word is kept on the spec because two later readers need to know which of
// the three it was: the tiebreak, which only reconsiders a floor refusal where a
// MODEL said the work was broad ([TaskNode.armedByJudgement]), and the project's
// own record, which otherwise cannot tell a task that refused to divide from one
// that was never allowed to (task_index.go).
func (a *Agent) armDivision(spec taskSpec) string {
	if a == nil || !a.config.Divide {
		return ""
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
		return ""
	}
	if spec.wide {
		return armedWide
	}
	if a.judgedDivisible(spec.request) || a.judgedDivisible(spec.brief) {
		return armedJudged
	}
	if enumeratesWidth(spec.title, spec.brief, spec.acceptance) {
		return armedCounted
	}
	return ""
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

// The two askers a division can have, and the five ways one ends. They are
// written down because the JOURNAL is what a bench reads (sessionfile.go's
// [journalDivision]), and a decision spelled two ways is two decisions to
// whatever is counting.
//
// THE GATE IS NAMED IN THE REFUSAL and not merely the fact of one, because the
// refusals are different findings about the same work: `floor` says the material
// does not enumerate enough to pay for parts, `lane` says nobody was free to pick
// them up, `scope` says two parts claimed the same path, `review` says a
// mastermind read the parts together and saw one job,
// and `nobody` says it read them and saw work no worker can do at all. A record
// that spelled them all "refused" could not tell a road that is working from a
// road that is switched off by a busy machine, and could not tell either of
// those from a task that is now sitting on a person.
const (
	divisionByWorker = "worker"
	divisionBySketch = "sketch"

	divisionAdmitted         = "admitted"
	divisionRefusedMalformed = "refused:arguments"
	divisionRefusedFloor     = "refused:floor"
	// divisionRefusedUnreviewed is a below-floor division on judge-armed work
	// that the reviewer never answered — reached or not, sense or not. The
	// person is told the floor's refusal; the record says the review is what
	// failed, and Error says how.
	divisionRefusedUnreviewed = "refused:review-unreached"
	divisionRefusedLane       = "refused:lane"
	divisionRefusedCap        = "refused:cap"
	divisionRefusedReview     = "refused:review"
	// divisionRefusedScope is two parts claiming the same path
	// (task_divide_scope.go). It is its own word because it is the one refusal
	// here that says the division was RIGHT and its boundaries were wrong: a
	// bench counting `refused:review` beside it can tell a road that read the
	// work as one job from a road that would have handed it out happily if the
	// briefs had drawn the line anywhere.
	divisionRefusedScope = "refused:scope"
	// divisionRefusedNobody is the reviewer saying the remainder is not work for
	// any worker — the approving review only a person may give, the credential
	// nobody here holds, the decision that is the person's to make. It is a
	// SEPARATE WORD FROM `review` because it has a separate ending: an ordinary
	// refusal leaves one worker carrying on, and this one stops a worker being
	// spent at all ([Agent.landNeedsPerson]).
	divisionRefusedNobody = "refused:nobody"
)

// divideWork is the tool's whole life, and it is a WRAPPER because the life is
// shared: the same request, the same gates and the same admission are reached by
// a worker calling the verb and by the harness submitting a drawing on a worker's
// behalf (task_divide_sketch.go). One body, so there is exactly one set of rules
// about what a division costs and what it is allowed to do.
//
// EVERY ANSWER IS AN ORDINARY TOOL RESULT and never a Go error, the way every
// other tool on this belt answers. A refusal is something the worker acts on by
// carrying on, and an error would end its turn over a question it was entitled
// to ask.
func (a *Agent) divideWork(ctx context.Context, args json.RawMessage) (string, bool, error) {
	// THE WORKER'S OWN ASK HAS NO USE FOR THE THIRD ANSWER, and that is a fact
	// about when it is asked rather than an omission. A human-only finding stops a
	// worker being STARTED (task_divide_sketch.go); this caller is a worker already
	// mid-turn, whose money is already being spent, and the honest thing to do with
	// it is tell it — which [divisionNeedsPerson] does, in the answer.
	answer, _, malformed := a.divideOnce(ctx, args, divisionByWorker)
	return answer, malformed, nil
}

// divideOnce reads the request, puts it to the two gates, and — if both say yes —
// admits the parts on the nesting road. It answers what the asker is told, THE
// PERSON'S OWN JOB where the reviewer said there is one, and whether what it read
// was malformed.
//
// THE SECOND ANSWER IS EMPTY ON EVERY ROAD BUT ONE. It carries the reviewer's own
// sentence when it said that what is left cannot be done by any worker
// ([divideReview.Nobody]), and it is the only thing on this road that can stop a
// worker being started at all — which is why it is a value handed back to the
// caller rather than a decision taken here: this function does not know whether
// its asker is a worker already running or a node that has not begun
// (task_divide_sketch.go).
//
// AND IT WRITES DOWN WHAT IT DECIDED, once, on every road out. The line is the
// last thing this function does whatever happened, which is why it is a deferred
// write over one record rather than a call at each ending: five endings and five
// call sites is four chances to add a sixth ending and forget (sessionfile.go's
// [journalDivision] carries what the record is for).
func (a *Agent) divideOnce(ctx context.Context, args json.RawMessage, source string) (string, string, bool) {
	parent := a.config.taskID
	line := journalDivision{TaskID: parent, Source: source}
	defer func() { a.file.appendDivision(line) }()

	parsed, problem := parseDivideArguments(args)
	for _, part := range parsed.Parts {
		line.Parts = append(line.Parts, part.Title)
	}
	if problem != "" {
		line.Decision = divisionRefusedMalformed
		return problem, "", true
	}
	line.Requested = len(parsed.Parts)
	graph := a.graph()

	// GATE ONE: THE EVIDENCE. What the worker SAW has to name enough separate
	// items for the parts to beat one worker doing them in order
	// (internal/splitgate carries the floor and the measurement behind it).
	//
	// AND ITS NO IS FINAL EXCEPT WHERE TWO READERS DISAGREE. The counter sees a
	// number standing beside a plural that is not a measure (internal/splitgate
	// says exactly what that means and why it fails open on domains it has
	// never met). Four whole ISSUES — each a complete ask with its own
	// done-condition, none of them counted — are still four ownable jobs, and
	// there is no honest way for a free counter over free text to tell that
	// case from the four modules the bench measured losing. So the floor does
	// not move. What changes is only WHOSE ANSWER IS LAST when the two readings
	// of this work contradict each other:
	//
	//   - ON UNARMED WORK, AND ON WORK THE COUNTER ITSELF ARMED, NOTHING MOVES.
	//     The refusal is free, final, and exactly what it always was — which is
	//     the bargain that lets this road be on by default.
	//   - WHERE A MODEL READ THE REQUEST AND SAID IT WAS BROAD, the free counter
	//     saying no is a disagreement rather than an answer, and it is put to
	//     the reviewer that was going to read this division anyway. The call is
	//     the one the review already makes, on the merits it already weighs:
	//     are the parts independent, is each a whole job somebody could own, and
	//     does handing them out pay. It fires only when both of those are true
	//     at once — a paid reading of breadth AND a free refusal on the count —
	//     which is rare, and it is one call per task however often a worker asks.
	//
	// THE REVIEWER'S POSTURE FLIPS WITH IT ([Agent.reviewDivision]). Everywhere
	// else the review may only improve a division two measured gates already
	// passed, so an answer nobody could get admits the parts unchanged. Here it
	// is the only thing standing between a below-floor division and the graph,
	// so an answer nobody could get refuses — saying honestly that nothing was
	// decided, and handing back the adjudication it never used
	// ([divisionUnadjudicated]).
	node := graph.node(parent)
	thin := splitgate.Armed() && !splitgate.WorthIt(parsed.Evidence)
	if thin && !node.armedByJudgement() {
		line.Decision = divisionRefusedFloor
		return divisionTooNarrow(parsed.Evidence), "", false
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
		line.Decision = divisionRefusedLane
		return divisionNoLane(len(parsed.Parts), graph.laneLimit()), "", false
	}

	// GATE THREE: NO TWO PARTS MAY OWN THE SAME PATH, and it is HERE because it
	// is free (task_divide_scope.go states the law and why the loss it prevents
	// is silent). Everything below this line costs something — the adjudication
	// on the next one, the reading on the one after — and a division whose parts
	// were never going to be allowed to stand should not spend either of them to
	// find that out. It reads the parts the WORKER wrote, which are the only
	// parts that exist yet.
	//
	// IT IS ASKED AGAIN BELOW, on the parts the reviewer settled, because the
	// reviewer may rewrite a brief into an overlap the worker never wrote. Two
	// askings, one rule, one function.
	if said := a.scopeRefusal(parsed.Parts, scopeSpentNothing); said != "" {
		line.Decision = divisionRefusedScope
		return said, "", false
	}

	// AND THEN THE PLAN IS READ, ONCE, BY THE TIER THAT THINKS. It comes after
	// both gates because it is the only step here that costs money: a division
	// nobody is free to pick up, or one the evidence does not support on work no
	// model called broad, is refused for free exactly as it always was. What
	// comes back is the parts as the reviewer wants them — approved, sharpened,
	// or merged — or a refusal. It FAILS OPEN on an ordinary division; see the
	// file header for why, and [Agent.reviewDivision] for the whole of what it
	// can and cannot change, including the one path where it fails closed.
	//
	// IT IS ALSO BEFORE THE SLOTS ARE CLAIMED, which is not an accident: a
	// review that merges five parts into three must be counted against the fan
	// cap as three, and a claim taken for a part the reviewer then merged away
	// would be a hand held for nobody.
	//
	// AND THE TIEBREAK IS TAKEN HERE, ON THE LINE THAT SPENDS IT. It stands below
	// the free-hand test on purpose: a division nobody could run is refused for
	// free and must not cost this task its one adjudication, and a worker that
	// asks again once a lane frees finds it still there. A second below-floor
	// ask after an ANSWERED one gets the counter's answer, final, for nothing —
	// while an ask the reviewer never answered is refunded below, because
	// silence is not the answer the bound was bought for
	// ([TaskNode.takeTiebreak]).
	if thin && !node.takeTiebreak() {
		line.Decision = divisionRefusedFloor
		return divisionTooNarrow(parsed.Evidence), "", false
	}
	// AND THE CARD SAYS THE READING IS HAPPENING, for the whole of it.
	//
	// This is the one step of a division that a person WAITS THROUGH. Everything
	// above is arithmetic on text and everything below is admission, and both are
	// instant; this is a full call to the tier that thinks, measured at thirteen
	// seconds and bounded at [divideReviewPatience] — during which the node's row
	// drew what it draws between two tool calls, which is a clock. On the road
	// where the harness submits a drawing before the worker's first request
	// (task_divide_sketch.go) that is a task card that has just appeared and has
	// nothing on it at all, and the run this was measured on read as a task that
	// had started and then hung.
	//
	// IT RIDES THE PHASE THE CHECK AND A REPAIR ROUND RIDE ([Agent.enterPhase]),
	// on the same event and into the same pulse file, so the card, the rail row,
	// the room header and the home row all say it without any of them learning a
	// new lane.
	//
	// AND IT IS SETTLED ON THE LINE THAT ENDS THE READING rather than deferred to
	// the end of this function. What follows is the admission, which is the node's
	// own work again and takes no time a person can see — leaving the sizing word
	// standing over it would be the row explaining a moment that had passed.
	settle := a.enterPhase(node, taskBeatSizing, 0, 0, "")
	parts, refusal := a.reviewDivision(ctx, node, parsed, thin)
	settle()
	if refusal.refused() {
		// A REVIEWER THAT NEVER ANSWERED ON THE ADJUDICATING PATH IS TOLD TO
		// THE WORKER AS EXACTLY THAT — nothing was decided, ask once more
		// ([divisionUnadjudicated]) — and the adjudication it never used is
		// given back ([TaskNode.refundTiebreak]). Both halves were learned
		// from the same live cell: the worker was twice told "0 separate
		// items" when the truth was "nobody could ask the reviewer", believed
		// the words, invented a reason they might be true, and routed around
		// the whole road — while the timeout had already spent the one
		// adjudication its honest retry would have needed. A worker acts on
		// what the refusal SAYS, so the refusal must say what happened.
		//
		// An answered refusal is the other case and is final: the reviewer's
		// own reason reaches the worker, and the tiebreak stays spent —
		// `why` carries the "refused: " prefix that tells the two apart.
		line.Decision = divisionRefusedReview
		line.Error = refusal.why
		if thin && !strings.HasPrefix(refusal.why, "refused") {
			line.Decision = divisionRefusedUnreviewed
			node.refundTiebreak()
		}
		// AND THE ONE REFUSAL THAT IS NOT ABOUT THE DIVISION AT ALL. Everything
		// above is a finding about whether these parts are worth handing out; this
		// is a finding about the WORK — that what is left of it is not work for any
		// worker — and it is the only answer on this road that its caller may act
		// on by not starting one.
		if refusal.nobody {
			line.Decision = divisionRefusedNobody
			return refusal.said, personsOwnJob(refusal.why), false
		}
		return refusal.said, "", false
	}
	parsed.Parts = parts

	// AND THE SAME RULE OVER THE PARTS THE REVIEWER SETTLED. Gate three above
	// read the parts the worker wrote; these are the parts that would actually
	// exist, and they are not the same list — the reviewer may merge two parts
	// into one, or sharpen a brief onto a file its sibling already owns. A rule
	// enforced only on the asked-for shape is a rule the settled shape can walk
	// around, so it is asked once more on the last thing anybody changes.
	//
	// IT STANDS ABOVE THE CLAIMS, so a refused division holds no hand. What it
	// cannot say is that nothing was spent: the reading above is paid for by the
	// time this line runs, and [scopeSpentTheRead] is that sentence told
	// honestly.
	if said := a.scopeRefusal(parsed.Parts, scopeSpentTheRead); said != "" {
		line.Decision = divisionRefusedScope
		return said, "", false
	}

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
			line.Decision = divisionRefusedCap
			return refusal, "", false
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
	// AND THE STORE IS ASKED BEFORE THE GRADE IS BELIEVED. A worker's `grade` is
	// a reading made from inside the material and it is the only reading there
	// was; what it cannot know is what has already HAPPENED to work of this shape
	// on this model here. The ratings store knows, because every settled node
	// writes its check's answer into it (taskgrade.go) — so a kind whose cheap
	// attempts keep being turned down earns the careful tier on evidence, and
	// "mechanical" stops being a guess a prompt taught and becomes a fact
	// somebody measured. It is READ ONCE for the whole division, beside the two
	// models, for the reason they are: a division whose third part was decided
	// against a store that moved under it is a family nobody can account for
	// afterwards.
	grades := graph.grades.reader(model)
	request := a.taskRequest()
	ids := make([]uint64, 0, len(parsed.Parts))
	titles := make([]string, 0, len(parsed.Parts))
	for _, part := range parsed.Parts {
		partModel := model
		switch {
		case part.careful():
			partModel = careful
		case careful != model && grades.saysCareful(taskKindOf(part.Title)):
			// THE EVIDENCE OVERRULES THE WORD, and only in this direction. A part
			// the worker called careful is never demoted by a store — the worker
			// read the material and this did not — while a part it called ordinary
			// is lifted where the record says ordinary is not what happens to work
			// of this shape. The lift is skipped entirely where the careful tier
			// resolves to the model the task is already on, because then there is
			// nothing to lift it to and the record would claim a decision nobody
			// made.
			partModel = careful
			line.Lifted = append(line.Lifted, part.Title)
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
			expects:    part.Expects,
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
	line.Decision, line.Admitted = divisionAdmitted, len(ids)
	return divisionDone(ids, titles, graph.machineBusy()), "", false
}

// personsOwnJob is the reviewer's reason with the record's own prefix taken back
// off, because the two readers want different things from the same sentence. The
// journal keeps "refused: " in front of it so an autopsy can tell an answered
// refusal from a review that came to nothing ([Agent.reviewDivision]); a person
// reading their task's report wants the sentence and not the bookkeeping.
func personsOwnJob(why string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(why), "refused:"))
}

// parseDivideArguments reads one call and says, in plain words, what is wrong
// with it.
func parseDivideArguments(args json.RawMessage) (divideArguments, string) {
	var parsed divideArguments
	if err := decodeToolArguments(args, &parsed); err != nil {
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
		// AND THE MANIFEST IS READ THE SAME WAY EVERY OTHER FIELD IS: cleaned,
		// bounded, and refused with a sentence rather than quietly dropped
		// (handoffcontract.go). A part that wrote none is the ordinary part.
		expects, problem := parseExpectations(part.Expects)
		if problem != "" {
			return parsed, fmt.Sprintf("%s (part %d)", problem, i+1)
		}
		parsed.Parts[i].Expects = expects
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
	// The errand shares this budget across its whole ladder rather than handing
	// it to the first rung ([Agent.callRole]), so a wedged mastermind endpoint
	// still leaves the fall-through rung time to answer inside these minutes.
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

  - two parts that would edit the same file, or whose scopes overlap. Fix the boundary in both briefs, or merge them into one part. An overlap you leave standing is not a rough edge: a division whose parts still name the same file when you are done is REFUSED OUTRIGHT and nothing is handed out, because everything the parts write goes into one deliverable and the file would be kept once, one part's work quietly over the other's.
  - a part that cannot start until another has finished. That is a stage and not a part: merge it into the part it waits on.
  - a brief that assumes what its author knew. Name the files, the symbols and the conventions THIS part works in. Where something nearby belongs to a sibling, say whose it is — "the report itself is another part's" — rather than listing its files under this one, so that no two briefs read as claiming the same thing.
  - a done-condition somebody else could not check without taking the part's own word for it.

Answer with ONE JSON object and nothing else — no prose, no code fence:

  {"parts": [{"title": "...", "summary": "...", "brief": "...", "acceptance": "...", "grade": "` + gradeMechanical + `"}, ...]}

Those are the parts that will exist, in the shape you give them. Repeat a part unchanged to approve it, rewrite its brief to sharpen it, give one part where there were two to merge them. Write every field on every part, the grade included — a part you leave a field off is a part that loses it. At least 2 parts and at most ` + strconv.Itoa(taskFanLimit) + `.

  grade  "` + gradeCareful + `" for a part whose failure mode is subtle wrongness — a design decision, tricky debugging, a judgement about somebody else's code — and "` + gradeMechanical + `" for a part whose failure mode is simply not being done yet. It decides how much thinking that part is done with, so it is worth being right about in both directions.

REFUSE ONLY WHEN THIS IS NOT A DIVISION AT ALL — the parts are stages of one procedure, or there is really one job here:

  {"refuse": true, "why": "one line, in a person's own words"}

Refusing throws nothing away: the worker carries on with the work in its own hands. It is not the answer to a division you would have written differently — amend that one instead.

AND ONE ANSWER MORE, FOR THE CASE WHERE THERE IS NO JOB LEFT FOR ANYBODY HERE:

  {"refuse": true, "nobody": true, "why": "one line, in a person's own words, saying what only a person can do"}

Add "nobody" ONLY when what is left cannot be done by a worker at all, however many of them there were: an approval or a signature only a named human may give, a credential or an account nobody here holds, a decision that is the person's to make, or work that is simply somebody else's system doing something by itself. Say what the person has to do, in their words.

IT IS NOT THE WORD FOR A DIVISION YOU THINK IS UNWISE. "This is really one job", "these parts overlap", "this would not pay" and "I would rather one worker did this" are all the plain refusal above, and the worker gets on with the work. "nobody" STOPS THE WORK AND PUTS IT IN FRONT OF A PERSON, so it is worth being sure: if one worker could still make progress on any part of what is left, this is not it.`

// divideAdjudicateAsk is the head of the question on the one path where this
// call decides whether the work divides at all rather than how well.
//
// IT ASKS FOR A JUDGEMENT AND NOT FOR A COUNT. What put this division in front
// of the reviewer is that counting the items said no while a model reading the
// request said the work was broad, and the reviewer settles that by reading the
// PARTS: independence, whether each is a whole job somebody could own, and
// whether handing them out pays for the copy of the repository and the check
// each one costs. The two answers it may give are the two it already has, so
// nothing new is being taught here — only which question is on the table.
const divideAdjudicateAsk = `THIS ONE IS YOURS TO DECIDE, not merely to sharpen. The work was started by somebody who read the request and judged it broad, and what the worker has found does not obviously enumerate a pile of like-for-like items. So the question is whether these parts are a division at all:

  - could ONE person take each part from start to finished, knowing nothing of what the others produced? Not a step of one job — a whole job with its own end.
  - is what each part owns genuinely separate from what the others own — different files, different questions, nothing half-finished passing between them?
  - and does handing them out actually pay? Every part costs its own copy of the repository, its own check and its own wait, so a handful of small edits one worker could do in order is not worth dividing however separate they are.

Yes to all three: answer with the parts, as usual. Any of them no: refuse, and the worker carries on as one worker with nothing lost.`

// divideReview is the reviewer's whole vocabulary: the parts as it wants them,
// or a refusal. An empty one is neither, and is read as "no answer" — which
// admits the original parts, or refuses as undecided where this call is the
// one adjudicating (see [Agent.reviewDivision]).
type divideReview struct {
	Refuse bool   `json:"refuse"`
	Why    string `json:"why"`
	// Nobody is the reviewer saying the remainder is not work for any worker at
	// all — the approving review only a person may give, the credential nobody
	// here holds, somebody else's system doing something by itself.
	//
	// IT IS A FIELD AND NOT A READING OF [divideReview.Why], and that is the
	// whole of why this wave is a field rather than a grep. The two answers a
	// reviewer gives here are one sentence apart in prose — "this is really one
	// job" and "nobody here can do this" are both a refusal explaining itself —
	// and the ending is not one sentence apart at all: the first leaves a worker
	// carrying on, and the second stops the work and puts it in front of a
	// person. A road that told them apart by matching words would park doable
	// work on somebody every time a cautious reviewer reached for a discouraging
	// phrase. So the reviewer has to REACH FOR THE WORD, and anything it merely
	// says is an ordinary refusal.
	Nobody bool         `json:"nobody"`
	Parts  []dividePart `json:"parts"`
}

// divisionRefusal is what a review that admitted no parts came to: the sentence
// its ASKER is told, the line the RECORD keeps, and whether the reviewer said the
// remainder is not work for any worker.
//
// IT IS A STRUCT BECAUSE THE THIRD FACT HAS A DIFFERENT ENDING FROM THE OTHER
// TWO. The sentence and the record were two return values for as long as every
// refusal ended the same way — the worker carries on — and a third bare string
// beside them would be a fourth thing to get in the wrong order at the one call
// site that decides whether somebody's money is spent.
type divisionRefusal struct {
	// said is what the asker reads, and an empty one is "not refused".
	said string
	// why is the line the journal keeps: the reviewer's own reason behind a
	// "refused: " prefix, or how the review came to nothing.
	why string
	// nobody is [divideReview.Nobody] as it survived the reviewer's answer.
	nobody bool
}

// refused reports whether there is a refusal here at all.
func (r divisionRefusal) refused() bool { return r.said != "" }

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
//
// EXCEPT WHEN IT IS ADJUDICATING, which is what `thin` says: the evidence gate
// refused this division on the floor and a model's reading of breadth is what
// put it here anyway ([Agent.divideWork]). On that path this call is not a
// second opinion at all — it is the FIRST and only reader that has said yes to
// these parts, so an answer nobody could get is not "the division stands" — it
// is a refusal that says nothing was decided ([divisionUnadjudicated]), with
// the unused adjudication handed back. Failing open there would let an
// unreachable mastermind admit every below-floor division the road ever armed,
// which is the floor switched off by an outage.
func (a *Agent) reviewDivision(ctx context.Context, parent *TaskNode, parsed divideArguments, thin bool) ([]dividePart, divisionRefusal) {
	// unanswered is what a review that could not be had comes to, and it is the
	// whole of the two postures in one place so they cannot drift apart. The
	// `why` says WHY there was no answer — a reviewer that could not be
	// reached and a reviewer that answered nonsense both refuse as undecided,
	// and the journal must be able to tell them apart from a counter that
	// simply said no (bench autopsy of a live cell could not). The prefix
	// matters: [Agent.divideOnce] reads a `why` that does not begin with
	// "refused" as no-answer and refunds the tiebreak on it.
	//
	// AND NOTHING THAT COMES THROUGH HERE IS EVER A HUMAN-ONLY FINDING. A review
	// nobody could have has read nothing, and the one answer on this road that
	// stops a worker being spent may only come from a reviewer that actually said
	// it (see [divideReview.Nobody]).
	unanswered := func(why string) ([]dividePart, divisionRefusal) {
		if thin {
			return nil, divisionRefusal{said: divisionUnadjudicated(), why: why}
		}
		return parsed.Parts, divisionRefusal{why: why}
	}
	ctx, cancel := context.WithTimeout(ctx, divideReviewPatience)
	defer cancel()

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	response, reviewer, err := a.callRole(ctx, roles.RoleDivision, model,
		[]ai.Message{
			textMessage("system", divideReviewBrief),
			textMessage("user", divideReviewQuestion(parent, parsed, thin)),
		},
		ai.WithMaxTokens(divideReviewTokens),
		ai.WithTemperature(divideReviewTemp))
	if err != nil || response == nil {
		why := "unreached"
		if err != nil {
			why = "unreached: " + err.Error()
		}
		return unanswered(why)
	}
	// The person pays for it, out of the pocket every auxiliary call comes from,
	// against the model that actually answered.
	a.addAuxiliaryUsage(response, reviewer, 1)

	raw, err := subharness.Salvage(response.Text())
	if err != nil {
		return unanswered("unparseable")
	}
	var review divideReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return unanswered("unparseable")
	}
	if review.Refuse {
		// The reviewer's own reason rides on the refusal so the record can
		// carry it: a refusal with no why is the one answer an autopsy cannot
		// learn from, and a live cell has already been read that way.
		//
		// AND A HUMAN-ONLY FINDING IS A DIFFERENT SENTENCE FOR A DIFFERENT
		// ENDING. `nobody` is the reviewer saying the work left over is not work
		// for any worker, so the asker is not told to carry on with it in its own
		// hands — which is what [divisionNotAsWritten] says, and what the measured
		// cell did before this existed: nine minutes and a dollar spent fixing a
		// file in an empty repository, over an approval GitHub was only ever going
		// to take from a person.
		if review.Nobody {
			return nil, divisionRefusal{
				said:   divisionNeedsPerson(review.Why),
				why:    "refused: " + strings.TrimSpace(review.Why),
				nobody: true,
			}
		}
		return nil, divisionRefusal{
			said: divisionNotAsWritten(review.Why),
			why:  "refused: " + strings.TrimSpace(review.Why),
		}
	}
	parts := make([]dividePart, 0, len(review.Parts))
	for _, part := range review.Parts {
		part = part.tidy()
		if !part.whole() {
			// A part with a field missing is an answer nobody can act on, and
			// there is no repair turn here to ask for it back.
			return unanswered("part not whole")
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
		return unanswered("shape")
	}
	return parts, divisionRefusal{}
}

// divideReviewQuestion is the division as the reviewer reads it: the work it
// came out of, what the worker saw, and every part in full.
//
// AND, WHERE IT IS ADJUDICATING, THE FACT THAT IT IS. A reviewer that is the
// last word on whether this divides at all must be told so, or it answers the
// improve-the-briefs question it is normally asked and its silence on the bigger
// one is read as a yes. The paragraph states the disagreement plainly — a
// counter of items said no, a reader of the request said broad — and asks the
// one question the counter could not: is each part a whole job somebody could
// own from start to finished, or is this one job cut up.
//
// IT NAMES NO FLOOR AND NO COUNT, deliberately. Telling the reviewer the number
// would hand it the free gate's answer and invite it to agree, which is the
// thing a second reader is worthless for (the confirm's own argument in
// route_judge.go).
func divideReviewQuestion(parent *TaskNode, parsed divideArguments, thin bool) string {
	var out strings.Builder
	if thin {
		out.WriteString(divideAdjudicateAsk)
		out.WriteString("\n\n")
	}
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

// divisionUnadjudicated is the answer when a below-floor division was owed a
// reading and the reader never answered — reached nobody, or answered nothing
// usable. IT REFUSES WITHOUT PRETENDING TO BE A DECISION: the one time this
// path lied and spoke the counter's words instead, the worker believed them,
// invented a reason its thirty-four items might count as zero, and abandoned
// the road. The invitation to ask again is safe because the unanswered ask was
// refunded ([TaskNode.refundTiebreak]) and the retry costs nothing until a
// reviewer actually answers.
func divisionUnadjudicated() string {
	return "not split: this needed a second reader to weigh it and none could be reached in time, so nothing was decided — your parts were neither taken nor turned down. Ask once more; if there is still no answer, carry on with the work in your own hands."
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

// divisionNeedsPerson is the answer to a division whose reviewer said the work
// left over is not work for any worker — and it is the ONE sentence in this
// family that does not end "carry on with the work in your own hands".
//
// THAT ENDING WOULD BE THE BUG. Every other refusal here is a finding about the
// DIVISION, and the honest thing after one is the work carrying on in one pair of
// hands. This is a finding about the WORK, and the measured cell is what telling a
// worker to carry on with it produces: a task whose whole remainder was an
// approving review GitHub only accepts from a human read "carry on", went looking
// for something it could do, fixed a file in an empty repository, and was failed
// by the check nine minutes and $1.24 later. So the sentence says the two things
// the asker can actually act on: this needs a person, and what the person has to
// do.
//
// It says the reviewer's own line, for [divisionNotAsWritten]'s reason — the
// difference between "somebody has to approve this" and "somebody has to give you
// an account with access" is the whole of what is useful here — and it says
// nothing about who decided, because a person reads this over the worker's
// shoulder in the journal.
func divisionNeedsPerson(why string) string {
	line := clip(firstLine(strings.TrimSpace(why)), divideRefusalBytes)
	if line == "" {
		return "not split: what is left of this work is not something a worker can do — it needs a person. Stop here and say so in your report, naming what has to be done and who has to do it; nothing is cancelled and nothing is lost."
	}
	return "not split: what is left of this work is not something a worker can do — it needs a person: " +
		line + ". Stop here and say so in your report, naming what has to be done and who has to do it; nothing is cancelled and nothing is lost."
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
