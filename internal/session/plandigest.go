package session

// THE CHAT SEES THE PLAN WHEN THE PERSON SPEAKS.
//
// A conversation that has handed work to a run is a manager, and a manager who
// does not know what is in flight cannot manage. The shape of the failure is
// this, and it has been watched: a person says "actually, skip the migration";
// one of the run's tasks is doing the migration at that moment; the chat has no
// way to know that, because nothing tells it what is open when a message
// arrives; so it answers the person, the task carries on, and the money spent
// after that sentence buys something the person has already said they do not
// want.
//
// Asking would not fix it. The `tasks` tool has been able to answer this since
// the first run, and the turn where the person changes direction is exactly the
// turn where nothing in what they said suggests looking. The information has to
// be PUSHED, and pushed on the one turn that matters, which is every turn while
// a run is live.
//
// SO: each of the person's messages opens with a compact digest of the plan —
// one line per task, id, title, state, and its newest note — and then their own
// words. Off entirely when nothing is live, so a conversation that has never
// handed work out pays nothing and reads nothing.
//
// WHAT IT MUST NOT CARRY is as much of the design as what it carries. No
// results, no steps, no transcripts: those are what `tasks #N` is for, asked
// when the chat has a reason. The thing being protected here is the chat's own
// context, and a digest that grows with the plan would defeat the whole purpose
// of having one — so the row count is bounded by [planDigestRows] and a plan
// wider than that says how many more there are and leaves the chat to ask.
//
// THE PERSON NEVER SEES IT. The digest rides in the message this turn reasons
// from and nowhere else: [Agent.recordUserLocked] journals their own sentence
// through [userMessage.said] (or past [userMessage.lead], on a message with
// pictures), so a resume, an export and the transcript on screen all show
// what they typed. That is standing_mark.go's law, applied to the second door
// that needs it.

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planDigestRows is how many task lines one digest carries. THE BOUND IS THE
// WHOLE POINT: a plan of forty rows would put forty lines in front of every
// sentence the person types, on every turn, for as long as the run lives, and a
// digest that costs more than the answer is a digest nobody should have built.
// Eight is the width at which a person can still see the shape of a plan at a
// glance, and a plan wider than eight is one where the chat is going to open
// `tasks` anyway. What the bound leaves out is counted and named, never
// silently dropped.
const planDigestRows = 8

// planDigestLineChars bounds one row's title and planDigestNoteChars its note,
// for the same reason and against the same risk: a title or a note is a string
// somebody else's model wrote, and an unbounded one would be a way for a run to
// spend the conversation's context by writing a long enough sentence.
const (
	planDigestLineChars = 90
	planDigestNoteChars = 160
)

// planDigestHeading opens the block, and it says what the rows ARE rather than
// naming a mechanism: the model is being handed a picture of work that is going
// on while the person talks, and what it is for is the sentence beneath it.
const planDigestHeading = "WORK RUNNING RIGHT NOW, while the person is speaking:"

// planDigestRule is the manager's whole job, said once, in the place it can act
// on. It is here and not only in the page because the page is read at the top of
// every request and this arrives ATTACHED TO THE SENTENCE that might have
// changed something — which is the moment the reasoning has to happen.
const planDigestRule = "If what the person just said changes what one of these should do, steer or stop THAT task before you answer them. Read the rest with `tasks` when you need more than a row."

