package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// REWIND MODE: the conversation, with a line drawn across it.
//
//	  › and now write the tests                         (the anchor, brightened)
//	─ ⟲ rewind here ──────────────────────────────────────────────────────────
//	  ⠿ everything under the line, washed out — this is what the cut drops
//
//	 ⟲ drops 2 turns          ↑↓ turns · ←→ steps · enter rewind · esc back
//
// THIS IS THE QUICK TIER OF TWO, and the transcript is its picker. The thing a
// person taking something back is deciding about is on the screen already, in
// the shapes they read it in the first time, and the whole of this mode is a line
// through it plus a bar where the box was. Nothing is drawn twice, and nothing
// covers the very rows the decision is about.
//
// THE DELIBERATE TIER IS THE TIMELINE (rewindsheet.go): the whole conversation as
// a full-frame list with a search, a preview and a two-stage enter, which /rewind
// opens and which `tab` in here lifts into with this cut carried over. It exists
// because this mode walks the DRAWN blocks and the drawn blocks are a windowed
// tail (replay.go's [replayTail]), so a resumed conversation's early turns cannot
// be reached from here at all. The two share the ⟲ glyph, the "drops N turns"
// arithmetic ([rewindDropCount]) and the cut itself ([app.rewindLand]).
//
// THE MODE BAR TAKES THE DRAFT'S OWN POSITION, and the draft is stashed for the
// duration. That is the recall walk's bargain (recall.go) applied to a bigger
// state: a mode that overwrote a half-written sentence would be a mode nobody
// could enter without first saving their own work somewhere else, and esc puts
// the sentence back exactly as it was.
//
// THE DOOR IS DOUBLE-ESC, and it is double for one reason: esc already means
// INTERRUPT while a turn runs, and that meaning is not for sale. So the first esc
// keeps whatever it always meant — it stops the model mid-turn, and at rest it
// does nothing at all — and it ARMS this mode for [rewindArmWindow]; a second esc
// inside that window opens it. Interrupt first, then rewind, in the order the
// engine's own refusal asks for ([session.ErrTurnInFlight]): the pair is one
// gesture, and it is the gesture a person's hand already makes when they want to
// take something back.
//
// KEYS: ↑/↓ walk the TURNS, which is the unit a person thinks in ("not that
// message"); ←/→ slide through the steps inside the turn the cut is in, for the
// rarer "not that part of it". enter commits, esc leaves with nothing changed.
// The pointer can do all of it too — a click chooses the nearest cut at or above
// the row, the cut line itself commits — but NOTHING here is pointer-only,
// because the mouse is opt-in on this surface (view.go's [app.View]).
//
// WHAT A COMMIT DOES: it calls the engine, and then it throws the drawn blocks
// away and builds them again from the transcript the engine now holds
// ([app.rebuildTranscript]). It does NOT prune the block list by hand. The blocks
// on screen and the messages in the session are two renderings of one
// conversation, and the only way to keep them from drifting after an edit is for
// one of them to be derived from the other — which is exactly what the resume
// path already does.

// rewindArmWindow is how long the first esc keeps rewind armed.
//
// HALF A SECOND, and the number is chosen against the HAND rather than against a
// reaction time. A deliberate double-tap — the double-click every pointer on this
// machine is calibrated for, and the "esc esc" that leaves an editor's mode —
// lands its second key inside 300ms; a person who pressed esc to interrupt a turn
// and then decided, having read something, to also take it back is a person
// making a second decision, and their second key arrives a second or more later.
// So the window has to be long enough that the first kind never misses and short
// enough that the second kind never fires by accident.
//
// It errs SHORT on purpose. A window that lapsed too early costs one extra
// keystroke; a window that lapsed too late puts a person who tapped esc twice to
// dismiss two things into a mode they did not ask for, over a conversation they
// are about to cut. The two mistakes are not the same size.
const rewindArmWindow = 500 * time.Millisecond

// rewindSayWindow is how long a sentence this mode could not act on stays in the
// hint slot — "nothing to rewind", and nothing else. Two and a half seconds is
// long enough to be read once and short enough that it is never furniture.
const rewindSayWindow = 2500 * time.Millisecond

