package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ── ONE GRAMMAR, EVERY PLACE ────────────────────────────────────────────────
//
// This is the whole key law of the places, in one function, and the reason it is
// one function is the finding tui2 already wrote down about the surface before
// this one: "self was a page with a page-local key grammar, a place enum, its own
// focus zone and its own esc ladder; services were a second page beside it; the
// notebook was an overlay. Four surfaces, four sets of keys, four ways to be
// lost." An enum is fine. A per-place key grammar is not.
//
// SIX CLASSES, AND A KEY BELONGS TO EXACTLY ONE (SCREEN 3a):
//
//	↑↓ enter esc tab      move, open, back out, next place      never text
//	any printable         goes to the composer, always          never a verb
//	alt+enter             send what you typed off as a task     one chord
//	alt+1…7               jump straight to a place              drawn on the map
//	alt+<letter>          change how THIS place is shown        drawn on the map
//	shift+←→↑↓            move this place's time window         no letters spent
//	→ then a letter       act on the row — letters are verbs only here
//
// Two adaptations to terminal reality, both forced and both stated:
//
//   - THE MAP IS A CHORD, NOT A HOLD. A terminal cannot tell a program that a
//     modifier is down; it only says what arrived. So SCREEN 3b's "hold alt and
//     the map appears" is `alt+.`, which draws the map in the cells a person was
//     already reading and leaves it there until the next key.
//   - `alt+b` AND `alt+f` ARE NOT AVAILABLE to the alt+letter class. They are
//     the word jumps inside every box on this surface, the manual says they
//     "work nearly everywhere", and a composer that lost them would be a
//     composer that got worse to type in so that a place could gain a view.
//
// AND A BOX THAT HAS TAKEN THE KEYBOARD OWNS ITS OWN KEYS. The router claims
// keys at the PLACE level; a layer inside a place that has deliberately taken
// the whole keyboard — home's errand pane, the settings panel's value editor and
// its model picker — is read before this function and keeps every key it had.
// That is not an exception to one grammar, it is the same arbitration the manual
// already states about `tab`: everything else that wants it gets it first, and
// the router is the last claimant rather than the first.
//
// WHERE IT IS CALLED FROM MATTERS. A case added to input.go's plain switch is
// invisible to every place, because each place's handler returns above it. So
// this function is the FIRST LINE of each place's own key handler — five call
// sites, each keeping its right of first refusal — which is also what leaves the
// task page's "every printable key is the filter" law untouched: `alt+` and
// `shift+` chords carry no text and never reach a default arm.

