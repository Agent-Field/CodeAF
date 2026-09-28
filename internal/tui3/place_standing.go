package tui3

// THE STANDING PLACE: /standing, AND WHAT IT KEEPS BETWEEN FRAMES.
//
// The ratification card (standing.go) is where a person says yes to one order.
// It is a good place to agree to something and a terrible place to remember
// having agreed to it: the card scrolls away, and what it left behind goes on
// working for a year in three different sizes — this conversation's, this
// project's, and every project's. This place is the other half of that sentence
// — WHAT IS ALREADY TRUE, and let me take one back — which is a LIST, the shape
// this surface already knows how to draw.
//
// This file is the PLACE and only the place: one thin state struct, and the
// methods the frame asks of it. What is on the screen is standingplace.go's, a
// pure reading of data assembled here; what a person reads is spelled in
// placeprose.go; which of the three reaches each voice uses is standingpage.go's.
//
// TWO SEAMS ANSWER ONE PAGE, and both are asked once, on the open:
//
//   - the CONVERSATION's engine ([standingHereAgent]) knows what stands over
//     the chat a person is sitting in, and can pause one, stop one, or except
//     this place from one;
//   - the MACHINE's store ([StandingSeam], through home's bands) knows every
//     order on the computer, including the ones in workspaces nobody has ever
//     held a conversation in ([app.readBareBands]).
//
// A page that opened on one and gated on the other refused to open over a
// machine plainly holding orders, which is the fault this file was rebuilt to
// end.

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// standRowsMax is how far `pgup` and `pgdown` step on a page that has not been
// painted yet, and that is now its whole job.
//
// IT WAS THE PAGE'S CEILING and it is not one any more: the list was an overlay
// under the draft, twelve rows at most, and a place takes the whole terminal —
// so the window a cursor is followed within is what the last paint had room for
// ([standingPlace.visible]) and never a constant. What a constant is still honest for
// is the one moment there is no paint to ask: a page key pressed before the
// first frame, which cannot happen on the surface and can in a test. Twelve is
// the number the overlay used, kept so that gesture moves by the same amount it
// always did.
const standRowsMax = 12

// standingPlace is the standing place's whole state, and the ONLY state it keeps.
// The zero value is closed.
type standingPlace struct {
	// rows is the last reading, held between frames because NOTHING READS THE
	// DISK ON A DRAW: the walk of every project's documents happens on the open
	// and after a write, and a keystroke rebuilds lines from this slice.
	rows []standRow
	// cursor is the row a verb acts on, and -1 when there is no such row. It
	// only ever rests on a [standRowItem]: a heading and a "not here" line are
	// things to read.
	cursor int
	top    int
	// shown is how many rows the LAST PAINT had room for, and it is how far the
	// cursor is followed ([standingPlace.visible]). It is not [standingPlace.win], which
	// is the TIME window this place is listing.
	//
	// IT IS WRITTEN BY THE PAINT because this is a place: the body is however
	// many rows the frame had left after the pulse, the tab bar, the composer
	// and the hint, which is a fact about this terminal at this instant. A page
	// that scrolled by a constant would step a forty-row screen twelve rows at a
	// time and leave the rest of the frame blank under the cursor.
	shown int
	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the same bargain the other panels make
	// (permissions.go, connectpanel.go). A heading answers to no row.
	owner []int
	// hover is the SCREEN LINE OF THE BLOCK the pointer is resting on, and -1
	// for none. It is a line and not a row index because that is what the fill
	// this list is drawn with compares against ([overlayFill.addTinted]): an
	// order draws two lines at most widths, and either of them is the pointer
	// being on it.
	//
	// IT IS THE PLACE'S OWN MAP. The overlay this list used to be read the
	// pointer off the chrome's marks, and the chrome does not draw it any more —
	// so for one wave the hover answered -1 by declaration and a pointer
	// crossing the standing place lit nothing at all. It is written by pages.go's
	// [app.placeBodyHover] against the `owner` map the draw wrote, which is the
	// same bargain every other hit map on this surface strikes.
	hover int
	// win is WHEN IT FIRED — this place's time axis (screen 3d), drawn as the
	// control on the header row and moved by the four shift-arrows. It opens
	// holding every firing the machine has, so the first frame hides nothing and
	// narrowing is the person's own act ([standingOpenWindow]).
	win session.UsageWindow
	// held is whether this machine holds ANY standing order, in any window, and
	// it is what tells the two empty pages apart.
	//
	// A LIST EMPTIED BY THE WINDOW IS NOT AN EMPTY PLACE. Both draw no rows, and
	// the right answer to each is the opposite of the right answer to the other:
	// a machine with nothing standing on it wants the whole frame spent saying
	// what standing orders ARE ([standingTeach]), while a window paged past the
	// last firing wants the HEADER — the control that pages it back — above
	// nothing at all. A place that taught in both cases would have swallowed the
	// only way out of the second.
	held bool
}

