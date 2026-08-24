package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// /standing: WHAT STANDS OVER THIS CONVERSATION, IN THREE SHELVES.
//
// The ratification card (standing.go) is where a person says yes to one order.
// It is a good place to agree to something and a terrible place to remember
// having agreed to it: the card scrolls away, and what it left behind goes on
// working for a year in three different sizes — this conversation's, this
// project's, and every project's. This page is the other half of that sentence
// — WHAT IS ALREADY TRUE HERE, and let me take one back — which is a LIST, the
// shape this surface already knows how to draw:
//
//	  standing orders
//	  in this conversation
//	  ◦ keep the tests green                     checked 4m ago · nothing
//	  for this project
//	  ◦ never touch the public API                        Mondays at 9am
//	  ─ not here: draft the weekly update
//
// Five decisions, and four of them are borrowed rather than invented:
//
//   - IT IS THE OVERLAY GRAMMAR (palette.go), exactly as /permissions and
//     /connect are: a short list under the draft, ↑↓, esc to leave. A second
//     list with its own manners would be a second thing to learn for a question
//     of the same shape.
//   - THE SHELVES ARE THE THREE REACHES, in the order a person reads outward
//     from where they are standing: this conversation, this project,
//     everywhere. Each one is a heading and no more — the rows under it are what
//     the shelf is about, and a heading with a count beside it would be the
//     screen counting what a person can see.
//   - AN EMPTY SHELF DRAWS NOTHING AT ALL. Not the heading, not a "nothing
//     here", not a rule where the rows would have been. That is the emptiness
//     law at its most literal, and it is what makes this page one line long on
//     the ordinary conversation with one order over it.
//   - THE STATUS CLAUSE IS HOME'S OWN ([standRollup]). An item's tail — needs
//     your look, checking now, paused, the cadence, what the last look found —
//     is one derivation with one set of words, and a page that wrote a second
//     one would be two screens disagreeing about the same item in front of the
//     same person.
//   - THE VERBS ARE HOME'S VERBS PLUS ONE ([homeItemActions]). enter opens the
//     conversation that asked, p pauses, s stops — the three keys a person
//     already learned on the other screen — and `n` is this page's own: not
//     here, the exception made from the place rather than from the record.
//
// AND NOTHING ON IT SAYS `altitude`, `scope` OR `machine`. Those are this
// codebase's words for the three reaches; the person reads `in this
// conversation`, `for this project` and `everywhere`, which are the words the
// card asked them in.

// standRowsMax is how many LINES the page takes at most, its heading among
// them. It is two more than [permRowsMax] and the two are the shelves: this
// list is the only one on the surface whose rows are interleaved with headings
// it did not choose, so a ten-line ceiling would spend three of its lines on
// furniture and draw seven orders where the other panels draw nine.
const standRowsMax = 12

// The sentences this page says. Every one of them is quoted in
// internal/manual/chat/standing-orders.md exactly as it is spelled here.
const (
	// standHeading is the page's one sentence, drawn above the shelves.
	standHeading = "standing orders"
	// The three shelves, in the order they are drawn.
	standInHereWord     = "in this conversation"
	standProjectWord    = "for this project"
	standEverywhereWord = "everywhere"
	// standJustHereWord is the conversation reach as the RATIFICATION CARD says
	// it, and it is deliberately not [standInHereWord]. A shelf heading names
	// the place a person is standing and files rows under it; the card names how
	// far the thing they are about to agree to will reach, and "in this
	// conversation" on a card reads as where the order was said rather than as
	// the whole of what it will govern.
	standJustHereWord = "just this conversation"
	// standJustHereTag is the conversation reach as a ROW'S TAIL says it, and it
	// is a third spelling of one fact for the reason [standJustHereWord] is a
	// second. A shelf heading files rows under a place and a card names how far
	// something will reach; a tail is two or three cells at the end of a row in a
	// column twenty-eight wide (margin.go), where `just this conversation` is the
	// whole row and `in this conversation` is most of it. What is left is the
	// half that carries the meaning: just here.
	standJustHereTag = "just here"
	// standWhereTag is the card's third band label, in the grammar of the two
	// beside it ([standWhenTag], [standCostTag]): a lower-case noun and not a
	// heading, because a card in a conversation with a heading on every row is a
	// form.
	standWhereTag = "where · "
	// standNotHereWord is the exception, said in the same three words wherever
	// it appears: the dim line under a shelf, and the receipt for the key that
	// wrote it.
	standNotHereWord = "not here"
	// standNothingWord is /standing on a conversation nothing stands over. It is
	// SAID rather than dropped, for [filesNothingWord]'s reason — silence after
	// a deliberate command reads as a command that broke — and no page is opened
	// behind it, for that list's other reason: an overlay with no rows is a trap
	// that has to be dismissed before it can be told it was useless.
	standNothingWord = "nothing stands here yet — say what should always be true, and I'll hold it."
	// standHereWord is enter on the order this very conversation asked for.
	// There is a door and it leads exactly where the person already is, so the
	// page says the fact instead of moving them nowhere.
	standHereWord = "you are already in it"
	// standResumedWord is what a paused order that has been started again is
	// called, on this page's receipt and on the one line of news in the
	// transcript ([standUpdateWord] says the same word about the same event).
	standResumedWord = "going again"
	// standPageVerbs is the hint slot's line while the page is up. It is
	// [homeItemActions] — the three keys a person already learned on home — plus
	// the one key that is this page's own.
	standPageVerbs = homeItemActions + " · n " + standNotHereWord + " · esc"
)

