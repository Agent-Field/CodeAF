package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// THE THINKING CHIP — how hard this conversation thinks, said beside the model
// that is doing the thinking.
//
//	─ porting the parser · glm-5.3-flash · ⠿ high · via deepinfra · main* ──── / commands ─
//	› what changed in the relay this week
//
// internal/effort landed the ladder and internal/session landed the dial, and
// the surface had nothing: the only door onto five rungs was a settings row
// that sets the INSTALL's default, which is the wrong scope for the question
// people actually ask. "Think harder about this one" is a sentence about the
// conversation in front of them, and it was answerable only by changing what
// every other conversation on the machine would do afterwards.
//
// So the conversation's own rung gets a cell on the seam, a chord and a
// five-row ladder, and all of them read and write ONE scope —
// [session.Agent.SetConversationEffort], which is sticky in this session's
// meta.json and reaches every turn and every task this conversation hands out.
//
// ── IT SITS BESIDE THE MODEL BECAUSE IT IS A FACT ABOUT THE MODEL ──────────
//
// Until 2026-09-09 this was a chip at the right end of the TRAY, and the seam
// said the rung as a colon suffix on the model id — `glm-5.3-flash:high` — so
// one ladder was spelled two ways on one frame, in two places, and the tray row
// existed on most sessions only because the dial was on it. The seam is where
// the answer belongs: the model, how hard it is being asked to think, and who is
// serving it are one sentence about the next turn, and a person reads them
// left to right on the line their eye already crosses on the way into the box.
//
// AND IT IS ANCHORED TO THE MODEL, NOT TO THE END OF THE LINE. The `via` rider
// comes and goes on a sighting's own clock ([app.modelRiderAt]) and the branch
// comes and goes with the width, so a rung drawn after either of them would
// slide sideways under a hand that had just learned where it was. It is drawn
// immediately after the model's name, which moves only when the name does.
//
// ── WHAT THE CHIP SAYS IS WHAT WILL HAPPEN ─────────────────────────────────
//
// The word is [session.Agent.ResolvedEffort] and never the stored rung. A chip
// that showed what somebody CHOSE would be blank on the ordinary session where
// nobody has chosen anything — and would be a lie on the session where a level
// dialled onto the model itself is winning. What a person wants off a dial is
// the number the machine is running at, so the chip is drawn from the resolver
// and the scope that decided it is nobody's business up here.
//
// ── AND ABSENCE IS A STATE, SO IT IS SAID: `⠿ auto` ────────────────────────
//
// THE SHIPPED SETTING IS ABSENCE — [effort.Ship] is [effort.None], nothing is
// asked for and the model thinks however it thinks — so on an install nobody
// has dialled the resolver answers "" and this cell, drawn from that word
// alone, WAS ABSENT ON EVERY FRESH CONVERSATION. Which is to say: on a shipped
// install it was absent always, on the in-process road and the hosted one
// alike, and the only person who ever saw a rung was one who already knew there
// was a dial to turn. The tray chip this replaced had the same hole, and that
// is exactly why the owner never learned the dial existed and asked for one to
// be built.
//
// A CONTROL THAT IS INVISIBLE UNTIL YOU HAVE ALREADY USED IT IS NOT A CONTROL
// (CLAUDE.md's discoverability law). So absence is drawn, by name — `⠿ auto`,
// the word the `thinking` settings row has offered for this state since the
// ladder landed ([effortAutoWord]).
//
// THIS IS NOT A BREACH OF THE EMPTINESS LAW. That law refuses a word for a
// number nobody has — `$0.00`, `0 tok` — and auto is not zero thinking. It is a
// decision about WHO DECIDES, it is the decision this install shipped with, and
// a state somebody chose the shape of is a fact to state. The cell is still
// absent where there is no dial at all — no agent, or a far engine whose
// welcome does not carry one ([app.effortDial]) — because that is the state the
// absence law is actually about.
//
// The one place that costs something is the chord: setting the conversation's
// rung cannot move a resolved rung the TURN scope is deciding (the model
// picker's ctrl+t, or `--reasoning`). That is a knob doing nothing, which is
// exactly the defect internal/session/effort.go's own header names — so the
// chord checks and says so, in the words of the door that would move it.
//
// ── IT IS FURNITURE, AND ACCENT FOR ONE MOMENT ─────────────────────────────
//
// THE ACCENT BUDGET is one lit element per screen and this is not it: a rung
// that sat lit above the box forever would spend the budget on a fact that
// changes once a week. So the cell is dim, like the rest of the seam. The
// exception is the moment it CHANGES, when it is briefly the one live thing on
// the frame and takes THE EMPHASIS LAW's two moves — the selected ground and the
// accent on its leading glyph — and then settles back, the same shape the copied
// rows keep for three seconds after a sweep (dragselect.go).
//
// ── THE PRESS WALKS IT, THE WAY THE PRESS ON A TASK WALKS THAT TASK'S ──────
//
// A click on the cell is [app.cycleEffort] and not a list, which is the gesture
// a person already met on a task: the room panel's thinking row steps the node's
// rung one place and says so in a note (roompanel.go's [app.roomPanelTake],
// taskeffort.go's [app.cycleNodeEffort]). One verb, one gesture, every scope —
// the same rule effortscope.go states about the chord.
//
// THE LADDER KEPT ITS DOOR AND IT IS A COMMAND NOW. The rows with their
// sentences used to open from a click on the tray chip, which is the gesture
// this ruling spends on the wheel; so `/effort` opens them and `/effort <rung>`
// picks one outright ([app.runEffort]). A capability whose only door was taken
// away is a capability that has been deleted by accident, and this one is worth
// keeping: the words with what each one buys is how a person decides between
// them, and the wheel alone can only be walked blind.
//
// AND THE WAY BACK TO auto IS BY NAME, NEVER BY THE WHEEL. The wheel has five
// stops and wraps off the top ([app.cycleEffort] states why absence is not a
// sixth), so `/effort auto` — and the ladder's own top row — is what hands the
// scope back to whatever stands above it.

