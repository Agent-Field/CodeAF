package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE CORRECTION IS DRAWN WHERE IT WAS SAID — a sentence typed INTO a running
// turn lands between the tool rows it interrupted, in the person's own column,
// so the page reads as a conversation in order.
//
//	› port the parser to the new lexer
//
//	  ├─▶ read lexer.go
//	  ╰─▶ read parse.go
//
//	…the reply the correction cut, as far as it got…
//
//	└ use the staging bucket, not production · ⠋ stopped the reply here
//
//	  ╰─▶ bash go build
//
// The engine's account of this is internal/session/steer.go, and the sentence
// that matters up here is its first: A STEER INTERRUPTS THE CURRENT GENERATION
// AND NOT THE TURN. The request in flight is cut, the assistant text that had
// already arrived stays where it is, and the SAME question carries on from the
// person's words. So the correction is not a new question — it keeps the ELBOW
// rather than the person's own `›`, because the mark says "still the same
// question" where a turn glyph would say "a new one" — and it is a block of the
// transcript like any other, at the position it arrived at.
//
// ── THE DEFECT THIS SHAPE ENDS ─────────────────────────────────────────────
//
// The elbows used to hang off the TRUNK: the block that opened the turn. On a
// turn with any work in it that block is above every tool row, and on a turn
// long enough to scroll it is above the top of the screen — so a person who
// pressed `→` watched their sentence leave the waiting strip and appear
// nowhere. "steering in chat when I press it the message just seems to
// disappear." It was on the page the whole time, forty rows up, under a
// question they could no longer see.
//
// Position is not decoration here. It is the one thing a transcript carries
// that a list of sentences does not: WHEN each was said, relative to the work
// around it. The engine's own journal already keeps a steer in place — an
// ordinary user message in the middle of the turn's messages
// (internal/session's sessionfile.go) — so drawing it in place is the surface
// agreeing with the record rather than re-arranging it.
//
// WHAT THE OTHER READING COSTS, AND WHY THE GLYPH STAYS. A steer is an ordinary
// user message in the transcript — it has to be, because that is what the model
// reads — so drawn with the person's own `›` a corrected turn read as a run of
// unrelated questions from somebody who kept interrupting themselves. The elbow
// is what keeps that from happening without moving the words: one mark says
// "this is the same question, continued", and the turn counter is not bumped
// for it (app.go's [app.submittingShown] only counts a message that started a
// stream), so nothing above or below it changes turn.
//
// ── THE INK, AND WHY IT IS THIS ────────────────────────────────────────────
//
// A STEER IS PART OF THE QUESTION'S FACT AND NEVER A NEW ACCENT. THE ACCENT
// BUDGET (docs/DESIGN-LANGUAGE.md) is one lit element per screen, and a
// correction the person typed four minutes ago is not it. So:
//
//	the elbow glyph   dim     furniture, like every mark this surface draws
//	                          about its own structure
//	the words         narr    ONE READING STEP BELOW THE QUESTION'S OWN
//
// The question's body is [palette.muted] (render.go's entryUser says why), and
// [palette.narr] is authored as exactly "one lightness step under the second
// voice" in the body's own hue family (styles.go). That is the step this design
// asks for, spelled with a constant that already exists — no new colour, and
// nothing that says "signal" where the fact is "the same person, still talking".
// The alternative was dim, and dim is THIS SURFACE'S OWN MURMUR: a person's
// sentence painted in the tier the notes and the hints wear would be the
// surface claiming words it did not write.
//
// AND IT IS FLUSH LEFT, in the person's own column. THE INDENT LAW (render.go's
// [app.deckRows]) is that flush-left is what was said to the person and two
// columns in is what was done on their behalf; a correction is a thing they
// said, so it stands in the same column their question stands in, with the tool
// rows it interrupted stepped in beside it.
//
// ── THE LANDING MOMENT ─────────────────────────────────────────────────────
//
// A steer is accepted at once and CONSUMED only when the model is actually
// given it, at the running turn's next step boundary. Those are two different
// facts about the same row and the row says both:
//
//	accepted, not yet consumed   the working idiom — the spinner every live row
//	                             on this surface turns, and the engine's own
//	                             account of where the words are landing
//	consumed                     the reading ladder's own fade: ink while it is
//	                             news, muted while it is recent, then the
//	                             settled narr forever ([hudFresh], [hudWarm])
//
// THE CLAUSE IS THE ENGINE'S SENTENCE AND NOT THIS FILE'S GUESS. Cutting the
// generation is one of three things the engine can do with a correction, and it
// says which in [session.SteerNote.Landing]: `stopped the reply here` when the
// request in flight was cut, `kept bash running as job 3` or `stopped the
// running command` when a long foreground bash was adopted or killed, and
// `waiting for the running step` when a short tool is being allowed to finish.
// A surface that drew its own word for all three would be describing a machine
// it cannot see; [steerPendingWord] is the fallback for a note that carries no
// account at all, which no session sends and a scripted event can.
//
// THE CLAUSE IS NEWS AND GOES WHEN THE NEWS DOES. It answers "what is happening
// to my words RIGHT NOW", so it is drawn while they are still waiting and is
// gone once they land — after that the block's own POSITION says where they
// landed, which is the whole of this file's design, and a clause repeating it
// forever would be furniture rather than an answer.
//
// NOTHING CLAIMS CONSUMED BEFORE [session.EventSteerConsumed]. The whole worth
// of the three events is that the surface can stop guessing, and a row that
// settled the instant somebody pressed the key would be a record of an
// intention (steer.go's [Agent.consumedSteerLocked] makes exactly this point
// about the journal).
//
// The fade reuses the status line's ramp and its two one-shot wakeups
// ([fadeTicks]) rather than a ticker, which is this surface's whole idle budget
// (effortscope.go took the same ramp for the same reason). Each block carries
// its own landing instant rather than sharing one "newest fact" record the way
// [effortMoved] does: two steers typed a step apart land at two boundaries and
// are two pieces of news, and two typed in one step land together and fade
// together, which is what actually happened.
//
// ── THE FALL-THROUGH LEAVES THE PAGE ───────────────────────────────────────
//
// A boundary is not promised. A steer still waiting when the turn ends never
// reached the model, so its block MUST NOT stay in the middle of that turn —
// the row would be this surface claiming the model read something it never saw,
// and the words are about to be drawn again a moment later as the question they
// become ([Agent.liftSteersLocked] re-homes them on the follow-up queue, and
// followup.go's [app.startFollow] draws that line like any other). Two copies
// of one sentence is the one thing this must not leave behind.
//
// So the block is WITHDRAWN, on echo.go's own rule and for echo.go's reason:
// truncated when it is last and emptied where it is not, because removing an
// entry from the middle would move every index after it and the forming rows,
// the selection and the thought marker are all held by index. An emptied block
// draws no rows at all — the layout pass drops a block with no rows before it
// gives it a row of its own — and [entryWithdrawn] is what keeps the fold and
// the answer hierarchy from reading a gap nobody can see as something that
// happened.
//
// AND A TURN THAT ENDED WITHOUT SAYING SO SWEEPS THE SAME WAY. After a person
// presses stop nothing new is drawn on this surface (app.go's [app.event]), so
// the fall-through's own event is swallowed with everything else — and a row
// left spinning inside a turn the model never carried it into would be the one
// lie this file exists to prevent. [app.settleSteers] withdraws every
// correction that never landed, at the moment the turn settles, under the same
// law: if the turn is over and the words were never consumed, they were never
// part of it.
//
// ── AND A FOLD MAY NOT HIDE THEM ───────────────────────────────────────────
//
// The `worked` chip collapses the machinery between a question and its answer,
// and THE ONE THING ON THIS SURFACE A FOLD MAY NEVER HIDE IS THE PERSON'S OWN
// WORDS. workfold.go's [deriveWorkfolds] already ends a fold group at one of
// their messages for exactly that reason; a correction is one of their
// messages, so it ends a group too, and a corrected turn folds around the
// sentence rather than over it.

