package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// SPELL IT OUT — the gap between what somebody meant and what they typed,
// closed at the box instead of at the answer.
//
// "build me a login page" is a finished sentence and an unfinished request.
// Everybody who types it wants the same dozen things and writes down none of
// them, so the model guesses, and which guesses were wrong is discovered by
// reading a whole answer. This is the offer to see it first: a dim line in the
// hint slot while the draft is making-shaped and short, one cheap call on the
// chord, and a block under the box saying what the sentence obviously means
// (internal/session's spellout.go writes it).
//
// ── THE FOUR LAWS OF THIS GESTURE ──
//
//   - IT NEVER FIRES ON ITS OWN. There is no safe trigger for spending a
//     person's money on a sentence they have not finished typing, so the only
//     thing that makes the call is [spellOutKey] under their own finger. The
//     hint is a courtesy that teaches the chord exists and nothing else.
//
//   - IT NEVER SENDS. `enter` on the block APPENDS it to the draft, and what
//     lands in the box is plain text with nothing special about it afterwards —
//     the person edits it, deletes half of it, or sends it, exactly as if they
//     had typed it. `esc` leaves no trace at all.
//
//   - A STALE EXPANSION IS NEVER ADDED TO A CHANGED SENTENCE. The block is shown
//     only while the draft is byte-identical to the draft it was made about
//     ([app.spellShowing]), so any key that edits the box dismisses it — not by a
//     hook that has to be remembered at twenty-two call sites, but because the
//     condition that draws it has stopped being true.
//
//   - THE EMPTINESS LAW, READ THE OTHER WAY. A draft that already spells out
//     what it wants gets no hint, and that is the point of the feature rather
//     than a limit on it: an offer to add detail to a detailed request is noise
//     under every draft on the surface, and a hint people learn to ignore is
//     worse than one they never saw.

// spellOutKey is the chord, and it is named ONCE: the hint under the box, the
// key router and the manual's own sentence are all this constant or a quotation
// of it.
//
// WHY THIS ONE. ctrl+g — the obvious reading of "go on and say more" — is spent
// twice over on this surface already: a running foreground command gets it
// first (background.go), and with none to keep it closes or restores the task
// column (task.go's railStowKey). Of what is left, ctrl+s
// and ctrl+q are flow control on a serial terminal, ctrl+d is end-of-file,
// ctrl+z is suspend and ctrl+h is what some terminals send for backspace — five
// chords whose failure mode is doing something the person cannot undo. ctrl+r is
// none of those: it is a plain control byte every terminal delivers, and the
// only place tui3 binds it is a task room's landed-files list (deliverables.go's
// filesRevealKey), which is a different page with a different keyboard. In the
// message box it has never meant anything.
//
// AND IT ACTS EXACTLY WHERE IT IS ADVERTISED. Pressed while the hint is not up,
// it does nothing at all — the same absence law ctrl+g keeps with no command
// running ([app.spellKey]).
const spellOutKey = "ctrl+r"

// The three sentences this gesture says. Two of them carry the whole of its
// vocabulary — `spell it out`, and internal/session's `taking it to mean —` —
// and nothing else here is a word a person has to learn.
var (
	// spellOutHint is the hint slot's line while the draft is making-shaped with
	// room to grow. It is the chord and what the chord does, in the slot's own
	// grammar (render.go's [app.hintWord]).
	spellOutHint = spellOutKey + " spell it out"
	// spellOutVerbs is the block's own last row: the two keys it answers, in the
	// order a person meets them. `enter` says where the text goes rather than
	// what happens to the block, because WHERE IT GOES is the fact somebody has
	// to have before they press it — this text is about to become part of their
	// own message.
	spellOutVerbs = "enter add it to what you're saying · esc leave it"
)

// spellOutPad is the two cells the prompt occupies, so the block sits under the
// draft's text rather than under its mark (input.go's [prompt]).
const spellOutPad = "  "

// spellOutRoom is how long a draft may be and still be offered the chord, in
// runes.
//
// IT IS THE EMPTINESS LAW IN A NUMBER. Past this the person has spelled it out
// themselves, and an offer to do it for them is the surface reading their
// paragraph and suggesting a paragraph. A hundred and twenty runes is about two
// lines of the box at an ordinary width — a sentence, or a sentence and its
// afterthought, which is exactly the draft this gesture is for.
const spellOutRoom = 120