// effortKey is the chord that walks the ladder, written down once: the router
// binds it, the manual prints it and the menu's foot names it, and a surface
// that printed a key nobody bound would be lying about itself.
//
// ctrl+v is free here and only here. In a terminal, paste is the TERMINAL's
// gesture — it arrives as a bracketed paste and never as a keystroke — so the
// chord reaches this application only on the terminals that decline to spend it
// themselves, and on those it was doing nothing at all.
const effortKey = "ctrl+v"

// glyphEffort marks the thinking chip, and it is [glyphThought] rather than a
// new symbol on purpose: the reasoning block already opens with the full
// braille cell, and what this chip says is how deep THAT block will go. One
// mark for one subject. The diamond the tray's other chip wears is spoken for
// (styles.go's [glyphHarness]) and two diamonds on one row meaning two things is
// a mark that has to be read twice to learn which one it is.
//
// The linear tier gets a name rather than a shape, on styles.go's terms: `~` is
// one byte, is not announced as a codepoint nobody chose, and reads as "about".
const (
	glyphEffort      = glyphThought
	glyphEffortASCII = "~"
)

// effortAutoWord is what this surface calls absence: the dial left alone, with
// nothing asked for and the model thinking however it thinks.
//
// IT IS THE SETTINGS ROW'S OWN WORD, taken from internal/config rather than
// spelled again here (CLAUDE.md's one-source-of-truth law). The `thinking` row
// in /settings has offered `auto` at the head of its choices since the ladder
// landed, and the seam calling the same state something else would be two names
// for one answer on one screen — the defect the colon suffix already made once.
var effortAutoWord = config.EffortWord(effort.None)

// effortFlashFor is how long the chip wears its change.
//
// It is shorter than the sweep's three seconds (dragselect.go's [dragFlashFor])
// because the two flashes answer different questions. A copy has to survive the
// person looking away at the window they are pasting into; this one only has to
// outlast the hand leaving the key, and a chord people press four times in a row
// to walk from low to max should have settled before they start their sentence.
const effortFlashFor = 2 * time.Second

// effortFlashMsg is the flash expiring: one repaint, so the chip comes down. It
// is [dragFlashMsg]'s twin and exists for the same reason — without it the
// emphasis would sit on an idle frame until something else asked for a redraw.
type effortFlashMsg struct{}

