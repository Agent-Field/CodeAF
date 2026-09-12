package session

// THE SKETCH IS THE DIVISION PROPOSAL.
//
// A checkpoint mark asks a mastermind what is left of a running turn and gets one
// line of parts and arrows back (checkpoint.go). When that line has independent
// parts in it the turn is handed over, the drawing goes to the head of the
// worker's brief, and the task is armed to divide. Everything up to there was
// measured working: on the four-issue batch the reader said SPLIT at the first
// mark three times out of three, and it kept its nerve on small work four times
// out of four.
//
// AND THEN NOTHING HAPPENED. Every converted cell landed `parts=0`,
// `peak_workers=1`, and the worker's journal held not one mention of
// `divide_work`. A mastermind had already read the work and written the division
// down; the cheap worker that was handed it never reached for the verb — which is
// the same failure the chat side had for sixty-five cells before this road
// existed. HOPING A CHEAP MODEL RE-DERIVES A DIVISION SOMEBODY ELSE HAS ALREADY
// WRITTEN IS NOT A MECHANISM.
//
// ── SO THE HARNESS SUBMITS IT, AND THROUGH THE EXISTING ROAD ──
//
// This file builds a [divideArguments] out of the drawing and puts it to the same
// body `divide_work` reaches ([Agent.weighDivision], then [Agent.admitDivision]),
// with the node's own worker as the caller. Not a second spawning road, not a
// shortcut past anything:
//
//   - THE EVIDENCE GATE reads what a mastermind actually judged (see
//     [drawnDivision.evidence]), and refuses below the floor exactly as it always
//     did.
//   - THE TIEBREAK still fires only where a model's reading of breadth armed this
//     work, which is precisely the case a mark's split creates — four whole issues
//     enumerate nothing a counter can see, so the division reaches the reviewer by
//     the road Lane J built for it.
//   - THE CAPACITY GATE still refuses to divide what nobody is free to pick up.
//   - THE REVIEWER still reads the parts together and may amend, merge or refuse
//     them, and A REFUSAL IS THE WHOLE ANSWER: the task runs as one worker, which
//     is what it was already doing. Nothing is cancelled and nothing is lost.
//   - THE PARENT STAYS. The parts are admitted under the node while its worker is
//     still reading, so the runner's tail loop finds children outstanding when the
//     worker's own turn ends, holds the node open, and folds every report into it
//     (task_child_run.go's [childRun.foldParts]) — which is the same coordinating
//     state a worker that called the verb mid-run lands in, reached by the same
//     code rather than by a second version of it.
//
// ── BESIDE THE WORKER, NEVER IN FRONT OF IT ──
//
// WHAT WAS TRUE: the drawing was put before the worker's first request, and the
// worker waited for the whole of the reading. On node 5 of conversation
// de9eabcb10cc1e45 that reading was two hundred and nineteen seconds of one model
// thinking — a stream that produced tokens the whole time and so was never cut —
// and the worker's first request went out four minutes and eighteen seconds after
// the turn that handed the work over had ended. The person saw a new card with a
// clock on it and nothing else.
//
// WHAT IS TRUE NOW: the reading starts when the worker is built, beside it
// (task_beside.go), and the worker's first request goes out at once on the brief
// it would have had anyway — the drawing at its head included. The same model is
// asked the same question about the same parts; only when it is asked has moved.
// When the answer lands ([Agent.deliverBeside]):
//
//   - PARTS: they are admitted exactly as before, and the receipt goes onto the
//     worker's own queue, so it reads it at its next step. A worker whose turn has
//     already ended parks on the parts like any worker that handed work out, and
//     reads the receipt with their reports.
//   - A REFUSAL, OR NOBODY TO ASK: nothing happens. The worker is already doing
//     the work as one worker, which is what a refusal always meant.
//   - WORK NO WORKER CAN DO: the worker is stopped — the one case where a started
//     worker is interrupted, and a rare one — and the node lands on the person
//     with the reader's own sentence, keeping whatever the worker wrote
//     ([Agent.landNeedsPerson]).
//   - AND IF THE WORKER HAS ALREADY SAID ITS LAST WORD, OR HANDED WORK OUT OF ITS
//     OWN ACCORD, the answer is dropped and written down as dropped. Parts admitted
//     under a worker that will never read again would be parts nobody folds, and a
//     second division laid over one the worker already made would be two answers
//     to one question.
//
// THE LAST CASE IS DECIDED UNDER THE LOCK THE RUNNER'S TAIL READS ITS NEWS UNDER.
// The tail decides the worker is finished by reading "is any part still out, is
// any report still owed" as one fact ([Agent.taskNewsStanding]), and the answer is
// admitted, or dropped, under that same hold. So there is no instant in which the
// tail has decided the worker is done and the parts are being admitted anyway:
// either the parts exist when the tail looks, and it folds them, or the tail has
// already withdrawn the worker from its room, and the reading sees that and drops.
//
// WHAT THIS COSTS, SAID PLAINLY. On a drawing the reader approves, the worker has
// spent the length of the reading on work that is then handed to parts; they
// start from its copy of the folder as it stood at that moment, so what it wrote is
// on their disk rather than lost, and the parts start exactly when they started
// before — the reading was always in front of them. On every drawing the reader
// refuses, or cannot be asked about, the worker has had the whole of the reading
// to work in instead of waiting through it.
//
// ── AND THE WORKER IS TOLD, IN THE WORDS IT WOULD HAVE READ ANYWAY ──
//
// A worker that called the verb reads [divisionDone] as its tool result. A worker
// the harness divided for reads THE SAME SENTENCES on its queue, under one line of
// the harness's own ([divisionHandedOutBeside]): what was handed out, that it
// must not wait for the parts, and that the work is not finished until it has
// made one deliverable out of their reports. Writing a second wording for the same
// fact is how the two paths would come to mean different things.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// drawnDivision is a division a second mind already wrote: the shape line and its
// legend, and the account they were drawn from.
//
// THE DIGEST IS HERE BECAUSE A DRAWING IS NOT EVIDENCE. `A | B | C | D` says
// nothing a gate can weigh, and the gates weigh evidence — so what rides along is
// the page the mastermind was actually shown when it said the work had four
// parts: the person's ask, one line per tool call, what has been written, and the
// last thing said (checkpoint.go's [checkpointDigest]). That is honest evidence in
// the sense the evidence gate means: what somebody SAW, not how convincingly the
// parts were written up.
type drawnDivision struct {
	sketch checkpointSketch
	digest string
}