// placeKey is the router's claim on one keypress. It reports whether it took it;
// when it did not, the place's own handler carries on exactly as it did before.
func (a *app) placeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// THE STRIP IS READ FIRST AND IT IS THE ONLY THING THAT IS. While it is
	// drawn the letters on it are verbs, and a router that claimed a key over
	// the top of a strip a person is looking at would be the exact defect
	// SCREEN 3a's clause forbids (verbstrip.go).
	if cmd, took := a.stripKey(msg); took {
		return cmd, true
	}
	key := msg.String()
	// AND THE MAP IS DISMISSED BY THE NEXT KEY, WHATEVER IT IS — then that key
	// does what it was always going to do. A map that had to be closed before
	// anything could be pressed would be a mode, and the whole point of drawing
	// it in the cells that were already there is that it is not one.
	if a.mapShowing {
		a.mapShowing = false
		a.touch()
		if key == "esc" || key == placeMapKey {
			return nil, true
		}
	}

	switch key {
	case placeMapKey:
		a.mapShowing = true
		a.touch()
		return nil, true

	case "tab":
		return a.showPage(nextPage(a.page, false)), true
	case "shift+tab":
		// THE CIRCLE WALKED THE OTHER WAY. It is not one of the six classes and
		// it does not need to be: it is `tab`'s own inverse, which every tab bar
		// in every program has meant since tab bars existed, and a person who
		// overshoots must not have to go round six more places to get back.
		return a.showPage(nextPage(a.page, true)), true

	case "alt+enter":
		return a.placeSend(), true

	case "shift+left", "shift+right", "shift+up", "shift+down":
		// TIME IS TWO AXES AND FOUR KEYS (SCREEN 3d): ←→ moves the window this
		// place is showing, ↑↓ changes how coarse it is. No place has a window
		// yet — the records that would give tasks, standing and spend one are
		// another lane's — so the hook exists, every place answers false, and the
		// keys do nothing rather than doing something undrawn.
		if a.placeWindow(key) {
			a.touch()
			return nil, true
		}
		return nil, true

	case "right":
		// `→` OPENS THE ROW'S VERBS, and only when the row has any. Where it does
		// not, the arrow keeps every meaning it already had on that place — the
		// fold ladder and the walk across the columns on home, the caret's step
		// inside a box everywhere — which is what makes this a new claim on the
		// key rather than a seizure of it.
		if a.openStrip() {
			a.touch()
			return nil, true
		}
		return nil, false
	}

	if id, ok := placeDigit(key); ok {
		return a.showPage(id), true
	}
	if letter, ok := placeAltLetter(key); ok {
		if a.placeAlt(letter) {
			a.touch()
			return nil, true
		}
		// AN UNDECLARED alt+<letter> IS SWALLOWED RATHER THAN PASSED DOWN. The
		// class belongs to the place; a chord that fell through to a box would
		// insert nothing on some terminals and a stray character on others, and
		// "it depends on your terminal" is not an answer this surface gives.
		return nil, true
	}
	return nil, false
}

// placeMapKey is the map (SCREEN 3b). The period is the one punctuation key with
// no meaning inside a word being typed as a chord, and it reads as "and what
// else is here".
const placeMapKey = "alt+."

// placeDigit is `alt+1`…`alt+7`: the place at that position in [pages].
//
// WHY alt AND NOT ctrl: `ctrl+1` has no encoding a terminal can send, and most
// drop it entirely. `alt+1` arrives as esc-then-1 and has for forty years, which
// is why it is the one modifier class this program can promise everywhere.
func placeDigit(key string) (page, bool) {
	if !strings.HasPrefix(key, "alt+") || len(key) != 5 {
		return 0, false
	}
	at := int(key[4] - '1')
	all := pages()
	if at < 0 || at >= len(all) {
		return 0, false
	}
	return all[at], true
}

// placeAltLetter is `alt+<letter>` with the two spellings the composer already
// owns held back ([app.placeKey]'s note says why `b` and `f` may not be taken).
func placeAltLetter(key string) (rune, bool) {
	if !strings.HasPrefix(key, "alt+") || len(key) != 5 {
		return 0, false
	}
	letter := rune(key[4])
	if letter < 'a' || letter > 'z' || letter == 'b' || letter == 'f' {
		return 0, false
	}
	return letter, true
}

// placeAlt is the "show it differently" hook: what one place does with one
// letter, and false everywhere the place has nothing to change.
//
// A PLACE DECLARES ONLY WHAT IT CAN ACTUALLY DO. Grouping home by project,
// hiding the quiet ones, showing only what was tidied — SCREEN 3b names all
// three — are real views that do not exist yet, and binding their letters now
// would put keys on the map that do nothing, which is this design's own worst
// failure mode.
func (a *app) placeAlt(letter rune) bool {
	switch a.page {
	case pageMemory:
		if letter == 's' && a.memPanel.open {
			// WHICH SHELF THIS PLACE IS SHOWING. It was `tab` while memory was a
			// modal overlay; `tab` is the way between places now, so the view key
			// moved into the class views belong to ([memoryPanel.cycleShelf]).
			a.memPanel.cycleShelf()
			return true
		}
	}
	return false
}