// effortDialer is the narrow slice of the session this surface needs to draw
// and move the conversation's rung (internal/session's effort.go).
//
// It is asserted on the agent rather than added to [Agent] on [harnessRunner]'s
// terms: every scripted agent in this package's own tests is an [Agent], and a
// method added to that interface is a method thirty test doubles have to grow
// before a chip can be drawn. A session that cannot say how hard it thinks
// simply has no chip — the design law that a capability which cannot work is
// absent rather than broken.
type effortDialer interface {
	// ResolvedEffort is the rung the next turn will actually ask for, whichever
	// scope decided it.
	ResolvedEffort() string
	// ConversationEffort is the rung THIS conversation was set to, "" when
	// nobody has set one.
	ConversationEffort() string
	// SetConversationEffort sets it and reports whether the word was a rung.
	SetConversationEffort(rung string) bool
}

// effortDial is the session's dial, and false where there is none.
//
// AND A CONNECTION ANSWERS FOR THE MACHINE AT THE OTHER END. A *remote.Agent
// always has these three methods on it, so the assertion alone says yes for
// every `--host` session whatever the far engine can do — and the answer cannot
// be read off the rung either, because "" is a real rung here (a conversation
// asking for no thinking at all). The engine states the capability at the door
// and this asks it, exactly as [app.taskEffortDoors] asks about a task's own
// rung (taskeffort.go).
func (a *app) effortDial() (effortDialer, bool) {
	if a.agent == nil {
		return nil, false
	}
	dial, ok := a.agent.(effortDialer)
	if host, hosted := a.agent.(interface{ EffortSupported() bool }); hosted {
		ok = ok && host.EffortSupported()
	}
	return dial, ok
}

// effortWord is the rung the resolver has settled on: what the next turn will
// ask for, and "" for a conversation nobody has dialled on an install that has
// chosen nothing either — which is what a shipped install answers.
//
// IT IS THE LADDER'S OWN VOCABULARY AND NOT THE CELL'S. "" is [effort.None]
// here, which is what the chooser opens its cursor on and what [effort.Rung]
// round-trips; the word a PERSON reads for that state is [effortAutoWord] and
// it is put on only at the two places a person reads one ([app.effortChipText]
// and the chooser's rows).
//
// It asks the agent on every frame that draws the seam, which is a lock and two
// map reads (internal/session's effortLocked) — and over a connection a read of
// the fact set the engine states unasked, never a call (internal/remote's
// effort.go). That is deliberately unlike the context meter, which is measured
// where the answer changes and never on the frame clock (app.go's
// [app.measureContext]): the difference is that measuring a conversation walks
// it and this does not.
func (a *app) effortWord() string {
	dial, ok := a.effortDial()
	if !ok {
		return ""
	}
	return dial.ResolvedEffort()
}

// effortChipText is the chip unpainted — the mark and the word beside it, which
// is what docs/DESIGN-LANGUAGE.md's refusal of icon-only minimalism demands of
// every mark on this surface.
//
// "" ONLY WHERE THERE IS NO DIAL, never where there is a dial nobody has turned:
// that one says `⠿ auto`, for the reason the header gives at length. The two
// states were one string until 2026-09-09 and the cell was therefore missing on
// every conversation of a shipped install, which is the whole defect.
func (a *app) effortChipText() string {
	dial, ok := a.effortDial()
	if !ok {
		return ""
	}
	word := dial.ResolvedEffort()
	if word == "" {
		word = effortAutoWord
	}
	mark := glyphEffort
	if a.pal.ascii || a.pal.linear {
		mark = glyphEffortASCII
	}
	return mark + " " + word
}

// paintEffortChip is the chip's one cell of colour, in the two states that are
// not the resting one.
//
// At rest it is dim, with the rest of the seam, because it is furniture — and
// that state is not drawn here at all: the seam paints its whole cluster in one
// tier and lifts this cell out of it only while there is something to lift
// (foot.go's [paintSpan] takes this as its lift). Under the pointer it takes the
// ground ladder's cursor step behind exactly its own cells, for hover.go's law:
// what lights is what the press acts on. And in the moment after a change it
// takes THE EMPHASIS LAW's two moves and no third — the selected ground, and the
// accent on the leading glyph — because that is the one moment this cell is the
// live thing on the screen.
func (a *app) paintEffortChip(text string) string {
	if a.effortFlashing() {
		mark, word, _ := strings.Cut(text, " ")
		lit := a.pal.accent(mark) + a.pal.ink(" "+word)
		return a.pal.background(lit, 0, a.pal.ramp.selected)
	}
	return a.pal.cursor(a.pal.dim(text), 0)
}