// spellOutMakings are the verbs that make a draft making-shaped. It is a LOCAL,
// CHEAP look and never doctrine: no model is asked whether to draw the hint,
// because a call to decide whether to offer a call is the cost this feature was
// supposed to be an alternative to.
//
// SO FALSE NEGATIVES ARE FREE AND THE LIST STAYS SHORT — standmark.go's rule
// about the same slot, for the same reason: a wider list would put a dim line
// under half the drafts on this surface. They are matched at a word boundary by
// [standMarkAt], which is this surface's one reading of "this phrase, at the
// start of a word", and spelled without a trailing space on purpose so that
// build, builds and building are all one entry. `add ` is the one exception and
// carries its own space, because the three letters on their own are the front of
// address and additional and would put the hint under drafts about neither.
var spellOutMakings = []string{
	"build",
	"create",
	"make",
	"write",
	"design",
	"add ",
	"implement",
	"set up",
	"generate",
	"draft",
	"put together",
}

// spellOutBullets are the line marks that say a draft has already been spelled
// out. Somebody who has typed a list has done this gesture's work by hand, and
// the hint under it would be the surface not reading what is in the box.
var spellOutBullets = []string{"- ", "* ", "• ", "1. "}

// spellState is what this gesture holds on the surface, and it is deliberately
// three fields with no identity in them: there is at most one of these at a
// time, because there is at most one draft.
type spellState struct {
	// asking is a call in flight. The hint slot turns the build's spinner while
	// it is true ([app.hintWord]) and the paint clock is kept awake for it
	// (app.go's animation list).
	asking bool
	// at is the draft the call was made about, kept as runes so the comparison
	// that decides whether the block is still current costs no allocation on a
	// surface that asks it every frame ([app.spellShowing]).
	at []rune
	// block is what came back, opened by internal/session's [session.SpellOutOpening].
	// Empty is the resting state and the whole of what a failure looks like.
	block string
}

// spellOutAgent is the door, and it is an OPTIONAL interface asserted here
// rather than a method on [Agent], for this package's usual reason: a capability
// every agent must have goes on the base seam, and one only some builds can
// answer goes in the file that uses it. A build whose agent has no SpellOut
// never draws the hint, which is the absent-not-broken law — there is no
// keystroke that fails and no note explaining why.
type spellOutAgent interface {
	SpellOut(ctx context.Context, draft string) string
}

// spelledMsg is one answer to one chord.
//
// IT CARRIES THE DRAFT IT WAS MADE ABOUT rather than reading the box on the way
// back (taskcommand.go's taskSizedMsg states the same rule): the person has had
// ten seconds to keep typing, and an answer matched against whatever is in the
// box now is how a stale expansion gets attached to a changed sentence.
type spelledMsg struct {
	draft string
	block string
}

// spellDoor is the agent's expansion door, when this build has one.
func (a *app) spellDoor() (spellOutAgent, bool) {
	door, ok := a.agent.(spellOutAgent)
	return door, ok
}

// spellOffered reports whether the hint slot should name the chord — which is
// exactly whether the chord would DO anything if it were pressed right now.
//
// A HINT MAY ONLY NAME A KEY THAT WORKS (render.go's [app.hintWord] states the
// whole law), so this one predicate is both the advertisement's condition and
// the key's guard, and the two cannot come apart.
//
// AND IT IS WHERE THE SLOT IS SHARED. Two conditional hints now want the same
// cells — this one and the standing chord's (standmark.go) — and THE STANDING
// HINT WINS, said once, here, rather than as an ordering in the slot's switch.
// The reason is which mistake each one prevents: a standing sentence read as
// one-off work is a rule that silently never existed, and a making-shaped draft
// sent unexpanded is a good answer to a slightly vague question. They collide
// about as often as somebody types "always build" — and when they do, the
// costlier miss keeps the line.
func (a *app) spellOffered() bool {
	if a.input.empty() || a.state == stateWorking || a.spell.asking {
		return false
	}
	// AND THE BLOCK IS ITS OWN ADVERTISEMENT. While one is up its last row names
	// the two keys that work, so the slot repeating a chord that would replace it
	// would be the surface offering to do again the thing it has just done.
	if a.spellShowing() {
		return false
	}
	if a.copy.on || a.rew.on || a.roomOpen() {
		return false
	}
	if _, ok := a.spellDoor(); !ok {
		return false
	}
	if a.standMarkOffered() {
		return false
	}
	return looksMaking(a.input.value)
}