// ── the interface the frame asks of a place ─────────────────────────────────

// open primes the place's caches and lays it out, once, on entry.
//
// A PLACE WITH NOTHING TO SHOW OPENS ANYWAY AND SAYS WHAT IT IS FOR. This used
// to be the opposite law — the reading was taken first and the frame was only
// taken once there was something to put in it, so a machine nothing stands on
// answered `alt+3` with one line and no page. That is exactly the machine a
// person is on for their first week, and the owner found it by running the
// binary on a fresh home: the tab was drawn, the key did nothing. SCREEN 1f'S
// PREAMBLE IS THE LAW: an almost-empty place is the best teacher on the machine,
// so the place opens on its heading and whisper instead ([placeWhisper]).
func (p *standingPlace) open(a *app) tea.Cmd {
	rows, win := a.standingPlaceReading()
	// WHETHER THE MACHINE HOLDS ANYTHING IS ASKED OF THE UNSCOPED PARTS, because
	// the window the reading measured holds every firing there is: rows can only
	// be empty above when there is nothing to put in them, and asking the parts
	// says so without depending on that being true.
	stand, excepted, elsewhere := a.standingPageParts()
	held := len(standingShelves(stand, excepted, elsewhere)) > 0
	*p = standingPlace{rows: rows, win: win, held: held, hover: -1}
	p.cursor = p.settle(0)
	a.closeLists()
	a.dismissWelcome()
	// AND THE CLOCK IS ARMED, because what this place is about goes on happening
	// while somebody is standing in front of it ([placeStanding.tick]).
	cmd := a.armPlaceClock()
	// THE BOARD IS TOLD HERE AND NOT AT A DOOR, because there are four doors —
	// `/standing`, `alt+3`, the tab bar, and the status row's own segment — and
	// the notice that retires on "you have seen this place" retired on exactly
	// one of them while this line sat in [app.openStandingAt].
	a.noticeEvent(eventStandingOpened)
	return cmd
}

// close writes the look stamp and forgets the reading. The stamp is what the
// tab bar's count is measured against, so it is written where a place is LEFT
// and never where one is entered (placecounts.go's [app.leavePage]).
func (p *standingPlace) close(a *app) {
	a.leavePage(pageStanding)
	*p = standingPlace{}
}