// The mode's words, written down once.
const (
	// rewindArmWord is what the hint slot says while the first esc is still warm.
	rewindArmWord = "esc again to rewind"
	// rewindEmptyWord is what it says when there is nothing to cut.
	rewindEmptyWord = "nothing to rewind"
	// rewindCutWord is the cut line's label, and rewindKeysWord the mode bar's
	// legend. The keys are QUOTED FROM [app.rewindKey] — a legend that disagreed
	// with the handler would be a legend somebody acts on.
	rewindCutWord = "rewind here"
	// THE LEGEND CARRIES THE DOOR ONTO THE TIMELINE, because a key nobody can see
	// is a key nobody finds: `tab` lifts this mode into the full-frame picker over
	// the WHOLE conversation, with the cut already chosen carried across
	// (rewindsheet.go). The inline mode walks the drawn blocks and the drawn
	// blocks are a windowed tail, so this is the only way out of that window from
	// inside the gesture that ran into it.
	rewindKeysWord = "↑↓ turns · ←→ steps · enter rewind · " + rewindSheetLiftWord + " · esc back"
)

// rewindMark is the mode's glyph, with the stand-in the linear tier reads out
// loud instead of a codepoint (styles.go, the same bargain [jumpLabel] makes).
func rewindMark(pal palette) string {
	if pal.linear {
		return "<<"
	}
	return "⟲"
}

// rewindAgent is the slice of *session.Agent this file needs, and it is ASSERTED
// rather than added to [Agent].
//
// The reason is [taskAgent]'s (task.go): the contract is OPTIONAL. Every other
// test in this package drives the surface with a scripted agent that has never
// heard of a rewind, and widening the package interface would make a session
// without one un-representable. A backend that does not implement these two
// methods simply has no rewind — esc-esc changes nothing, and the door says so.
type rewindAgent interface {
	// RewindPoints is every place the conversation can be cut, oldest first.
	RewindPoints() []session.RewindPoint
	// RewindAt cuts at one of them. It refuses while a turn is in flight, which
	// is why the mode STAYS UP on an error and prints the sentence.
	RewindAt(index int) ([]session.DisplayEntry, error)
}

// rewinder is the agent under this surface, when it can rewind at all.
func (a *app) rewinder() (rewindAgent, bool) {
	agent, ok := a.agent.(rewindAgent)
	return agent, ok
}

// rewindMode is the whole of the mode's state. The zero value is off.
type rewindMode struct {
	on bool
	// points is the engine's list, captured on the way in, and at is the index
	// of the chosen one. The list is captured rather than re-asked per frame for
	// the reason the recall walk captures its own: the thing being walked must not
	// change under the walk.
	points []session.RewindPoint
	// anchors is each point's cut line as a DRAWN BLOCK index — see
	// [app.rewindAnchors] for why the two lists have to be matched rather than
	// indexed into each other.
	anchors []int
	at      int
	// draft and cursor are the person's own sentence, held for them.
	draft  []rune
	cursor int
	// said is the mode bar's feedback line: the engine's refusal, in the engine's
	// own words, until a key moves the cut.
	said string
}

// ── THE DOOR ────────────────────────────────────────────────────────────────

// enterRewind opens the mode. It is the door a /rewind command calls, and the
// door the second esc calls, so the two can never mean slightly different things.
func (a *app) enterRewind() tea.Cmd {
	// The frame stack decides where this can be opened from: a room, a frozen
	// viewport and the fullscreen panels all draw over the place the mode bar
	// stands in, and a bar nobody can see is a mode nobody can leave (view.go).
	if a.rew.on || a.rewSheet.open || a.copy.on || a.roomOpen() || a.sheet.open || a.railFull() {
		return nil
	}
	agent, ok := a.rewinder()
	if !ok {
		return a.sayRewind(rewindEmptyWord)
	}
	points := agent.RewindPoints()
	if len(points) == 0 {
		return a.sayRewind(rewindEmptyWord)
	}
	a.disarmRewind()
	a.closeLists()
	a.dropHover()
	a.rew = rewindMode{
		on:      true,
		points:  points,
		anchors: a.rewindAnchors(points),
		draft:   append([]rune(nil), a.input.value...),
		cursor:  a.input.cursor,
	}
	// THE CUT STARTS AT THE LAST THING THE PERSON SAID, because that is what a
	// person reaching for this means nine times out of ten: take back the message
	// I just sent. Everything older is one ↑ away.
	a.rew.at = lastTurnPoint(points)
	a.input.reset()
	a.sel = -1
	a.reveal(a.rewindAnchor(a.rew.at))
	a.touch()
	return a.wake()
}

