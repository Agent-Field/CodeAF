package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE PERSON'S LINE AND ITS LANDING CLAUSE.
//
//	› port the parser to the new lexer
//	…partial answer already received…
//	› use the staging bucket, not production
//	└ stopped the reply here
//
// A steer is an ordinary user message in the transcript because that is what
// the model reads. The surface therefore draws it in the person's ordinary
// register immediately on acceptance. The elbow beneath it belongs to the
// surface and is muted: it says where the words are landing without putting
// machinery vocabulary into the person's mouth.
//
// ── THE INK, AND WHY IT IS THIS ────────────────────────────────────────────
//
// THE ACCENT BUDGET (docs/DESIGN-LANGUAGE.md) still applies. The person's line
// uses [entryUser]'s existing hue, and only the structural clause uses dim ink.
//
//	the elbow glyph   dim     furniture, like every mark this surface draws
//	                          about its own structure
//	the words         dim     the surface's account of where it landed
//
// The person's body remains [palette.muted] for the reason entryUser states.
// The clause is [palette.dim] because it is the surface's own murmur, not a
// syllable of what the person sent.
//
// ── THE LANDING MOMENT ─────────────────────────────────────────────────────
//
// A steer is accepted at once and CONSUMED only when the next request contains
// it. Those are two different engine facts, but the person-facing clause already
// knows where the words are landing: the provider was cut, a bash became a job,
// or a short tool is reaching its boundary. It therefore stays still instead of
// adding a generic spinner beside a more useful answer.
//
// NOTHING CLAIMS CONSUMED BEFORE [session.EventSteerConsumed]. The whole worth
// of the three events is that the surface can stop guessing, and a row that
// settled the instant somebody pressed the key would be a record of an
// intention (steer.go's [Agent.consumedSteerLocked] makes exactly this point
// about the journal).
//
// ── THE FALL-THROUGH REMOVES THE PROVISIONAL LINE ──────────────────────────
//
// A boundary is not promised. A steer still waiting when the turn ends never
// reached the model, so it MUST NOT remain on that turn. The provisional user
// line leaves and becomes the next question, which is exactly what the
// engine does with it (steer.go's [Agent.liftSteersLocked] re-homes it on the
// follow-up queue), drawn by the drain that already draws a waiting message
// when its turn starts (followup.go's [app.startFollow]).
//
// AND A TURN THAT ENDED WITHOUT SAYING SO SWEEPS THE SAME WAY. After a person
// presses stop nothing new is drawn on this surface (app.go's [app.event]), so
// the fall-through's own event is swallowed with everything else — and an elbow
// left spinning on a question the model never carried it into would be the one
// lie this file exists to prevent. [app.settleSteers] drops every elbow that
// never landed, at the moment the turn settles, under the same law: if the turn
// is over and the words were never consumed, they were never part of it.
//
// ── COLLAPSE KEEPS THE PERSON'S LINE AND ITS CLAUSE ────────────────────────
//
// The clause lives on the person's own block, which is the ONE block a past turn
// never folds away: the worked chip collapses the machinery between the
// question and the answer and starts BELOW the question (workfold.go's
// [deriveWorkfolds] stops at the person's message). That is the point of
// putting it here rather than in the turn's own row run — a turn folded to
// `▸ worked · 10 tool calls` still reads back as everything that was asked and
// where the steer entered it.

// glyphSteer is the elbow, and glyphSteerASCII is what a terminal with no
// box-drawing gets. `+` is this surface's own ASCII corner already — it is the
// first cell of [railASCII], where `╰` is the first cell of [railLast] — so a
// reader who has met one has met the other.
const (
	glyphSteer      = "└ "
	glyphSteerASCII = "+ "
)

// steerWindow is how many elbows stay on screen when a question collected more
// than a person can hold. It is [toolWindow] and not a number of its own: three
// is the count that file already argues for — the newest and the two before it
// — and two spellings of one design decision is one of them going stale.
const steerWindow = toolWindow

// steerFoldWhat is the plural noun the fold line names, in [bandFoldWord]'s own
// grammar: `└ …2 more steers`.
const steerFoldWhat = "steers"