// body is the rows and the hit map, painted into exactly the room the frame
// reserved. It reads the cached rows and never a seam — that is the whole of
// "nothing reads the disk on a draw".
func (p *standingPlace) body(a *app, width, room int) []placeRow {
	if !p.held {
		// THE WHISPER IS THE WHOLE OF AN EMPTY PLACE, drawn instead of the
		// header row rather than under it: the header carries the time window
		// ([standingHeaderRow]), and a control naming a span of days on a machine
		// that has never held a standing order is a control about nothing. A list
		// emptied by the window keeps its header ([standingPlace.held] says why).
		p.top, p.shown, p.owner = 0, 0, nil
		return placeWhisperRows(pageStanding, width, room, a.pal)
	}
	lines, owner, top, shown := standingLines(
		p.rows, p.win, p.cursor, p.top, width, room, -1, a.pal, a.now())
	p.top, p.shown, p.owner = top, shown, owner
	rows := make([]placeRow, 0, room)
	for i, text := range lines {
		at := -1
		if i < len(owner) {
			at = owner[i]
		}
		rows = append(rows, placeRow{text: text, hit: at})
	}
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

// rowAt is which ORDER one screen line of the block belongs to. A heading, a
// "not here" line, a "last look" paragraph and the blank under the last row
// belong to none, and are swallowed rather than resolved to whichever row they
// happened to be nearest.
//
// It is the reading of the `owner` map and nothing else, because the arithmetic
// that turns a row of the TERMINAL into a line of a body is the same on every
// place and lives once (placemouse.go, pages.go's [app.placeBodyPress]).
func (p *standingPlace) rowAt(line int) (int, bool) {
	if line < 0 || line >= len(p.owner) || p.owner[line] < 0 {
		return 0, false
	}
	return p.owner[line], true
}

// stops is the cursor-legal rows of the last reading.
func (p *standingPlace) stops() []int { return standRowStops(p.rows) }

// enter is the provenance door, and it is home's road walked from the other end
// ([app.homeItemEnter]): "why is this true here?" must open the conversation
// that made it, an order made at home that never became a conversation SAYS SO
// rather than offering a door onto nothing, and an order this very conversation
// asked for says that too.
func (p *standingPlace) enter(a *app) tea.Cmd {
	item, ok := p.current()
	if !ok {
		return nil
	}
	// LEAVING IS THE ROUTER'S, on both roads out below ([app.leavePlace]): the
	// page closes, its look stamp is written, and the conversation this door
	// opened is underneath.
	transcript := strings.TrimSpace(item.Origin.Transcript)
	switch {
	case transcript == "":
		a.note(homeItemNoDoor)
		return nil
	case transcript == a.file:
		a.leavePlace()
		a.note(standHereWord)
		return nil
	}
	// The picker's own road, with the picker's own sentence for a refusal
	// (welcome.go): a conversation another window is holding, or a surface with
	// no way to open one, answers in words a person can act on.
	cmd, refusal := a.openSession(Session{File: transcript})
	if refusal != "" {
		a.note(refusal)
		return nil
	}
	a.leavePlace()
	return cmd
}

// verbs is the `→` strip for the row under the cursor.
//
// WHICH SHELF THE ROW IS ON DECIDES WHAT IT CAN BE ASKED TO DO. The three that
// reach this conversation go through the engine sitting under it, which knows
// what stands here and can except this place from one; an order in another
// project has none of that behind it and gets the two writes the store can make.
//
// AND A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. `not here` names a
// place an order in another project was never over, so it is not offered there
// at all; a window whose door wired no way to write ([StandingSeam.Save] is nil)
// offers nothing on that shelf rather than keys that would fail.
//
// THE LETTERS ARE THE ONES A PERSON ALREADY LEARNED. `p` pauses and `s` stops on
// home, on the three shelves and here; a shelf that spelled the same two verbs
// `r` and `x` would be one page teaching two keyboards.
func (p *standingPlace) verbs(a *app) []verb {
	row, ok := p.choice()
	if !ok {
		return nil
	}
	if row.elsewhere {
		if a.stands.Save == nil {
			return nil
		}
		item := row.view.Item
		word, status := homeItemPauseWord, standing.StatusPaused
		if item.Status == standing.StatusPaused {
			word, status = standResumeWord, standing.StatusActive
		}
		return []verb{
			{key: 'p', word: word, do: func() tea.Cmd { return p.write(a, item, status) }},
			{key: 's', word: homeItemStopWord, do: func() tea.Cmd { return p.write(a, item, standing.StatusRetired) }},
		}
	}
	return []verb{
		{key: 'p', word: homeItemPauseWord, do: func() tea.Cmd { return p.ask(a, standPause) }},
		{key: 's', word: homeItemStopWord, do: func() tea.Cmd { return p.ask(a, standDown) }},
		{key: 'n', word: standNotHereWord, do: func() tea.Cmd { return p.ask(a, standExcept) }},
	}
}

// rowID is the order under the cursor, named by the document's own id
// (pages.go's [place.rowID]). The window re-groups this list and the beat
// re-reads it, so a row's position here is the least durable thing about it.
func (p *standingPlace) rowID() string {
	row, ok := p.choice()
	if !ok {
		return ""
	}
	return row.view.Item.ID
}

// window is the four time keys of screen 3d, and this place's axis is WHEN IT
// FIRED.
//
// IT RE-GROUPS THE CACHED READING AND NEVER WALKS THE STORE AGAIN. The orders
// are already in memory — the open collected them — so moving the window is
// arithmetic over a slice, which is what lets a person hold the arrow down. A
// window key that reached the disk would be the law this place is built on
// broken by the one gesture most likely to repeat.
//
// AND A FRAME TOO NARROW TO DRAW THE CONTROL HAS NO WINDOW AT ALL. The same
// predicate answers the paint and the keys ([standingWindowRoom]), so the arrows
// are never bound where they are not drawn — a capability that cannot be seen is
// absent rather than silently working.
func (p *standingPlace) window(a *app, key string) bool {
	width, _ := a.size()
	if !standingWindowRoom(width, p.win) {
		return false
	}
	// AND THE ZOOM IS BOUND ONLY WHERE ITS OWN CLAUSE IS DRAWN. The header has
	// room for the arrows long before it has room for `shift+↑ coarser` beside
	// them, and the two halves of the control are gated separately for the reason
	// the whole predicate exists ([placeWindowFits]).
	if (key == "shift+up" || key == "shift+down") && !standingGrainRoom(width, p.win) {
		return false
	}
	next := placeWindowStep(p.win, key)
	if next == p.win {
		return false
	}
	p.win = next
	// THE CURSOR GOES BACK TO THE TOP because the list under it is a different
	// list: a window that dropped four rows would otherwise leave the cursor on
	// whatever slid into its line number, which is [app.refreshHome]'s own
	// warning about a list reordering under a cursor.
	p.rows = a.standingPageRows(p.win)
	p.top = 0
	p.cursor = p.settle(0)
	return true
}

// note is the line a place may say about what it is HOLDING, under the rule and
// above the composer. This place says nothing there: every fact it has is about
// one order, which is what the row and the strip are for, and a count of what a
// person can already see is the emptiness law broken from the other end.
//
// AND IT SAYS NOTHING OVER --host EITHER, WHICH IS NEW. Both halves of this page
// are the engine machine's now: what stands on THIS conversation always crossed
// the wire, and the walk of "what else keeps an eye on that machine" reads the
// far world ([app.readWorld]) and asks the far store about paths that are real
// there. There is no missing half left to apologise for.
func (p *standingPlace) note(a *app, width int) []string { return nil }

// hint is the line under the box: what enter does, what `→` reaches, and the way
// out.
//
// NOTHING IS NAMED THAT IS NOT BOUND, which is why it is derived from the row
// under the cursor rather than written once as a constant. The three verbs of
// the conversation's own shelves are not the two an order in another project
// has, and a line promising `not here` over a row that cannot make an exception
// would be this surface advertising a key that does nothing — the exact fault
// the verb strip was built to end (verbstrip.go's header).
func (p *standingPlace) hint(a *app) string {
	// AND A PAGE WITH NO ORDER UNDER THE CURSOR NAMES NO ROW KEY EITHER. On a
	// machine that has been asked to keep nothing true the body teaches what a
	// standing order is, and the foot under it went on saying `enter open where
	// it was asked` over a page with nothing to open — [standingPlace.enter]
	// already asks the same question of the same cursor and does nothing when
	// there is no order there, so the clause was the one half of that pair that
	// had not been told.
	if _, ok := p.choice(); !ok {
		return "esc"
	}
	verbs := p.verbs(a)
	words := make([]string, 0, len(verbs))
	for _, v := range verbs {
		words = append(words, v.word)
	}
	line := homeItemEnterWord
	if len(words) > 0 {
		// ONE KEY, ONE CLAUSE ([homeStripWord]). Joining the verbs with the same
		// `·` that separates the clauses made `stop` and `not here` read as verbs
		// with no keys of their own.
		line += " · " + homeStripWord(words...)
	}
	return line + " · esc"
}

// changed is the tab's count: how many orders on this machine have fired since
// the person last looked at this place.
func (p *standingPlace) changed(a *app, since time.Time) int {
	n := 0
	for _, view := range a.standingPlaceViews() {
		if !view.Item.LastFired.IsZero() && view.Item.LastFired.After(since) {
			n++
		}
	}
	return n
}

// ── the cursor ──────────────────────────────────────────────────────────────

// land puts the cursor on one order by id, and leaves it where it was when
// nothing on the page is that order — the page opened from the margin on a row
// another window has since stood down is still the page a person asked for.
func (p *standingPlace) land(id string) {
	if id == "" {
		return
	}
	for at, row := range p.rows {
		if row.kind == standRowItem && row.view.Item.ID == id {
			p.cursor = at
			p.follow()
			return
		}
	}
}

// adopt takes a re-read of the same page under the cursor, after a verb changed
// what stands. The cursor holds its PLACE rather than its row ([permPanel.adopt]
// states the law): the row it was on is usually the one that just went away, and
// the next thing a person wants to look at is whatever moved up into its
// position.
//
// AN EMPTIED PAGE STAYS OPEN, with its heading and nothing under it. That is
// [permPanel]'s own answer to the same moment, and it is the honest one here:
// the receipt for what was just stopped is in the conversation behind this list,
// and a page that closed itself out from under a person would look like the
// keystroke had done something else.
func (p *standingPlace) adopt(rows []standRow) {
	p.rows = rows
	p.cursor = p.settle(p.cursor)
	p.follow()
}

// settle is the nearest row a cursor may rest on, searching forward first
// because a row that went away is followed by whatever took its place.
func (p *standingPlace) settle(from int) int {
	if from < 0 {
		from = 0
	}
	for at := from; at < len(p.rows); at++ {
		if p.rows[at].kind == standRowItem {
			return at
		}
	}
	for at := min(from, len(p.rows)) - 1; at >= 0; at-- {
		if p.rows[at].kind == standRowItem {
			return at
		}
	}
	return -1
}

// move walks the orders and STEPS OVER everything else, which is what makes the
// headings furniture: a cursor that could rest on `for this project` would be a
// selection no verb on this page has anything to do with.
//
// It stops at the ends rather than wrapping, which is [moveCursor]'s own law and
// its reason: a cursor that reappeared at the far end would put a stop key under
// a hand that was walking away from one.
func (p *standingPlace) move(delta int) {
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	at := p.cursor
	for n := 0; n < delta; n++ {
		next := -1
		for i := at + step; i >= 0 && i < len(p.rows); i += step {
			if p.rows[i].kind == standRowItem {
				next = i
				break
			}
		}
		if next < 0 {
			break
		}
		at = next
	}
	p.cursor = at
	p.follow()
}

// follow scrolls the window by the least that keeps the cursor inside it, which
// is what makes every order reachable however many there are. The list used to
// stop at four rows behind a `▸ N more` line no key answered; a place takes the
// whole terminal, so there is nothing for a fold to save and nothing behind it a
// person could get to.
func (p *standingPlace) follow() {
	p.top = listTop(p.cursor, p.top, len(p.rows), p.visible())
}

// visible is how many rows the cursor is followed within, and how far a page key
// steps: what the last paint had room for, and [standRowsMax] before there has
// been a paint to ask.
//
// It is NOT called `window`, and the difference matters on this place more than
// anywhere: [standingPlace.window] is screen 3d's TIME window — which firings the
// page is listing — while this is how much of the list the terminal can show at
// once. Two senses of one word on one struct is how a keystroke ends up
// scrolling the calendar.
func (p *standingPlace) visible() int {
	if p.shown > 0 {
		return p.shown
	}
	return standRowsMax - 1
}

// at resolves one line.
func (p *standingPlace) at(index int) (standRow, bool) {
	if index < 0 || index >= len(p.rows) {
		return standRow{}, false
	}
	return p.rows[index], true
}

// choice is the ROW under the cursor, and false when the cursor is on nothing a
// verb can act on. It answers the row and not merely the order on it because
// which SHELF a row is filed under decides what may be done to it
// ([standRow.elsewhere]).
func (p *standingPlace) choice() (standRow, bool) {
	row, ok := p.at(p.cursor)
	if !ok || row.kind != standRowItem {
		return standRow{}, false
	}
	return row, true
}

// current is the order under the cursor, and false when there is none — a verb
// pressed on a page with nothing to act on must do nothing at all.
func (p *standingPlace) current() (standing.Item, bool) {
	row, ok := p.choice()
	return row.view.Item, ok
}

// press resolves a click on one of the page's rows to the order it names, and
// reports whether it landed on one; the place then enters it ([place.press]).
// Every verb here is still a key.
func (p *standingPlace) press(a *app, y int) bool {
	// A ROW OF THE TERMINAL BECOMES A ROW OF THE BODY BY SUBTRACTING THE HEAD,
	// and the head is one number for every place ([placeHeadRows]). It used to
	// resolve against the chrome's overlay marks, which is what an overlay had
	// and a place does not — a place is the whole frame, so there is no chrome
	// under it to ask.
	at, ok := p.rowAt(y - placeHeadRows)
	if !ok {
		// A heading, a "not here" line, a "last look" paragraph, or a blank under
		// the last row: a line belonging to no order. It is swallowed rather than
		// resolved to whichever row it happened to be nearest.
		return false
	}
	p.cursor = at
	a.touch()
	return true
}

// ── the writes ──────────────────────────────────────────────────────────────

// standVerb is which of the three engine writes a key asked for.
type standVerb uint8

const (
	standPause standVerb = iota
	standDown
	standExcept
)

// ask is `p`, `s` and `n` on an order that stands over this conversation.
//
// IT GOES THROUGH THE ENGINE AND THEN REDRAWS FROM THE ENGINE, which is
// [app.homeItemWrite]'s law: the row is not repainted from what this function
// wishes were true — the write is attempted, the page is read again, and what
// the person sees is what the store says. A row that showed `paused` over an
// engine that refused the write would be the screen lying about the machine.
//
// A REFUSAL IS SAID OUT LOUD, AND IN ITS OWN WORDS. What comes back from the
// seam is a sentence written for a person ("standing orders are not built yet"),
// so it is said as it stands rather than being wrapped in a second sentence
// about a key that did not work.
func (p *standingPlace) ask(a *app, which standVerb) tea.Cmd {
	row, ok := p.choice()
	if !ok {
		return nil
	}
	if row.elsewhere {
		// The strip does not offer these three there; this is the second lock on
		// the same door, and it SAYS SO rather than returning in silence.
		a.note(standNotOursWord)
		return nil
	}
	item := row.view.Item
	agent, seam := a.standingSeam()
	if !seam {
		a.note(homeItemNoStore)
		return nil
	}
	var (
		err     error
		receipt string
	)
	switch which {
	case standPause:
		var status standing.Status
		status, err = agent.StandingPause(item.ID)
		receipt = homeItemPaused
		if status == standing.StatusActive {
			receipt = standResumedWord
		}
	case standDown:
		err = agent.StandingStandDown(item.ID)
		receipt = homeItemStopped
	default:
		err = agent.StandingExcept(item.ID)
		receipt = standNotHereWord
	}
	if err != nil {
		a.note(err.Error())
		return nil
	}
	a.note(receipt + " · " + strings.TrimSpace(item.Title()))
	// AND THE COLUMN BEHIND THE PAGE IS TOLD, because it reads the same engine on
	// a three-second beat (margin.go's [app.marginStanding]) and a row still
	// standing there after it was stopped here would be one surface arguing with
	// the other in front of the person who stopped it.
	a.standRailAt = time.Time{}
	// THE PAGE IS REBUILT AND THE DISK IS NOT READ AGAIN. None of these three
	// writes can change what stands in another project, so the fourth shelf is
	// rebuilt from the bands the open already collected — and an order excepted
	// from here, which stops being a row and becomes a `not here` line, is still
	// accounted for and so still deduplicated out of that shelf.
	p.adopt(a.standingPageRows(p.win))
	return nil
}

// write is `p` or `s` on an order in another project, and it goes through the
// STORE because that is the only seam that has heard of it.
//
// IT REDRAWS FROM THE STORE, which is [app.homeItemWrite]'s law said again on
// this side: the write is attempted, the bands are read again, and what the
// person sees is what the disk says. A refusal is said in the store's own words,
// because that sentence was written for a person and wrapping it in a second one
// about a key that did not work would be the surface talking over the machine.
func (p *standingPlace) write(a *app, item standing.Item, status standing.Status) tea.Cmd {
	if a.stands.Save == nil {
		a.note(homeItemNoStore)
		return nil
	}
	item.Status = status
	// Changing the item is the person's answer to whatever its last firing
	// stopped on ([standing.Item.ClearNeedsPerson]).
	item = item.ClearNeedsPerson()
	if status == standing.StatusRetired {
		// THE DOCUMENT RECORDS WHY IN THE PERSON'S OWN TERMS, in the one spelling
		// every surface that stops an order uses ([homeStoppedWhy]).
		item.RetiredWhy = homeStoppedWhy
	}
	if err := a.stands.Save(item); err != nil {
		a.note(err.Error())
		return nil
	}
	receipt := homeItemPaused
	switch status {
	case standing.StatusActive:
		receipt = standResumedWord
	case standing.StatusRetired:
		receipt = homeItemStopped
	}
	a.note(receipt + " · " + strings.TrimSpace(item.Title()))
	a.readStandingElsewhere()
	// THE UNSCOPED READING IS TAKEN TOO, because stopping the last order is how a
	// machine gets back to holding none and the page has to notice ([standingPlace.held]).
	p.held = len(a.standingPageRows(session.UsageWindow{})) > 0
	p.adopt(a.standingPageRows(p.win))
	return nil
}

// ── the app's side: the doors, and the two seams ────────────────────────────

// openStanding is /standing.
func (a *app) openStanding() tea.Cmd { return a.openStandingAt("") }

// openStandingAt is /standing opened ON one order: the same place, with the
// cursor already standing where the person pressed.
//
// IT IS THE MARGIN'S DOOR (margin.go). A row in that column is a whole order's
// worth of thing to do — pause it, stop it, keep it out of here — and every one
// of those is a key on this page, so the row's press has to land on the row and
// not merely open a list for the person to find it again in. An id nothing on
// the page answers to leaves the cursor where the open put it, which is the
// honest answer to an order that has just been stood down in another window.
func (a *app) openStandingAt(id string) tea.Cmd {
	// IT GOES THROUGH THE ROUTER LIKE EVERY OTHER DOOR (pages.go's
	// [app.showPage]): what was standing is closed, its look stamp is written,
	// and this place opens. A door that raised the page itself would be a second
	// answer to "which place is up".
	cmd := a.showPage(pageStanding)
	// A CLOSED PAGE LANDS NOWHERE, which is what makes this two lines rather than
	// a condition: [standingPlace.land] walks the rows it has, and a page that did
	// not open has none.
	a.orders.land(id)
	a.touch()
	return cmd
}

// standingPlaceKey routes one keypress while this place owns the keyboard.
func (a *app) standingPlaceKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.orders
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		a.leavePlace()
		return nil
	// THE CURSOR WALKS THE ROWS THE BODY WAS PAINTED FROM, and there is one such
	// list ([app.standingPageRows]). Three arithmetics over three lists — one
	// clamping into the orders, one resolving against a shorter fold, one
	// drawing headings none of them counted — is three answers to "which row is
	// the cursor on", and the page will be wrong about at least two of them.
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-p.visible())
	case "pgdown":
		p.move(p.visible())
	case "enter":
		cmd = p.enter(a)
		// `p`, `s` AND `n` USED TO BE BARE LETTERS HERE, and the comment above this
		// function said exactly why they could be: "no draft is under this list for a
		// letter to fall through into". There is one now — this is a place, and a
		// place has a composer — so the three verbs moved onto the row's `→` strip,
		// where a letter is a verb only while the line naming it is on screen
		// (verbstrip.go). Every printable key belongs to the composer again, which
		// is the trade the promotion makes.
	}
	a.touch()
	return cmd
}