// proposes reports whether there is a division here at all. It is
// [checkpointSketch.split] and nothing else — the same reading of the same line
// that decided to hand the turn over in the first place, because a harness that
// converted a turn on two parts and then proposed one would be two answers to one
// question.
func (d drawnDivision) proposes() bool { return d.sketch.split() }

// sizingBeside is the drawing this node was admitted with, being weighed beside
// the node's first worker.
type sizingBeside struct {
	reading *besideWork
	// person and receipt are what the reading came to: the reader's own sentence
	// where it stopped the worker because what is left is not work for any
	// worker, and the receipt the worker was handed where parts were admitted.
	// Both are written by the reading and read only after [sizingBeside.end] has
	// joined it, which is what makes them safe to read without a lock.
	person  string
	receipt string
}

// sizeBeside starts the division's reading beside this worker, and answers nil
// when there is nothing to weigh. stopWork is the worker's own run's cancel,
// which the reading holds for exactly one answer.
//
// IT ASKS TWO OF THE THREE QUESTIONS THE BELT ASKS BEFORE IT OFFERS THE VERB
// ([Agent.mayDivide]) up front: the road is on, and this agent is a worker that
// may have children. The third — did something that arms work say this work is
// wide — belongs to where the division comes from, and [Agent.proposalBeside]
// asks it of each source in its own terms.
//
// AND IT IS SUBMITTED ONCE. A node that already has children has already been
// divided — by an earlier worker of this same node, before a provider fault sent
// [Agent.workTaskNode] round again — and dividing the same work twice would hand
// out parts nobody drew.
func (a *Agent) sizeBeside(ctx context.Context, room *taskRoom, stopWork context.CancelFunc) *sizingBeside {
	if !a.config.Divide || !a.config.mayFanOut() {
		return nil
	}
	graph := a.graph()
	node := graph.node(a.config.taskID)
	if node == nil || len(graph.children(node.id)) > 0 {
		return nil
	}
	propose := a.proposalBeside(node)
	if propose == nil {
		return nil
	}
	sizing := &sizingBeside{}
	sizing.reading = beside(ctx, func(ctx context.Context) {
		proposal, ok := propose(ctx)
		if !ok {
			return
		}
		args, err := json.Marshal(proposal.args)
		if err != nil {
			return
		}
		division := a.weighDivision(ctx, args, proposal.asker)
		sizing.person, sizing.receipt = a.deliverBeside(ctx, room, &division, proposal.after, stopWork)
		a.recordDivision(division)
	})
	return sizing
}