// placeWindow is the time-window hook: which stretch of time a place is showing,
// and how coarse.
//
// SCREEN 3d ASKS THESE FOUR KEYS OF EVERY PLACE THAT HAS A WINDOW — `shift+←→`
// pages the window by its own length, `shift+↑↓` changes how coarse its buckets
// are — and the label between the arrows is both the control and the reading. A
// place that has no window answers false, and the key then does nothing rather
// than doing something undrawn.
func (a *app) placeWindow(key string) bool {
	switch a.page {
	case pageTasks:
		return a.taskSheet.window(a, key)
	case pageStanding:
		return a.standPage.window(a, key)
	case pageSpend:
		return a.spendWindowKey(key)
	}
	return false
}

// placeBox is the composer: the one box this place types into.
//
// EVERY PLACE'S BOX IS ITS OWN, AND IT IS ONE BOX WITH TWO READINGS. Home proved
// the shape — "the one foot box: new message AND live query at once, no mode" —
// and the settings panel proved it independently, searching across every tab and
// moving the tab to the first match. So the composer does not replace the
// filters: it IS them, on every place, and `alt+enter` is what tells the two
// readings apart at the moment it matters.
func (a *app) placeBox() *editor {
	switch a.page {
	case pageHome:
		if !a.home.open {
			return nil
		}
		// THE FOOT BELONGS TO WHOEVER HOLDS THE KEYBOARD: a focused errand brings
		// its own line and the caret sits in it (homeexchange.go).
		if ex := a.paneExchange(); ex != nil && ex.focused {
			return &ex.box
		}
		return &a.home.box
	case pageTasks:
		if a.taskSheet.open {
			return &a.taskSheet.query
		}
	case pageSettings:
		if a.sheet.open {
			// A SUBMENU'S OWN BOX OUTRANKS THE PANEL'S SEARCH, in the order the
			// panel already claims the keyboard in ([app.sheetKey]): the value
			// being edited, then the model picker's filter, then the search that
			// crosses every section.
			if a.sheet.edit != nil {
				return &a.sheet.edit.box
			}
			if a.sheet.sel != nil {
				return &a.sheet.sel.pick.filter
			}
			return &a.sheet.query
		}
	case pageMemory:
		if a.memPanel.open {
			if a.memPanel.edit != nil {
				return a.memPanel.edit
			}
			return &a.memPanel.filter
		}
	case pageStanding, pageSpend, pageSearch:
		return &a.compose
	}
	return nil
}

// placeSend is `alt+enter`: what is in the composer leaves as a task.
//
// FROM A PLACE THAT IS NOT HOME IT CARRIES YOU TO HOME AND ASKS THERE, and that
// is a design decision rather than a shortcut. An errand's answer is drawn in
// home's own column ([app.showExchanges]); minting one from the spend place and
// leaving the person on the spend place would be work started somewhere they
// cannot watch it — the failure [app.askHere]'s own comment calls "the one
// failure worse than saying no". So the verb is in reach from every place, and
// pressing it puts you where the answer will arrive.
func (a *app) placeSend() tea.Cmd {
	box := a.placeBox()
	if box == nil {
		return nil
	}
	text := strings.TrimSpace(box.String())
	if text == "" {
		return nil
	}
	if a.page == pageHome {
		return a.askHere(text)
	}
	box.reset()
	open := a.showPage(pageHome)
	if !a.home.open {
		return open
	}
	return tea.Batch(open, a.askHere(text))
}

// placeTalk is `enter` on a place with something in the composer and no row to
// open: TALK ABOUT IT, which is a conversation and not a task.
//
// It is [app.homeStart] without home's list under it — a fresh conversation
// carrying the sentence that opened it — and it leaves the place for the same
// reason home does: what was asked for is now happening somewhere a person can
// watch it, and that somewhere is the conversation.
func (a *app) placeTalk() tea.Cmd {
	box := a.placeBox()
	if box == nil {
		return nil
	}
	text := strings.TrimSpace(box.String())
	if text == "" {
		return nil
	}
	if !a.canStart() {
		a.pageMsg = newUnavailableWord
		return nil
	}
	box.reset()
	a.standDownFullscreen()
	renewed := a.renew()
	return tea.Batch(renewed, a.submit(text))
}
