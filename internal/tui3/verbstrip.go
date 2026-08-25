package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
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

// placeStrip is the strip row (or rows) in the frame's foot: the verbs when they
// are up, and otherwise home's answer strip, which is the same object opened by
// the row having a question rather than by a key.
func (a *app) placeStrip(width int) []string {
	if a.strip.open {
		return a.verbStripRow(width)
	}
	if a.page == pageHome && a.home.open {
		return a.answerStrip(width, time.Now())
	}
	return nil
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
	switch a.page {
	case pageHome:
		return a.homeRowVerbs()
	case pageStanding:
		return a.standRowVerbs()
	case pageMemory:
		return a.memoryRowVerbs()
	}
	return nil
}

// homeRowVerbs is the strip on home. On the resting switcher the verbs are the
// READING's — the question's own option words on a row that is asking, and the
// doors a row with an address has (homeswitch.go's [app.homeSwitchVerbs]); on a
// row built any other way it carries the two actions home has been ADVERTISING
// on a standing item without binding (`homeItemActions`, homestanding.go), which
// were bound to ctrl+e and ctrl+x, which the line never named, and whose bare
// `p` and `s` typed.
func (a *app) homeRowVerbs() []verb {
	if verbs := a.homeSwitchVerbs(); len(verbs) > 0 {
		return verbs
	}
	line, ok := a.home.previewLine()
	if !ok || line.kind != homeItem {
		return nil
	}
	return []verb{
		{key: 'p', word: homeItemPauseWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusPaused) }},
		{key: 's', word: homeItemStopWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusRetired) }},
	}
}

// standRowVerbs is the standing place's strip. Its three letters were bare while
// it was a modal overlay with no box under them; as a PLACE it has a composer,
// so they move here — which is the trade the promotion makes and the reason the
// strip had to exist before the promotion could.
func (a *app) standRowVerbs() []verb {
	if _, ok := a.standPage.current(); !ok {
		return nil
	}
	return []verb{
		{key: 'p', word: homeItemPauseWord, do: func() tea.Cmd { a.standPageWrite(standPause); return nil }},
		{key: 's', word: homeItemStopWord, do: func() tea.Cmd { a.standPageWrite(standDown); return nil }},
		{key: 'n', word: standNotHereWord, do: func() tea.Cmd { a.standPageWrite(standExcept); return nil }},
	}
}

// memoryRowVerbs is the memory place's strip, and it closes a real bug: `u`
// (undo a forget) was matched ahead of the filter's default arm, so a person
// could not type a `u` into the filter box at all — a search for "must" lost its
// second letter and put a memory back instead. On the strip the letter is a verb
// only while the strip is drawn, and the filter gets every letter of the
// alphabet back.
func (a *app) memoryRowVerbs() []verb {
	p := &a.memPanel
	var verbs []verb
	// THE TWO THAT ACT ON A LINE ARE OFFERED ONLY WHILE THERE IS A LINE. A shelf
	// heading, a section line, the prose at the top of a nearly-empty page — the
	// cursor stands on all of them and none of them has wording to fix or
	// anything to forget ([memoryPanel.choice] answers only on a line).
	if memory, ok := p.choice(); ok {
		verbs = append(verbs,
			verb{key: 'e', word: memoryFixWord, do: func() tea.Cmd {
				box := editor{}
				box.setText(memory.Text)
				p.edit, p.editID = &box, memory.ID
				return nil
			}},
			verb{key: 'f', word: memoryForgetWord, do: func() tea.Cmd {
				if a.memory != nil && a.memory.ForgetMemory(memory.ID) == nil {
					p.undoID, p.undoName = memory.ID, memory.Title
					p.footer = "forgot '" + memory.Title + "' · → " + memoryUndoWord
					p.forget(memory.ID)
				}
				return nil
			}})
	}
	// AND THE UNDO WHENEVER THERE IS SOMETHING TO PUT BACK, WITH OR WITHOUT A ROW
	// UNDER THE CURSOR. It is the one verb here that is about the PLACE and not
	// about a line — the line it would put back is, by definition, not on the
	// screen — and forgetting the last thing on a shelf must not be the one
	// forget that cannot be taken back.
	if p.undoID != "" {
		verbs = append(verbs, verb{key: 'u', word: memoryUndoWord, do: func() tea.Cmd {
			if a.memory == nil || a.memory.RestoreMemory(p.undoID) != nil {
				return nil
			}
			// AND THE PAGE IS RE-READ RATHER THAN PATCHED. Every other change this
			// place makes is one field this process just wrote and can therefore
			// correct in the held snapshot; a restore puts back a row that was
			// REMOVED from it, with counts and a shelf and a status the store owns,
			// so the honest redraw is the store's own answer ([app.refreshMemory]
			// is the same two statements the clock runs).
			a.refreshMemory()
			p.footer = "put '" + p.undoName + "' back"
			p.undoID, p.undoName = "", ""
			return nil
		}})
	}
	return verbs
}

// The words the strips say. Each is quoted in the manual exactly as it is
// spelled here, and the two home already had are borrowed from its own legend
// rather than written a second time.
const (
	memoryFixWord    = "fix the wording"
	memoryForgetWord = "forget it"
	memoryUndoWord   = "put it back"
)
