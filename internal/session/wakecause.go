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
// steered it, and the ORIGINAL REQUEST of every result it carries. That
// provenance already exists — a landing note carries [TaskReplyTag] with the
// node's own request, frozen at admission, and a batch of landings keeps one tag
// per task ([batchSessionNotes]) — so the ask is read from the turn's own
// arrivals rather than from whatever was typed most recently.
//
// Nothing here reads outcome text and nothing asks a model. A failed task's tag
// is a tag like any other, so remediation is still judged against the request
// that task was for.
//
// WHAT IT DOES NOT CHANGE: the gating. A woken turn is still read, still priced
// as a wake, and still carried on when the reader says ITS request is unfinished.

import (
	"fmt"
	"slices"
	"strings"
)

// rememberOwedLocked records what one arriving message makes this turn owe: the
// person's own words, or the requests behind the results it carries.
//
// The caller holds a.mu: it runs beside [Agent.rememberAskLocked], at the two
// places a message reaches the transcript — the turn's opening and the steering
// drain — so a landing that arrives mid-turn is owed by the turn it lands in.
func (a *Agent) rememberOwedLocked(user userMessage) {
	// THE PERSON'S OWN MESSAGE, on the same test [Agent.rememberAskLocked] makes:
	// a note the session authored and a wake are the session talking to itself.
	if !user.authored && !user.wake {
		a.oweLocked(user.text())
	}
	// AND WHAT EACH RESULT WAS OWED, which is the effective target where the
	// person moved the goal while it ran and the original words otherwise
	// ([TaskReplyTag.owed]).
	for _, tag := range user.replyTags {
		a.oweLocked(tag.owed())
	}
}

// owed is the request one result must be judged against: the effective target
// when the person moved the goal while it ran, and their original words
// otherwise ([TaskReplyTag]).
func (t TaskReplyTag) owed() string {
	if strings.TrimSpace(t.Obligation) != "" {
		return t.Obligation
	}
	return t.Request
}

// oweLocked adds one request, once. Identical requests — the parts of one
// division inherit theirs — are written a single time, and an empty one (a
// standing firing has no person's sentence behind it) is not written at all.
func (a *Agent) oweLocked(request string) {
	request = strings.TrimSpace(request)
	if request == "" || slices.Contains(a.owedAsks, request) {
		return
	}
	a.owedAsks = append(a.owedAsks, request)
}

// forgetOwedLocked clears the previous turn's owed requests. Called once, where
// a turn opens.
func (a *Agent) forgetOwedLocked() { a.owedAsks = nil }

// turnAsk is the ask this turn's endings are read against.
//
// ONE ARRIVAL READS EXACTLY AS IT ALWAYS DID. Several are numbered in arrival
// order under [owedAsksLead], because a turn can owe two things and a reader
// shown a bare conjunction of them cannot tell a SECOND ask from a CORRECTION of
// the first — and the person's later words are the ones that stand.
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
// supersede rule: order is meaning.
const owedAsksLead = "In the order they arrived, oldest first. Where two of these conflict the LATER one is what stands, and the earlier is history:"

// owedAsksText renders what a turn owes.
func owedAsksText(owed []string) string {
	switch len(owed) {
	case 0:
		return ""
	case 1:
		return owed[0]
	}
	var out strings.Builder
	out.WriteString(owedAsksLead)
	for index, ask := range owed {
		fmt.Fprintf(&out, "\n%d. %s", index+1, ask)
	}
	return out.String()
}

// ── the effective target of one finished task ───────────────────────────────

// obligationNow is this node's effective target and assignment version, taken as
// one snapshot where its report is composed ([Agent.deliverTaskNote]).
//
// NOTHING CAN MOVE A RUNNING NODE'S GOAL ON THIS BRANCH, so it answers the
// unrevised pair and every tag carries the admitted ask alone. The steering
// merge (assignment.go, d93f6a0b4) is what fills it in, and this is the whole of
// that change — one body, under the graph's lock, from the assignment's own
// snapshot rather than from any field read separately:
//
//	now := n.assignmentLocked()
//	return obligationText(n.request(), n.assignment.revisedSaid(),
//	    now.deliverable, now.acceptance, now.version), now.version
//
// The delivery module moves the caller from [Agent.deliverTaskNote] to
// postTaskMessage; the three tag fields and this call travel together.
func (n *TaskNode) obligationNow() (string, uint64) { return "", 0 }

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