// standingPlaceReading is the whole of what opening this place decides: every
// order the machine holds, the span they fall in, and the shelves scoped by it.
//
// The window is measured BEFORE anything is scoped by it, over every order the
// machine holds, which is what makes the span it opens on the true one and the
// first frame's list complete.
//
// IT USED TO HAVE A SECOND CALLER — a `pageReady` the tab walk asked before
// turning any handle, so that `tab` could step past a room that would refuse.
// Nothing refuses now (pages.go's [app.showPage]), so the walk turns every
// handle and this is the open's own reading again.
func (a *app) standingPlaceReading() ([]standRow, session.UsageWindow) {
	a.readStandingElsewhere()
	stand, excepted, elsewhere := a.standingPageParts()
	win := standingOpenWindow(a.now(), stand, excepted, elsewhere)
	return standingShelvesIn(win, stand, excepted, elsewhere), win
}

// standingPageRows is the WHOLE page: what the conversation's engine says stands
// here, and what the machine's store says stands anywhere else.
//
// IT IS ONE FUNCTION AND EVERY OPENING AND EVERY REDRAW GOES THROUGH IT. The
// page is built on the open and rebuilt after each of the five writes, and the
// cursor's arithmetic is done against the list it produced; three callers each
// assembling the shelves in their own order is three answers to "which row is
// row four".
func (a *app) standingPageRows(win session.UsageWindow) []standRow {
	stand, excepted, elsewhere := a.standingPageParts()
	return standingShelvesIn(win, stand, excepted, elsewhere)
}

