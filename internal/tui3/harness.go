package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE HARNESS OFFER.
//
// internal/session matched what somebody just typed against this build's
// sub-harness registry and found one that says it does exactly this
// (internal/subharness). The turn is held — before its first request — on one
// question:
//
//	? run harness "research"? · finds an answer across sources · [enter] run · [esc] no
//
// ONE ROW, and the row is the whole design. Every other question on this
// surface is a block: the approval question redraws the call it is about, the
// connect offer names the account and says who wants it. This one is a row,
// because of what it interrupts and what it costs.
//
//   - IT INTERRUPTS SOMEBODY MID-SENTENCE. The other two questions arrive
//     inside work a person asked for — a call they watched start, an account
//     the agent reached for. This one arrives on a turn nobody said was
//     unusual, and it arrives on turns that are NOT harnesses too, whenever the
//     matcher is generous. A block for that would be the surface stopping the
//     room to make an offer.
//   - THE NO COSTS NOTHING. Declining does not refuse a call, cancel a
//     connection or lose a turn: the turn the person typed runs, unchanged,
//     immediately. A question whose worst answer is free does not deserve four
//     rows and a rule.
//   - THE NAME IS THE WHOLE QUESTION. "run harness research?" is answerable by
//     somebody who has never read this file. The description rides beside it
//     when the frame has room and is the first thing dropped when it does not,
//     because it is context for the name, not the question.
//
// It sits under the connect offer, which sits under the approval question:
// three rungs of the same lane, urgent first. It is modal like they are — while
// it is up, keys that are not answers do nothing — for the shorter version of
// their reason: the session is holding a turn on this answer.

// harnessAsk is one unanswered offer.
type harnessAsk struct {
	// id is the token [session.Agent.ResolveHarness] takes back.
	id uint64
	// name is the harness, and the word the row says out loud.
	name string
	// desc is the entry's own sentence about itself, drawn dim beside the name
	// when the row has room for it (session.Event's Hint).
	desc string
	// model is what the run will ride, when the person's own words chose one —
	// "research the pricing tiers with opus" (session's harness.go). Empty is
	// the ordinary offer, which says nothing about models because nothing about
	// the model is unusual.
	model string
	// note is what the session could not do with a model the turn DID name: a
	// word no model here answers to. Its words are the session's, printed as
	// they arrived, and it is never a refusal — the row is the same question
	// either way, and yes still runs the harness.
	note string
}

// harnessTap is one pressable answer on the row.
type harnessTap struct {
	span hudSpan
	run  bool
}

// askHarness takes one session.EventHarnessOffer.
func (a *app) askHarness(ev session.Event) {
	if ev.Text == "" {
		// A harness with no name is a question nobody can answer. The session
		// never sends one; the check is here so that a surface cannot draw a
		// card that says `run harness ""?`.
		return
	}
	// The typed lists follow the draft, and the draft is suspended while a
	// question is up — a list left open under a modal is a list answering keys
	// nobody is pressing (consent.go says it first).
	a.closeLists()
	if a.pick.open {
		a.pick.close()
	}
	a.harnessAsks = append(a.harnessAsks, harnessAsk{
		id: ev.ID, name: ev.Text, desc: ev.Hint,
		model: ev.Model, note: ev.ModelNote,
	})
	a.follow()
	a.touch()
}

// asksHarness reports whether an offer owns the keyboard.
func (a *app) asksHarness() bool { return len(a.harnessAsks) > 0 }

// answerHarness resolves the offer at the head of the queue.
//
// NOTHING IS WRITTEN TO THE TRANSCRIPT IN EITHER DIRECTION. A yes is followed
// by the run, which the session announces and [app.noteHarness] draws; a no
// changed nothing, and a surface that recorded "you were offered a harness and
// said no" would be keeping a note about a thing that did not happen — the
// connect offer's own law, one rung down.
//
// THE MODEL THE ROW SHOWED GOES BACK WITH THE ANSWER. The person read "model:
// opus" and pressed enter, so that is what the answer is about; handing back
// nothing would leave the surface trusting that the session still remembers the
// same thing the row was drawn from.
func (a *app) answerHarness(run bool) {
	if len(a.harnessAsks) == 0 {
		return
	}
	head := a.harnessAsks[0]
	a.harnessAsks = a.harnessAsks[1:]
	if a.agent != nil {
		a.agent.ResolveHarness(head.id, run, head.model)
	}
	a.touch()
}

