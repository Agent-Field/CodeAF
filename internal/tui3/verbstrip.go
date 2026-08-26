package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE VERB STRIP ──────────────────────────────────────────────────────────
//
// NO KEY DOES ANYTHING THAT IS NOT DRAWN ON SCREEN RIGHT NOW (SCREEN 3a). That
// one clause is the whole design of this file, and it cuts both ways:
//
//   - a bare letter may not be a verb, because every printable key belongs to
//     the composer and always will — that is the product, not a compromise;
//   - and a page may not ADVERTISE a letter it has not bound, which home was
//     doing for three waves: `homeItemActions` says "p pause · s stop" on a
//     standing item's card and on home's own hint line, while home binds those
//     two actions to ctrl+e and ctrl+x and lets bare `p` and `s` fall through
//     and type. The strip fixes both directions at once — the letters become
//     real, and they become real only while the line naming them is on screen.
//
// So: `→` on a row that has verbs draws them, and while that strip is drawn the
// letters on it are the verbs and THE COMPOSER IS ASLEEP. `esc` or `←` closes
// it, `enter` still opens the row, and the strip displaces the body by its own
// height — that visible displacement is what makes the bare letters safe, and it
// is the reason the strip is a row of the frame rather than a popup.
//
// The strip is [app.answerStrip] generalised. That function already held this
// law in its own words — "IT IS AN ANSWER, NOT A MIRROR: it draws only when the
// cursor's row carries a question the person can answer from here" — and all
// that changes is what opens it: a question being on the row, or a person
// pressing `→`.

// verb is one thing that can be done to the row under the cursor, in the row's
// own vocabulary. The letter is a MNEMONIC and never an index — `p` is pause
// wherever a row can be paused, on every place — because a person learns a verb
// once and meets it again.
type verb struct {
	key  rune
	word string
	do   func() tea.Cmd
}

// verbStrip is the strip's whole state: whether it is drawn, and what is on it.
//
// The verbs are captured when the strip OPENS rather than asked for on every
// frame, so a letter cannot act on a row that has moved out from under it — the
// same reason home's hit maps are written by the draw.
type verbStrip struct {
	open  bool
	verbs []verb
}

// stripHint is the line under the strip while it is up (SCREEN 3c). It names
// both ways out and the one key the strip does not take.
const stripHint = "esc or ← to leave · enter opens it instead"

func (a *app) closeStrip() {
	a.strip = verbStrip{}
}

// openStrip is `→` on a row that has verbs. It answers false when the row has
// none, and the arrow keeps every other meaning it already had on that place —
// which on home is the fold ladder and the walk across the columns.
func (a *app) openStrip() bool {
	verbs := a.rowVerbs()
	if len(verbs) == 0 {
		return false
	}
	a.strip = verbStrip{open: true, verbs: verbs}
	return true
}

// stripKey is every key while the strip is up. It is read before the place's own
// handler and before the composer, because a strip that could be typed over
// would be a strip whose letters were a lottery.
func (a *app) stripKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.strip.open {
		return nil, false
	}
	switch msg.String() {
	case "esc", "left":
		a.closeStrip()
		return nil, true
	case "enter":
		// ENTER STILL OPENS THE ROW. The strip is a second reading of the thing
		// under the cursor and never a mode over it, so the key that has always
		// meant "go into this" keeps meaning it, and the strip goes away because
		// the row it was about is no longer the thing on screen.
		a.closeStrip()
		return nil, false
	case "up", "down", "pgup", "pgdown", "tab", "shift+tab":
		// A KEY THAT MOVES OFF THE ROW CLOSES THE STRIP AND THEN MOVES. Verbs
		// belong to one row; carrying them onto the next would be exactly the
		// stale-map bug this file's capture avoids.
		a.closeStrip()
		return nil, false
	}
	for _, v := range a.strip.verbs {
		if msg.String() == string(v.key) {
			cmd := v.do()
			a.closeStrip()
			a.touch()
			return cmd, true
		}
	}
	// EVERY OTHER PRINTABLE IS SWALLOWED WHILE THE STRIP IS UP. It is the one
	// state on this surface where a letter is not a character, and a letter that
	// fell through into the composer here would be a letter the person believed
	// was a verb.
	if len(msg.String()) == 1 {
		return nil, true
	}
	return nil, false
}