// standingPageParts is the three readings the page is laid out from, taken
// together: what the conversation's engine says stands here, what the person
// excepted from here, and what the machine's store holds anywhere.
//
// THEY ARE GATHERED ONCE AND HANDED ON, because the first of them reaches
// through the engine. A caller that wanted the window measured over everything
// AND the rows scoped by it would otherwise ask the engine twice for one frame.
func (a *app) standingPageParts() (stand, excepted, elsewhere []StandingItemView) {
	if agent, ok := a.standingSeam(); ok {
		here, out := agent.StandingHere()
		stand, excepted = a.standViews(here), a.standViews(out)
	}
	return stand, excepted, a.standingPlaceViews()
}

// standViews resolves the one fact a standing document does not hold — whether a
// pass has it in its hands at this instant — so that the reading downstream is
// plain data and the mark on a row and the clause beside it are one answer read
// once ([StandingItemView] states the law).
func (a *app) standViews(items []standing.Item) []StandingItemView {
	if len(items) == 0 {
		return nil
	}
	views := make([]StandingItemView, 0, len(items))
	for _, item := range items {
		mark, running := a.standRunning(item.ID)
		views = append(views, StandingItemView{Item: item, Running: running, Mark: mark})
	}
	return views
}

// readStandingElsewhere takes the machine-wide half of this place's reading, and
// it is taken on the OPEN and after a write, never on a draw.
//
// NOTHING READS THE DISK ON A DRAW. Walking every project's documents is what
// answers "what else stands on this machine", and it happens at the one moment a
// person asked for this screen; a cursor move, a resize and a repaint all read
// the slice it left behind.
//
// IT PRIMES HOME'S CACHE ON PURPOSE, AND IN HOME'S OWN ORDER.
// [app.readStandBands] reads each project's band out of [homeView.world], so a
// place that called it over whatever world some earlier screen happened to leave
// there would be listing the machine home last looked at rather than the one in
// front of the person. The world is read first and the bands second, which is
// the same pair in the same order [app.refreshHome] takes them in.
//
// AND A SURFACE WITH NO STORE ASKS NOTHING AT ALL. There is nothing to
// enumerate, so the disk is not touched, and the three shelves the conversation
// seam answers are the whole of the page.
func (a *app) readStandingElsewhere() {
	if a.stands.Items == nil {
		return
	}
	a.home.world, a.home.known = a.readWorldKnown()
	a.readStandBands()
}