// dropHarnessAsks forgets every unanswered offer. It runs where the other two
// questions are dropped and for the same reason (app.go's [app.settle]): the
// turn that raised them is over, so the answers are late.
func (a *app) dropHarnessAsks() {
	if len(a.harnessAsks) == 0 {
		return
	}
	a.harnessAsks = nil
	a.touch()
}

// noteHarness draws one session.EventHarnessRun: the dim one-liner the nudge
// and the provider's retries use, because it is the same kind of fact. The
// person already said yes; this is the surface confirming what they said yes to
// is what started.
func (a *app) noteHarness(name string) {
	if name == "" {
		return
	}
	a.note("harness · " + name)
}

// ── the keys ────────────────────────────────────────────────────────────────

// The two answers, in the two keys this surface's other offers already answer
// to: enter is yes, esc is no. y and n are read silently beside them, exactly
// as the connect offer reads them — a hand that has learned them one rung up
// reaches for them here, and neither can collide with anything while the draft
// is suspended.
func (a *app) harnessAskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.asksHarness() {
		return nil, false
	}
	if msg.String() == "ctrl+c" {
		// Leaving is never modal, and mid-turn ctrl+c is the interrupt — which
		// releases the held turn the honest way.
		return nil, false
	}
	switch msg.String() {
	case "enter", "y":
		a.answerHarness(true)
	case "esc", "n":
		a.answerHarness(false)
	}
	// Everything else does nothing rather than typing into a conversation that
	// cannot move (consent.go states the modal rule in full).
	return nil, true
}

// ── the row ─────────────────────────────────────────────────────────────────

// harnessAskHeight is how many rows the offer takes: one, and one more for the
// count when a second offer is queued behind it. The queue is nearly always
// empty — the session asks at most once per turn — but a wake landing on a held
// turn can put a second one there, and a person who answers one question and
// gets another must have been told it was coming.
func (a *app) harnessAskHeight() int {
	if !a.asksHarness() {
		return 0
	}
	if len(a.harnessAsks) > 1 {
		return 2
	}
	return 1
}

// harnessAskRows draws the offer, under the connect block and above the draft.
func (a *app) harnessAskRows(width int) []string {
	// The targets are rewritten by every layout and by nothing else: a stale
	// span is a press that answers about the previous offer.
	a.harnessTaps = nil
	if !a.asksHarness() {
		return nil
	}
	head := a.harnessAsks[0]
	out := []string{a.harnessOffer(head, width)}
	if more := len(a.harnessAsks) - 1; more > 0 {
		out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
	}
	return out
}

// harnessOffer is the row: the question, the model when the turn chose one, the
// description when it fits, and the two answers.
//
// THE ANSWERS ARE NEVER WHAT GETS CUT, and the MODEL OUTLIVES THE DESCRIPTION.
// The line is assembled longest-first and shortened a piece at a time: the
// description goes first because it is context for a name a person can already
// read, and the model survives one rung longer because it is the one thing on
// the row that says this run would not be the ordinary one. The keys are never
// dropped at all — a row that fits by losing them is a question with no visible
// way to answer it.
func (a *app) harnessOffer(head harnessAsk, width int) string {
	question := glyphAsk + ` run harness "` + head.name + `"?`
	answers := []string{" · ", "[enter]", " run · ", "[esc]", " no"}
	parts := append([]string{question}, answers...)
	// Longest first, then each shorter reading in turn; the last one that fits
	// wins, and the bare question is what nothing fitting falls through to.
	for _, middle := range harnessMiddles(head) {
		wider := append(append([]string{question}, middle...), answers...)
		if harnessRowWidth(wider) <= width {
			parts = wider
			break
		}
	}
	if harnessRowWidth(parts) > width {
		// Too narrow even for the bare question and its keys. It is cut rather
		// than re-spelled, and it records no targets: a target under an ellipsis
		// is a press that answers something a person cannot read.
		return a.pal.ask(fit(strings.Join(parts, ""), width))
	}
	a.recordHarnessTaps(parts)
	var out string
	for _, part := range parts {
		switch {
		case part == "[enter]" || part == "[esc]":
			out += a.pal.askBold(part)
		case part == question:
			out += a.pal.askBold(glyphAsk) + a.pal.ask(part[len(glyphAsk):])
		case head.desc != "" && part == " · "+head.desc:
			out += a.pal.dim(part)
		case part == harnessModelPart(head):
			// The model is dim beside the question for the reason the description
			// is: the QUESTION is "run this harness?", and what it will run on is
			// the answer to a smaller question the person already asked when they
			// typed it.
			out += a.pal.dim(part)
		default:
			out += a.pal.ask(part)
		}
	}
	if a.hoveringHarnessAsk() {
		return a.pal.hover(out, width)
	}
	return out
}

