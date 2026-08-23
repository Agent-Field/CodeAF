package tui3

// THE BRIDGE: HOME AS THREE COLUMNS.
//
// Everything this file arranges was built somewhere else. The zones are
// homeattention.go's, the machine's own card is homemachine.go's, the pulse over
// the top is pulse.go's and the strip under the foot is answerstrip.go's; the
// list in the middle is the one home has always drawn. What is here is the
// ARRANGEMENT — where each of them stands at which width, which one the cursor
// is in, and the one cell on the whole page that is allowed to move
// (homespinner.go).
//
//	aforge                                  on watch · 4 orders · $1.10 · fri 9:41
//	───────────────────────────────────────────────────────────────────────────────
//	 needs you                aforge                        keeping an eye on
//	 ▲ approve schema         ● odysseys wave 4    ⠹ 8m     ◦ tests sweep     in 2h
//	 moving                   › rename plan      yesterday  since you left
//	 ● port sweep    wisp     hax-sdk                       ◆ 2 tasks landed  aforge
//	                          ▲ schema migration      2h    today
//	                          ─ elsewhere ─                  3 chats · $1.10
//	───────────────────────────────────────────────────────────────────────────────
//
// ── THE LAWS ────────────────────────────────────────────────────────────────
//
//   - THE RIGHT COLUMN IS ALWAYS A CARD. The row under the cursor has its own
//     card there, and the cursor on NO row leaves the machine's ([app.homeDetail]
//     already answers both, and this file only widens the frame it answers in).
//     There is no state of this screen where the third column is a second list.
//
//   - HOME OPENS AT REST, so the first thing on the screen is that machine card
//     — the morning glance is the default view and not a place you navigate to
//     ([app.openHome]). The first ↓ or the first tab lands in `needs you`.
//
//   - ONE CURSOR, IN READING ORDER. The three columns are ONE line list drawn in
//     two places ([homeView.zoneSplit] says where it is cut), so ↑ and ↓ walk the
//     zones and then the places exactly as they walked the strips and then the
//     places one tier down, and every door, fold, digit and key that worked on a
//     zone row before this file existed works on it unchanged. tab is the
//     shortcut across ([homeView.tab]).
//
//   - THE LADDER IS ONE DECISION, TAKEN ONCE PER WIDTH. [homeTierAt] is the only
//     place a number is compared, and [app.homeFrame] settles it beside the phone
//     tier's own — before the column is built, because the shape decides what the
//     column HOLDS (the zones keep their labels over nothing at the two wider
//     tiers) and a flag settled at the draw would build one shape and paint
//     another.
//
//   - NO BORDERS, AND THE COLUMNS ARE MADE OF ALIGNMENT. The gutter is the same
//     four cells the card has always been separated by ([homeGutter]), drawn on
//     every row whether or not the column to its left had anything to say, so
//     each column's edge is a straight line the eye can find. This surface draws
//     no rules but the two it already had.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── the ladder ──────────────────────────────────────────────────────────────

// homeTier is which of home's three shapes a frame is wide enough for. The
// phone's own inbox is a fourth shape and is decided by [layoutTier], which is
// the whole program's breakpoint table; this ladder is home's own, and the two
// are settled in the same breath ([app.homeFrame]).
type homeTier int

const (
	// homeTierList is the narrow frame: the list takes the whole of it and there
	// is no card at all, which is the honest answer under [homeMinDetail].
	homeTierList homeTier = iota
	// homeTierCard is the everyday frame: the list and the card beside it, with
	// the zones as two strips over the list. It is what this screen was before
	// the bridge and it is unchanged by it.
	homeTierCard
	// homeTierColumns is the wide frame: the zones leave the list and take a
	// column of their own on the left, the places keep the middle, and the card
	// keeps the right.
	homeTierColumns
)