// lastTurnPoint is the newest USER message in a point list, or the newest point
// of any kind in a list with no turn in it at all.
func lastTurnPoint(points []session.RewindPoint) int {
	for at := len(points) - 1; at >= 0; at-- {
		if points[at].Turn {
			return at
		}
	}
	return len(points) - 1
}

// leaveRewind closes the mode. restore puts the stashed sentence back, which is
// what esc does and what a step cut does; a turn cut replaces it with the message
// the cut took out of the conversation ([app.commitRewind]).
func (a *app) leaveRewind(restore bool) {
	if !a.rew.on {
		return
	}
	if restore {
		a.input.value = append(a.input.value[:0], a.rew.draft...)
		a.input.cursor = min(a.rew.cursor, len(a.input.value))
	}
	a.rew = rewindMode{}
	a.dropHover()
	a.touch()
}

// ── THE DOUBLE ESC ──────────────────────────────────────────────────────────

// escRewind is esc's rewind half. It reports whether it TOOK the key: the second
// esc inside the window opens the mode and is taken, and the first one arms and
// is NOT — it goes on to mean whatever it always meant, which mid-turn is the
// interrupt (input.go's esc case). The command it returns is carried down both
// paths, because arming needs the frame clock to run the window down.
func (a *app) escRewind() (tea.Cmd, bool) {
	if !a.rewindReady() {
		return nil, false
	}
	if a.rewindArmed() {
		return a.enterRewind(), true
	}
	a.escArm = a.now()
	a.touch()
	return a.wake(), false
}

// rewindReady reports whether esc may arm the mode at all: nothing else on this
// surface is holding the keyboard, and the agent under it can rewind.
//
// It is the LIST OF STATES esc already means something in, read from the routers
// that take the key before input.go's own switch does (input.go, app.go's Update)
// — a modal state whose dismiss key silently armed a second mode would be a
// surface where esc means two things at once.
func (a *app) rewindReady() bool {
	if a.rew.on {
		return false
	}
	if _, ok := a.rewinder(); !ok {
		return false
	}
	switch {
	case a.rewSheet.open,
		a.sheet.open, a.taskSheet.open, a.deck.open, a.expand.open, a.pick.open, a.roster.open,
		a.connPanel.open, a.menu.open, a.comp.open, a.welcome.open,
		a.copy.on, a.recalling(), a.roomOpen(), a.railHold, a.railFull(),
		a.asking(), a.awaitingTask(), a.guard != nil, len(a.connAsks) > 0,
		a.asksHarness():
		return false
	}
	return true
}

// rewindArmed reports whether the first esc is still warm.
func (a *app) rewindArmed() bool {
	return !a.escArm.IsZero() && a.now().Sub(a.escArm) < rewindArmWindow
}

// disarmRewind forgets it.
func (a *app) disarmRewind() { a.escArm = time.Time{} }

// sayRewind puts one sentence in the hint slot for a moment — "nothing to
// rewind", which is the whole of what this is for.
func (a *app) sayRewind(text string) tea.Cmd {
	a.rewSay, a.rewSayAt = text, a.now()
	a.disarmRewind()
	a.touch()
	return a.wake()
}

// rewindSaying reports whether that sentence is still up.
func (a *app) rewindSaying() bool {
	return a.rewSay != "" && a.now().Sub(a.rewSayAt) < rewindSayWindow
}

// rewindTicking says the frame clock has a reason to keep turning even with
// nothing else happening: a window or a sentence with an end to reach.
func (a *app) rewindTicking() bool { return a.rewindArmed() || a.rewindSaying() }