// looksMaking is the shape law: a making verb, and room left to grow.
//
// It reads the draft's RUNES rather than a string of them for [editor.empty]'s
// reason — this question is asked on every frame the box is not empty, and a
// paste is what people put in this box — and the length test comes first because
// it is the one that costs nothing.
func looksMaking(draft []rune) bool {
	if len(draft) == 0 || len(draft) > spellOutRoom {
		return false
	}
	head := strings.ToLower(string(draft))
	trimmed := strings.TrimSpace(head)
	// A COMMAND IS SAID TO THIS SURFACE AND NOT TO THE MODEL, so there is no
	// request here to spell out (standmark.go reads a slash the same way).
	if strings.HasPrefix(trimmed, "/") {
		return false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		for _, bullet := range spellOutBullets {
			if strings.HasPrefix(strings.TrimSpace(line), bullet) {
				return false
			}
		}
	}
	for _, verb := range spellOutMakings {
		if standMarkAt(head, verb) {
			return true
		}
	}
	return false
}

// spellShowing reports whether the block is on the frame — and it is the law
// about stale expansions, written as the condition that draws it.
//
// The block belongs to ONE draft. While the box still holds that draft the block
// is current; the moment a keystroke changes it — a letter, a backspace, a
// paste, a history recall, a send that empties the box — it is not, and it is
// gone from the frame in the same paint. Nothing has to remember to dismiss it,
// which is the only version of this rule that cannot be forgotten at one of the
// twenty-two places a draft is edited (draft.go's [app.edited]).
func (a *app) spellShowing() bool {
	return a.spell.block != "" && sameRunes(a.spell.at, a.input.value)
}

// sameRunes is equality over two rune slices, without building a string of
// either. It exists for [app.spellShowing]'s sake: that question is asked twice
// a frame and a paste is what people put in this box.
func sameRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spellKey is the whole of this gesture's claim on the keyboard, and it is ONE
// hook in input.go's router for that reason: which keys it takes and when is
// decided here, beside the state that answers them.
//
// It is read after the typed lists have had the keyboard and before the plain
// switch, so a completion menu's own enter and esc are never taken from it.
func (a *app) spellKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case spellOutKey:
		if !a.spellOffered() {
			// THE KEY IS ABSENT WHEREVER IT CANNOT WORK, and absent means it does
			// nothing at all rather than saying it cannot: pressed over a detailed
			// draft this falls through to the bottom of input.go's router, where a
			// key that carries no text has always done nothing.
			return nil, false
		}
		return a.spellAsk(), true
	case "enter":
		if !a.spellShowing() || a.state == stateWorking {
			return nil, false
		}
		return a.spellAdd(), true
	case "esc":
		// THE INTERRUPT IS NOT FOR SALE (input.go says the same about the rewind
		// arm): while a turn is running esc stops it, whatever else is on the
		// frame. The block cannot ordinarily be up then — sending empties the box
		// and the block goes with the draft — and where it somehow is, the key
		// keeps its more important meaning.
		if !a.spellShowing() || a.state == stateWorking {
			return nil, false
		}
		a.spellDrop()
		return nil, true
	}
	return nil, false
}

// spellAsk makes the one call.
//
// It goes through a command rather than the Update loop for [app.submitStanding]'s
// reason: the call talks to a provider and the loop is not a place to wait. The
// hint slot turns the build's spinner while it is out, and [app.wake] is what
// keeps the frames coming for it — nothing else on the surface is moving.
func (a *app) spellAsk() tea.Cmd {
	door, ok := a.spellDoor()
	if !ok {
		return nil
	}
	// The helper reads the draft as the model would — every paste unfolded —
	// and spends nothing: the chips are still the person's (pastechip.go).
	draft := a.pastesUnfolded(a.input.String())
	a.spell = spellState{asking: true, at: append([]rune(nil), a.input.value...)}
	ctx := a.ctx
	a.touch()
	return tea.Batch(a.wake(), func() tea.Msg {
		return spelledMsg{draft: draft, block: door.SpellOut(ctx, draft)}
	})
}

