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
// This file builds a [divideArguments] out of the drawing and puts it to
// [Agent.divideOnce] — the same body `divide_work` reaches, with the node's own
// worker as the caller. Not a second spawning road, not a shortcut past anything:
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
//     is what every converted cell does today. Nothing is cancelled and nothing is
//     lost.
//   - THE PARENT STAYS. The parts are admitted under the node before its worker's
//     first request, so the runner's tail loop finds children outstanding the
//     moment the worker's own turn ends, holds the node open, and folds every
//     report into it (task_run.go's [runTaskChild]) — which is the same
//     coordinating state a worker that called the verb mid-run lands in, reached
//     by the same code rather than by a second version of it.
//
// ── WHERE IT IS SUBMITTED, AND WHY THERE ──
//
// At the top of [Agent.workTaskNode], after the worker is built and before it is
// handed its brief. That is the earliest moment there is a caller to divide ON
// BEHALF OF: the verb belongs to a node's agent, its gates are asked of that
// node's place in the graph, and its parts are owned by it. Admission time is
// too early — the node has no worker, no worktree and no lane — and anything
// after the first request is the failure this file exists to close.
//
// It also means the two gates see exactly what they would have seen a moment
// later: the parent is running and holding a lane, so [TaskGraph.freeHands]
// counts what it counts for any other division, and no rule had to be written for
// this one.
//
// ── AND THE WORKER IS TOLD, IN THE WORDS IT WOULD HAVE READ ANYWAY ──
//
// A worker that called the verb reads [divisionDone] as its tool result. A worker
// the harness divided for reads THE SAME SENTENCES, at the end of its brief: what
// was handed out, that it must not wait for the parts, and that the work is not
// finished until it has made one deliverable out of their reports. Writing a
// second wording for the same fact is how the two paths would come to mean
// different things.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// divideFromSketch puts the drawing this node was admitted with to the division
// road, on the worker's behalf, and answers two things: what the worker is told
// about it — an empty string when nothing was handed out, whatever the reason —
// and THE PERSON'S OWN JOB, which is empty on every road but one.
//
// THE SECOND ANSWER IS WHY THIS FUNCTION IS ASKED BEFORE THE FIRST REQUEST AND
// NOT AFTER IT. The reviewer that reads a drawn division may come back saying the
// work left over is not work for any worker at all — an approving review only a
// person may give, a credential nobody here holds — and it says it having read
// the parts, the evidence and the brief together, for about two cents, before
// this node has spent anything. That finding used to reach exactly nobody: it was
// journalled, the empty string above was returned, and the worker ran anyway. On
// the cell that produced this wave that was nine minutes, $1.24, a repair round
// and two checks, spent fixing a file in an empty repository over two GitHub
// approvals nothing in this building was ever going to be allowed to give. So the
// sentence is handed back, and [Agent.workTaskNode] lands the node on it
// ([Agent.landNeedsPerson]) instead of starting a worker.
//
// IT ASKS THE SAME THREE QUESTIONS THE BELT ASKS BEFORE IT OFFERS THE VERB
// ([Agent.mayDivide]): the road is on, this agent is a worker that may have
// children, and this task was armed at admission. A worker that would not have
// been given `divide_work` must not have a division submitted for it either —
// that is the whole meaning of arming, and a harness that reached past it would be
// the one caller in the building exempt from the rule.
//
// AND IT IS SUBMITTED ONCE. A node that already has children has already been
// divided — by this, or by its own first worker before a provider fault sent
// [Agent.workTaskNode] round again — and dividing the same work twice would hand
// out five parts nobody drew.
func (a *Agent) divideFromSketch(ctx context.Context) (string, string) {
	if !a.mayDivide() {
		return "", ""
	}
	graph := a.graph()
	parent := a.config.taskID
	node := graph.node(parent)
	if node == nil || len(graph.children(parent)) > 0 {
		return "", ""
	}
	drawn := node.drawn()
	if !drawn.proposes() {
		return "", ""
	}
	proposal, ok := drawn.proposal()
	if !ok {
		return "", ""
	}
	args, err := json.Marshal(proposal)
	if err != nil {
		return "", ""
	}
	answer, person, _ := a.divideOnce(ctx, args, divisionBySketch)
	// THE PERSON'S JOB IS CARRIED STRAIGHT OUT, and nothing below it runs. There
	// are no parts to describe on this road and no worker to describe them to.
	if person != "" {
		return "", person
	}
	// WHETHER THE PARTS EXIST IS ASKED OF THE GRAPH AND NEVER OF THE SENTENCE.
	// Every refusal on that road is an ordinary receipt written for a person to
	// read over a worker's shoulder, and matching prose to decide whether work was
	// handed out would put a road behind a wording somebody is free to improve.
	if len(graph.children(parent)) == 0 {
		return "", ""
	}
	var out strings.Builder
	out.WriteString(divisionAlreadyHandedOut)
	out.WriteString("\n")
	out.WriteString(answer)
	if after := drawn.afterParts(); after != "" {
		out.WriteString("\n")
		out.WriteString(after)
	}
	return out.String(), ""
}

// landNeedsPerson settles a node whose work turned out to be work only a person
// can do, WITHOUT EVER STARTING A WORKER FOR IT.
//
// IT IS NOT A NEW ENDING, and that is deliberate down to the line: it is
// [Agent.landShifted] with a different reason (task_run.go), which is itself the
// unverified landing reached by a third road. The branch is committed and kept
// ([keptWork]) exactly as it is for the landing nobody could judge, the report
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
// THE REASON RIDES IN THE REPORT AND NOWHERE ELSE, which is what puts it in front
// of both readers without a second channel: the person reads it on the card, whose
// first line is this one, and the model reads it inside the landing note. There is
// no worker's own account to stand under it, because there was no worker.
//
// AND IT IS CALLED BEFORE THE FIRST REQUEST, which is the whole saving. The
// worktree and the child agent are already made by then and both are cheap; what
// is on the other side of this line is the run, the check, the repair round and
// the check again.
func (a *Agent) landNeedsPerson(node *TaskNode, tree taskTree, why string, log io.Writer) TaskState {
	merge, kept := keptWork(tree, node.title(), nil)
	fmt.Fprintf(log, "no worker was started: %s\n", why)
	node.finish(yourCallLead(TaskFacts{Merge: merge})+why, kept, tree.branch, merge)
	return TaskUnverified
}

// divisionAlreadyHandedOut is the one sentence of the harness's own that stands
// over the receipt, and it says the single thing the worker cannot work out for
// itself: this happened before it started, so the parts are already running.
//
// EVERYTHING ELSE IS [divisionDone]'S WORDS, unchanged. What a worker needs to
// know after a division — do not wait, the reports arrive here, this is not
// finished until they are one deliverable — is already written for the worker that
// asked, and a second wording of it is how the two paths come to mean different
// things.
const divisionAlreadyHandedOut = "PARTS OF THIS WORK ARE ALREADY IN OTHER HANDS. Before you started, the pieces named above were handed out:"

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
// line apiece. So the cuts are newlines, semicolons and commas, and nothing
// cleverer: a parser that tried to understand the sentence would be a second
// reading of an answer whose whole value is that it was cheap.
//
// A LEGEND THAT MATCHES NOTHING COSTS NOTHING. Every part falls back to the piece
// the shape drew, and the division is put with the letters as their own names —
// which is worse and is not wrong.
func legendSegments(legend string) []string {
	fields := strings.FieldsFunc(legend, func(letter rune) bool {
		return letter == '\n' || letter == ';' || letter == ','
	})
	segments := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
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