// planDigest is the block that opens a person's turn while a run is live, and
// the empty string every other time — no run, nothing open in it, or a
// conversation whose hand-offs are not runs at all.
//
// IT IS THE SAME READ THE `tasks` TOOL MAKES ([Agent.runPlanTasks]), narrowed
// to a row and pushed rather than pulled. It runs no model and opens no file
// the surface's own pane does not already open every second, so a turn pays
// one store read for it.
func (a *Agent) planDigest() string {
	// THE NODE ROAD HAS NO PLAN TO DIGEST. A conversation whose hand-offs are
	// nodes of the session tree has no store, so this is absent there rather
	// than empty — the same predicate the hand-off facts branch on.
	if !a.config.oneTaskRoad() {
		return ""
	}
	rows := a.runPlanTasks()
	if !planAnyOpen(rows) {
		return ""
	}
	// THE LABELS ARE TAKEN BEFORE THE ROWS ARE REORDERED. A part's `#2.1` is its
	// place under its parent in the store's own order, which is the order the
	// rows arrive in, so reading them after the reorder would renumber parts.
	labels := planTaskLabels(rows)
	rows = planDigestOrder(rows)
	var b strings.Builder
	b.WriteString(planDigestHeading + "\n")
	shown := 0
	for _, row := range rows {
		if shown == planDigestRows {
			break
		}
		shown++
		// THE RAIL'S WORD AND NOT THE STORE'S ([PlanTaskRow.StateWord]). The
		// person reads `stopped` and `running` beside the row; a digest that said
		// `cancelled` and `claimed` of the same rows would have the conversation
		// and the person describing one plan in two vocabularies.
		fmt.Fprintf(&b, "%s · %s", labels[row.ID], cutChars(row.Title, planDigestLineChars))
		if word := row.StateWord(); word != "" {
			fmt.Fprintf(&b, " · %s", word)
		}
		// THE NEWEST NOTE AND NOTHING OLDER. A note is the one thing on a row
		// that can say the plan has gone wrong — a worker's finding, the
		// person's own word from the task page — and the newest is the one that
		// has not been acted on yet.
		if note := summaryFirstLine(row.Note, planDigestNoteChars); note != "" {
			fmt.Fprintf(&b, " · note: %s", note)
		}
		b.WriteByte('\n')
	}
	// WHAT THE BOUND LEFT OUT IS COUNTED. A chat told nothing about the rest
	// would reason as though the plan were eight rows wide, which is worse than
	// a chat that knows it is not seeing all of it.
	if over := len(rows) - shown; over > 0 {
		fmt.Fprintf(&b, "… and %d more, which `tasks` lists.\n", over)
	}
	b.WriteString(planDigestRule)
	return b.String()
}

// planDigestOrder is the rows with THE LIVE RUN FIRST, and every ended run's
// rows after it in the order they came. Store order is kept inside each half.
//
// [Agent.PlanTasks] answers ended runs oldest first and the live run last,
// which is the right order for a list a person scrolls and the wrong one for a
// digest cut at [planDigestRows]: a conversation with eight rows of history
// behind it was handed eight `cancelled` rows of runs long over and not one
// line of the run the person was talking about. The rows the bound leaves out
// are the old ones, and they are still counted.
func planDigestOrder(rows []PlanTaskRow) []PlanTaskRow {
	ordered := make([]PlanTaskRow, 0, len(rows))
	for _, row := range rows {
		if !row.Archived {
			ordered = append(ordered, row)
		}
	}
	for _, row := range rows {
		if row.Archived {
			ordered = append(ordered, row)
		}
	}
	return ordered
}

// planAnyOpen answers whether anything in the run is still going. A run whose
// every row has ended is a run nobody can steer, so its digest would be a
// paragraph of history in front of every sentence the person types — which is
// the cost this whole file is written to keep small.
func planAnyOpen(rows []PlanTaskRow) bool {
	for _, row := range rows {
		switch plandb.Status(row.Status) {
		case plandb.StatusDone, plandb.StatusFailed, plandb.StatusCancelled:
			continue
		}
		return true
	}
	return false
}

// planDigested is the person's message with the digest in front of it for the
// model, and their own sentence alone for the journal. It is [standingMarked]'s
// shape and it is that shape deliberately: the two doors differ in what the
// model reads, and in nothing else.
func planDigested(digest, text string) userMessage {
	return userMessage{
		message: textMessage("user", digest+"\n\n"+text),
		said:    text,
	}
}

// planDigestedParts is [planDigested] for a message another door has already
// assembled out of parts — the person's words and the pictures they attached
// (image.go). The digest is its own leading part, and [userMessage.lead]
// counts it, so the journal keeps the pictures and the words and not the rows.
//
// EVERY DOOR THE PERSON SPEAKS THROUGH OPENS ON THE ROWS. The digest reached
// the plain sentence only, so a person who changed their mind while attaching a
// screenshot, or while marking a draft standing, was talking to a conversation
// that could not see what was running — the one failure this file exists to end.
func planDigestedParts(digest string, user userMessage) userMessage {
	parts := make([]ai.ContentPart, 0, len(user.message.Content)+1)
	parts = append(parts, ai.ContentPart{Type: "text", Text: digest})
	user.message.Content = append(parts, user.message.Content...)
	user.lead++
	return user
}