// The mark a "not here" line leads with, and its stand-in on a terminal that
// cannot draw it. It is a RULE and not a glyph out of [standing.Item.Glyph]'s
// four: those four say what an order is doing, and this line is about an order
// that is doing nothing at all here — the shelf it would have been on, with the
// place struck through.
const (
	standNotHereGlyph = "─"
	standNotHereASCII = "-"
)

// standingHereAgent is the slice of the engine this page needs, and it is
// asserted rather than added to [Agent] — the standing side is OPTIONAL,
// exactly as the card's own contract is ([standingAgent] says why at more
// length). A scripted agent that has never heard of a standing order is a
// session with the ambient side off, and it must stay representable.
type standingHereAgent interface {
	// StandingHere answers what stands over this conversation — its own, its
	// project's and the machine's, in that order — and separately what the
	// person excepted from here (internal/session's standing_orders.go).
	StandingHere() (stand []standing.Item, excepted []standing.Item)
	// StandingExcept records that one order does not reach this place.
	StandingExcept(id string) error
	// StandingStandDown retires one order, recording that the person stopped it.
	StandingStandDown(id string) error
	// StandingPause pauses an active order or starts a paused one again, and
	// answers the status it now has.
	StandingPause(id string) (standing.Status, error)
}

// standingSeam is the engine under this surface, when it has one that can
// answer about standing orders.
func (a *app) standingSeam() (standingHereAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	agent, ok := a.agent.(standingHereAgent)
	return agent, ok
}

// ── the rows ────────────────────────────────────────────────────────────────

// standRowKind is what one line of the page is. A shelf heading and a "not
// here" line are drawn and never chosen; only an order is a row a verb can act
// on.
type standRowKind uint8

const (
	standRowShelf standRowKind = iota
	standRowItem
	standRowNotHere
)

// standRow is one line of the page: a shelf's heading, an order, or an order
// that deliberately does not reach here.
type standRow struct {
	kind standRowKind
	// shelf is the heading's words, on a heading row and nowhere else.
	shelf string
	// view is the order as a row needs it, on the other two kinds. It is
	// [StandingItemView] and not a bare item so that the clause this page draws
	// is the clause home draws, from the same input.
	view StandingItemView
}

// standShelfWord is one reach as its shelf is headed.
func standShelfWord(level standing.Altitude) string {
	switch level {
	case standing.AltitudeConversation:
		return standInHereWord
	case standing.AltitudeMachine:
		return standEverywhereWord
	}
	return standProjectWord
}

// standLevelWord is one reach as the RATIFICATION CARD names it. It is a second
// spelling of one fact on purpose; [standJustHereWord] says why.
func standLevelWord(level standing.Altitude) string {
	switch level {
	case standing.AltitudeConversation:
		return standJustHereWord
	case standing.AltitudeMachine:
		return standEverywhereWord
	}
	return standProjectWord
}