// standingPlaceViews is every standing order this machine holds, in one list.
//
// IT IS BOTH KINDS OF BAND. [app.readStandBands] keys a band by the bucket of
// each project that has conversations in it, and [app.readBareBands] adds the
// OTHER kind — a workspace this machine holds orders for and no conversation at
// all — whose own comment says why leaving it out is a fault: "the person set a
// thing up and the screen that exists to show them what is true showed them
// nothing". So the keys of both are walked here, and a list that had only the
// first would be that same fault a second time.
//
// THE ORDER IS THE KEYS' OWN, SORTED. A map is walked in a different order every
// time Go feels like it, and a list of orders that reshuffled itself between two
// keystrokes would be a cursor moving on its own. Inside one band the order is
// home's triage ([standTriage]), which is the order the same rows have on the
// other screen.
func (a *app) standingPlaceViews() []StandingItemView {
	keys := make([]string, 0, len(a.home.items)+len(a.home.bare))
	for key := range a.home.items {
		keys = append(keys, key)
	}
	for _, bare := range a.home.bare {
		if _, ok := a.home.items[bare.project.Dir]; !ok {
			keys = append(keys, bare.project.Dir)
		}
	}
	sort.Strings(keys)
	var views []StandingItemView
	for _, key := range keys {
		views = append(views, a.home.items[key]...)
	}
	return views
}

