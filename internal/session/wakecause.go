package session

// wakecause.go answers ONE question for the end of a turn: which request is this
// turn's ending judged against?
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// A conversation delegated a build and was asked, while it ran, for a checksum
// word reversed. It answered that (journal line 35), the task landed later, and
// the landing woke a turn that reported the marker (line 43). The end-of-turn
// reader was then shown THE LAST THING THE PERSON HAD TYPED as the ask — the
// checksum question, answered a turn earlier and therefore nowhere in this
// turn's digest — and said "the reversed checksum word has not been given"
// (line 46). The turn was carried on and the same word was repeated (line 56).
//
// ── THE RULE ────────────────────────────────────────────────────────────────
//
// A turn owes what arrived in it: the person's own message when they opened or
// steered it, and the TARGET of every result it carries. Both ride provenance
// that already exists — a landing note carries [TaskReplyTag], a batch keeps one
// tag per task ([batchSessionNotes]) — so the ask is read from the turn's own
// arrivals rather than from whatever was typed most recently.
//
// A RESULT'S TARGET IS WHAT ITS OWN WORK WAS FOR AT THE END, which is the
// admitted request on the nearly all tasks nobody redirects, and the effective
// one where the person moved that task's goal in its room while it ran
// ([TaskReplyTag.Obligation], composed from the assignment's snapshot by
// [TaskNode.resultTagLocked]). It is never the whole conversation's newest
// instruction: a slow task is not an authority on what has been asked since, and
// [owedAsk] keeps the source of every line for exactly that reason.
//
// Nothing here reads outcome text and nothing asks a model. A failed task's tag
// is a tag like any other, so remediation is still judged against what that task
// was for.
//
// WHAT IT DOES NOT CHANGE: the gating. A woken turn is still read, still priced
// as a wake, and still carried on when the reader says ITS request is unfinished.

import (
	"fmt"
	"strings"
)

// owedAsk is one thing a turn owes an answer to, and WHO IT CAME FROM.
//
// The source is not decoration. A person's later sentence overrules an earlier
// one; a result that lands late does not overrule anything — it carries the
// target ITS OWN work was for, which may be older than what the person has since
// asked for. Collapsing the two into a bare list is how a slow task would look
// like the newest instruction in the room.
type owedAsk struct {
	text string
	from owedFrom
	// task is the node a result came from, and 0 for the person's own words.
	task uint64
}

// owedFrom is who an owed ask came from.
type owedFrom uint8

const (
	owedByPerson owedFrom = iota
	owedByResult
	owedByBackground
)

// rememberOwedLocked records what one arriving message makes this turn owe: the
// person's own words, or the target behind each result it carries.
//
// The caller holds a.mu: it runs beside [Agent.rememberAskLocked], at the two
// places a message reaches the transcript — the turn's opening and the steering
// drain — so a landing that arrives mid-turn is owed by the turn it lands in.
func (a *Agent) rememberOwedLocked(user userMessage) {
	// THE PERSON'S OWN MESSAGE, on the same test [Agent.rememberAskLocked] makes:
	// a note the session authored and a wake are the session talking to itself.
	if !user.authored && !user.wake {
		a.oweLocked(owedAsk{text: user.text(), from: owedByPerson})
	}
	// AND WHAT EACH RESULT WAS OWED, which is the effective target where the
	// person moved that task's goal while it ran and its original words
	// otherwise ([TaskReplyTag.owed]).
	// Jobs, fired watches and other background news have no task tag. They
	// still owe a report of their outcome, never another answer to whichever
	// unrelated question the person happened to ask most recently.
	if user.otherResults {
		a.oweLocked(owedAsk{text: backgroundReplyObligation, from: owedByBackground})
	}
	for _, tag := range user.replyTags {
		a.oweLocked(owedAsk{text: tag.owed(), from: owedByResult, task: tag.ID})
		// AND WHICH NODE ARRIVED, whatever its words were. This is the id half of
		// the same arrival, kept because [Agent.oweLocked] drops an ask with no
		// text and the write seam needs the node rather than the sentence
		// (writeseam.go's [Agent.deliveringOwnedResult]).
		a.arrivedLocked(tag.ID)
	}
}

// arrivedLocked records one result that landed in this turn, once. The caller
// holds a.mu.
func (a *Agent) arrivedLocked(id uint64) {
	if id == 0 {
		return
	}
	for _, already := range a.turnResults {
		if already == id {
			return
		}
	}
	a.turnResults = append(a.turnResults, id)
}

// This is a runtime reply duty, not a claim that the person issued new work.
const backgroundReplyObligation = "Report the new background outcomes in this turn, using their completion notes and logs as evidence. State relevant results or action needed. Do not re-answer unrelated earlier questions."

// owed is the target one result must be judged against: the effective one when
// the person moved that task's goal while it ran, and the words it was admitted
// with otherwise ([TaskReplyTag]).
func (t TaskReplyTag) owed() string {
	if strings.TrimSpace(t.Obligation) != "" {
		return t.Obligation
	}
	return t.Request
}

// oweLocked adds one ask, once. The same text twice — the parts of one division
// inherit their request — is written a single time, and an empty one (a standing
// firing has no person's sentence behind it) is not written at all.
func (a *Agent) oweLocked(ask owedAsk) {
	ask.text = strings.TrimSpace(ask.text)
	if ask.text == "" {
		return
	}
	for _, already := range a.owedAsks {
		if already.text == ask.text {
			return
		}
	}
	a.owedAsks = append(a.owedAsks, ask)
}

// forgetOwedLocked clears the previous turn's owed asks and the results they
// arrived with. Called once, where a turn opens.
func (a *Agent) forgetOwedLocked() { a.owedAsks, a.turnResults = nil, nil }

