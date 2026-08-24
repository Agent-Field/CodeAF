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

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/splitgate"
)

// divideDescription is what the worker reads. THE FLOOR AND THE FAN CAP ARE
// INTERPOLATED for taskSchemaJSON's stated reason: a number a model reasons
// with must be the number the code enforces.
var divideDescription = "Hand the parts of THIS work out when the material turns out to be wider than one worker's share — you have opened it, and there are many independent items in front of you rather than the one job the brief described. Each part becomes a worker of its own under this task, in its own copy of the repository, and you stay: their reports come back to you and you make one deliverable out of them. USE IT ONLY FOR GENUINE WIDTH. The parts must be independent — nothing half-finished passing between them, no shared file being edited by two of them — and there must be enough of them to be worth it: this is refused unless your evidence names at least " + strconv.Itoa(splitgate.Floor) + " separate items, because below that a worker doing them in order is faster than paying for a copy of the repository, a check and a wait per part. Sequential work is never divided. Up to " + strconv.Itoa(taskFanLimit) + " parts. `evidence` is what you actually saw that revealed the width — the listing, the count, the search result — and it is what the decision is made on, so state the items and how many. If the answer is no, simply carry on with the work in your own hands; nothing is cancelled and nothing is lost."

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
	`"acceptance":{"type":"string","description":"DONE WHEN — the observable done-condition somebody else could check without taking this part's word for it"}},` +
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
// TWO SIGNALS, AND BOTH ARE FREE — no model call is made here, because this
// question is asked of every task that is ever admitted.
//
//   - THE JUDGE ALREADY SAID SO. `/task <brief>` asks a sizing judge whether
//     the work parallelizes (task_person.go's [Agent.judgeDecomposable]) and
//     then starts ONE WORKER whatever the answer was — a yes arms that worker
//     rather than opening a planner, and the surface says so in one dim line
//     (internal/tui3's taskcommand.go). THAT IS THE POINT OF THE WAVE, and it
//     is now the shape wide work takes by default: the upfront choice is gone
//     because a single worker that turns out to be holding six jobs can say so
//     from the material instead of from the brief.
//   - THE WORK'S OWN TEXT ENUMERATES ENOUGH ITEMS. This is
//     cmd/aforge/cooperative.go's plan-time gate asked of a task instead of a
//     plan: a brief that already names eleven adapters is a brief that may
//     divide. It is what arms the road for work nobody ran the judge over — a
//     proposal the chat model groomed, `/task solo`, a person whose standing
//     answer is `single`.
//
// Anything else is not armed, and a task that is not armed is byte-identical to
// a task from before this wave.
func (a *Agent) armDivision(spec taskSpec) bool {
	if a == nil || !a.config.Divide {
		return false
	}
	if a.judgedDivisible(spec.request) || a.judgedDivisible(spec.brief) {
		return true
	}
	return splitgate.WorthIt(spec.title + "\n" + spec.brief + "\n" + spec.acceptance)
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
	// GATE TWO: THE FREE HANDS. Parts that can only queue behind lanes that are
	// already full buy no time at all and cost a copy of the repository each.
	if graph.freeHands() <= 0 {
		return divisionNoHands(len(parsed.Parts)), false, nil
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
	model := a.resolveTaskModel("").model
	request := a.taskRequest()
	ids := make([]uint64, 0, len(parsed.Parts))
	titles := make([]string, 0, len(parsed.Parts))
	for _, part := range parsed.Parts {
		spec := taskSpec{
			title: part.Title,
			// A MODEL WROTE THIS TITLE as a name, so the namer leaves it alone
			// (taskname.go).
			named:      true,
			summary:    part.Summary,
			request:    request,
			brief:      part.Brief,
			acceptance: part.Acceptance,
			model:      model,
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
	return divisionDone(ids, titles), false, nil
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
		parsed.Parts[i].Title = strings.TrimSpace(parsed.Parts[i].Title)
		parsed.Parts[i].Summary = strings.TrimSpace(parsed.Parts[i].Summary)
		parsed.Parts[i].Brief = strings.TrimSpace(parsed.Parts[i].Brief)
		parsed.Parts[i].Acceptance = strings.TrimSpace(parsed.Parts[i].Acceptance)
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

// divisionNoHands is the answer to a division nobody is free to pick up.
func divisionNoHands(parts int) string {
	return fmt.Sprintf(
		"not split: there is no free hand to take the %d parts right now, and parts that can only wait would cost a copy of the repository each and save nothing. Carry on with the work in your own hands; ask again later if it is still too wide for one.",
		parts)
}

// divisionDone is the receipt: what the work was split into, and what happens
// next. It says the parts do not have to be waited for, because they do not —
// this node stays open and every report is put in front of it as it lands
// ([runTaskChild]'s tail loop) — and a worker that sat on a wait would spend its
// whole allowance doing nothing.
func divisionDone(ids []uint64, titles []string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "split into %d parts:", len(ids))
	for i, id := range ids {
		fmt.Fprintf(&out, "\n  %d — %s", id, titles[i])
	}
	out.WriteString("\nEach works from its own brief, in its own copy of the repository, and its branch comes home into yours. Keep working — do not wait for them; each report arrives here when it lands, and this work is not finished until you have folded them into one deliverable.")
	return out.String()
}

// ── how many hands are free ─────────────────────────────────────────────────

// freeHands is how many more workers could START right now, not counting the
// one asking. It is the session engine's answer to the resident's slot probe
// (internal/resident's runner.go), asked of the two ceilings this scheduler
// actually has ([TaskGraph.runFrontier] holds a ready node on exactly these).
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
// ask for; the machine's own ceiling is asked first and is what says no on a
// box that is genuinely loaded.
func (g *TaskGraph) freeHands() int {
	if g == nil {
		return 0
	}
	// Asked before the lock, exactly as the frontier asks it: it is two small
	// file reads behind a one-second cache, and no reading of /proc belongs
	// under the lock that everything announcing a node holds.
	if g.governor.holds() {
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
