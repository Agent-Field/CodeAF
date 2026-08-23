package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
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

// askHarnessDesign takes one session.EventHarnessDesignDone: a page this
// conversation asked for, finished, and waiting to be kept or dropped.
//
// The page lands in the transcript, not in the offer queue. That distinction is
// the feature: a design is ordinary scrollable content and never owns the
// keyboard merely because it finished while somebody was typing.
func (a *app) askHarnessDesign(ev session.Event) {
	if ev.Harness == nil {
		return
	}
	a.finishHarnessCard(ev)
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
	a.harnessStep = ""
	// WHICH HARNESS is the only thing this line says that the person does not
	// already have, so it is the one that steps up (payload.go).
	a.noteFacts("harness · "+name, name)
}

// stepHarness draws one session.EventHarnessStep: the step the run just
// finished, on the one row under the announcement.
//
// THE ROW IS REPLACED AND NEVER APPENDED. A harness run takes minutes and its
// report carries the whole trail (subharness.RunCard), so every step kept in the
// feed would be the trail written down twice — the rule the design lane's own
// live row is written to, one lane over ([app.progressHarnessRoom]). What this
// row answers is "which part of it is happening", and only the current answer to
// that is worth a line.
//
// It is drawn by [subharness.StepLine], which is the same renderer the report's
// card uses, so the step a person watched and the step they read back afterwards
// cannot say two different things.
func (a *app) stepHarness(ev session.Event) {
	if ev.Step == nil {
		return
	}
	line := strings.TrimSpace(subharness.StepLine(*ev.Step))
	if line == "" {
		return
	}
	a.harnessStep = line
	a.touch()
}

// dropHarnessStep clears that row. A run that is over is a run with no step in
// flight, and the report is on screen by then saying what every step did.
func (a *app) dropHarnessStep() {
	if a.harnessStep == "" {
		return
	}
	a.harnessStep = ""
	a.touch()
}

// ── the design lane ─────────────────────────────────────────────────────────
//
// A design is asked for in a sentence and answered minutes later, on no turn at
// all (session's harness_build.go). So it arrives the way a task node's landing
// arrives: on a STANDING subscription this surface holds for the life of the
// session, pumped into the program loop with a generation, because a lane from
// an agent that has been replaced must not put a card on the screen of the
// conversation that replaced it.
//
// IT CARRIES TWO THINGS AND NOT THREE. The announce, and the card. What BECAME
// of a design used to ride here as a third — a note saying it was saved, dropped
// or failed — and it does not any more: the design is a task, and a task's
// ending is a settle card in the transcript with the outcome on it (taskdone.go).
// A note beside that card would be the same news drawn twice.

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
	if leavable, ok := agent.(leavableDesigner); ok {
		a.designLane, a.stops.designs = leavable.WatchHarnessDesigns()
	} else {
		a.designLane, a.stops.designs = agent.HarnessDesigns(), nil
	}
	return waitDesign(a.designLane, a.designGen)
}

// leavableDesigner is the design lane WITH A WAY OUT OF IT (session's
// harness_build.go). It is asserted separately from [designAgent] for that
// interface's own reason, and a nil stop is an agent that can only be abandoned
// (switcher.go's [laneStops]).
type leavableDesigner interface {
	WatchHarnessDesigns() (<-chan session.Event, func())
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
		//
		// AND IT NAMES THE TASK, which is the whole of what changed when a design
		// became a node (session's harness_task.go). The line is one line, and the
		// number on the end of it is the door: the roster has a row with that id,
		// pressing it walks into the design's own room, and the id is what a
		// person says out loud afterwards.
		a.beginHarnessCard(ev)
	case session.EventHarnessProgress:
		a.progressHarnessCard(ev)
		a.progressHarnessRoom(ev)
	case session.EventHarnessDesignDone:
		a.askHarnessDesign(ev)
	case session.EventSubharnessProposal:
		// CHAT OFFERING A SAVED PROGRAM, and it arrives HERE because it is raised
		// on this lane and no other (session's tools_subharness.go calls
		// emitHarness): the turn that asked is parked inside its own batch, so
		// the question cannot ride the turn's stream the way an approval does.
		// A surface that only read the turn's events would never see it, which
		// is what left this card undrawn for as long as the lane had no case.
		a.proposeSubharness(ev)
	case session.EventSubharnessProposalOff:
		// AND THE SAME LANE TAKES IT BACK DOWN. The window on the tool call has
		// closed or the turn was interrupted, and nothing is listening to that
		// card any more (subharness.go's [app.withdrawSubharnessProposal]).
		a.withdrawSubharnessProposal(ev.ID, ev.Text)
	case session.EventHarnessDesignRevising:
		// THE CARD COMES DOWN because the page on it is about to stop existing:
		// the person said what was wrong with it and the designer is rewriting it
		// (session's EventHarnessDesignRevising). Left standing it would be a save
		// key over a draft that has been replaced.
		a.withdrawHarnessCard(ev)
	}
	return tea.Batch(waitDesign(a.designLane, a.designGen), a.wake())
}

// designTaskWord is the tail of that note: which task the design is running as.
//
// It is EMPTY WHERE THERE IS NO NODE, by the emptiness law and for a real case
// rather than a defensive one: a surface talking to an older engine, or to one
// over --host where designing is off entirely, gets an event with nothing on it,
// and a dangling separator in front of no number is punctuation pretending to be
// information.
func designTaskWord(notice *session.TaskNotice) string {
	if notice == nil || notice.ID == 0 {
		return ""
	}
	return " — task " + itoa(int(notice.ID))
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

// firstLineOf keeps a note to one row. A goal is a sentence somebody typed and
// can carry newlines; the note lane is one line.
func firstLineOf(text string) string {
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return strings.TrimSpace(text)
}

// ── the row a design used to have ───────────────────────────────────────────
//
// A DESIGN WAS THE ONE PIECE OF WORK ON THIS SURFACE THAT HAPPENED IN SILENCE,
// and it had a chip of its own here to answer for that: a spinner, the words
// "designing a harness", the goal, a clock and a ✕. It was a statement and not a
// door, because there was nothing to walk into — no room, no transcript, no
// number a person could say out loud.
//
// EVERY ONE OF THOSE EXISTS NOW, so the chip is gone and the design takes an
// ordinary place on the strip beside the tasks (session's harness_task.go). It
// has an id, a title that leads with the word "harness", a phase on its row that
// moves from "designing" to "awaiting your look", a room with the whole design
// thread in it, and the same ✕ every other node is stopped by. A second chip for
// the same work would have been the strip drawing it twice, and the one it kept
// is the one that opens.

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
	rows := 1
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
		return a.pal.cursor(out, width)
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
// frame's half of the same geometry ([app.chrome]).
func (a *app) harnessMark(i int) chromeRow {
	if !a.asksHarness() {
		return chromeRow{}
	}
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