// effortSeamLit reports whether the seam's rung is wearing anything other than
// the line's own tier this frame — the flash it takes after a change, or the
// pointer.
//
// THE FLASH WINS WHERE BOTH ARE TRUE, and one of them has to: the cell is
// painted by a single lift closure and a hue inside a hue ends at the inner
// one's reset (styles.go's [palette.paint]). It is the flash because the press
// that lights it is made WITH THE POINTER ON THE CELL — a hover that outranked
// it would mean a click on the rung was the one gesture that never showed the
// change it made. Two seconds later the pointer's own step comes back.
func (a *app) effortSeamLit() bool { return a.hoveringEffort() || a.effortFlashing() }

// hoveringEffort is the pointer over the seam's rung, for the paint and for
// nothing else — the press asks [app.seamEffortSpan] itself.
func (a *app) hoveringEffort() bool { return a.hot.kind == hoverEffort }

// effortFlashing reports whether the chip is still wearing its last change.
//
// IT ASKS WHETHER THE NEWEST MOVE WAS ITS. The window records one move
// ([app.effortLit]), so a rung moved on a task or on home since takes the
// emphasis off this chip on the same frame it lights that card — one answer on
// the screen to "what just changed", which is what [effortMoved] is for.
func (a *app) effortFlashing() bool {
	if a.effortLit.where != effortScopeConversation || a.effortLit.at.IsZero() {
		return false
	}
	return a.now().Sub(a.effortLit.at) < effortFlashFor
}

// ── the chord ───────────────────────────────────────────────────────────────

// cycleEffort is [effortKey]: the conversation walks one rung up the ladder and
// wraps off the top back onto the cheapest.
//
// IT STARTS FROM WHAT THE CHIP SAYS. The word on screen is the RESOLVED rung, so
// a cycle that stepped from the stored one would move the chip from a word
// nobody could see to a word nobody expected — the first press on a session that
// has never touched the dial would jump from `high` to `low` because "" comes
// before every rung. Stepping from what is drawn is the only reading under which
// one press means one step.
//
// AND FROM `auto` THE FIRST PRESS LANDS ON `low`, which is [effortNext]'s own
// law: absence is where the wheel starts and never a stop on it (effortscope.go
// states it in full). So the cell a fresh conversation draws is the beginning of
// the walk and not a rung in the middle of one.
//
// THE CYCLE HAS FIVE STOPS AND absence IS NOT ONE OF THEM. A five-key walk with
// a sixth state hidden in it is a walk people lose their place in, and clearing
// a rung somebody paid for by pressing a key once too often is a move no wheel
// should be able to make. The way back to auto is by NAME — `/effort auto` and
// the ladder's own top row ([app.runEffort]) — which is a deliberate act, as
// handing the scope back to whatever stands above it ought to be.
func (a *app) cycleEffort() tea.Cmd {
	dial, ok := a.effortDial()
	if !ok {
		return nil
	}
	// THE WHEEL IS ONE FUNCTION FOR ALL FOUR SURFACES ([effortNext] in
	// effortscope.go). A second copy here would be a second place for "and it
	// wraps" to stop being true, and a person who learns the walk on the chip
	// knows it on a task for exactly as long as the two agree.
	return a.setEffortRung(dial, effortNext(effort.Rung(dial.ResolvedEffort())))
}