// glyphSteer is the elbow, and glyphSteerASCII is what a terminal with no
// box-drawing gets. `+` is this surface's own ASCII corner already — it is the
// first cell of [railASCII], where `╰` is the first cell of [railLast] — so a
// reader who has met one has met the other.
const (
	glyphSteer      = "└ "
	glyphSteerASCII = "+ "
)

// steerPendingWord is what an elbow says while the model has NOT been given it
// yet AND the engine sent no account of where the words are landing
// ([session.SteerNote.Landing] is the sentence it sends when it has one). It is
// the verb the engine and the person both already use for the act, and it is a
// word rather than a bare spinner because every mark on this surface has a word
// near it (docs/DESIGN-LANGUAGE.md's refusal of icon-only minimalism).
const steerPendingWord = "steering"

// steerFellWord is the honesty line, in the dim "· " lane this surface says
// everything of its own in. It states the two halves a person needs: their
// words missed the answer they were aimed at, and they were not thrown away.
const steerFellWord = "your correction came after the answer finished — asking it as a new question"

// steerElbow is one correction as the surface holds it: the words, when they
// were sent, and which of the three things happened to them.
//
// The ID is the engine's ([session.SteerNote]) and it is what pairs an outcome
// with the row the acceptance drew. It is NOT matched by text on purpose: two
// identical corrections typed a second apart are two steers, and a surface that
// paired them by their words would settle the wrong row.
type steerElbow struct {
	id    uint64
	words string
	at    time.Time
	// landed is the instant [session.EventSteerConsumed] said the model had been
	// given these words, and the anchor the fade above is measured from. Zero
	// while the steer is still waiting for a boundary.
	landed time.Time
	// landing is the ENGINE'S OWN account of where these words are going to
	// arrive, carried on the acceptance ([session.SteerNote.Landing]): the reply
	// in flight was cut, a long bash was adopted as a job or killed, or a short
	// tool is being allowed to finish. It is the clause the row wears while it
	// waits, and it is empty on a note that carried none.
	landing string
	// consumed is the fact itself. It is a field beside the instant rather than
	// `!landed.IsZero()` because a REPLAYED elbow knows it landed and does not
	// know when the surface would have said so — the journal keeps the SEND's
	// instant, not the boundary's (sessionfile.go's [journalSteer]) — so a
	// replayed row is settled and unfaded, which is what a fact from yesterday is.
	consumed bool
}