// The three columns at their floors — what each of them needs to be worth
// drawing at all.
const (
	// homeAttentionCol is the zones' own column, and it is the one that does not
	// grow. What it holds is a mark, a name and a place ([app.attentionRow]) — a
	// SUMMARY standing beside a list, whose rows are all short and all read at a
	// glance. Cells spent widening it would come out of the two columns whose
	// rows can use them.
	homeAttentionCol = 28
	// homePlacesCol is the floor under the middle column. It is wider than either
	// of its neighbours on purpose and at every width (see [homeThreeColumns]):
	// the places column holds every conversation on the machine, each row a name
	// and a note against the right edge, and a dashboard whose middle column is
	// the thinnest thing on it has been arranged backwards.
	homePlacesCol = 38
	// homeCardCol is what a card needs to stay a card. Under it the bands start
	// giving their clauses up to each other, and a card that cannot say a whole
	// sentence is a preview nobody reads.
	homeCardCol = 36
)

// homeMinColumns is the width at which the zones stop being strips over the list
// and become a column beside it — WHICH IS THE SUM OF THE THREE FLOORS AND THE
// TWO GUTTERS, and not a number chosen next to them. A hundred and ten cells is
// where all three first fit; the tier begins exactly there because that is what
// the parts add up to, and it moves by itself the day one of them changes.
const homeMinColumns = homeAttentionCol + homeGutter + homePlacesCol + homeGutter + homeCardCol

// homeListCap is the widest the places column is drawn at the two-pane tier.
// Past forty-six cells a conversation's name has all the room it will ever ask
// for, and the cells after that do more good in the card beside it.
const homeListCap = 46

// homeListFloor is the narrowest a list column is worth drawing beside anything.
const homeListFloor = 30

// homeTierAt is the whole ladder, and it is the ONE place a width is compared.
func homeTierAt(width int) homeTier {
	switch {
	case width >= homeMinColumns:
		return homeTierColumns
	case width >= homeMinDetail:
		return homeTierCard
	}
	return homeTierList
}

// homeTierNow is that decision about the frame this app is drawing into. It is
// asked when home OPENS as well as when it is drawn, because the tier is a
// property of the terminal — known before the first frame — and a flag left at
// its zero value until the draw would build the column one shape and hand a
// cursor to it in another.
func (a *app) homeTierNow() homeTier {
	width, _ := a.size()
	return homeTierAt(width)
}

// wide reports that the frame holds a card beside the list, which is also the
// width at which the zones keep their labels over nothing (homeattention.go's
// fourth law).
func (h *homeView) wide() bool { return h.tier >= homeTierCard }

// columns reports the three-column tier by WIDTH alone.
func (h *homeView) columns() bool { return h.tier == homeTierColumns }

// threeColumns reports the three-column tier as it is actually DRAWN: wide
// enough, and with something to put in the first column.
//
// A COLUMN WITH NOTHING IN IT HAS NOTHING TO SIT BESIDE — [app.homeBody]'s own
// law about the empty machine, said once more one floor up. The zones stand down
// entirely under a search and on a machine that has never held a conversation
// ([homeView.buildAttention]), and on those two frames the places column takes
// the room the zones would have had. THE CARD DOES NOT MOVE WHEN THAT HAPPENS:
// [homeColumns] measures the card off the width and never off what the zones
// found, so its edge is in the same place with the zones drawn and without them.
func (h *homeView) threeColumns() bool { return h.columns() && h.zoneSplit() > 0 }

// homeThreeColumns is the three-column split: the zones, the places, the card.
//
// THE FLOORS FIRST, AND THEN THE SURPLUS SPLIT BETWEEN THE TWO COLUMNS THAT CAN
// USE IT — the places and the card — with the odd cell going to the places. So a
// wider terminal widens both of the columns a person reads words in, neither of
// them starves the other, and PLACES IS THE WIDEST COLUMN AT EVERY WIDTH this
// tier is drawn at, which is the one thing the design asks of this arithmetic
// (docs/HOME-BRIDGE.md's width ladder).
//
// It is asked only at [homeTierColumns], where the frame is [homeMinColumns] or
// wider and the three floors are therefore already paid for.
func homeThreeColumns(width int) (zone, places, card int) {
	zone, places, card = homeAttentionCol, homePlacesCol, homeCardCol
	if extra := width - zone - 2*homeGutter - places - card; extra > 0 {
		card += extra / 2
		places += extra - extra/2
	}
	return zone, places, card
}