// turnAsk is the ask this turn's endings are read against.
//
// ONE ARRIVAL READS EXACTLY AS IT ALWAYS DID. Several are listed in arrival
// order under [owedAsksLead] and each says where it came from, because a reader
// shown a bare conjunction cannot tell a SECOND ask from a CORRECTION of the
// first — nor a slow task's target from what the person has asked for since.
//
// THE FALLBACK IS THE PERSON'S NEWEST MESSAGE, which is what every reader here
// used to be given: a turn that opened on nothing this file can name — a
// continuation, a door that starts a turn without a message — is read exactly as
// it was before.
func (a *Agent) turnAsk() string {
	// A NODE IS UNCHANGED. Its ask is the contract it was admitted with, which
	// [Agent.taskRequest] already answers from the frozen spec, and its work is
	// judged by the auditor rather than here.
	if a.config.InTask {
		return a.taskRequest()
	}
	a.mu.Lock()
	asked := owedAsksText(a.owedAsks)
	a.mu.Unlock()
	if asked != "" {
		return asked
	}
	return a.taskRequest()
}

// owedAsksLead says how to read a list of them, and it is the whole of the
// precedence rule: the person is the authority, and a result speaks only for
// its own work.
const owedAsksLead = "In the order they arrived, oldest first. Where these conflict THE PERSON'S LATEST WORDS stand; a task line is only what that work was for, and a result landing late replaces nothing they asked for after it:"

// owedAsksText renders what a turn owes.
func owedAsksText(owed []owedAsk) string {
	switch len(owed) {
	case 0:
		return ""
	case 1:
		return owed[0].text
	}
	var out strings.Builder
	out.WriteString(owedAsksLead)
	for index, ask := range owed {
		fmt.Fprintf(&out, "\n%d. %s %s", index+1, ask.label(), ask.text)
	}
	return out.String()
}

// label names the source of one owed ask for the line it is written on.
func (o owedAsk) label() string {
	switch {
	case o.from == owedByPerson:
		return "they asked:"
	case o.from == owedByBackground:
		return "new background outcomes require:"
	case o.task == 0:
		return "a finished task was for:"
	default:
		return fmt.Sprintf("task %d was for:", o.task)
	}
}

// ── the effective target of one finished task ───────────────────────────────

// resultTagLocked is the citation and the causal target of ONE finished attempt,
// composed with the graph held so it is a snapshot of this node as it landed.
//
// IT IS TAKEN WITH THE NOTICE AND THE ATTEMPT ([TaskNode.resultOf]) AND CARRIED
// WHOLE THROUGH DELIVERY. A landing is announced some way after it happened —
// there is a claim to win and a reader to find — and in that gap a node can be
// re-armed for directions and revised again ([TaskGraph.runAgainForDirections]).
// Reading the live node at the far end would then judge the OLD result by the
// NEW target, which is the same class of mistake as judging it by an unrelated
// question.
//
// Request stays the person's original words, whatever the goal became: it is the
// citation a surface prints beside the answer.
func (n *TaskNode) resultTagLocked() TaskReplyTag {
	obligation, revision := n.obligationLocked()
	return TaskReplyTag{
		ID: n.id, Title: n.spec.title, Request: n.spec.request,
		Obligation: obligation, Revision: revision,
	}
}

// resultTag is [TaskNode.resultTagLocked] for a caller that holds nothing. It is
// for a message ABOUT a node rather than the delivery of one attempt's result —
// the re-addressed lead a settled parent leaves ([Agent.bubbleUnverifiedChildren])
// — which is about the node as it stands now.
func (n *TaskNode) resultTag() TaskReplyTag {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.resultTagLocked()
}

// obligationLocked is this node's effective target and assignment version, from
// the assignment's own snapshot ([TaskNode.assignmentLocked]) rather than from
// fields read separately: a version taken beside a deliverable the next revision
// has already moved would be a target the work never had.
//
// It is empty on an unrevised node, which is nearly all of them, and the tag
// then carries the admitted ask alone.
func (n *TaskNode) obligationLocked() (string, uint64) {
	now := n.assignmentLocked()
	return obligationText(n.spec.request, n.assignment.revisedSaid(),
		now.deliverable, now.acceptance, now.version), now.version
}

// obligationText composes what a finished task's report is owed against when the
// person moved its goal while it ran, for [TaskReplyTag.Obligation].
//
// It is composed from ONE SNAPSHOT of the assignment, at the moment the report
// is delivered, and never by reading a live node later: an assignment that moves
// between the delivery and the reading would judge the work by a target it never
// had.
//
// WHAT GOES IN IT AND WHAT DOES NOT. The admitted ask, named as history; the
// person's own applied directions, in order, which is the only text here with
// their authority behind it; and the deliverable and done-condition as they then
// stood. NOT the assembled brief — that is the model's writing and labelling it
// as the person's is how a worker's own words become the thing it is judged by —
// and not the admission context or the transcript.
//
// An unrevised assignment answers "", so its tag carries only the original
// request and nothing downstream changes.
func obligationText(original, applied, deliverable, acceptance string, revision uint64) string {
	if revision == 0 || strings.TrimSpace(applied) == "" {
		return ""
	}
	var out strings.Builder
	if original = strings.TrimSpace(original); original != "" {
		out.WriteString("First asked (history, superseded below): " + original + "\n")
	}
	fmt.Fprintf(&out, "Then said, and this is what the work was for (revision %d):\n%s",
		revision, strings.TrimSpace(applied))
	if deliverable = strings.TrimSpace(deliverable); deliverable != "" {
		out.WriteString("\nWhat must exist now: " + deliverable)
	}
	if acceptance = strings.TrimSpace(acceptance); acceptance != "" {
		out.WriteString("\nDone now means: " + acceptance)
	}
	return out.String()
}