// standScopeTail is one reach as a ROW'S DIM TAIL says it, and NOTHING at the
// default.
//
// THE DEFAULT IS SILENT, which is the emptiness law applied to a fact rather
// than to a figure. An order said in a conversation governs the project
// ([standing.AltitudeProject] is the zero value and D6's default), so a tail
// saying so on every row would be a column printing what is already true of
// nearly everything on it. The two reaches that are NOT the default earn their
// word, because those are the ones a person would be surprised by.
//
// It lives here, beside the shelf headings and the card's own words, because
// these three spellings of the three reaches are one vocabulary and a fourth
// written somewhere else is how a surface ends up calling one thing two things.
func standScopeTail(level standing.Altitude) string {
	switch level {
	case standing.AltitudeMachine:
		return standEverywhereWord
	case standing.AltitudeConversation:
		return standJustHereTag
	}
	return ""
}

// standingRows reads the seam once and lays the page out: three shelves, in
// order, each one dropped whole when nothing is on it.
//
// THE SEAM'S ORDER IS KEPT INSIDE A SHELF. It answers recent first within each
// reach (internal/session's StandingHere), and a page that sorted again would
// be a second authority on the same order.
func (a *app) standingRows() []standRow {
	agent, ok := a.standingSeam()
	if !ok {
		return nil
	}
	stand, excepted := agent.StandingHere()
	var rows []standRow
	for _, level := range []standing.Altitude{
		standing.AltitudeConversation, standing.AltitudeProject, standing.AltitudeMachine,
	} {
		var shelf []standRow
		for _, item := range stand {
			if item.Level() != level {
				continue
			}
			mark, running := a.standRunning(item.ID)
			shelf = append(shelf, standRow{
				kind: standRowItem,
				view: StandingItemView{Item: item, Running: running, Mark: mark},
			})
		}
		for _, item := range excepted {
			if item.Level() != level {
				continue
			}
			// A PLACE AN ORDER DOES NOT REACH IS NOT A ROW, it is a footnote to
			// the shelf: nothing about it is running, nothing about it is due,
			// and the only fact worth a line is that it was excepted from here.
			shelf = append(shelf, standRow{kind: standRowNotHere, view: StandingItemView{Item: item}})
		}
		if len(shelf) == 0 {
			// THE EMPTINESS LAW REACHES THE WHOLE SHELF, heading and all.
			continue
		}
		rows = append(rows, standRow{kind: standRowShelf, shelf: standShelfWord(level)})
		rows = append(rows, shelf...)
	}
	return rows
}

// ── the page ────────────────────────────────────────────────────────────────

// standPage is the overlay's whole state. The zero value is closed.
type standPage struct {
	open bool
	rows []standRow
	// cursor is the row a verb acts on, and -1 when there is no such row. It
	// only ever rests on a [standRowItem]: a heading and a "not here" line are
	// things to read.
	cursor int
	top    int
	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the same bargain the other panels make
	// (permissions.go, connectpanel.go). A heading answers to no row.
	owner []int
}

func (p *standPage) close() { *p = standPage{} }

func (p *standPage) start(rows []standRow) {
	*p = standPage{open: true, rows: rows}
	p.cursor = p.settle(0)
}

// land puts the cursor on one order by id, and leaves it where it was when
// nothing on the page is that order — the page opened from the margin on a row
// another window has since stood down is still the page a person asked for.
func (p *standPage) land(id string) {
	if id == "" {
		return
	}
	for at, row := range p.rows {
		if row.kind == standRowItem && row.view.Item.ID == id {
			p.cursor = at
			p.follow(standRowsMax - 1)
			return
		}
	}
}

// adopt takes a re-read of the same page under the cursor, after a verb changed
// what stands. The cursor holds its PLACE rather than its row ([permPanel.adopt]
// states the law): the row it was on is usually the one that just went away,
// and the next thing a person wants to look at is whatever moved up into its
// position.
//
// AN EMPTIED PAGE STAYS OPEN, with its heading and nothing under it. That is
// [permPanel]'s own answer to the same moment, and it is the honest one here:
// the receipt for what was just stopped is in the conversation behind this
// list, and a page that closed itself out from under a person would look like
// the keystroke had done something else.
func (p *standPage) adopt(rows []standRow) {
	p.rows = rows
	p.cursor = p.settle(p.cursor)
	p.follow(standRowsMax - 1)
}