// placeStrip is what the frame's FOOT carries above the hint: home's answer
// strip, which is the same object as the verbs' and is opened by the row having
// a question rather than by a key.
//
// THE VERBS ARE NOT HERE ANY MORE (SCREEN 3c). They are drawn inline, under the
// row they belong to, by [placeStripInline]; this file's own law says why —
// "the strip displaces the body by its own height, and that visible displacement
// is what makes the bare letters safe" — and a strip under the composer displaces
// nothing at all. The answer chips stay at the foot because they are not a row's
// verbs: they are what a conversation ANOTHER window is holding is waiting for,
// and they belong beside the box that could answer it.
func (a *app) placeStrip(width int) []string {
	if a.strip.open {
		return nil
	}
	if a.at(pageHome) {
		return a.answerStrip(width, time.Now())
	}
	return nil
}

// placeStripInline puts the verb strip INTO the body, directly under the row its
// letters are about (SCREEN 3c: "the strip pushes the list down and takes the
// letters with it").
//
// THE ROW IS THE PLACE'S OWN ANSWER and this function knows nothing about which
// place is up ([place.cursorRow]). The rows below the strip move down by its
// height and the body was built one row shorter to pay for it, so the frame is
// exactly the height it was before `→` was pressed.
//
// A CURSOR ON NOTHING THIS FRAME DREW PUTS THE STRIP LAST. It cannot happen from
// a keypress — the strip only opens over a row that offered verbs, and every
// place's window follows its own cursor — but a frame resized to two rows can
// leave the row off the bottom, and a strip that vanished there would be four
// letters bound and nothing on screen naming them, which is the one state this
// whole file exists to prevent.
func placeStripInline(a *app, rows []placeRow, strip []string) []placeRow {
	if len(strip) == 0 || len(rows) == 0 {
		return rows
	}
	at := len(rows) - 1
	if pl := a.showing(); pl != nil {
		if row := pl.cursorRow(a, rows); row >= 0 && row < len(rows) {
			at = row
		}
	}
	out := make([]placeRow, 0, len(rows)+len(strip))
	out = append(out, rows[:at+1]...)
	for _, line := range strip {
		// THE STRIP'S OWN ROWS ANSWER THE POINTER WITH NOTHING. They are not a row
		// of the place's reading, so a press on one must not resolve against the
		// line that happened to be at that index (pages.go's [placeHitsOf] turns a
		// nil hit into each place's own "no row").
		out = append(out, placeRow{text: line})
	}
	return append(out, rows[at+1:]...)
}

// placeRowAtLine is which of the rows just built carries one line of a place's
// own reading, for the places whose hit IS that line number. It is the answer
// [place.cursorRow] gives on standing and memory, and it is one function so the
// two cannot come to disagree about what a hit means.
func placeRowAtLine(rows []placeRow, line int) int {
	if line < 0 {
		return -1
	}
	for i, row := range rows {
		if at, ok := row.hit.(int); ok && at == line {
			return i
		}
	}
	return -1
}

// stripRow paints the verbs: the letter in the payload rule's own ink, the word
// beside it dim, one gap between pairs. It is [app.answerChipLines]' shape said
// on one line, because a strip that wrapped would move the body under it by a
// different amount depending on how many verbs a row happened to have.
func (a *app) verbStripRow(width int) []string {
	if width < 1 || len(a.strip.verbs) == 0 {
		return nil
	}
	pal := a.pal
	painted := make([]string, 0, len(a.strip.verbs))
	plain := make([]string, 0, len(a.strip.verbs))
	for _, v := range a.strip.verbs {
		painted = append(painted, pal.data(string(v.key))+pal.dim(" "+v.word))
		plain = append(plain, string(v.key)+" "+v.word)
	}
	line := " " + strings.Join(painted, verbGap)
	if ansi.StringWidth(" "+strings.Join(plain, verbGap)) > width {
		return []string{fit(line, width)}
	}
	return []string{line}
}

// verbGap is the space between two verbs. Three cells, not a separator dot: the
// strip is a row of choices rather than a sentence about them, and a `·` between
// them would read as prose.
const verbGap = "   "

// rowVerbs is what the row under the cursor can be asked to do, on whichever
// place is up.
//
// THE PLACE SUPPLIES THEM AND THIS FILE INVENTS NOTHING. A conversation that is
// not asking anything has no `y`; a row that cannot be paused has no `p`. That is
// the same law the answer chips already follow, and it is what makes a letter
// safe: the strip cannot offer a verb the row has no way to perform.
func (a *app) rowVerbs() []verb {
	pl := a.showing()
	if pl == nil {
		return nil
	}
	return pl.verbs(a)
}

// The words the strips say. Each is quoted in the manual exactly as it is
// spelled here, and the two home already had are borrowed from its own legend
// rather than written a second time.
const (
	memoryFixWord    = "fix the wording"
	memoryForgetWord = "forget it"
	memoryUndoWord   = "put it back"
)