// setEffortRung is the ONE path from the chord and from the menu to the session,
// so the two can never mean slightly different things.
//
// IT CHECKS THAT IT WORKED, and that is not defensive coding. A level dialled
// onto the model itself — the picker's ctrl+t, or `--reasoning` at launch — is
// the turn scope, and the turn scope beats the conversation's (internal/effort's
// [Resolve]). Without this the chord would write a rung the resolver then
// ignored, and the chip would sit at a word the person had just pressed a key
// four times to move: a knob that does nothing with no way to tell from the
// outside, which is the exact defect the ladder was written to end.
func (a *app) setEffortRung(dial effortDialer, rung effort.Rung) tea.Cmd {
	if !dial.SetConversationEffort(rung.String()) {
		return nil
	}
	a.effortLit = effortMoved{where: effortScopeConversation, at: a.now()}
	a.touch()
	if got := dial.ResolvedEffort(); got != rung.String() {
		// The note names the model whose own level is winning and the door that
		// moves it, because "this did not take" without either is a message that
		// leaves a person pressing the key harder. THE PAYLOAD RULE lifts the id
		// and the chord — the two things being pointed at — and leaves the
		// sentence around them in the dim tier every note wears.
		// THE EMPTINESS LAW has a sentence-shaped edge here, the one crew.go's
		// unchanged clause has: a session with no model named yet has nothing to
		// put after "the level set on", and a gap there reads as a line that was
		// cut. The clause says the fact without the id instead, which is still
		// true and still points at the right door.
		id, on := modelBase(a.model), "the model's own level"
		facts := []string{"ctrl+t"}
		if id != "" {
			on, facts = "the level set on "+id, []string{id, "ctrl+t"}
		}
		a.noteFacts("thinking stays "+got+" · "+on+
			" decides this conversation — ctrl+t in /model changes it", facts...)
	} else if rung == effort.None {
		// CLEARING THE DIAL IS THE ONE MOVE WHOSE RESULT IS NOT A RUNG, so it is
		// the one move that says something. The five leave a word on the seam that
		// answers "what did that do" by itself; `auto` leaves a word that reads
		// like the dial went away, and a person who has just handed the scope back
		// deserves to be told what now decides. It is said in the flat dotted
		// grammar the other notes on this surface are written in.
		a.noteFacts("thinking · "+effortAutoWord+" · the model decides", effortAutoWord)
	}
	return tea.Tick(effortFlashFor, func(time.Time) tea.Msg { return effortFlashMsg{} })
}

// ── the menu ────────────────────────────────────────────────────────────────

// effortMenu is the six-row chooser the chip opens: auto and then the whole
// ladder, cheapest first, with the rung in force marked.
//
// Its zero value is closed, like [crewPicker], whose shape this is — a fixed,
// bottom-anchored list with no filter, because six words is a thing you read
// rather than a thing you search.
type effortMenu struct {
	open   bool
	cursor int
	// current is the rung in force when the menu opened, which is the row that
	// wears the chosen step. It is captured rather than re-read on every frame so
	// the marked row cannot move under a cursor that is walking past it.
	current effort.Rung
}

// effortMenuRungs is the chooser's rows: ABSENCE FIRST, then the ladder as
// internal/effort holds it.
//
// IT IS THE LADDER PLUS ONE AND NEVER A SECOND COPY OF IT (CLAUDE.md's
// one-source-of-truth law), so a rung that displaces another in [effort.Rungs]
// moves this list, the refusal that names the words, and the heights all at
// once. Auto leads because the rows are cheapest first and it is the cheapest
// thing there is — nothing is asked for — and because it is what the install
// ships at, which is the row most people opening this list are standing on.
var effortMenuRungs = append([]effort.Rung{effort.None}, effort.Rungs...)

func (m *effortMenu) start(current effort.Rung) {
	*m = effortMenu{open: true, current: current}
	for at, rung := range effortMenuRungs {
		if rung == current {
			m.cursor = at
			return
		}
	}
}

func (m *effortMenu) close() { *m = effortMenu{} }

func (m *effortMenu) move(delta int) {
	m.cursor = (m.cursor + delta + len(effortMenuRungs)) % len(effortMenuRungs)
}

// The chooser's fixed lines around the rows, spelled once so the height and the
// rows cannot count them differently.
const (
	// effortScopeLine is the header: what the rows below move, and what they do
	// not. It is the sentence the whole control exists to make plain, because the
	// settings row people have already met sets a different scope.
	effortScopeLine = "how hard the model thinks in THIS conversation and its work"
	// effortDefaultLine is the closing note: where the answer for every other
	// conversation is set, which this chooser deliberately does not touch.
	effortDefaultLine = "other conversations follow the thinking row in /settings"
	// effortFrameRows is how many of the chooser's rows are not rung rows: the
	// scope line and the closing note.
	effortFrameRows = 2
	// effortWordWidth is the column the rung words are padded to, so the
	// sentences after them line up. "medium" is the longest of the six and this
	// is its width; a loop to find the longest of six literals is machinery for
	// nothing (crew.go's [crewClassWidth] made the same trade).
	effortWordWidth = 6
)