// settle is the nearest row a cursor may rest on, searching forward first
// because a row that went away is followed by whatever took its place.
func (p *standPage) settle(from int) int {
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
func (p *standPage) move(delta int) {
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
	p.follow(standRowsMax - 1)
}

func (p *standPage) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.rows), height)
}

// at resolves one line.
func (p *standPage) at(index int) (standRow, bool) {
	if index < 0 || index >= len(p.rows) {
		return standRow{}, false
	}
	return p.rows[index], true
}

// current is the order under the cursor, and false when there is none — a verb
// pressed on a page with nothing to act on must do nothing at all.
func (p *standPage) current() (standing.Item, bool) {
	row, ok := p.at(p.cursor)
	if !ok || row.kind != standRowItem {
		return standing.Item{}, false
	}
	return row.view.Item, true
}

// label is an order's own half of its row: the mark every aforge screen agrees
// on ([standing.Item.Glyph] is the authority), and what the order is called.
func (p *standPage) label(index int, pal palette) string {
	row, ok := p.at(index)
	if !ok || row.kind != standRowItem {
		return ""
	}
	glyph := standGlyph(row.view.Item, row.view.Running, false, pal.ascii)
	return glyph + " " + strings.TrimSpace(row.view.Item.Title())
}

// note is the dim tail: where the order stands, in [standRollup]'s words and
// never in a second set of them. A heading and a "not here" line have no tail
// at all, which is also what makes them one line at every width
// ([overlayItemLines] counts the tail).
//
// THE WORDS OUTRANK THE ROLLUP, exactly as they do on home's own row
// ([standFitNote] is that clip): the tail can be a whole sentence a run stopped
// on, and a row that spent all of a narrow frame on it would be a page of
// clauses with nothing to tell the orders apart. At [tierPhone] the tail has a
// line of its own and is left whole.
func (p *standPage) note(index, width int, now time.Time) string {
	row, ok := p.at(index)
	if !ok || row.kind != standRowItem {
		return ""
	}
	note := standRollup(row.view, now)
	if phoneList(width) {
		return note
	}
	return standFitNote(note, width)
}

// notHere is the footnote line under a shelf: one order that deliberately does
// not reach this place.
func (p *standPage) notHere(row standRow, pal palette) string {
	glyph := standNotHereGlyph
	if pal.ascii {
		glyph = standNotHereASCII
	}
	return glyph + " " + standNotHereWord + ": " + strings.TrimSpace(row.view.Item.Title())
}

// height is how many lines the overlay wants: the heading, plus whatever the
// rows take. An open page with nothing on it wants exactly ONE, which is the
// emptiness law drawn — the heading, and nothing under it.
func (p *standPage) height(width int, now time.Time) int {
	if !p.open {
		return 0
	}
	return 1 + overlayWindow(width, p.top, len(p.rows), standRowsMax-1,
		func(at int) string { return p.note(at, width, now) })
}

