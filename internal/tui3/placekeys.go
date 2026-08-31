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
// AND THE TAB BAR IS ONE OF THE ROWS `↑↓` MOVE THROUGH. It is drawn over every
// place's body, so the row above the first row of a body is the same row on all
// seven: `↑` off the top lands on it, `←`/`→` walk the seven words without
// opening anything, `enter` or `↓` goes into the one under the cursor, and `esc`
// comes back down. Two arrows change meaning up there and the change is stated
// where a person can see it — `←`/`→` are ROW verbs (the strip, the folds) and
// the bar is not a row of any place's reading — so nothing on the bar acts on a
// row that is not under the cursor (pages.go's [barCursor]).
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

// placeKeyPress is every key on whatever place is standing: the router's own six
// classes first, and then that place's own reading of whatever is left.
//
// IT IS THE ONE DOOR, and that is what makes the grammar one grammar. Each place
// used to open its own handler with a call to [app.placeKey], which worked and
// was five copies of the same two lines — a place added later that forgot them
// would be a room `tab` could not leave.
func (a *app) placeKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	pl := a.showing()
	if pl == nil {
		return nil
	}
	// THE ROUTER'S OWN LAYER IS READ BEFORE THE PLACE'S, and it is the only thing
	// on this surface that is. The composer layer belongs to no place — the three
	// facts it settles are the same three wherever the sentence was typed — so a
	// place asked first would be a place answering keys for a decision it has
	// never heard of (composerlayer.go).
	if cmd, took := a.composerLayerKey(msg); took {
		return cmd
	}
	// A LAYER INSIDE THE PLACE THAT HAS THE WHOLE KEYBOARD IS READ NEXT, before
	// the router claims a single chord ([place.owns] holds the argument).
	if cmd, took := pl.owns(a, msg); took {
		return cmd
	}
	if cmd, took := a.placeKey(msg); took {
		return cmd
	}
	return pl.key(a, msg)
}

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
	// THE MAP'S SECOND ENCODING IS FOLDED INTO ITS FIRST HERE, and nowhere else.
	// A terminal that answered the keyboard query can send `ctrl+.` — which has
	// no legacy encoding and therefore arrives only from a terminal that took the
	// disambiguation flag — and normalising it to the one spelling above the
	// switch is what keeps the dismissal, the claim and the map's own hint line
	// reading a single key name (chords.go).
	if key == chordMapAlias && a.ctrlDigits() {
		key = placeMapKey
	}
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

	// THE CURSOR MAY BE STANDING ON THE TAB BAR, and while it is, five keys mean
	// something there rather than in the body: `←`/`→` walk the words, `↓` and
	// `enter` go into the one under the cursor, `esc` puts the cursor back in the
	// body (pages.go's [barCursor]). Everything else — `tab`, the numbers, the
	// map, a printable character — falls through to the arms below and means
	// exactly what it means everywhere else, which is what keeps the bar a row
	// rather than a mode.
	if cmd, took := a.barKey(msg); took {
		return cmd, true
	}

	switch key {
	case placeMapKey:
		a.mapShowing = true
		a.touch()
		return nil, true

	case "tab":
		// THE NEXT PLACE A PERSON CAN ACTUALLY GET INTO. A place that refuses to
		// open is walked past rather than walked into ([app.walkPage] tells the
		// whole story of what pressing `tab` on a fresh machine used to do).
		return a.walkPage(false), true
	case "shift+tab":
		// THE CIRCLE WALKED THE OTHER WAY. It is not one of the six classes and
		// it does not need to be: it is `tab`'s own inverse, which every tab bar
		// in every program has meant since tab bars existed, and a person who
		// overshoots must not have to go round six more places to get back.
		return a.walkPage(true), true

	case "alt+enter":
		return a.placeSend(), true

	case "alt+w", "alt+o":
		// THE LAYER'S TWO CHORDS ARE HELD BACK FROM THE PLACES. They mean one
		// thing and only inside the layer, and a place that bound either of them
		// would be a place whose view moved when somebody was aiming at a
		// destination. The layer is read above this function, so a press that
		// reaches here has no layer up and there is nothing to do.
		return nil, true

	case "shift+left", "shift+right", "shift+up", "shift+down":
		// TIME IS TWO AXES AND FOUR KEYS (SCREEN 3d): ←→ moves the window this
		// place is showing, ↑↓ changes how coarse it is. Three places have one —
		// tasks (when it ran), standing (when it fired) and spend (which days) —
		// and each of them draws the control on its own head row, through the one
		// helper they share (placeprose.go's [placeHeadRow]). A place with no
		// window, and a frame too narrow to draw the control, both answer false,
		// so the keys do nothing rather than doing something undrawn.
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

	case "up", "ctrl+p":
		// `↑` OFF THE FIRST ROW OF THE BODY LANDS ON THE TAB BAR, on every place
		// (pages.go's [barCursor]). It is one arm here rather than seven arms in
		// seven key handlers for the reason the whole of this function is one
		// function: the bar is drawn on all seven, so "what is above the first
		// row" has to have one answer.
		//
		// ANYWHERE ELSE THE KEY IS THE PLACE'S OWN and this router does not touch
		// it: [app.barReach] is false in the middle of a list, false on a frame
		// that drew no bar at all, and false while the cursor is already up there
		// — so the walk a person's hands know is untouched everywhere it was
		// already a walk.
		if a.barReach() {
			a.barRaise()
			return nil, true
		}
		return nil, false
	}

	if cmd, took := a.placeJumpKey(msg); took {
		return cmd, true
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

// placeJumpKey is `alt+1`…`alt+7` ALONE, and it is its own function because it
// is the one class of the six that belongs to no place.
//
// THE CONVERSATION IS A SURFACE THE NUMBERS HAVE TO WORK ON. Every other class
// in [app.placeKey] is about the room a person is standing in — the strip is its
// row's, the alt+letter is its view, the shift arrows are its window — so the
// whole grammar was reached only from the seven places' own key handlers. The
// jump is not like them: it is how a person GETS to a room, and the surface they
// are most often on when they want one is the conversation, where the chord did
// nothing at all and said nothing either. The manual has promised it there in as
// many words the whole time ("alt+1 goes straight there from anywhere",
// home.md), which made the program's own account of itself the thing that was
// wrong on the screen.
//
// AND EVERY ROOM OPENS, so this key never has to answer for one that would not:
// a place with nothing in it spends the frame saying what it is for (pages.go's
// [app.showPage] holds the law and the story of the three that used to refuse).
func (a *app) placeJumpKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	id, ok := placeDigit(key)
	if !ok && a.ctrlDigits() {
		// AND THE SECOND ENCODING, WHERE THE TERMINAL SAID IT HAS ONE. `ctrl+1`
		// is unreachable on a terminal with only the legacy encodings and is
		// exactly what a terminal that took the kitty protocol's disambiguation
		// flag delivers, so the alias is bound off that terminal's own reply and
		// never off a guess about which emulator this is (chords.go).
		id, ok = chordCtrlDigit(key)
	}
	if !ok {
		return nil, false
	}
	return a.showPage(id), true
}