// effortLines is what each row buys, in a person's words rather than the
// adapter's. The two top rungs say the shape of what they ask for — a deeper
// pass, paid for in time — because "more than high" is the only honest reading
// of a ladder whose provider vocabulary stops at high (internal/effort).
//
// AUTO'S LINE NAMES IT AS THE SHIPPED SETTING AND `high`'S NO LONGER DOES. That
// sentence sat on `high` and was simply false: [effort.Ship] is [effort.None],
// so an install nobody has touched asks for nothing at all — and a person
// reading the ladder to find out where they started was being pointed at the
// wrong row.
var effortLines = map[effort.Rung]string{
	effort.None:   "the model decides — the shipped setting",
	effort.Low:    "answers quickly and barely deliberates",
	effort.Medium: "a short think before it answers",
	effort.High:   "thinks before it answers",
	effort.XHigh:  "a deeper pass, and it takes the time that costs",
	effort.Max:    "the deepest pass there is",
}

func (m *effortMenu) height() int {
	if !m.open {
		return 0
	}
	return effortFrameRows + len(effortMenuRungs)
}

// rows is the chooser drawn, in the bottom-overlay row vocabulary every other
// list on this surface uses.
//
// THE RUNG IN FORCE AND THE CURSOR ARE TWO FACTS, AND A ROW CAN BE BOTH — the
// distinction crew.go's chooser had to learn the hard way. The rung a person is
// actually running is chosen and persistent, so it takes THE GROUND LADDER's
// selected step and its word turns accent; the cursor is where ↑/↓ has got to, so
// it takes the cursor step, the same step the pointer takes. The lead glyph says
// which of the two is which where they land on one row.
func (m *effortMenu) rows(width, n int, pal palette, hover int) []string {
	if !m.open || n <= 0 {
		return nil
	}
	out := make([]string, 0, m.height())
	out = append(out, pal.dim(fit(effortScopeLine, width)))
	for at, rung := range effortMenuRungs {
		oncursor, current := at == m.cursor, rung == m.current
		hovered := hover == len(out)
		lead := "  "
		switch {
		case oncursor:
			lead = pal.accent("› ")
		case hovered:
			lead = pal.accent("· ")
		}
		// The top row is absence, and it is named rather than left blank — a row
		// with an empty first column is a row nobody can tell is selectable.
		word := config.EffortWord(rung)
		for len(word) < effortWordWidth {
			word += " "
		}
		label := word + "  " + effortLines[rung]
		switch {
		case current:
			label = pal.accent(label)
		case oncursor, hovered:
			label = pal.ink(label)
		default:
			label = pal.dim(label)
		}
		row := lead + fit(label, width-2)
		switch {
		case current:
			row = pal.selected(row, width)
		case oncursor, hovered:
			row = pal.cursor(row, width)
		}
		out = append(out, row)
	}
	out = append(out, pal.dim(fit(effortDefaultLine, width)))
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// runEffort is `/effort`, and it is the LADDER'S DOOR now that the pointer's
// gesture on the cell is the wheel ([app.cycleEffort]).
//
// Bare, it opens the rows. With a word after it, it sets that row outright —
// through the one path the chord and the ladder already share, so a rung typed
// and a rung picked cannot mean slightly different things, and the note about a
// level on the model winning is the same sentence in all three.
//
// `/effort auto` IS THE WAY BACK, and `off` is its legacy alias — the two words
// internal/effort's [effort.Parse] already lands on [effort.None]. This door
// used to refuse both with the words that are not rungs at all, on the grounds
// that absence belonged to the settings row; that reading was wrong in the one
// direction that matters. The wheel must not reach absence (a rung cleared by
// one press too many is a rung cleared by accident) but a person must be able
// to GET THERE — the shipped setting is absence, so a conversation dialled up
// once had no way back to what it started at, and the seam now names that state
// on every fresh conversation.
//
// AN UNKNOWN WORD CHANGES NOTHING AND SAYS THE SIX, which is the shape every
// choice this surface refuses takes (crew.go's [app.runCrew]): a refusal that
// only said no would leave a person guessing at a word they were one letter
// away from.
func (a *app) runEffort(arg string) tea.Cmd {
	dial, ok := a.effortDial()
	if !ok {
		a.note(effortUnavailableWord)
		return nil
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		a.openEffortMenu()
		return nil
	}
	rung, parsed := effort.Parse(arg)
	if !parsed {
		a.noteFacts("/effort "+arg+" · not a thinking level · "+
			strings.Join(effortMenuWords(), " · "), effortMenuWords()...)
		return nil
	}
	return a.setEffortRung(dial, rung)
}

// effortUnavailableWord is what a conversation with no dial answers, in the
// shape [taskEffortUnavailableWord] says the same thing about one task.
const effortUnavailableWord = "how hard this conversation thinks is unavailable — this session has no dial onto it"

// effortMenuWords is every word this door takes, as a sentence lists them: auto
// and then the five. It is read off [effortMenuRungs] rather than repeated
// (CLAUDE.md's one-source-of-truth law: a rung added or dropped in
// internal/effort moves every refusal that names them), and each row is spelled
// by the same function the chooser spells it with.
func effortMenuWords() []string {
	words := make([]string, 0, len(effortMenuRungs))
	for _, rung := range effortMenuRungs {
		words = append(words, config.EffortWord(rung))
	}
	return words
}

// openEffortMenu opens the rows, and it TOGGLES: a door that opened a list
// and then ignored the same word typed again would be a door with no way back
// through the gesture that got you there.
func (a *app) openEffortMenu() {
	if a.effPick.open {
		a.effPick.close()
		a.touch()
		return
	}
	// The typed overlays are derived from the draft and would come straight back
	// on the next keystroke; they are closed the way every other command that
	// opens a fixed list closes them (crew.go's [app.runCrew]).
	a.closeLists()
	a.effPick.start(effort.Rung(a.effortWord()))
	a.touch()
}

// effortMenuKey routes one keypress while the chooser is up. It takes EVERY key,
// which is the idiom the fixed bottom-anchored lists on this surface keep
// (crew.go's [app.crewPickerKey], input.go's router): a plain letter typed into
// the box under a list a person is reading is a letter they have to find and
// delete afterwards. ctrl+c is excepted upstream, as it is for every modal here.
func (a *app) effortMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		a.effPick.close()
	case "enter":
		cmd = a.pickEffortRow(a.effPick.cursor)
	case "up", "ctrl+p":
		a.effPick.move(-1)
	case "down", "ctrl+n":
		a.effPick.move(1)
	case effortKey:
		// THE CHORD STILL WALKS THE LADDER WITH THE MENU UP. A key that opened a
		// list and then stopped meaning what it means everywhere else would be two
		// controls wearing one chord; here it moves the cursor, which is the same
		// step it makes on the chip.
		a.effPick.move(1)
	}
	a.touch()
	return cmd
}

// pickEffortRow is enter on the chooser, and the click that means the same
// thing: that row becomes the conversation's, and the list closes. The top row
// is absence and is picked like any other — it is the ladder's one door back to
// auto, since the wheel deliberately has none.
func (a *app) pickEffortRow(at int) tea.Cmd {
	if at < 0 || at >= len(effortMenuRungs) {
		return nil
	}
	rung := effortMenuRungs[at]
	a.effPick.close()
	dial, ok := a.effortDial()
	if !ok {
		return nil
	}
	return a.setEffortRung(dial, rung)
}

// effortMenuPress resolves a click on one of the chooser's rows, and reports
// whether it took the press. A click anywhere else falls through untouched: this
// list is not modal to the POINTER — the conversation under it is still a
// conversation — which is [app.harnessPickPress]'s own bargain.
func (a *app) effortMenuPress(y int) (tea.Cmd, bool) {
	if !a.effPick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		return nil, false
	}
	// The header is row zero and the closing note is the last row; neither is a
	// row of the ladder, and a press on a sentence does nothing at all.
	at := mark.index - 1
	if at < 0 || at >= len(effortMenuRungs) {
		return nil, true
	}
	a.effPick.cursor = at
	return a.pickEffortRow(at), true
}