// standingChangedSince is the tab bar's count, asked of the place that owns it.
func (a *app) standingChangedSince(seen time.Time) int {
	return a.orders.changed(a, seen)
}

// ── the place ───────────────────────────────────────────────────────────────

// placeStanding is this place's handle on the registry. Every method is
// [standingPlace]'s own, one line each: the state struct next door has carried this
// shape since before the interface existed (pages.go's [place] states the
// contract and why the handle holds no state itself).
type placeStanding struct{ placeBase }

func init() { registerPlace(placeStanding{}) }

func (placeStanding) id() page      { return pageStanding }
func (placeStanding) word() string  { return "standing" }
func (placeStanding) counted() bool { return true }

func (placeStanding) open(a *app) tea.Cmd { return a.orders.open(a) }
func (placeStanding) close(a *app)        { a.orders.close(a) }

// tick is the three-second beat, and THIS IS THE ONE PLACE ON THE SURFACE WHOSE
// WHOLE SUBJECT IS WHAT HAPPENS WHILE NOBODY IS LOOKING.
//
// It never turned the beat at all for a wave: the reading was taken on the open
// and rebuilt only after one of this place's own writes, so an order that fired,
// one made in the next terminal, one paused elsewhere, one whose person is
// wanted — none of them appeared while somebody stood here watching for exactly
// that. The relative ages moved, because they are measured at draw time from
// a.now(); the data behind them was frozen.
//
// It re-takes the reading the way [standingPlace.window] does — the rows, then
// the cursor settled onto the list they made — because a list rebuilt under a
// cursor that was not moved with it is a cursor standing on whatever slid into
// its line number.
func (placeStanding) tick(a *app, now time.Time) (bool, tea.Cmd) {
	p := &a.orders
	a.readStandingElsewhere()
	p.rows = a.standingPageRows(p.win)
	p.cursor = p.settle(p.cursor)
	return true, nil
}
func (placeStanding) body(a *app, width, room int) []placeRow {
	return a.orders.body(a, width, room)
}
func (placeStanding) stops(a *app) []int { return a.orders.stops() }