func (p *standPage) draw(width, n int, pal palette, hover int, now time.Time) []string {
	if n <= 0 || !p.open {
		return nil
	}
	fill := newOverlayFill(width, n, pal, hover)
	// THE HEADINGS ARE [overlayFill.plain] LINES, which is what makes them
	// unpressable without anything downstream having to know they are headings:
	// plain records the line as belonging to row -1, and [app.standPagePress]
	// already swallows -1.
	fill.plain(pal.dim(fit("  "+standHeading, width)))
	p.follow(overlayItems(n-1, width))
	for at := p.top; at < len(p.rows) && fill.room(); at++ {
		row := p.rows[at]
		var fitted bool
		switch row.kind {
		case standRowShelf:
			fitted = fill.plain(pal.dim(fit("  "+row.shelf, width)))
		case standRowNotHere:
			fitted = fill.plain(pal.dim(fit("  "+p.notHere(row, pal), width)))
		default:
			fitted = fill.add(at, p.label(at, pal), p.note(at, width, now), at == p.cursor, false)
		}
		if !fitted {
			break
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The heading is what makes the promise
	// breakable here: [standPage.height] counted it against the top the last
	// frame left behind, and the scroll above may have moved that top since.
	for len(lines) < n {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	p.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openStanding is /standing.
//
// The orders are read HERE and not held from boot, on the terms /permissions and
// /connect read their own rows: an order agreed to in another window five
// minutes ago is one this page has to know about, and asking costs one small
// read of a directory of documents.
func (a *app) openStanding() { a.openStandingAt("") }

// openStandingAt is /standing opened ON one order: the same page, with the
// cursor already standing where the person pressed.
//
// IT IS THE MARGIN'S DOOR (margin.go). A row in that column is a whole order's
// worth of thing to do — pause it, stop it, keep it out of here — and every one
// of those is a key on this page, so the row's press has to land on the row and
// not merely open a list for the person to find it again in. An id nothing on
// the page answers to leaves the cursor where [standPage.start] put it, which is
// the honest answer to an order that has just been stood down in another window.
func (a *app) openStandingAt(id string) {
	a.noticeEvent(eventStandingOpened)
	rows := a.standingRows()
	if len(rows) == 0 {
		a.note(standNothingWord)
		return
	}
	a.closeLists()
	a.dismissWelcome()
	a.standPage.start(rows)
	a.standPage.land(id)
	a.touch()
}

// standPageKey routes one keypress while the page owns the keyboard.
//
// The letters are bare rather than chords, which is what being modal buys: no
// draft is under this list for a letter to fall through into, so `p`, `s` and
// `n` can mean what they say (permissions.go's `d` is claimed on the same
// terms).
func (a *app) standPageKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.standPage
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		p.close()
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-(standRowsMax - 1))
	case "pgdown":
		p.move(standRowsMax - 1)
	case "enter":
		cmd = a.standPageEnter()
	case "p":
		a.standPageWrite(standPause)
	case "s":
		a.standPageWrite(standDown)
	case "n":
		a.standPageWrite(standExcept)
	}
	a.touch()
	return cmd
}

// standPagePress resolves a click on one of the page's rows.
//
// THE POINTER MOVES THE CURSOR AND NEVER ACTS, which is where this page parts
// company with the panel it borrows its shape from. /permissions asks before it
// drops, so a mis-aimed click there costs a second press; every verb here is a
// key, and enter takes a person out of the conversation they are sitting in — a
// click that did that would be a gesture nobody could aim.
func (a *app) standPagePress(y int) tea.Cmd {
	p := &a.standPage
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else closes it, which is what pressing outside a
		// modal list means everywhere on this surface.
		p.close()
		a.touch()
		return nil
	}
	at := -1
	if mark.index >= 0 && mark.index < len(p.owner) {
		at = p.owner[mark.index]
	}
	if at < 0 {
		// A heading, a "not here" line, or a blank under the last row: a line
		// belonging to no order. It is swallowed rather than resolved to
		// whichever row it happened to be nearest.
		return nil
	}
	p.cursor = at
	a.touch()
	return nil
}

// standPageEnter is the provenance door, and it is home's road walked from the
// other end ([app.homeItemEnter]): "why is this true here?" must open the
// conversation that made it, an order made at home that never became a
// conversation SAYS SO rather than offering a door onto nothing, and an order
// this very conversation asked for says that too.
func (a *app) standPageEnter() tea.Cmd {
	item, ok := a.standPage.current()
	if !ok {
		return nil
	}
	transcript := strings.TrimSpace(item.Origin.Transcript)
	switch {
	case transcript == "":
		a.note(homeItemNoDoor)
		return nil
	case transcript == a.file:
		a.standPage.close()
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
	a.standPage.close()
	return cmd
}

// standVerb is which of the three writes a key asked for.
type standVerb uint8

const (
	standPause standVerb = iota
	standDown
	standExcept
)

// standPageWrite is `p`, `s` and `n` on an order.
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
func (a *app) standPageWrite(verb standVerb) {
	p := &a.standPage
	item, ok := p.current()
	if !ok {
		return
	}
	agent, seam := a.standingSeam()
	if !seam {
		a.note(homeItemNoStore)
		return
	}
	var (
		err     error
		receipt string
	)
	switch verb {
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
		return
	}
	a.note(receipt + " · " + strings.TrimSpace(item.Title()))
	// AND THE COLUMN BEHIND THE PAGE IS TOLD, because it reads the same engine on
	// a three-second beat (margin.go's [app.marginStanding]) and a row still
	// standing there after it was stopped here would be one surface arguing with
	// the other in front of the person who stopped it.
	a.standRailAt = time.Time{}
	p.adopt(a.standingRows())
}