// harnessMiddles is what sits between the question and its keys, longest
// reading first: everything, then the model alone, then whichever of the two the
// offer actually has. An offer that carries neither answers with nothing, which
// is the bare row every offer drew before a turn could name a model.
func harnessMiddles(head harnessAsk) [][]string {
	desc, model := "", harnessModelPart(head)
	if head.desc != "" {
		desc = " · " + head.desc
	}
	switch {
	case desc != "" && model != "":
		return [][]string{{desc, model}, {model}}
	case model != "":
		return [][]string{{model}}
	case desc != "":
		return [][]string{{desc}}
	}
	return nil
}

// harnessModelPart is the row's model segment, and it is the same string
// wherever it is read: `· model: opus` when the turn named one this install
// has, and the session's own note when it named one this install does not.
//
// The id sheds its vendor ("anthropic/claude-opus-5" draws as
// "claude-opus-5"), which is what every other model on this surface does with
// the same width for the same reason (render.go's [modelBase]) — the vendor is
// nine cells of a name the person already recognizes without it.
func harnessModelPart(head harnessAsk) string {
	if model := modelBase(strings.TrimSpace(head.model)); model != "" {
		return " · model: " + model
	}
	if note := strings.TrimSpace(head.note); note != "" {
		return " · " + note
	}
	return ""
}

// recordHarnessTaps writes the row's pressable columns: each key chip and the
// word beside it are one target, which is the connect offer's own rule — `[esc]`
// is five cells and `[esc] no` is nine, and that is the difference between a
// target a person hits and one they aim at.
func (a *app) recordHarnessTaps(parts []string) {
	taps := make([]harnessTap, 0, 2)
	at := 0
	for i, part := range parts {
		width := ansi.StringWidth(part)
		run, ok := false, false
		switch part {
		case "[enter]":
			run, ok = true, true
		case "[esc]":
			run, ok = false, true
		}
		if !ok {
			at += width
			continue
		}
		to := at + width
		if i+1 < len(parts) {
			// The separator between the two answers belongs to neither — a press
			// in the gap must not resolve as either.
			to += ansi.StringWidth(strings.TrimSuffix(parts[i+1], " · "))
		}
		taps = append(taps, harnessTap{span: hudSpan{from: at, to: to}, run: run})
		at += width
	}
	a.harnessTaps = taps
}

// harnessPress resolves a click on the row, and reports whether it took it.
//
// THE ROW SWALLOWS EVERY PRESS ON IT, answer or no answer, for the reason the
// blocks above it do: it is a thing the session is waiting on, and a press that
// missed the keys and fell through would expand a tool call while somebody was
// answering a question about their turn.
func (a *app) harnessPress(x, y int) bool {
	if !a.asksHarness() || a.copy.on || a.sheet.open {
		return false
	}
	// THE ROW IS RESOLVED BEFORE THE COLUMN: laying the chrome out is what
	// writes the spans, and reading them first would be reading where the
	// answers were drawn on the frame before this one.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeHarnessAsk {
		return false
	}
	for _, tap := range a.harnessTaps {
		if !tap.span.holds(x) {
			continue
		}
		a.answerHarness(tap.run)
		return true
	}
	return true
}

// harnessMark is what the pointer is over on row i of the block, which is the
// frame's half of the same geometry ([app.chrome]). The offer is the first row
// and the only pressable one; the count under it is a statement.
func (a *app) harnessMark(i int) chromeRow {
	if i == 0 {
		return chromeRow{kind: chromeHarnessAsk, index: i}
	}
	return chromeRow{}
}

// hoveringHarnessAsk reports whether the pointer is on the offer row.
func (a *app) hoveringHarnessAsk() bool { return a.hot.kind == hoverHarnessAsk }

// harnessRowWidth is what a row assembled from these parts costs in cells.
func harnessRowWidth(parts []string) int {
	return ansi.StringWidth(strings.Join(parts, ""))
}