// rewindSweep runs both of those clocks down. It is called from [app.paint] and
// from nowhere else — NO GOROUTINE OF ITS OWN, for the reason the spinner and the
// countdowns have none (app.go): this surface has exactly one clock, and a second
// one is a second wakeup per second and two states that disagree about the time.
func (a *app) rewindSweep() {
	if !a.escArm.IsZero() && !a.rewindArmed() {
		a.disarmRewind()
		a.touch()
	}
	if a.rewSay != "" && !a.rewindSaying() {
		a.rewSay, a.rewSayAt = "", time.Time{}
		a.touch()
	}
}

// ── THE KEYS ────────────────────────────────────────────────────────────────

// rewindKey routes the mode's keys and says whether it took one.
//
// It takes EVERYTHING, for copy mode's reason (copymode.go): while the mode is up
// the draft box is not on the frame — the mode bar stands in its place — so a key
// that fell through would type into a box whose effect nobody can see. ctrl+c is
// read above this and stays the door.
func (a *app) rewindKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.rew.on {
		return nil, false
	}
	switch msg.String() {
	case "esc":
		a.leaveRewind(true)
	case "enter":
		a.commitRewind()
	case rewindSheetLiftKey:
		// tab LIFTS, and it is the one key here that opens something rather than
		// deciding something: the same conversation, drawn whole, with this cut
		// carried over (rewindsheet.go's [app.liftRewind]).
		return a.liftRewind(), true
	case "up":
		a.walkRewind(-1)
	case "down":
		a.walkRewind(1)
	case "left":
		a.stepRewind(-1)
	case "right":
		a.stepRewind(1)
	}
	return nil, true
}

// walkRewind is ↑/↓: the next TURN point in that direction, or nothing when
// there is none. The cut never falls off either end of the list — a walk that
// left the mode by arriving at the edge would be a mode you leave by accident.
func (a *app) walkRewind(delta int) {
	for at := a.rew.at + delta; at >= 0 && at < len(a.rew.points); at += delta {
		if a.rew.points[at].Turn {
			a.chooseRewind(at)
			return
		}
	}
}

// stepRewind is ←/→: one point inside the current turn, bounded by that turn's
// own span. → is toward the newer end, which is the direction the conversation
// reads.
func (a *app) stepRewind(delta int) {
	low, high := a.rewindSpan()
	a.chooseRewind(clampInt(a.rew.at+delta, low, high))
}

// rewindSpan is the run of points ←/→ walk: the turn point the cut is inside,
// and every step up to the next turn.
func (a *app) rewindSpan() (int, int) {
	low, high := 0, len(a.rew.points)-1
	for at := a.rew.at; at >= 0; at-- {
		if a.rew.points[at].Turn {
			low = at
			break
		}
	}
	for at := a.rew.at + 1; at < len(a.rew.points); at++ {
		if a.rew.points[at].Turn {
			high = at - 1
			break
		}
	}
	return low, high
}

