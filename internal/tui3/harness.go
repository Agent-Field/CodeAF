package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

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
//	? run harness "research" [1] run it · [2] not now · [esc] later
//	  finds an answer across sources · model: claude-opus-5
//
// THE BLOCK DRAWS IT NOW (question.go). What used to be here was a row of this
// lane's own — its own layout arithmetic, its own two keys, its own pressable
// columns and its own idea of what `esc` meant — and every law it carried had to
// be argued again in eight other blocks that never got round to it
// (docs/design/questions/DESIGN.md).
//
// It is still ONE ROW, which was the whole of that row's design and is now a
// fact about the object rather than about a renderer: the offer names a
// [session.FormLine], because the name is the whole question and the no costs
// nothing — declining refuses no call, cancels no connection and loses no turn,
// so the turn the person typed runs unchanged and immediately.
//
// What is left in this file is the lane's own two things: the model the offer
// SHOWED, handed back with the answer so that what runs is what was read, and
// the design lane below.

// askHarness takes one session.EventHarnessOffer.
func (a *app) askHarness(ev session.Event) {
	if ev.Text == "" {
		// A harness with no name is a question nobody can answer. The session
		// never sends one; the check is here so that a surface cannot draw a
		// card that says `run harness ""?`.
		return
	}
	// The typed lists follow the draft, and the draft is where a correction gets
	// typed: a completion list left open under a question answering to digits is
	// two readers for one keystroke (consent.go says it first).
	a.closeLists()
	if a.pick.open {
		a.pick.close()
	}
	a.raiseQuestion(a.harnessShown(ev))
	a.follow()
	a.touch()
}

// harnessShown is one offer as the block holds it: the engine's own question
// object, plus the one thing the object does not carry — the model the row
// showed, on its way back to [Agent.ResolveHarness].
//
// THE OBJECT IS THE ENGINE'S OWN BUILDER AND NOT A SECOND ONE
// ([session.HarnessQuestion]). Every other lane on this block says its question
// twice — once here and once in internal/session — because the two roads reach
// the surface a moment apart and the block keys a question by its lane and its
// id; this one is built where it is defined, so there is nothing to drift.
func (a *app) harnessShown(ev session.Event) questionShown {
	model := strings.TrimSpace(ev.Model)
	return questionShown{
		question: session.HarnessQuestion(ev.ID, ev),
		answered: func(answer session.Answer) session.Answer {
			if model == "" {
				return answer
			}
			// THE MODEL THE ROW SHOWED GOES BACK WITH THE ANSWER. The person read
			// `model: opus` and pressed a key, so that is what the answer is
			// about; handing back nothing would leave this surface trusting that
			// the session still remembers the same thing the row was drawn from
			// ([session.HarnessModelNote]).
			if answer.Comments == nil {
				answer.Comments = map[string]string{}
			}
			answer.Comments[session.HarnessModelNote] = model
			return answer
		},
	}
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

// asksHarness reports whether an offer is open on the block.
func (a *app) asksHarness() bool {
	for _, open := range a.questions {
		if open.question.Kind == session.QuestionHarness && open.question.Ask == session.AskPermission {
			return true
		}
	}
	return false
}

// dropHarnessAsks forgets every unanswered OFFER. It runs where the other
// questions are dropped and for the same reason (app.go's [app.settle]): the
// turn that raised them is over, so the answers are late.
//
// A DESIGN IS NOT AN OFFER AND IS NOT DROPPED. Its question outlives every turn
// — the page was asked for in a sentence and finished minutes later, on no turn
// at all — which is the whole distinction [session.HarnessQuestion] draws
// between the lane's two shapes.
func (a *app) dropHarnessAsks() {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionHarness || open.question.Ask != session.AskPermission {
			continue
		}
		a.withdrawQuestion(open.question, harnessOfferGoneWord)
		return
	}
}

// harnessOfferGoneWord is why an offer stopped being a question: the turn that
// made it is over, so the answer would land nowhere. It is said out loud because
// a question that simply vanished would leave somebody pressing `1` at nothing.
const harnessOfferGoneWord = "the turn it was offered on has finished"

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