// ── the events ──────────────────────────────────────────────────────────────

// steerAccepted puts one correction into the transcript, at the point in it
// where the person said it.
//
// IT GOES THROUGH [app.said] and not through a bare append, for that door's own
// reason: a block appended while the model is mid-paragraph must not cut the
// live paragraph in two, and [app.said] is the one place that keeps the live
// index pointing at the block the next delta will grow. What the reader sees is
// the answer so far, whole, then the correction under it.
//
// It carries the RUNNING TURN'S number, because a steer does not open a turn
// (app.go's [app.submittingShown] only counts a message that started a stream).
// That is what keeps the tool rows around it in one cluster's turn and what
// makes the fold group end here rather than somewhere else.
func (a *app) steerAccepted(note *session.SteerNote) tea.Cmd {
	if note == nil || strings.TrimSpace(note.Words) == "" {
		return nil
	}
	if at := a.elbowOf(note.ID); at >= 0 {
		// The acceptance is sent once, but a surface that attaches to a turn
		// mid-flight reads the hub's backlog ([eventHub.attach]) and meets it
		// again. One steer is one block whatever number of times its news arrives.
		return nil
	}
	a.said(entry{
		kind: entrySteer, turn: a.turn,
		steer: &steerElbow{
			id: note.ID, words: strings.TrimSpace(note.Words), at: note.At,
			landing: strings.TrimSpace(note.Landing),
		},
	})
	a.follow()
	a.touch()
	return nil
}

// steerConsumed is the landing: the model has been given those words, inside
// the turn they were aimed at. The row lights and comes back down on its own.
func (a *app) steerConsumed(note *session.SteerNote) tea.Cmd {
	if note == nil {
		return nil
	}
	at := a.elbowOf(note.ID)
	if at < 0 {
		return nil
	}
	e := &a.entries[at]
	if e.steer.consumed {
		return nil
	}
	e.steer.consumed = true
	e.steer.landed = a.now()
	e.stale = true
	a.touch()
	// The two wakeups the fade needs and no ticker, which is [fadeTicks]' whole
	// bargain: a surface with nothing happening on it wakes twice and stops.
	return fadeTicks()
}