// placeMapKey is the map (SCREEN 3b). The period is the one punctuation key with
// no meaning inside a word being typed as a chord, and it reads as "and what
// else is here".
const placeMapKey = "alt+."

// placeDigit is `alt+1`…`alt+7`: the place at that position in [pages].
//
// WHY alt IS THE ONE THIS PROGRAM PROMISES EVERYWHERE: `alt+1` arrives as
// esc-then-1 and has for forty years, while `ctrl+1` has no legacy encoding at
// all and most terminals drop it. It is bound as a SECOND spelling only where
// the terminal has answered that it disambiguates ([app.ctrlDigits]) — never as
// the first, and never on a terminal that has said nothing.
func placeDigit(key string) (page, bool) { return placeDigitAt(chordAltWord, key) }

// placeDigitAt is the digit reading behind both spellings, so a place can never
// be in one position under `alt+` and another under `ctrl+`.
func placeDigitAt(prefix, key string) (page, bool) {
	if !strings.HasPrefix(key, prefix) || len(key) != len(prefix)+1 {
		return 0, false
	}
	at := int(key[len(prefix)] - '1')
	all := pages()
	if at < 0 || at >= len(all) {
		return 0, false
	}
	return all[at], true
}

// placeAltLetter is `alt+<letter>` with the two spellings the composer already
// owns held back ([app.placeKey]'s note says why `b` and `f` may not be taken).
func placeAltLetter(key string) (rune, bool) {
	if !strings.HasPrefix(key, chordAltWord) || len(key) != 5 {
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
	pl := a.showing()
	return pl != nil && pl.alt(a, letter)
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
	pl := a.showing()
	return pl != nil && pl.window(a, key)
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
	pl := a.showing()
	if pl == nil {
		return nil
	}
	return pl.box(a)
}

// placeSend is `alt+enter` over a composer with something in it: THE COMPOSER
// LAYER OPENS, and a second press is what sends (composerlayer.go, SCREEN 2e).
//
// IT USED TO SEND ON THE FIRST PRESS, and what that cost is the whole reason the
// layer exists: a task left with three facts nobody had been shown — where it
// would run, what its work would run on, how much it could spend — and the only
// way to learn any of them was to watch what it did. The layer is the design's
// own answer, and it is a layer rather than a confirmation: the page behind
// dims, the box does not move, and `esc` puts you back exactly where you were.
//
// FROM A PLACE THAT IS NOT HOME IT STILL CARRIES YOU TO HOME, and that is a
// design decision rather than a shortcut. An errand's answer is drawn in home's
// own column ([app.showExchanges]); minting one from the spend place and leaving
// the person on the spend place would be work started somewhere they cannot
// watch it — the failure [app.askHere]'s own comment calls "the one failure
// worse than saying no". So the verb is in reach from every place, and the
// second press puts you where the answer will arrive.
func (a *app) placeSend() tea.Cmd {
	asked, opened := a.openComposerLayer()
	if opened {
		a.touch()
	}
	return asked
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
	cmd, started := a.placeTalkAbout(box.String())
	if started {
		box.reset()
	}
	return cmd
}

// placeTalkAbout is that door with the sentence handed IN rather than typed: a
// fresh conversation carrying one line of words, and the place left behind
// because what was asked for is now happening where a person can watch it.
//
// IT IS A SECOND CALLER AND NOT A SECOND DOOR. `enter` on a memory line is
// `ask me about it` (SCREEN 1f) and the line's own words are the message
// ([placeMemory.enter]); a place that started its own conversation would be a
// second answer to what starting one means. The bool is whether one was
// actually started, so a caller holding something it must only clear on the way
// out — the composer — can tell a refusal from a start.
func (a *app) placeTalkAbout(text string) (tea.Cmd, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	if !a.canStart() {
		a.pageMsg = newUnavailableWord
		return nil, false
	}
	a.leavePlace()
	a.standDownFullscreen()
	renewed := a.renew()
	return tea.Batch(renewed, a.submit(text)), true
}
