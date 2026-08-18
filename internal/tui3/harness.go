package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
	// card is the DESIGN's page, drawn (subharness.CardLines), and nil on the
	// ordinary offer. It is what makes this struct carry two questions rather
	// than two structs carrying one each: both are answered by the same method
	// with the same two keys, and the only difference is what a person is reading
	// while they answer — a name, or the shape somebody is about to keep.
	card []string
	// designed says this is a page waiting to be SAVED rather than a harness
	// waiting to be RUN. It is a field of its own rather than len(card) > 0
	// because it decides the words on the answer row, and a design whose card
	// could not be drawn is still a design.
	designed bool
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

// askHarnessDesign takes one session.EventHarnessDesignDone: a page this
// conversation asked for, finished, and waiting to be kept or dropped.
//
// It joins the SAME queue the offer joins, because it is the same question one
// rung along — this surface asks it in the same place, answers it with the same
// two keys, and hands the answer back through the same method. What it adds is
// the page: nobody approves a name they have not seen the shape of, so the card
// is drawn above the row (internal/subharness's card.go is the renderer every
// surface shares).
func (a *app) askHarnessDesign(ev session.Event) {
	if ev.Harness == nil {
		return
	}
	page := *ev.Harness
	a.closeLists()
	if a.pick.open {
		a.pick.close()
	}
	a.harnessAsks = append(a.harnessAsks, harnessAsk{
		id: ev.ID, name: page.Id.Name, desc: page.Id.Desc,
		model: ev.Model, card: subharness.CardLines(page), designed: true,
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

// dropHarnessAsks forgets every unanswered OFFER. It runs where the other two
// questions are dropped and for the same reason (app.go's [app.settle]): the
// turn that raised them is over, so the answers are late.
//
// A DESIGN CARD IS NOT DROPPED, and that is the one place these two questions
// part. An offer is a question ABOUT A TURN — the session is holding one on it,
// and when the turn ends the question has no subject left. A design outlives its
// turn by construction (session's harness_build.go): the turn ended the moment
// the design started, so a settle that swept the card away would throw away the
// answer to the thing the person actually asked for, seconds before they gave
// it.
func (a *app) dropHarnessAsks() {
	if len(a.harnessAsks) == 0 {
		return
	}
	kept := a.harnessAsks[:0]
	for _, ask := range a.harnessAsks {
		if ask.designed {
			kept = append(kept, ask)
		}
	}
	if len(kept) == len(a.harnessAsks) {
		return
	}
	a.harnessAsks = kept
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

// ── the design lane ─────────────────────────────────────────────────────────
//
// A design is asked for in a sentence and answered minutes later, on no turn at
// all (session's harness_build.go). So it arrives the way a task node's landing
// arrives: on a STANDING subscription this surface holds for the life of the
// session, pumped into the program loop with a generation, because a lane from
// an agent that has been replaced must not put a card on the screen of the
// conversation that replaced it.

// designAgent is the slice of *session.Agent this lane needs, asserted rather
// than added to [Agent] for [taskAgent]'s reason: the harness designer is
// OPTIONAL. Every scripted agent in this package's own tests has never heard of
// one, and widening the package interface would make a session without a
// designer un-representable.
type designAgent interface {
	// HarnessDesigns is the standing subscription: the design starting, the card
	// asking whether to keep the page it wrote, and the notes that say a design
	// failed, was declined or was saved.
	HarnessDesigns() <-chan session.Event
}

// designer is the agent under this surface, when it has a designer at all.
func (a *app) designer() (designAgent, bool) {
	agent, ok := a.agent.(designAgent)
	return agent, ok
}

// watchDesigns opens the lane and starts pumping it. It is called wherever
// [app.watchTasks] is, and for the same reason: the channel belongs to the agent
// that handed it over, so a replaced conversation gets a new one.
func (a *app) watchDesigns() tea.Cmd {
	agent, ok := a.designer()
	if !ok {
		return nil
	}
	a.designGen++
	a.designLane = agent.HarnessDesigns()
	return waitDesign(a.designLane, a.designGen)
}

// waitDesign takes one event off the lane and asks for the next.
func waitDesign(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return designLaneClosedMsg{gen: gen}
		}
		return designEventMsg{gen: gen, ev: ev}
	}
}

// designEvent folds one event from the lane in and re-arms the pump.
func (a *app) designEvent(ev session.Event) tea.Cmd {
	switch ev.Kind {
	case session.EventHarnessDesign:
		// The turn is already over — that is the whole arrangement — so this is
		// the only thing on screen saying that work is happening. It is a note for
		// the reason the run announcement is one: nobody has to answer it.
		a.note("harness · " + harnessDesignLead(ev.Model) + firstLineOf(ev.Text))
	case session.EventHarnessDesignDone:
		// A QUESTION OUTRANKS A PANEL, on the terms every other question on this
		// surface states: the block is drawn above the input, and a question drawn
		// under a fullscreen sheet is an answer nobody can reach.
		a.closeSettings()
		a.closeExpand()
		a.askHarnessDesign(ev)
	case session.EventNotice:
		// What became of a design: it failed, it was dropped, it was saved. The
		// session writes the sentence (harness_build.go) so that every surface says
		// the same thing about the same outcome.
		a.note(ev.Text)
	}
	return tea.Batch(waitDesign(a.designLane, a.designGen), a.wake())
}

// harnessDesignLead opens the note a starting design writes, and it NAMES THE
// MODEL when the session resolved one.
//
// A design is two model calls on a model the person did not type: with a high
// tier configured it is RoleDesigner's and not the one this conversation is on
// (session's harness_build.go), and this note is the only thing on screen while
// it runs. So the note says whose judgement is writing the page.
//
// With no model on the event the old sentence is left exactly as it was, down to
// the single space: a separator standing in front of a goal with no fact behind
// it is punctuation pretending to be information.
func harnessDesignLead(model string) string {
	if model = strings.TrimSpace(model); model == "" {
		return "designing "
	}
	return "designing with " + model + " · "
}

// ── the row while a harness is being written ────────────────────────────────
//
// A DESIGN WAS THE ONE PIECE OF WORK ON THIS SURFACE THAT HAPPENED IN SILENCE.
// The turn ends the moment it starts (session's harness_build.go), so the
// conversation goes idle; the note above is one dim line that the next thing
// anybody types scrolls away; and the card is a minute or two off. From the
// outside — somebody who asked for a harness and is watching the screen — that
// is indistinguishable from a program that did nothing.
//
// So it takes a place on the strip, which is the row this surface already keeps
// for what is ALIVE (taskstrip.go), in front of the tasks and beside a running
// harness:
//
//	  ⠙ ◆ designing a harness · triage flaky tests · 1m 12s
//
// IT IS NOT A TASK AND ITS ROW DOES NOT PRETEND TO BE ONE. Every other chip on
// that row is a DOOR: it stands for a node with an id a person can say out loud,
// a room with its own transcript, a branch and a report, and pressing it walks
// in. A design has none of those — nothing is written down until somebody
// approves the card, there is no room to walk into and no number that names it
// in a sentence — so this chip carries no identity mark keyed to an id, no
// title of the work's own, and no door. It is a statement, and the strip's press
// swallows it like every other press on that row.
//
// AND IT INVENTS NOTHING. Two model calls against a long guide have no progress
// to report: there is no percentage, no bar and no step count here, because
// nothing in this program knows one. The row is the fact that a harness is being
// written, what it is being written for, and how long that has taken — which is
// the whole of what anybody knows, and by the emptiness law it is therefore the
// whole of what is drawn.
//
// THE ONE THING IT OFFERS IS THE ONE THING THAT CAN BE DONE. A design in flight
// can be ended (session's cancel.go), so the chip carries the same ✕ every other
// piece of live work on this surface is stopped by, raising the same card
// (stop.go). It is drawn on the chip itself rather than following the roster's
// cursor the way a task's ✕ does, because the roster's cursor cannot reach a
// thing that is not on the roster — and a second cursor for one chip would be a
// keyboard model invented for a row that has no keys.
//
// IT IS ASKED FOR AND HELD NOWHERE ([session.Agent.HarnessesBeingDesigned]),
// which is the bargain the running harness chip makes one file along. That is
// what makes it CLEAR RELIABLY: the finished card, the failure, the decline, the
// stop and the window running out all take the row away for the same reason —
// the session stopped naming that work — and so do /new and a resumed session,
// because the question is put to whichever agent is under the surface now.

// designLive is the slice of the session this row needs, asserted rather than
// added to [Agent] for [designAgent]'s reason: the designer is OPTIONAL, and a
// session that has never heard of one must stay representable. It is also what
// keeps this row off a remote screen — building a harness is deliberately off
// over --host (cmd/aforge's engine.go leaves the store nil), so there is never
// anything for it to name there.
type designLive interface {
	// HarnessesBeingDesigned is what this session is still writing, oldest
	// first.
	HarnessesBeingDesigned() []session.HarnessBeingDesigned
}

// designsInFlight is what is being written right now, and nothing at all under a
// session that cannot design.
func (a *app) designsInFlight() []session.HarnessBeingDesigned {
	live, ok := a.agent.(designLive)
	if !ok {
		return nil
	}
	return live.HarnessesBeingDesigned()
}

// designingHarness reports whether anything is being written — the cheap half of
// the question, for the strip's own showing test and the paint clock.
func (a *app) designingHarness() bool { return len(a.designsInFlight()) > 0 }

// designChipCap is how much of the brief rides on the chip. It is the strip's
// own title budget, so a goal is cut exactly where a task's title is and the two
// read as one row.
const designChipCap = stripTitleCap

// designWord is what the chip calls the work, in the words a person would use
// about it. The plural is spelled out rather than counted with a "+N", because
// this is a sentence and not a list.
func designWord(n int) string {
	if n == 1 {
		return "designing a harness"
	}
	return "designing " + itoa(n) + " harnesses"
}

// designChip is the chip for what is being written: the spinner, the harness
// mark, the words, and — where the row has the cells for them — the goal and the
// clock. It answers with the painted chip, the CELLS it occupies, and where the
// ✕ inside it landed, which is the trio the strip's budget and its press are
// spent in.
//
// THE GOAL IS THE FIRST THING DROPPED and the clock outlives it, which is the
// offer row's law one rung along (see [harnessMiddles]): the brief is context
// for something the person typed themselves a minute ago, and the elapsed time
// is the only fact on the chip that is news — it is what answers "is this still
// moving".
func (a *app) designChip(designs []session.HarnessBeingDesigned, width, at int) (string, int, hudSpan) {
	if len(designs) == 0 {
		return "", 0, hudSpan{}
	}
	glyph := a.pal.accent(tokens.Spinner(a.paints / spinnerStep))
	if a.linear {
		glyph = a.pal.accent(glyphRunASCII)
	}
	mark := a.linearMark(glyphHarness, glyphHarnessASCII)
	word := designWord(len(designs))
	head := stripPad + glyph + " " + a.pal.muted(mark) + " " + a.pal.accent(word)
	cols := stripPadCols + ansi.StringWidth(glyph) + 1 + ansi.StringWidth(mark) + 1 + ansi.StringWidth(word)

	// The ✕ rides only where the frame has cells to spend on a control and only
	// when there is exactly one design behind it (see [designStopTarget]).
	stopMark, stopCols := "", 0
	if layoutTier(width) == tierWide && !designStopTarget(designs).empty() {
		stopMark = a.linearMark(roomStopMark, roomStopMarkASCII)
		stopCols = ansi.StringWidth(stopMark) + 1 // the space that separates it from the words
	}
	tail := ""
	for _, reading := range designReadings(designs, a.now()) {
		if cols+ansi.StringWidth(reading)+stopCols <= width-at {
			tail = reading
			break
		}
	}
	cols += ansi.StringWidth(tail) + stopCols

	chip := head + a.pal.dim(tail)
	if stopMark != "" {
		chip += " " + a.pal.dim(stopMark)
	}
	return chip + stripPad, cols, stripStopSpan(at, cols, stopCols)
}

// designReadings is what may sit between the words and the chip's right edge,
// longest first, ending in nothing at all — which is what a frame with no cells
// to spare draws, and it still says a harness is being written.
//
// A design under a second old has no clock (countUpWord's own floor), and more
// than one in flight has no goal: two briefs behind one set of words would have
// the chip naming one of them and standing for both. The clock in that case is
// the OLDEST one's, which is the honest answer to how long this has been going
// on for.
func designReadings(designs []session.HarnessBeingDesigned, now time.Time) []string {
	goal, clock := "", countUpWord(now.Sub(designs[0].Since))
	if len(designs) == 1 {
		goal = fit(firstLineOf(designs[0].Goal), designChipCap)
	}
	var out []string
	switch {
	case goal != "" && clock != "":
		out = append(out, railSep+goal+railSep+clock, railSep+clock)
	case goal != "":
		out = append(out, railSep+goal)
	case clock != "":
		out = append(out, railSep+clock)
	}
	return append(out, "")
}

// firstLineOf keeps a note to one row. A goal is a sentence somebody typed and
// can carry newlines; the note lane is one line.
func firstLineOf(text string) string {
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return strings.TrimSpace(text)
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

// harnessCardRows is how much of a design's page is drawn above the question.
//
// It is a cap and not a window: the card is a numbered list in RUN ORDER, so its
// first lines are the ones that say what the thing does, and a page longer than
// this is a page whose tail is bounds a person can read in /harness once it is
// saved. What is cut is SAID — a block that quietly stopped at fourteen lines
// would be asking somebody to approve a shape while showing them part of it.
const harnessCardRows = 14

// harnessCard is the page as this block draws it, capped.
func (a *app) harnessCard(head harnessAsk) []string {
	if len(head.card) <= harnessCardRows {
		return head.card
	}
	out := append([]string(nil), head.card[:harnessCardRows-1]...)
	return append(out, "… "+itoa(len(head.card)-(harnessCardRows-1))+" more lines")
}

// harnessAskHeight is how many rows the block takes: the question, the card
// above it when the question is about a design, and one more for the count when
// a second question is queued behind it.
//
// The queue is nearly always empty — the session offers at most once per turn —
// but a design landing while an offer is up can put a second one there, and a
// person who answers one question and gets another must have been told it was
// coming.
func (a *app) harnessAskHeight() int {
	if !a.asksHarness() {
		return 0
	}
	rows := 1 + len(a.harnessCard(a.harnessAsks[0]))
	if len(a.harnessAsks) > 1 {
		rows++
	}
	return rows
}

// harnessAskRows draws the block, under the connect block and above the draft.
func (a *app) harnessAskRows(width int) []string {
	// The targets are rewritten by every layout and by nothing else: a stale
	// span is a press that answers about the previous offer.
	a.harnessTaps = nil
	if !a.asksHarness() {
		return nil
	}
	head := a.harnessAsks[0]
	var out []string
	// THE PAGE IS DIM AND THE QUESTION IS NOT. What a person is being asked is
	// the last line of the block; the card above it is what they are answering
	// about, drawn the way every other quotation on this surface is.
	for _, line := range a.harnessCard(head) {
		out = append(out, a.pal.dim(fit(line, width)))
	}
	out = append(out, a.harnessOffer(head, width))
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
	if head.designed {
		// THE VERB IS WHAT CHANGES, and it is the only thing that does. Saying yes
		// here writes a page into the registry rather than starting a run, and a
		// row that said "run" would be asking the wrong question about the same
		// name — the design has not run and is not about to.
		question = glyphAsk + ` save harness "` + head.name + `"?`
		answers = []string{" · ", "[enter]", " save · ", "[esc]", " discard"}
	}
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
	if head.desc != "" && !head.designed {
		// A DESIGN'S ROW NEVER REPEATS ITS DESCRIPTION: the card's own head line,
		// two rows up, is `name · draft · what it is for`, and saying it twice
		// would cost the sentence that says what the harness will run on.
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
// frame's half of the same geometry ([app.chrome]). The QUESTION is the only
// pressable row: the card above it is a quotation and the count under it is a
// statement, and a target on either would be a press that answers a question the
// pointer was not on.
func (a *app) harnessMark(i int) chromeRow {
	if !a.asksHarness() {
		return chromeRow{}
	}
	if i == len(a.harnessCard(a.harnessAsks[0])) {
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