// steerFellThrough is the honest ending: the turn finished before a boundary
// came, so those words were never part of that question.
//
// THE BLOCK COMES OFF FIRST. Everything else here is about where the words go
// next; this is the part that is about what the transcript says happened, and a
// row left standing inside that turn would say the wrong thing about it forever
// — and would say it twice, because the same sentence is about to be drawn
// again as the question it becomes.
func (a *app) steerFellThrough(note *session.SteerNote) tea.Cmd {
	if note == nil {
		return nil
	}
	if at := a.elbowOf(note.ID); at >= 0 {
		a.withdrawSteer(at)
	}
	// AND THE SURFACE SAYS SO, once, in the lane it says everything of its own
	// in. The words are about to appear again as an ordinary question a moment
	// later, and without this the person reads their own correction being asked
	// back to them with no account of why.
	a.note(steerFellWord)
	return nil
}

// settleSteers is the sweep a turn's end owes every correction that never
// landed. See the header: after a stop nothing new is drawn, so the
// fall-through's own event never reaches the screen — and the law it carries is
// true anyway. A turn that is over never gave the model those words.
//
// It runs from [app.settle], which is the one place every ending goes through:
// a completed turn, an interrupted one, an error, and a stream abandoned when
// the conversation was replaced.
//
// IT WALKS BACKWARDS so that the truncating case can actually be taken. A run of
// unlanded corrections at the tail comes off one at a time from the end, which
// leaves no emptied blocks behind at all; walking forwards would empty the first
// and then find the second no longer last.
func (a *app) settleSteers() {
	for at := len(a.entries) - 1; at >= 0; at-- {
		e := &a.entries[at]
		if e.kind != entrySteer || e.steer == nil || e.steer.consumed {
			continue
		}
		a.withdrawSteer(at)
	}
}

// withdrawSteer takes one correction off the page.
//
// THE BLOCK IS TRUNCATED WHEN IT IS LAST AND EMPTIED OTHERWISE, which is
// [app.dropLive]'s rule and echo.go's, and it is here for their reason:
// removing an entry from the middle would move every index after it, and the
// live block, the forming rows, the selection and the thought marker are all
// held by index. An emptied block renders nothing at all and is stepped over by
// everything that reads a run of blocks ([entryWithdrawn]).
func (a *app) withdrawSteer(at int) {
	if at < 0 || at >= len(a.entries) || a.entries[at].kind != entrySteer {
		return
	}
	if at == len(a.entries)-1 {
		a.entries = a.entries[:at]
	} else {
		a.entries[at].steer = nil
		a.entries[at].stale = true
	}
	a.touch()
}

// entryWithdrawn reports whether this block is one that USED to be on the page
// and now draws nothing — a correction the turn ended without ever giving the
// model.
//
// IT EXISTS SO THAT A GAP LEAVES NO TRACE. The walks that read a run of blocks
// to decide something — where a fold group ends (workfold.go's [groupBreaks]),
// whether an answer had work behind it (hierarchy.go's [answerBreath]), which
// drawn block a rewind's cut lands on (rewind.go's [fromTranscript]) — would
// otherwise treat a row nobody can see as a thing that happened. So each of
// them steps over it, and the page reads as it would have read had the
// correction never been typed.
func entryWithdrawn(e *entry) bool {
	return e.kind == entrySteer && e.steer == nil
}

// elbowOf finds one correction's block by the engine's id. It walks backwards
// because the newest turn is the one being steered on every frame this is asked
// on, and answers -1 for an id this page is not holding.
func (a *app) elbowOf(id uint64) int {
	for at := len(a.entries) - 1; at >= 0; at-- {
		if e := &a.entries[at]; e.kind == entrySteer && e.steer != nil && e.steer.id == id {
			return at
		}
	}
	return -1
}

// elbowMoving reports whether this block's paint is a function of the FRAME
// rather than of anything that arrived — a spinner turning, or a landing still
// on its way down the fade.
//
// It is what keeps the row cache honest, exactly as a running compaction, an
// open proposal and a waiting sign-in do (render.go's [app.entryRows]): a
// cached row with an animation in it is a still photograph of one.
func (a *app) elbowMoving(elbow *steerElbow) bool {
	if elbow == nil {
		return false
	}
	if !elbow.consumed {
		return true
	}
	return !elbow.landed.IsZero() && a.now().Sub(elbow.landed) < hudWarm
}