// besideProposal is one division put to the road beside a worker: the parts,
// the step the proposal put behind them, and who proposed it.
type besideProposal struct {
	asker divisionAsker
	args  divideArguments
	after string
}

// proposalBeside is WHERE THE DIVISION A NODE'S FIRST WORKER IS STARTED BESIDE
// COMES FROM, and nil where it comes from nowhere. There are two sources, and
// both answer inside the reading, never in front of the worker:
//
//   - A DRAWING SOMEBODY ALREADY WROTE ([taskSpec.drawn]): a mark's second
//     reader drew the parts of a turn it handed over. It is weighed only on work
//     that was armed at admission, which that road always is — a worker that
//     would not have been given `divide_work` must not have a division
//     submitted for it either.
//   - A PERSON'S SENTENCE NOBODY HAS READ FOR WIDTH ([taskSpec.unsized]): the
//     sizing judge is asked, beside the worker, and its yes IS the arming — the
//     same model's reading of breadth that used to arm the work at admission,
//     arriving after the belt was built and so arriving as parts instead of as a
//     verb ([askedByJudge]). A no, or a judge nobody could reach, is one worker,
//     which is what is already running.
//
// Both then go through the one body every division goes through
// ([Agent.weighDivision]), so the evidence gate, the free hands, the scope rules
// and the reviewer weigh a judge's parts exactly as they weigh a worker's.
func (a *Agent) proposalBeside(node *TaskNode) func(context.Context) (besideProposal, bool) {
	if drawn := node.drawn(); drawn.proposes() {
		if !node.dividing() {
			return nil
		}
		return func(context.Context) (besideProposal, bool) {
			args, ok := drawn.proposal()
			return besideProposal{asker: askedBeside, args: args, after: drawn.afterParts()}, ok
		}
	}
	judge := a.graph().home
	request, unsized := node.widthToRead()
	if judge == nil || !unsized {
		return nil
	}
	return func(ctx context.Context) (besideProposal, bool) {
		wide, parts, why := judge.judgeDecomposable(ctx, request)
		if !wide {
			return besideProposal{}, false
		}
		args, ok := judgedDivision(request, parts, why)
		return besideProposal{asker: askedByJudge, args: args}, ok
	}
}

// widthToRead answers the person's sentence where nobody has read it for width
// yet, and spends the flag in the same breath: the judge is asked about a node
// once, whatever becomes of its answer ([taskSpec.unsized]).
func (n *TaskNode) widthToRead() (string, bool) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if !n.spec.unsized {
		return "", false
	}
	n.spec.unsized = false
	return n.spec.request, true
}

// judgedDivision is the sizing judge's yes written out as a division: one part
// per part it named, and the sentence it read as the evidence.
//
// THE EVIDENCE IS WHAT THE JUDGE WAS SHOWN, which is the person's own sentence,
// with its reason under it — the same rule a drawing follows ([drawnDivision.evidence]):
// what the gates and the reviewer weigh is what somebody saw, not how well the
// parts were written up. The parts are the judge's own six words apiece, named
// through [sketchName] so a bare label never reaches the rail, and each carries
// the stand-in done-condition the reviewer is asked to sharpen
// ([divisionStandInDone]); the family's brief is composed around every part by
// the one composer (task_divide_compose.go).
//
// A YES WITH FEWER PARTS THAN A DIVISION NEEDS IS NOT ONE, by the same floor a
// drawing is held to ([checkpointSketchParts]).
func judgedDivision(request string, named []string, why string) (divideArguments, bool) {
	if len(named) < checkpointSketchParts {
		return divideArguments{}, false
	}
	parts := make([]dividePart, 0, len(named))
	for index, said := range named {
		parts = append(parts, dividePart{
			Title:      sketchName(said, said, index),
			Summary:    said,
			Brief:      said,
			Acceptance: divisionStandInDone(said),
		})
	}
	return divideArguments{Evidence: clip(withReport(request, why), divideReviewEvidenceBytes), Parts: parts}, true
}