// steerPendingWord is what an elbow says while the model has NOT been given it
// yet. It is the verb the engine and the person both already use for the act,
// and it is a word rather than a bare spinner because every mark on this
// surface has a word near it (docs/DESIGN-LANGUAGE.md's refusal of icon-only
// minimalism).
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
	// consumed is the fact itself. It is a field beside the instant rather than
	// `!landed.IsZero()` because a REPLAYED elbow knows it landed and does not
	// know when the surface would have said so — the journal keeps the SEND's
	// instant, not the boundary's (sessionfile.go's [journalSteer]) — so a
	// replayed row is settled and unfaded, which is what a fact from yesterday is.
	consumed bool
	// status says words are the surface's muted landing clause under the
	// person's own line, not another sentence the person supplied.
	status bool
}

// ── the events ──────────────────────────────────────────────────────────────

// steerAccepted draws one correction as the person's own transcript line now.
//
// The turn number does not move: a steer is a user message inside the turn that
// is already running, not a new turn. The engine's id pairs later consumption
// or fall-through with exactly this provisional line.
func (a *app) steerAccepted(note *session.SteerNote) tea.Cmd {
	if note == nil || strings.TrimSpace(note.Words) == "" {
		return nil
	}
	for at := range a.entries {
		e := &a.entries[at]
		if !e.steerLine {
			continue
		}
		for _, held := range e.steers {
			// The acceptance is sent once, but a surface that attaches to a turn
			// mid-flight reads the hub's backlog ([eventHub.attach]) and meets it
			// again. One steer is one elbow whatever number of times its news arrives.
			if held.id == note.ID {
				return nil
			}
		}
	}
	landing := strings.TrimSpace(note.Landing)
	if landing == "" {
		landing = "waiting for the running step"
	}
	a.said(entry{
		kind: entryUser, text: strings.TrimSpace(note.Words), turn: a.turn,
		began: note.At, steerLine: true,
		steers: []steerElbow{{id: note.ID, words: landing, at: note.At, status: true}},
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
	at, on := a.elbowOf(note.ID)
	if at < 0 {
		return nil
	}
	e := &a.entries[at]
	if e.steers[on].consumed {
		return nil
	}
	e.steers[on].consumed = true
	e.steers[on].landed = a.now()
	e.stale = true
	a.touch()
	if e.steers[on].status {
		return nil
	}
	// The two wakeups the fade needs and no ticker, which is [fadeTicks]' whole
	// bargain: a surface with nothing happening on it wakes twice and stops.
	return fadeTicks()
}

// steerFellThrough is the honest ending: the turn finished before a boundary
// came, so those words were never part of that question.
//
// THE ELBOW COMES OFF FIRST. Everything else here is about where the words go
// next; this is the part that is about what the transcript says happened, and a
// row left hanging under the trunk would say the wrong thing about it forever.
func (a *app) steerFellThrough(note *session.SteerNote) tea.Cmd {
	if note == nil {
		return nil
	}
	if at, on := a.elbowOf(note.ID); at >= 0 {
		if a.entries[at].steerLine {
			a.entries = append(a.entries[:at], a.entries[at+1:]...)
		} else {
			e := &a.entries[at]
			e.steers = append(e.steers[:on], e.steers[on+1:]...)
			e.stale = true
		}
		a.touch()
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
func (a *app) settleSteers() {
	keptEntries := make([]entry, 0, len(a.entries))
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryUser || len(e.steers) == 0 {
			keptEntries = append(keptEntries, *e)
			continue
		}
		kept := e.steers[:0]
		for _, elbow := range e.steers {
			if elbow.consumed {
				kept = append(kept, elbow)
			}
		}
		if len(kept) != len(e.steers) {
			e.steers, e.stale = kept, true
		}
		if e.steerLine && len(e.steers) == 0 {
			continue
		}
		keptEntries = append(keptEntries, *e)
	}
	a.entries = keptEntries
}

// trunkOf is the person's own block that OPENED a turn — the trunk every elbow
// of that turn hangs from. It walks backwards because the newest turn is the
// one being steered on every frame this is asked on.
func (a *app) trunkOf(turn int) int {
	for at := len(a.entries) - 1; at >= 0; at-- {
		if e := &a.entries[at]; e.kind == entryUser && e.turn == turn {
			return at
		}
	}
	return -1
}

// elbowOf finds one steer by the engine's id: which block holds it, and where
// in that block's list. It walks backwards for [app.trunkOf]'s reason.
func (a *app) elbowOf(id uint64) (int, int) {
	for at := len(a.entries) - 1; at >= 0; at-- {
		e := &a.entries[at]
		if e.kind != entryUser {
			continue
		}
		for on := range e.steers {
			if e.steers[on].id == id {
				return at, on
			}
		}
	}
	return -1, -1
}

// steersMoving reports whether this block has an elbow whose paint is a
// function of the FRAME rather than of anything that arrived — a spinner
// turning, or a landing still on its way down the fade.
//
// It is what keeps the row cache honest, exactly as a running compaction, an
// open proposal and a waiting sign-in do (render.go's [app.entryRows]): a
// cached row with an animation in it is a still photograph of one.
func (a *app) steersMoving(e *entry) bool {
	for _, elbow := range e.steers {
		if elbow.status {
			continue
		}
		if !elbow.consumed {
			return true
		}
		if !elbow.landed.IsZero() && a.now().Sub(elbow.landed) < hudWarm {
			return true
		}
	}
	return false
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

// steerElbowRows draws the family under a question that collected corrections,
// and hands back the block's rows unchanged when it collected none — which is
// every question in nearly every conversation, and is why a transcript with no
// steers in it renders byte for byte as it always did.
//
// The fold's row index is recorded on the block so the layout pass can make
// that one row a door (render.go's [app.deckRows] does the same for a
// proposal's choices row, and for the same reason: the columns and the row a
// thing landed on are decided by the render that drew it).
func (a *app) steerElbowRows(rows []string, e *entry, width int) []string {
	e.steerFoldRow = 0
	if len(e.steers) == 0 || width < 4 {
		return rows
	}
	out := rows
	shown := e.steers
	// THE FOLD LINE COMES FIRST AND THE NEWEST THREE UNDER IT. That is the tool
	// cluster's own shape (toolview.go's [app.clusterRows]) and it is the right
	// one here for the same reason: the correction that matters most is the last
	// one made, and a fold that hid it to keep the first three would hide the one
	// still landing.
	if len(shown) > steerWindow && !a.steerOpen[e.turn] {
		e.steerFoldRow = len(out)
		out = append(out, a.pal.dim(fit(steerGlyph(a.pal)+
			bandFoldWord(len(shown)-steerWindow, steerFoldWhat, true), width)))
		shown = shown[len(shown)-steerWindow:]
	} else if len(shown) > steerWindow {
		e.steerFoldRow = len(out)
		out = append(out, a.pal.dim(fit(steerGlyph(a.pal)+
			bandFoldWord(len(shown)-steerWindow, steerFoldWhat, false), width)))
	}
	for _, elbow := range shown {
		out = append(out, a.elbowRows(elbow, width)...)
	}
	// A PATH INSIDE A CORRECTION IS A DOOR TOO, on the person's own message's
	// terms (render.go's entryUser, pathlink.go's law): the commonest steer of
	// all is "no, the OTHER file", and the file is named. The fold line is left
	// out of the sweep because it is this surface's own words and not a syllable
	// of anybody's sentence — the same order the turn's context mark is added in.
	linked := a.linkPaths(out[len(rows):])
	return append(out[:len(rows):len(rows)], linked...)
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
	if len(out) == 0 || elbow.consumed || elbow.status {
		return out
	}
	// THE WORKING CLAUSE, on the row the sentence ended on when there is room —
	// the spacing ladder's clause step, exactly as the turn's context mark takes
	// it (turncontext.go) — and on a row of its own when there is not.
	spin, tail := a.pal.muted(a.steerSpin()), a.pal.dim(" "+steerPendingWord)
	room := ansi.StringWidth(steerClauseSep) + 1 + ansi.StringWidth(" "+steerPendingWord)
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
	if elbow.status {
		return a.pal.dim
	}
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

// ── the fold's door ─────────────────────────────────────────────────────────

// toggleSteerFold opens the elbows a question folded, and shuts them again. It
// is the click on `└ …2 more steers`, and it is a toggle for [app.openEffortMenu]'s
// reason: a control that opened a list and ignored the second press on the same
// cell is a control with no way back through the gesture that got you there.
func (a *app) toggleSteerFold(turn int) {
	if a.steerOpen == nil {
		a.steerOpen = map[int]bool{}
	}
	a.steerOpen[turn] = !a.steerOpen[turn]
	for i := range a.entries {
		if a.entries[i].kind == entryUser && a.entries[i].turn == turn {
			a.entries[i].stale = true
		}
	}
	a.touch()
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