// homeLeftColumns is the same split read off the width [homeColumns] gave the
// two left columns together, which is what the body and the pointer both hold.
func homeLeftColumns(left int) (zone, places int) {
	return homeAttentionCol, left - homeAttentionCol - homeGutter
}

// ── where the line list is cut ──────────────────────────────────────────────

// zoneSplit is where the zones END and the places begin.
//
// IT IS FOUND AND NOT COUNTED. The zones are built first and the world after
// them ([homeView.buildFor]), so the zone lines are a prefix of the list — but
// how MANY there are is a question about two strips, a fold and whatever they
// gathered, and a number computed here would be a second answer to it that goes
// wrong the first time a zone learns a new kind of row.
func (h *homeView) zoneSplit() int {
	for at, line := range h.lines {
		if line.kind == homeAttentionZone || line.kind == homeAttentionMore || line.zone != nil {
			continue
		}
		return at
	}
	return len(h.lines)
}

// placesFrom is the first line of the places column.
//
// THE BLANK BETWEEN THE TWO IS NEITHER COLUMN'S. With the strips over the list a
// blank row is what separates them ([homeView.blank] puts one before the first
// project); side by side there is nothing to separate, and a column that began
// with an empty row would be spending its first line saying so.
func (h *homeView) placesFrom() int {
	at := h.zoneSplit()
	for at < len(h.lines) && h.lines[at].kind == homeBlank {
		at++
	}
	return at
}

// ── opening ─────────────────────────────────────────────────────────────────

// openAt is where the cursor stands the moment home appears.
//
// HOME OPENS AT REST (docs/HOME-BRIDGE.md). The morning glance is what this
// screen is FOR when nobody is pointing at anything yet, so it is the default
// view and not a state to navigate to: nothing is highlighted, the card on the
// right is the machine's own, and the first ↓ or the first tab lands in `needs
// you` ([homeView.move] and [homeView.tab] are the two ways in).
//
// AND ONLY WHERE THERE IS A CARD TO OPEN ONTO. Under [homeMinDetail] the frame
// has no second column at all, so a cursor on nothing would be a screen with
// nothing selected AND nothing said about it — the emptiness law arguing against
// itself. That frame opens on the conversation this window is in, which is where
// home has always opened and which is still the honest answer at that width.
func (h *homeView) openAt(file string) {
	if h.restable() && h.wide() {
		h.cursor = homeRest
		return
	}
	h.point(file)
}

// ── the window ──────────────────────────────────────────────────────────────

// homeWindow settles which rows each column shows, and it is asked once per
// frame before anything is drawn.
//
// TWO COLUMNS OF ONE LIST NEED TWO WINDOWS. [listTop] follows a cursor through a
// list of a known length; here there are two lengths and one cursor, and the
// column the cursor is NOT in must not be scrolled by it — a person reading down
// the places column would otherwise push `needs you` off the top of a column that
// has four rows in it.
func (a *app) homeWindow(room int) {
	h := &a.home
	if !h.threeColumns() {
		h.zoneTop = 0
		h.top = listTop(h.cursor, h.top, len(h.lines), room)
		return
	}
	split, from := h.zoneSplit(), h.placesFrom()
	// THE ZONES HANG FROM THE TOP UNLESS THE CURSOR IS IN THEM. `needs you` has no
	// cap of its own (homeattention.go says why), so a machine with a page of
	// stopped things can outgrow the frame — and then the column follows the
	// cursor exactly as the list does.
	h.zoneTop = 0
	if h.cursor >= 0 && h.cursor < split {
		h.zoneTop = listTop(h.cursor, h.zoneTop, split, room)
	}
	cursor := h.cursor
	if cursor < from {
		cursor = from
	}
	h.top = from + listTop(cursor-from, h.top-from, len(h.lines)-from, room)
}

// ── the body ────────────────────────────────────────────────────────────────