// ── the rows ────────────────────────────────────────────────────────────────

// steerGlyph is the elbow's mark, [bandFoldMark]'s shape: the glyph, or the
// ASCII stand-in on a terminal that was not given one.
func steerGlyph(pal palette) string {
	if pal.ascii || pal.linear {
		return glyphSteerASCII
	}
	return glyphSteer
}

// steerBlockRows is one correction as a block of the transcript: the elbow, the
// words, and the working clause while the model has not been given them yet.
//
// A PATH INSIDE A CORRECTION IS A DOOR (pathlink.go's law, and render.go's
// entryUser applies it to the question this continues): the commonest steer of
// all is "no, the OTHER file", and the file is named.
//
// A WITHDRAWN BLOCK DRAWS NOTHING, which is the whole of how the fall-through
// leaves no trace — an entry whose rows are empty is skipped by the layout pass
// before it is ever given a row of its own ([app.deckRows]).
func (a *app) steerBlockRows(e *entry, width int) []string {
	if e.steer == nil || width < 4 {
		return nil
	}
	return a.linkPaths(a.elbowRows(*e.steer, width))
}

// elbowRows is one correction: its glyph, its words, and — while the model has
// not been given them yet — the working clause that says so.
//
// IT WRAPS RATHER THAN BEING CUT, onto a hanging indent under its own first
// character, which is [bandClauses]' rule and the person's own block's: the
// glyph marks the correction and the column belongs to the sentence.
func (a *app) elbowRows(elbow steerElbow, width int) []string {
	mark := steerGlyph(a.pal)
	cols := ansi.StringWidth(mark)
	lead := strings.Repeat(" ", cols)
	ink := a.elbowInk(elbow)
	body := wrap(elbow.words, width-cols)
	out := make([]string, 0, len(body)+1)
	for i, line := range body {
		if i == 0 {
			out = append(out, a.pal.dim(mark)+ink(line))
			continue
		}
		out = append(out, lead+ink(line))
	}
	if len(out) == 0 || elbow.consumed {
		return out
	}
	// THE WORKING CLAUSE, on the row the sentence ended on when there is room —
	// the spacing ladder's clause step, exactly as the turn's context mark takes
	// it (turncontext.go) — and on a row of its own when there is not. Its words
	// are the engine's account of where this correction is landing, and
	// [steerPendingWord] only when it sent none.
	word := elbow.landing
	if word == "" {
		word = steerPendingWord
	}
	spin, tail := a.pal.muted(a.steerSpin()), a.pal.dim(" "+word)
	room := ansi.StringWidth(steerClauseSep) + 1 + ansi.StringWidth(" "+word)
	if ansi.StringWidth(out[len(out)-1])+room <= width {
		out[len(out)-1] += a.pal.dim(steerClauseSep) + spin + tail
		return out
	}
	out = append(out, lead+spin+tail)
	return out
}

// steerClauseSep joins the working clause to the sentence in front of it: the
// spacing ladder's own divider, spelled the way every other clause on this
// surface is spelled ([turnContextSep] is the same three cells).
const steerClauseSep = " · "

// elbowInk is the tier one elbow's words are drawn in this frame.
//
// A LANDING IS NEWS AND THEN IT IS NOT. The ramp is the status line's own — the
// only one on this surface — and it ends at [palette.narr] rather than at dim,
// because what a settled elbow is is the person's own prose one reading step
// under their question, and dim is the tier this surface talks about ITSELF in.
func (a *app) elbowInk(elbow steerElbow) func(string) string {
	if !elbow.consumed || elbow.landed.IsZero() {
		return a.pal.narr
	}
	switch age := a.now().Sub(elbow.landed); {
	case age < hudFresh:
		return a.pal.ink
	case age < hudWarm:
		return a.pal.muted
	default:
		return a.pal.narr
	}
}