// chooseRewind moves the cut and keeps it on screen.
func (a *app) chooseRewind(at int) {
	if at < 0 || at >= len(a.rew.points) || at == a.rew.at {
		return
	}
	a.rew.at = at
	// A key that moved the cut has answered the refusal: the sentence was about
	// the cut that was tried, and this is a different one.
	a.rew.said = ""
	a.reveal(a.rewindAnchor(at))
	a.touch()
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// rewindPress is a click while the mode is up. The cut line commits; any other
// transcript row moves the cut to the nearest point AT OR ABOVE it, which is the
// only reading that cannot surprise: a click never drops less than the row you
// pointed at.
func (a *app) rewindPress(y int) {
	r, ok := a.rowAt(y)
	if !ok {
		return
	}
	if r.hit == hitRewind {
		a.commitRewind()
		return
	}
	if r.entry < 0 {
		return
	}
	a.chooseRewind(a.rewindPointAtEntry(r.entry))
}

// rewindPointAtEntry resolves a drawn block to the cut a click on it chooses:
// the last point whose line sits at or above that block. A click above every
// point takes the oldest one, which is the nearest legal cut in the direction the
// pointer was reaching.
func (a *app) rewindPointAtEntry(entry int) int {
	at := -1
	for i := range a.rew.points {
		if a.rewindAnchor(i) > entry {
			break
		}
		at = i
	}
	if at < 0 && len(a.rew.points) > 0 {
		return 0
	}
	return at
}

// ── THE COMMIT ──────────────────────────────────────────────────────────────

// commitRewind is enter, and the click on the cut line: the engine cuts, and the
// conversation on screen is rebuilt from what it now holds.
//
// A REFUSAL KEEPS THE MODE UP. The one the engine actually raises is "a turn is
// in flight" — a turn winding down between the interrupt and this keystroke — and
// the answer to it is to press enter again a moment later, which is only possible
// if the mode is still there to press it in.
func (a *app) commitRewind() {
	_, ok := a.rewinder()
	if !ok || a.rew.at < 0 || a.rew.at >= len(a.rew.points) {
		a.leaveRewind(true)
		return
	}
	point := a.rew.points[a.rew.at]
	// The count is taken BEFORE the cut, because after it the blocks it counted
	// are gone.
	word := a.rewindDropWord()
	stash, cursor := a.rew.draft, a.rew.cursor
	if err := a.rewindLand(point, word, stash, cursor, func() { a.rew = rewindMode{} }); err != nil {
		a.rew.said = errText(err)
		a.touch()
	}
}

// rewindLand IS THE CUT, AND THERE IS ONE OF IT. Both rewind surfaces — the
// inline mode above and the timeline (rewindsheet.go) — come through here, so
// there is exactly one account of what a rewind does to this window: the engine
// cuts, the mode that asked for it leaves, the drawn conversation is thrown away
// and rebuilt from what the session now holds, one note says so, and the box is
// refilled.
//
// leave is the caller's own way out, and it is called AFTER the engine has said
// yes and never before: a refused cut leaves the surface exactly as it was, with
// the mode still up and the sentence to press again in a moment.
func (a *app) rewindLand(point session.RewindPoint, word string, stash []rune, caret int, leave func()) error {
	agent, ok := a.rewinder()
	if !ok {
		return session.ErrNothingToRewind
	}
	if _, err := agent.RewindAt(point.Index); err != nil {
		return err
	}
	leave()
	a.dropHover()
	a.rebuildTranscript()
	// The note is the compaction mark's voice: one dim line, said once, about
	// something the surface did to the conversation rather than about anything
	// anybody said. A conversation that silently lost its tail is a conversation
	// the person cannot reason about (render.go's [app.divider] makes the same
	// argument for the same reason).
	a.note(rewindMark(a.pal) + " rewound · " + word)
	if point.Turn {
		// THE MESSAGE COMES BACK TO THE BOX. Taking back what you said and then
		// having to retype it is the half of this gesture that would make people
		// stop using it — the point of a rewind is almost always to say the same
		// thing better.
		a.input.setText(point.Said)
	} else {
		a.input.value = append(a.input.value[:0], stash...)
		a.input.cursor = min(caret, len(a.input.value))
	}
	a.stick = true
	a.follow()
	a.measureContext()
	a.touch()
	return nil
}

// rebuildTranscript throws the drawn blocks away and builds them again from the
// session's own transcript.
//
// IT REUSES THE RESUME PATH ([app.replay]) rather than pruning the block list in
// place, and that is the whole of why a rewind cannot leave the screen disagreeing
// with the session: there is one function that turns a transcript into blocks, and
// after this there is nothing on screen that did not come out of it. Everything
// keyed by a block INDEX is dropped in the same breath — the selection, the
// pointer, the folds, the live block — because an index into a list that has been
// replaced points at somebody else's row.
func (a *app) rebuildTranscript() {
	a.entries = nil
	a.turn = 0
	a.live = -1
	a.think = -1
	a.sel = -1
	a.unfolded = map[int]bool{}
	a.rows, a.rowsWidth = nil, 0
	a.hudStale = true
	a.dropHover()
	a.replay()
}

// ── WHAT THE CUT COSTS ──────────────────────────────────────────────────────

// rewindDrops is what the chosen cut takes, counted in the DRAWN conversation:
// how many of the person's own messages go, and how many blocks in total.
//
// It counts what is on the screen rather than what is in the session because the
// screen is what the sentence is about — a person reading "drops 2 turns" is
// looking at the two turns under the line.
func (a *app) rewindDrops() (turns, blocks int) {
	for at := a.rewindAnchor(a.rew.at); at >= 0 && at < len(a.entries); at++ {
		if a.entries[at].kind == entryUser {
			turns++
		}
		blocks++
	}
	return turns, blocks
}

// rewindDropWord spells that count.
func (a *app) rewindDropWord() string { return rewindDropCount(a.rewindDrops()) }

// rewindDropCount is HOW A CUT'S COST IS SPELLED, and there is one of it because
// both rewind surfaces say the same sentence about the same arithmetic: a cut on
// a turn boundary is measured in turns, and a cut INSIDE the newest turn takes no
// whole turn with it, so it is measured in the steps it does take rather than
// claiming a round zero. The timeline counts its rows and the inline mode counts
// its blocks; the words that go around the number are these.
func rewindDropCount(turns, steps int) string {
	if turns > 0 {
		return itoa(turns) + " " + plural("turn", turns)
	}
	return itoa(steps) + " " + plural("step", steps)
}

// ── DRAWING ─────────────────────────────────────────────────────────────────

// rewindPass draws the mode over a finished row list: the cut line above the
// anchor, everything from there down washed out, and the chosen block — plus
// whichever one the pointer is offering — brightened.
//
// IT IS A PASS OVER THE JOINED ROWS, exactly like [app.hoverPass] and for exactly
// its reason: a wash is a property of the SCREEN and not of the conversation, and
// the entries' rows are CACHED (render.go's [app.entryRows]). Painting the wash
// into that cache would mean invalidating every block below the cut on every
// keystroke that moved it, and leaving a washed-out block behind on the way out.
// Nothing here writes an entry; [app.touch] is what makes the frame redraw.
func (a *app) rewindPass(out []row, width int) []row {
	if !a.rew.on {
		return out
	}
	anchor := a.rewindAnchor(a.rew.at)
	hot := -1
	if a.hot.kind == hoverRewind {
		hot = a.rewindAnchor(a.hot.index)
	}
	// The line goes above the anchor's FIRST row. A cut past everything drawn
	// falls to the foot of the list, which is where a cut that drops nothing on
	// screen belongs.
	cut := len(out)
	for i, r := range out {
		if r.entry >= 0 && r.entry >= anchor {
			cut = i
			break
		}
	}
	rows := make([]row, 0, len(out)+1)
	rows = append(rows, out[:cut]...)
	rows = append(rows, row{text: a.rewindCut(width), entry: -1, hit: hitRewind})
	for _, r := range out[cut:] {
		// THE WASH IS A REPAINT, not a filter: the row arrives painted — accent on
		// a person's message, hues on a diff — and a dim wash that left any of that
		// showing would say "this row is different from the ones beside it" when
		// the true statement is "all of this goes".
		r.text = a.pal.dim(ansi.Strip(r.text))
		if r.entry >= 0 && (r.entry == anchor || r.entry == hot) {
			r.text = a.pal.hover(r.text, width)
		}
		rows = append(rows, r)
	}
	return rows
}

// rewindCut is the line itself: the surface's one horizontal rule, with the
// label in the accent because at that moment it is the live thing on the frame.
//
// It is the legend's shape (render.go's [app.legendLine]) rather than the
// compaction divider's centred one, and deliberately: a centred label reads as a
// SEAM between two halves of a conversation, and this line is not a seam — it is
// a cut, and a cut starts at the left edge and runs to the right.
func (a *app) rewindCut(width int) string {
	if width < 1 {
		return ""
	}
	label := rewindMark(a.pal) + " " + rewindCutWord
	head := "─ " + label + " "
	fill := width - ansi.StringWidth(head)
	if fill < 1 {
		return a.pal.dim(ansi.Truncate("─ ", width, "")) + a.pal.accent(fit(label, width-2))
	}
	return a.pal.dim("─ ") + a.pal.accent(label) + a.pal.dim(" "+strings.Repeat("─", fill))
}

// rewindBar is what stands in the draft box's position while the mode is up:
// what the cut costs on the left, the keys that move and commit it on the right.
//
// The two halves are the frame's own division of labour (view.go's status row):
// FACTS on the left, KEYS on the right. On a frame too narrow for both the legend
// goes and the fact stays — the keys are recoverable by pressing them, and the
// number of turns about to be dropped is not recoverable by anything.
func (a *app) rewindBar(width int) []string {
	if width < 1 {
		return []string{""}
	}
	left, paint := rewindMark(a.pal)+" drops "+a.rewindDropWord(), a.pal.dim
	if a.rew.said != "" {
		// THE ENGINE'S OWN SENTENCE, where the mode's feedback belongs: in the bar
		// the person is looking at, not in the transcript, which is the thing they
		// are deciding about.
		left, paint = a.rew.said, a.pal.warn
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(rewindKeysWord)
	if gap < 1 {
		return []string{paint(fit(left, width))}
	}
	return []string{paint(left) + strings.Repeat(" ", gap) + a.pal.dim(rewindKeysWord)}
}

// ── THE TWO LISTS ───────────────────────────────────────────────────────────

// rewindAnchor is one point's cut line, as an index into the drawn blocks.
func (a *app) rewindAnchor(at int) int {
	if at < 0 || at >= len(a.rew.anchors) {
		return len(a.entries)
	}
	if anchor := a.rew.anchors[at]; anchor >= 0 && anchor <= len(a.entries) {
		return anchor
	}
	return len(a.entries)
}

// rewindAnchors matches the engine's points onto the blocks this surface drew.
//
// THE TWO LISTS ARE NOT THE SAME LIST, and no index can be carried from one to
// the other. [session.RewindPoint.Entry] indexes the SESSION's transcript; the
// blocks on screen are what [app.replay] made of that transcript — minus the
// messages that draw nothing (an assistant step that only called tools, a result
// message), capped at [replayTail], and with this surface's own blocks mixed in
// among them: its notes, the reasoning it collapsed, the cards a task wrote.
//
// So the two are matched from the END, where they are guaranteed to agree: the
// newest thing in the session is the newest thing on the screen. Walking both
// backwards, every transcript entry that WOULD be drawn consumes one drawn block,
// the surface's own blocks are stepped over, and a transcript entry that draws
// nothing cuts where the next drawn thing does. Anything older than the drawn
// window lands at the top of it — the cut is real, the line is simply drawn as
// high as the screen goes.
func (a *app) rewindAnchors(points []session.RewindPoint) []int {
	transcript := a.agent.Transcript()
	// drawn[i] is where a cut at transcript entry i falls in the block list.
	drawn := make([]int, len(transcript))
	at := len(a.entries)
	for i := len(transcript) - 1; i >= 0; i-- {
		if !replayDraws(transcript[i]) {
			drawn[i] = at
			continue
		}
		for at > 0 && !fromTranscript(a.entries[at-1].kind) {
			at--
		}
		if at == 0 {
			break
		}
		at--
		drawn[i] = at
	}
	out := make([]int, len(points))
	for i, point := range points {
		switch {
		case point.Entry >= 0 && point.Entry < len(drawn):
			out[i] = drawn[point.Entry]
		default:
			out[i] = len(a.entries)
		}
	}
	return out
}

// replayDraws reports whether one transcript entry becomes a block on screen. It
// is [app.replay]'s own rule, asked as a question — a second reading of it would
// put the cut line one block away from where the conversation is.
func replayDraws(e session.DisplayEntry) bool {
	text := strings.TrimSpace(e.Text)
	switch e.Role {
	case "user":
		return text != "" || len(e.ImageRefs) > 0
	case "assistant", "note":
		return text != ""
	case "tool":
		return strings.TrimSpace(e.Tool) != ""
	}
	return false
}

// fromTranscript reports whether a block on screen came out of the session's
// transcript at all. The others are this surface talking — its own notes, a
// reasoning block the journal never held, the cards the tasker draws — and they
// are stepped over rather than matched, because nothing in the transcript
// corresponds to them.
func fromTranscript(kind entryKind) bool {
	switch kind {
	case entryUser, entryAssistant, entryTool, entryDivider:
		return true
	}
	return false
}