// end joins the reading and answers the person's own job, empty on every road
// but the one where the reading stopped the worker for it.
func (s *sizingBeside) end() string {
	if s == nil {
		return ""
	}
	s.reading.end()
	return s.person
}

// handedOut is the receipt the worker was given for the parts, and an empty
// string wherever nothing was handed out. It is read after [sizingBeside.end],
// by a second worker built for the same node: the same node with the same parts
// already running must read the same sentence.
func (s *sizingBeside) handedOut() string {
	if s == nil {
		return ""
	}
	return s.receipt
}

// deliverBeside is the reading's answer reaching the worker it was started
// beside, and it answers the person's own job where the worker was stopped for
// one and the receipt where parts were handed out.
//
// A REFUSAL REACHES NOBODY. The worker never asked, so there is no answer to put
// a refusal in, and a refusal's whole meaning — carry on as one worker — is what
// the worker is already doing. The record says what the reading found.
//
// EVERYTHING ELSE IS DECIDED UNDER THE WORKER'S HANDOVER HOLD, the lock the
// runner's tail reads "is any part still out, is any report owed" under
// ([Agent.taskNewsStanding]). The file header says why that is what makes a late
// answer safe: the tail cannot decide the worker is finished in the middle of
// this, and this cannot admit parts after the tail has decided it. THE HOLD IS
// TAKEN ONCE PER NODE AND COVERS ONE ADMISSION — a commit of the worker's own
// files and a handful of admits — so the tail's next read waits that long at
// most, and only on the one pass it coincides with.
func (a *Agent) deliverBeside(ctx context.Context, room *taskRoom, division *weighedDivision, after string, stopWork context.CancelFunc) (string, string) {
	if !division.admissible() && division.person == "" {
		return "", ""
	}
	a.handover.Lock()
	defer a.handover.Unlock()
	// THE WORKER IS STILL READING WHILE IT HOLDS THE ROOM. The runner withdraws
	// it at its last read (task_child_run.go) and on every road out of the run,
	// and a reading its owner has ended is one nobody is waiting for.
	if ctx.Err() != nil || room.speaker() != a {
		division.drop("the worker had finished before the reading answered")
		return "", ""
	}
	if division.person != "" {
		stopWork()
		return division.person, ""
	}
	if len(a.graph().children(a.config.taskID)) > 0 {
		division.drop("the worker had already handed work out")
		return "", ""
	}
	said := a.admitDivision(division)
	// WHETHER THE PARTS EXIST IS ASKED OF THE RECORD AND NEVER OF THE SENTENCE.
	// The two refusals admission can still make — the fan cap and a family world
	// that would not freeze — are receipts written for a worker that asked, and
	// this one did not.
	if division.line.Admitted == 0 {
		return "", ""
	}
	receipt := withReport(divisionHandedOutBeside, withReport(said, after))
	a.enqueueNote(briefNote(receipt))
	return "", receipt
}