// spelled takes the answer.
//
// A FAILED OR SLOW CALL JUST PUTS THE HINT BACK. There is no note, no error
// prose and no row: nobody was promised this, the draft is untouched, and the
// slot returning to `ctrl+r spell it out` is the honest whole of what happened.
// A stale answer — the draft changed while the call was out — is dropped by the
// same silence, because the block it would draw is about a sentence that no
// longer exists.
func (a *app) spelled(msg spelledMsg) {
	if !a.spell.asking {
		return
	}
	a.spell.asking = false
	a.touch()
	// A STALE ANSWER IS FORGOTTEN AND NOT MERELY HIDDEN. The block is drawn only
	// while the box still holds the draft it is about ([app.spellShowing]), so
	// keeping this one would be harmless right up until somebody undid their edit
	// and an expansion they never asked for came back on screen.
	if msg.draft != string(a.spell.at) || !sameRunes(a.spell.at, a.input.value) {
		a.spell = spellState{}
		return
	}
	a.spell.block = msg.block
}

// spellAdd is `enter` on the block: the text goes into the draft, and after that
// it is the person's own sentence.
//
// IT IS APPENDED VERBATIM AND PLAIN. Nothing is remembered about which part of
// the box came from here — no styling, no marker, no second state to keep in
// step — so the very next keystroke edits it like anything else and the message
// that goes is one ordinary message. That is also why nothing is sent: the block
// is a draft's worth of text, and what to do with a draft is not this gesture's
// decision.
func (a *app) spellAdd() tea.Cmd {
	block := a.spell.block
	if block == "" {
		return nil
	}
	a.spell = spellState{}
	// A blank line between the sentence and the block, unless the person's draft
	// already ends in one. The two are separate paragraphs of the same message.
	draft := a.input.String()
	lead := "\n\n"
	if strings.HasSuffix(draft, "\n") {
		lead = "\n"
	}
	// [editor.setText] and not [editor.insert], because the caret may be anywhere
	// in the sentence and this text belongs at the END of the message whatever the
	// person was mid-word on. It parks the caret after it, which is where somebody
	// who has just added three clauses is about to type.
	a.replaceInput(len(a.input.value), len(a.input.value), lead+block)
	a.input.cursor = len(a.input.value)
	return a.edited()
}

// spellDrop is `esc`: zero trace. The draft is not touched, nothing is noted and
// nothing is remembered — the next chord makes a fresh call, because the point
// of leaving it is that it was not wanted.
func (a *app) spellDrop() {
	a.spell = spellState{}
	a.touch()
}

// spellWorkingWord is the hint slot while the call is out: the build's own
// working treatment — the braille spinner every live row on this surface turns
// (homespinner.go) — in front of the same words the slot was already saying.
//
// NO NEW VOCABULARY FOR THE WAIT. A second sentence here would be a second thing
// to read in a slot somebody glances at, and the one fact worth saying is that
// the thing they pressed is happening.
func (a *app) spellWorkingWord() string {
	return tokens.Spinner(a.paints/spinnerStep) + " " + strings.TrimPrefix(spellOutHint, spellOutKey+" ")
}

// spellHeight is how many rows the block takes: its own lines, then the one dim
// line of verbs. Zero on every frame of an ordinary conversation.
func (a *app) spellHeight() int {
	width, _ := a.size()
	return len(a.spellRows(width))
}

// spellRows draws the block, under the box and clearly not yet part of the
// message.
//
// IT IS ALL ONE DIM TIER, which is the whole visual claim: this text is not
// something that happened, not something being said, and not in the message
// — it is an offer sitting where an offer can be read in the same glance as the
// draft it is about. The box above it does not move for any of this; the block
// is drawn below it, in rows the frame is charged for ([app.chromeHeight]).
func (a *app) spellRows(width int) []string {
	if !a.spellShowing() || width < 8 {
		return nil
	}
	out := make([]string, 0, spellRowCap)
	for _, line := range wrap(a.spell.block, width-len(spellOutPad)) {
		out = append(out, a.pal.dim(spellOutPad+line))
		if len(out) == spellRowCap-1 {
			break
		}
	}
	return append(out, a.pal.dim(fit(spellOutPad+spellOutVerbs, width)))
}

// spellRowCap bounds the block on screen: the opening line, six clauses, and the
// verbs. It is internal/session's own clause limit read as a height, and it is
// here so that a clause that wrapped cannot take rows the conversation needed —
// the text a person adds is [spellState.block] whole, whatever this drew.
const spellRowCap = 8