// homeLeft is everything to the left of the card: two columns at the columns
// tier, and the one list home has always drawn at every other width.
//
// IT RETURNS ROWS OF THE SAME SHAPE EITHER WAY, which is what keeps the card,
// the gutter, the hit map and the pointer from learning that this tier exists:
// [app.homeBody] pads whatever comes back to `width` and hangs the card off it,
// exactly as it did when there was one column here.
func (a *app) homeLeft(width, room int, pal palette) []homeDrawn {
	h := &a.home
	if !h.threeColumns() {
		return a.homeList(width, room, pal)
	}
	zone, places := homeLeftColumns(width)
	// THE TWO COLUMNS ARE DRAWN FROM THE ONE LINE LIST, each from its own end of
	// the cut, so a zone row is the same row it was as a strip — same kind, same
	// door, same card, same digits (homeattention.go's closing law).
	above := a.homeRows(h.zoneTop, h.zoneSplit(), zone, room, pal)
	beside := a.homeRows(h.top, len(h.lines), places, room, pal)
	drawn := make([]homeDrawn, 0, room)
	for i := 0; i < room; i++ {
		row := homeDrawn{hit: -1, pane: -1, zone: -1}
		if i < len(above) {
			row.text, row.zone = above[i].text, above[i].hit
		}
		if i < len(beside) {
			row.hit = beside[i].hit
			if beside[i].text != "" {
				// THE GUTTER IS PADDED ON EVERY ROW for [homeGutter]'s own reason:
				// the places column's edge is a straight line at a fixed cell, and
				// it is that straightness — and nothing drawn — that separates the
				// two columns.
				pad := zone - ansi.StringWidth(row.text)
				if pad < 0 {
					pad = 0
				}
				row.text += strings.Repeat(" ", pad+homeGutter) + beside[i].text
			}
		}
		drawn = append(drawn, row)
	}
	return drawn
}

// ── the pointer ─────────────────────────────────────────────────────────────

// homeZoneHit is which line of the zones' column the pointer is over, and false
// everywhere else on the frame.
//
// ONE SCREEN ROW NOW ANSWERS FOR TWO LINES — a zone row on the left of it and a
// places row across the gutter — so the hit map alone can no longer say what a
// press is aimed at, and the x is what tells them apart. THE GUTTER BELONGS TO
// THE COLUMN ON ITS RIGHT, which is the rule the card's own gutter already keeps
// ([app.homePane]).
func (a *app) homeZoneHit(x, y int) (int, bool) {
	h := &a.home
	if !h.threeColumns() || y < 0 || y >= len(h.zoneRows) {
		return 0, false
	}
	width, _ := a.size()
	left, right := homeColumns(width)
	if right <= 0 {
		return 0, false
	}
	if zone, _ := homeLeftColumns(left); x < 0 || x >= zone {
		return 0, false
	}
	at := h.zoneRows[y]
	if at < 0 || at >= len(h.lines) {
		return 0, false
	}
	return at, true
}

// ── tab ─────────────────────────────────────────────────────────────────────

// homeTabWord is the key the wide tier adds to the line under the foot. It names
// the ZONE and not the column, because that is the word the zones' own labels
// and the manual both use for the thing tab moves between.
const homeTabWord = "tab next zone"

// tabStops is where tab can land, in the order it walks them: the first row of
// each zone that has any, and then the top of the places column.
//
// THE ZONES ARE READ OFF THE TABLE and not named here, which is homeattention.go's
// own law holding one floor up: a third zone is a row in [homeZones] and a third
// stop on this line, with nothing to change here.
//
// A ZONE WITH NO ROWS IS NOT A STOP. Its label still draws — stable geography is
// the whole point of that law — but a label is not a thing a cursor may rest on
// ([homeLine.stop]), and tab that landed on one would be a key that appears to
// do nothing.
func (h *homeView) tabStops() []int {
	var stops []int
	for _, zone := range homeZones {
		for at, line := range h.lines {
			if attentionWordOf(line) == zone.word && line.stop() {
				stops = append(stops, at)
				break
			}
		}
	}
	for at := h.placesFrom(); at < len(h.lines); at++ {
		if h.lines[at].stop() {
			stops = append(stops, at)
			break
		}
	}
	return stops
}