// cursorAt is the row of the shelves the cursor is on (pages.go's
// [place.cursorAt]).
func (placeStanding) cursorAt(a *app) int { return a.orders.cursor }
func (placeStanding) cursorRow(a *app, rows []placeRow) int {
	return placeRowAtLine(rows, a.orders.cursor)
}

// standingPlaceFrame is this place, drawn: the shared frame with this place's body
// in it, and the hit map cast back into the body lines this place answers with
// (pages.go's [app.placeDraw] and [placeLineHits]).
func (a *app) standingPlaceFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeStanding{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}
func (placeStanding) enter(a *app) tea.Cmd { return a.orders.enter(a) }
func (placeStanding) verbs(a *app) []verb  { return a.orders.verbs(a) }
func (placeStanding) rowID(a *app) string  { return a.orders.rowID() }
func (placeStanding) window(a *app, key string) (bool, tea.Cmd) {
	return a.orders.window(a, key), nil
}
func (placeStanding) note(a *app, width int) []string { return a.orders.note(a, width) }
func (placeStanding) about() string                   { return "the standing orders" }

func (placeStanding) hint(a *app) string                  { return a.orders.hint(a) }
func (placeStanding) changed(a *app, since time.Time) int { return a.orders.changed(a, since) }

// summary is what home's typed drop-up says is behind this place, and it is
// READ OFF HOME'S OWN CACHED BANDS rather than off the store. The bands are
// taken on home's three-second beat ([app.readStandBands]), so the clause costs
// a walk of a slice already in memory — which is the only kind of answer a row
// drawn while somebody is typing may be built from (pages.go's [place.summary]).
func (placeStanding) summary(a *app) string {
	return standingSummary(a.standingPlaceViews(), a.now())
}

func (placeStanding) press(a *app, y int) (tea.Cmd, bool) {
	if a.orders.press(a, y) {
		return a.orders.enter(a), true
	}
	return nil, true
}

// hover is A SCREEN LINE OF THE BLOCK and not a row index, because that is what
// the fill this list is drawn with compares against ([overlayFill.addTinted]) —
// a two-line row is hovered by either of its lines.
func (placeStanding) hover(a *app, y int) bool {
	next := -1
	if at := y - placeHeadRows; at >= 0 && at < len(a.orders.owner) && a.orders.owner[at] >= 0 {
		next = at
	}
	row := -1
	if next >= 0 {
		row = a.orders.owner[next]
	}
	return placeHoverMoved(&a.orders.hover, &a.orders.cursor, next, row, a)
}

func (placeStanding) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.orders.move(delta)
	a.touch()
	return nil, true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeStanding) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.standingPlaceKey(msg) }