// steerSpin is the one moving cell an elbow spends, on the house grid so it
// never beats against the spinners elsewhere on the screen ([spinnerStep]).
// Linear mode and a terminal with no braille get the still `*`, for the reason
// every spinner here does: a claim made thirty times a second is heard thirty
// times a second by a surface being read aloud.
func (a *app) steerSpin() string {
	if a.linear || a.pal.ascii {
		return glyphRunASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}

// ── the fall-through's lane ─────────────────────────────────────────────────
//
// A STEER IS HANDED A STREAM OF ITS OWN, and on the ordinary path nothing needs
// it: the acceptance, the landing and the fall-through all ride the turn's own
// channel too, because they go through the hub every subscriber is on
// (steer.go). The one thing that does NOT is the turn a fell-through steer
// starts of its own accord — the engine carries that stream across the seam so
// the person keeps one channel — so the surface has to be holding it or the
// answer to their own words arrives nowhere.
//
// [app.holdSteer] is the whole of that seam and it is one call: the door that
// sends a steer hands over the channel session gave it and forgets about it.
// The lane then does one of two things and never a third — a steer that LANDED
// is spent, so its copy of the turn is drained and dropped, and a steer that
// FELL THROUGH is a message waiting for a turn of its own, which is exactly
// what [app.follows] holds.

// steerFellMsg is one lane reporting the seam: these words fell through, and
// this channel is now the stream of the turn they are about to start.
type steerFellMsg struct {
	gen   int
	words string
	ch    <-chan session.Event
}

// holdSteer parks the channel [session.Agent.Steer] handed back. The caller is
// the key that sent the steer; everything after this is this file's.
func (a *app) holdSteer(ch <-chan session.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return waitSteerLane(ch, a.gen)
}

// waitSteerLane reads one steer's own stream until that steer's story is over,
// and returns at most one message.
//
// IT LOOPS INSIDE THE COMMAND rather than re-arming through the program loop.
// A steer's channel carries the WHOLE of the turn it was typed into — it is an
// ordinary subscriber — so a re-arming wait would put every text delta of that
// turn through [app.Update] a second time to be thrown away. What the loop is
// looking for is two events out of thousands, so it looks for them here.
//
// THE LANE LEARNS ITS OWN ID FROM THE FIRST ACCEPTANCE IT SEES, which is its
// own: the stream is adopted onto the hub BEFORE the acceptance is sent and
// under the same lock (steer.go's [Agent.Steer]), so every earlier steer's news
// is already past and the next one to arrive is this one's.
func waitSteerLane(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		var id uint64
		for ev := range ch {
			if ev.Steer == nil {
				continue
			}
			if id == 0 {
				if ev.Kind == session.EventSteerAccepted {
					id = ev.Steer.ID
				}
				continue
			}
			if ev.Steer.ID != id {
				continue
			}
			switch ev.Kind {
			case session.EventSteerConsumed:
				// It landed, so this copy of the turn is spent. The reader has to
				// leave or the engine's pump parks forever on a send nobody takes
				// (session's [eventStream.pump]), and there is nothing left on it
				// this surface has not already drawn from the turn's own channel.
				go func() {
					for range ch { //nolint:revive // draining is the whole body
					}
				}()
				return nil
			case session.EventSteerFellThrough:
				return steerFellMsg{gen: gen, words: ev.Steer.Words, ch: ch}
			}
		}
		return nil
	}
}

// steerFell takes the seam: the words become a message waiting for a turn of
// its own, on the queue the surface already holds waiting messages in, and the
// drain that starts them draws the person's line exactly as it draws any other
// (followup.go's [app.startFollow]).
//
// A TURN SOMEBODY STOPPED DRAINS NOTHING. The engine drops a follow-up queued
// behind an interrupted turn and closes its stream with no events at all
// ([Agent.dropFollowUpsLocked]), so a surface that queued this one would draw a
// question and then sit under it with no answer coming. The stop is the last
// thing that turn writes on this screen, here as everywhere.
func (a *app) steerFell(msg steerFellMsg) tea.Cmd {
	if msg.ch == nil || msg.gen != a.gen || a.windingDown() || a.state == stateInterrupted {
		if msg.ch != nil {
			go func() {
				for range msg.ch { //nolint:revive // draining is the whole body
				}
			}()
		}
		return nil
	}
	a.follows = append(a.follows, queued{text: msg.words, ch: msg.ch})
	a.touch()
	if a.stream != nil {
		return nil
	}
	return a.startFollow()
}