// tab moves the cursor to the next zone, and round again from the last.
//
// FROM REST IT ENTERS THE FIRST ZONE, which is the same landing the first ↓
// makes: home opens on the machine's card with the cursor on nothing, and either
// key is a person saying "now show me what needs me" ([homeRest]).
func (h *homeView) tab() {
	stops := h.tabStops()
	if len(stops) == 0 {
		return
	}
	next := stops[0]
	if !h.resting() {
		here := h.cursorZone()
		for i, at := range stops {
			if attentionWordOf(h.lines[at]) == here {
				next = stops[(i+1)%len(stops)]
				break
			}
		}
	}
	h.cursor, h.picked = next, true
}

// homeTab is tab as the keyboard reads it, and it answers false when this frame
// has no zones to cycle — where the key keeps the one meaning it already had
// (homeexchange.go: it hands the keyboard to the errand in the pane).
//
// THE PANE IS THE LAST STOP AND ONLY WHEN IT CAN HOLD THE KEYBOARD. An errand is
// the one card on this screen with a box in it, and tab from its own row in the
// LIST has meant "I am talking to this one now" since before there was a third
// column; a card about a conversation has nothing to type into, so the cycle goes
// straight back to the first zone rather than pausing on a column that cannot
// answer. From a row in a zone tab stays the zone key and `enter` is the way in,
// which is the one it already was on that row ([app.homeEnter]).
func (a *app) homeTab() bool {
	h := &a.home
	if !h.columns() {
		return false
	}
	if ex := a.paneExchange(); ex != nil && !h.resting() && h.cursorZone() == "" {
		ex.focused = true
		return true
	}
	h.tab()
	return true
}

// crossColumns is `→` off a zone row and `←` off a places row at the columns
// tier: the arrow follows the geography. The zones stand to the LEFT of the
// places, so → from `needs you` lands in the list and ← from the list lands
// back in the zones — tab's circle, unrolled onto the two keys that already
// point the way. It reports whether it landed, so the caller can let the arrow
// keep its other meanings on a frame where there is nothing to cross to.
//
// THE SAME LIVE OBJECT IS PREFERRED. A conversation standing in `needs you`
// usually has its own row in the list, and the arrow lands there, so a person
// stepping across the gutter stays on the thing they were reading
// ([attentionSame] is the test, exactly as the rescan uses it). When the other
// column holds no view of it — a folded project, a row of another kind — the
// arrow lands on the first row a cursor may rest on, which is where tab lands.
func (h *homeView) crossColumns(rightward bool) bool {
	here, ok := h.focusedLine()
	if !ok {
		return false
	}
	from, to := 0, h.zoneSplit()
	if rightward {
		from, to = h.placesFrom(), len(h.lines)
	}
	land := -1
	for at := from; at < to; at++ {
		if !h.lines[at].stop() {
			continue
		}
		if land < 0 {
			land = at
		}
		if attentionSame(h.lines[at], here) {
			land = at
			break
		}
	}
	if land < 0 {
		return false
	}
	h.cursor, h.picked = land, true
	return true
}

// homeHintWithTab puts `tab next zone` on the line under the foot, and puts it
// BEFORE THE WAY OUT: every hint this screen draws ends with `esc`, because the
// way out is the last thing a person needs to be told and the first thing they
// look for.
func homeHintWithTab(hint string, tab bool) string {
	if !tab || strings.Contains(hint, homeTabWord) {
		return hint
	}
	if at := strings.LastIndex(hint, " · esc "); at >= 0 {
		return hint[:at] + " · " + homeTabWord + hint[at:]
	}
	return hint + " · " + homeTabWord
}

// homeTabbable reports that tab has somewhere to go on this frame — the columns
// tier, with more than one zone standing on it.
//
// AN ERRAND HOLDING THE KEYBOARD SAYS ITS OWN THING ABOUT TAB, which is that the
// key gives the keyboard back (homeexchange.go's [exchangeHint]). Two claims
// about one key on one line is a line nobody believes.
func (a *app) homeTabbable() bool {
	if ex := a.paneExchange(); ex != nil && ex.focused {
		return false
	}
	return a.home.columns() && len(a.home.tabStops()) > 1
}