// landNeedsPerson settles a node whose work turned out to be work only a person
// can do, keeping whatever its worker had written before it was stopped.
//
// IT IS NOT A NEW ENDING, and that is deliberate down to the line: it is
// [Agent.landShifted] with a different reason (task_run.go), which is itself the
// unverified landing reached by a third road. The branch is committed and kept
// ([keepHome]) exactly as it is for the landing nobody could judge, the report
// leads with [yourCallLead] in the same person's words, and everything
// downstream — the settle card, the rail's mark, the note's `your call` word,
// the bubbling of a still-undecided child up to whoever is left to decide
// ([Agent.bubbleUnverifiedChildren]) — is machinery that was already there.
// Nothing about this landing has to know why it was asked for.
//
// UNVERIFIED RATHER THAN FAILED IS THE STATE THAT MATCHES THE SENTENCE. Nothing
// went wrong, nobody made a finding against the work, and there is nothing to try
// again: what is left needs a person, and the one state in this graph that WAITS
// ON A PERSON is this one. Failing it would put a ✗ beside work that was read
// correctly and stopped early, and done would claim something happened.
//
// THE REASON LEADS THE REPORT, which is what puts it in front of both readers
// without a second channel: the person reads it on the card, whose first line is
// this one, and the model reads it inside the landing note. What the worker had
// said before it was stopped stands under it, because that is the person's only
// account of what is already in the kept branch.
//
// IT IS NO LONGER CALLED BEFORE A WORKER STARTS. The reading that finds this runs
// beside the worker now (the file header says why), so the saving is everything
// AFTER the stop — the rest of the run, the check, the repair round and the check
// again — rather than the whole of it.
func (a *Agent) landNeedsPerson(node *TaskNode, tree taskTree, why string, changed []string, report string, log io.Writer) TaskState {
	merge, changed := keepHome(node, tree, changed, a.signsGitWork())
	fmt.Fprintf(log, "the worker was stopped: %s\n", why)
	node.finish(withReport(withYourCallLead(TaskFacts{Merge: merge}, why), report), changed, tree.branch, merge)
	return TaskUnverified
}

// divisionHandedOutBeside is the one sentence of the harness's own that stands
// over the receipt, and it says the two things the worker cannot work out for
// itself: parts it never asked to hand out are now somebody else's, and they
// start from its copy of the folder as it stood when they were handed out.
//
// EVERYTHING ELSE IS [divisionDone]'S WORDS, unchanged. What a worker needs to
// know after a division — do not wait, the reports arrive here, this is not
// finished until they are one deliverable — is already written for the worker that
// asked, and a second wording of it is how the two paths come to mean different
// things.
const divisionHandedOutBeside = "PARTS OF THIS WORK ARE NOW IN OTHER HANDS. The pieces below were handed out from this copy of the folder as it stood then, so what had been written by that moment is on their disk. They are theirs: do not do them yourself."

// drawn is the division this node was admitted with, or an empty one. It is
// [taskSpec.drawn] read under the graph's lock, which is the only way anything
// outside admission may read the spec.
func (n *TaskNode) drawn() drawnDivision {
	if n == nil {
		return drawnDivision{}
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.drawn
}

// ── the drawing, as a division ──────────────────────────────────────────────

// THE BOUNDARY IS NOT WRITTEN HERE ANY MORE, and neither is the parent's brief.
// What a part is told about its family — the work being divided and which scopes
// somebody else owns — is composed for EVERY part of EVERY division by one
// composer (task_divide_compose.go), on the road both this file and `divide_work`
// come through. This file writes one thing and it is the thing only the drawing
// knows: what each part owns, in the legend's own words.

// divisionStandInDone is the done-condition a part is given when nobody has
// written one, and it is deliberately the WEAKEST honest thing that can be said:
// this part's own scope, finished, with the report saying how anybody could tell.
//
// THE REVIEWER IS THE HONEST AUTHOR OF A DONE-CONDITION and this is a stand-in
// for it, not a replacement. A part is finished by a checker judging it against
// its acceptance ALONE (task_audit.go), so the mastermind that reads this division
// is asked for the same sharpening it already gives every other brief — and it
// is asked in the words the review's own contract uses, "a done-condition
// somebody else could not check without taking the part's own word for it".
//
// IT IS NOT AN EMPTY FIELD, and that is the whole reason it exists. A part is
// admitted with whatever the reviewer answered, and the reviewer FAILS OPEN: a
// division whose parts went in with no acceptance would be admitted with none the
// moment a mastermind timed out, and a part with no done-condition is a part
// judged against nothing.
func divisionStandInDone(scope string) string {
	return scope + " is done, and the report says how anybody else could check it"
}

// proposal is the drawing written out as a division: one part per top-level piece
// of the shape, the legend's own words for each, and the evidence the reader was
// shown. The second answer is false where there is nothing to propose.
//
// WHAT IT WRITES IS THE SCOPE AND NOTHING ELSE. The parent's brief — which is
// where the turn's findings are (checkpoint.go's dowry) — and the map of which
// scopes somebody else owns are composed around every part of every division by
// [divisionFamily] on the road below, so a part drawn out of a sketch and a part
// a worker wrote get the same world by construction rather than because two
// files agree today.
//
// NOTHING HERE CLIPS THE SHAPE TO THE FAN CAP. A sketch with more parts than one
// piece of work may be split into is put whole and refused whole by
// [TaskGraph.claimChild], for the reason a division is refused whole there: the
// harness quietly handing out the first five of seven would be a shape nobody
// drew.
func (d drawnDivision) proposal() (divideArguments, bool) {
	pieces := readShape(d.sketch.shape).parts
	if len(pieces) < checkpointSketchParts {
		return divideArguments{}, false
	}
	// THE LEGEND IS READ ONCE PER PIECE AND THE SCOPES ARE ALL BUILT BEFORE ANY
	// PART IS: every part names every other one, so the last piece's words have to
	// exist before the first part's brief is written.
	segments := legendSegments(d.sketch.legend)
	said := make([]string, len(pieces))
	scopes := make([]string, len(pieces))
	for index, piece := range pieces {
		said[index] = sketchSaid(piece, segments)
		scopes[index] = sketchScope(piece, said[index])
	}
	parts := make([]dividePart, 0, len(pieces))
	for index, piece := range pieces {
		summary := said[index]
		if summary == "" {
			summary = piece
		}
		parts = append(parts, dividePart{
			Title:   sketchName(said[index], piece, index),
			Summary: summary,
			Brief:   scopes[index],
			// THE DONE-CONDITION IS READ ALONE, by somebody who cannot see the shape
			// (task_audit.go's auditQuestion), so it is written out of the legend's
			// WORDS and never out of the letter beside them: "A: the release notes is
			// done" is a sentence about a coordinate in a drawing nobody showed them.
			Acceptance: divisionStandInDone(summary),
		})
	}
	return divideArguments{Evidence: d.evidence(), Parts: parts}, true
}

// afterParts is the step the drawing put BEHIND the parts, said to the worker
// that is going to do it, or an empty string where the drawing put nothing there.
//
// IT IS THE OTHER HALF OF READING THE FIRST STAGE. `(A | B | C) > D` is the shape
// a mastermind actually draws for a batch of jobs — three that can start now and
// one that gathers them — and the harness hands out the three ([readShape]). D is
// not lost and it is not another part: it is what the PARENT does once the reports
// land, which is the parent-stays law already (task_divide.go's header), said in
// the drawing's own letters so the worker can see which one it is.
//
// It says nothing at all where the drawing put nothing behind the parts, by the
// emptiness law: a worker told about a gathering step nobody drew would go looking
// for it. `A > B | C > D` is exactly that case — its arrows are inside the parts,
// so no stage stands after the division and [shapeReading] reports none.
func (d drawnDivision) afterParts() string {
	after := readShape(d.sketch.shape).after
	if len(after) == 0 {
		return ""
	}
	segments := legendSegments(d.sketch.legend)
	scopes := make([]string, 0, len(after))
	for _, stage := range after {
		scopes = append(scopes, sketchScope(stage, sketchSaid(stage, segments)))
	}
	return "AND THIS IS YOURS, ONCE THEIR REPORTS ARE IN: " + strings.Join(scopes, ", then ") +
		". That is the step the parts were cut out from in front of, and nobody else is doing it."
}

// evidence is what the gates and the reviewer are shown: the drawing, and the
// account it was drawn from.
//
// THE DRAWING GOES FIRST AND THE ACCOUNT UNDER IT, which is the order they were
// made in and the order they are useful in — a reader that opened on eighty lines
// of ledger would meet the parts last. It is bounded to the same figure the
// reviewer clips evidence to, so nothing is carried that nothing will read.
func (d drawnDivision) evidence() string {
	var out strings.Builder
	out.WriteString(d.sketch.shape)
	if legend := strings.TrimSpace(d.sketch.legend); legend != "" {
		out.WriteString("\n")
		out.WriteString(legend)
	}
	if digest := strings.TrimSpace(d.digest); digest != "" {
		out.WriteString("\n\n")
		out.WriteString(digest)
	}
	return clip(out.String(), divideReviewEvidenceBytes)
}

// sketchScope is how one part is named to itself and to its siblings: the letter
// the drawing used, and the legend's words for it where the legend named it. Both
// halves matter — the letter is what the shape at the head of the parent's brief
// says, and the words are what anybody can act on.
func sketchScope(piece, said string) string {
	if said == "" {
		return piece
	}
	return piece + ": " + said
}

// sketchName is the label a part wears in a narrow column beside its siblings.
//
// THE LEGEND'S WORDS FIRST, the shape's own piece second, and a numbered part
// last. A shape may be drawn either way — `A | B | C` with a legend that says what
// the letters are, or `fix the redirect | the flaky fixture | the changelog` with
// the words in the shape itself — and both are the sidecar answering the question
// it was asked. What is never a name is a bare coordinate: `A` on the rail tells
// a person nothing at all, and a numbered part at least says which of how many.
//
// IT GOES THROUGH THE HAND THAT CLEANS EVERY OTHER NAME ON THIS SURFACE
// ([cleanTaskName], taskname.go) rather than a second one of this file's own: the
// cut to [TaskNameWords], the markup a model puts round a label, and the refusal
// of an answer that is really an instruction are one behaviour wherever a name is
// minted.
func sketchName(said, piece string, index int) string {
	for _, candidate := range []string{said, piece} {
		if name := cleanTaskName(candidate); name != "" && !bareLabel(name) {
			return name
		}
	}
	return "part " + strconv.Itoa(index+1)
}

// bareLabel reports whether a name is a coordinate in a drawing rather than a
// name: one word of one or two characters, which is what `A`, `B` and `C1` are.
func bareLabel(name string) bool {
	fields := strings.Fields(name)
	return len(fields) == 1 && len([]rune(fields[0])) <= 2
}

// ── reading the legend ──────────────────────────────────────────────────────

// legendSegments cuts the legend into the clauses that name one letter each.
//
// THE ASK IS WHAT DECIDES THE SHAPE OF THIS. It says "one sentence saying what
// each letter is" ([checkpointSketchAsk]), and what a model writes for that is
// one sentence with the letters separated by commas — "A is the validation
// workflow, B is the docs sweep, C is the release notes" — or, about as often, a
// line apiece. So punctuation is the first cut and nothing cleverer: a parser
// that tried to understand the sentence would be a second reading of an answer
// whose whole value is that it was cheap.
//
// AND THE LABEL ITSELF IS THE SECOND CUT, which punctuation alone missed. The
// legend on task 1 of conversation 57d51779f63ac603 read
//
//	**A:** read `seam.start` in `cmd/aforge/chatv3.go` … **B:** trace the
//	folder-pick path — `folderConfirm` in `folderact.go` → … **C:** …
//
// on ONE line with no separator between the clauses. A's clause swallowed the
// whole of it, B and C matched nothing, and the parts went out called "read
// seam.start in" and "part 2" — the second being [sketchName]'s answer for a
// letter the legend never named, about a letter the legend named perfectly well.
// A label opening a clause is a boundary wherever it stands, so it is one here:
// see [legendInlineLabel] for the four spellings.
//
// A LEGEND THAT MATCHES NOTHING COSTS NOTHING. Every part falls back to the piece
// the shape drew, and the division is put with the letters as their own names —
// which is worse and is not wrong.
func legendSegments(legend string) []string {
	segments := make([]string, 0, 8)
	for _, clause := range legendClauses(legend) {
		for _, field := range strings.FieldsFunc(clause, func(letter rune) bool {
			return letter == '\n' || letter == ';' || letter == ','
		}) {
			if trimmed := strings.TrimSpace(field); trimmed != "" {
				segments = append(segments, trimmed)
			}
		}
	}
	return segments
}

// legendInlineLabel is a legend opening a clause about one letter: `**B:**`,
// `B:`, `B —`, `B - `, or `(B)`.
//
// IT DEMANDS A SEPARATOR AFTER THE LETTER, and that is the whole of what keeps it
// from cutting a sentence to pieces. `(wired at line 519 as ...)` is not a label
// because `w` is not followed by one; `folder-pick` is not a label because the
// hyphen has a letter in front of it rather than a space; a `—` that opens a
// clause of its own is not a label because there is no single letter before it.
// The letter may carry one digit, because `C1` is a coordinate a drawing writes.
//
// THE `is` FORM IS DELIBERATELY NOT HERE. "A is the workflow and B is the sweep"
// is already cut by the comma or the newline a model writes it with, and a rule
// that started a clause at every "x is" would cut inside the words it was trying
// to keep — "the one place the answer is assembled" being a legend clause, not
// two.
var legendInlineLabel = regexp.MustCompile(
	`(?:^|\s)(?:` +
		"\\*{0,2}\\(?[A-Za-z][0-9]?\\)?\\*{0,2}\\s*(?:[:：]|—|–|-\\s)" +
		`|\([A-Za-z][0-9]?\)\s` +
		`)`)

// legendClauses cuts a legend wherever it opens a clause about a letter, and
// answers the whole legend when it never does.
func legendClauses(legend string) []string {
	cuts := legendInlineLabel.FindAllStringIndex(legend, -1)
	if len(cuts) == 0 {
		return []string{legend}
	}
	clauses := make([]string, 0, len(cuts)+1)
	last := 0
	for _, cut := range cuts {
		// A cut at the very start of the legend opens nothing: the first clause
		// begins there anyway, and an empty leading piece would be a segment with
		// no label that every reader below has to skip.
		if cut[0] <= last {
			continue
		}
		clauses = append(clauses, legend[last:cut[0]])
		last = cut[0]
	}
	return append(clauses, legend[last:])
}

// sketchSaid is the legend's own words for one piece of the shape, or an empty
// string where the legend never named it.
//
// THE MATCH IS ON THE FIRST WORD OF EACH, which is the only thing the two texts
// reliably share: the shape writes `A` and the legend opens `A is …`, `A: …` or
// `A — …`. Punctuation comes off both ends of both before they are compared, and
// the comparison folds case, because a model that draws `a | b` writes the legend
// with capitals about as often as not.
//
// WHAT IS ANSWERED IS THE WORDS AND NOT THE CLAUSE. The letter and the copula are
// dropped, so "A is the validation workflow" answers "the validation workflow" —
// which is what can stand as a name, as a summary, and inside a done-condition.
func sketchSaid(piece string, segments []string) string {
	// A PIECE MAY BE A CHAIN. The first stage of `A > B | C > D` hands out two
	// parts that are each a short procedure — a module, then its test — and a
	// part named after its first letter alone would be called "the slugify
	// module" while it also owns the test. So every letter in the piece is read
	// off the legend and the words are joined in order, "then" between stages.
	var said []string
	for _, key := range labelsOf(piece) {
		if words := legendWords(key, segments); words != "" {
			said = append(said, words)
		}
	}
	return strings.Join(said, ", then ")
}

// labelsOf is every label in a piece, in order: the letters of a chain, with
// the arrows and brackets that joined them dropped.
func labelsOf(piece string) []string {
	var labels []string
	for _, field := range strings.FieldsFunc(piece, func(r rune) bool {
		return r == '>' || r == '(' || r == ')' || r == ' '
	}) {
		if key := labelOf(field); key != "" {
			labels = append(labels, key)
		}
	}
	return labels
}

// legendWords is what the legend says one label is, or "" when it says nothing.
func legendWords(key string, segments []string) string {
	if key == "" {
		return ""
	}
	for _, segment := range segments {
		if labelOf(segment) != key {
			continue
		}
		fields := strings.Fields(segment)[1:]
		// The copula the legend joined the letter to its words with. Anything else
		// is already the words.
		for len(fields) > 0 {
			word := strings.ToLower(strings.Trim(fields[0], ".,:;=-—–"))
			if word != "is" && word != "are" && word != "" {
				break
			}
			fields = fields[1:]
		}
		if said := strings.TrimSpace(strings.Join(fields, " ")); said != "" {
			return said
		}
	}
	return ""
}

// labelOf is the first word of a piece or a legend clause, stripped of the
// punctuation either side of it and folded to lower case — the one token the two
// are compared on.
func labelOf(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.Trim(fields[0], ".,:;=-—–()[]{}\"'`*_"))
}
