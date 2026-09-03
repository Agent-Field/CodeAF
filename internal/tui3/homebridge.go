package tui3

// THE BRIDGE: HOW WIDE HOME HAS TO BE BEFORE IT DRAWS A CARD.
//
// Everything this file arranges was built somewhere else. The list is
// switcher.go's flat ranked reading (place_home.go wires it), the pulse over the
// top is pulse.go's, the frame around all of it is the router's (pages.go), and
// the card on the right is the band registry's (homebands.go). What is here is
// the ARRANGEMENT — where each of them stands at which width, and the one cell
// on the whole page that is allowed to move (homespinner.go).
//
//	aforge                    2 want you · 4 moving · $0.55 today · tue 1:11pm
//	 home   tasks 1   standing   memory 2   spend   search   settings
//	───────────────────────────────────────────────────────────────────────────
//	 since you left · 3h
//	 a watch fired at 6am — nothing had changed, and it says so      standing
//	 20 chats · what wants you first                    alt+g group by project
//	 ? Swarm Task Splitting     aforge-v2   asks: add a --report-only mode?  2h
//	 ◐ Bounty Reward Companies  leadgen     2 tasks running · reading filings 3h
//	 ▸ 15 more, quiet since 6d
//	───────────────────────────────────────────────────────────────────────────
//
// ── THE LAWS ────────────────────────────────────────────────────────────────
//
//   - ONE COLUMN, AND A SECOND ONE ONLY WHEN THE WIDTH IS REALLY THERE. The
//     zones' column and the everyday card tier both went with the strips: at
//     every ordinary width the right-hand note on each row carries the one fact
//     the card was for, and a card drawn beside it was showing a person what
//     pressing enter shows a beat later (SCREEN 1a). Past [homeCardMin] the
//     width is genuinely spare, and there the card is not a preview but a thing
//     that ACTS — a question you can answer without opening anything (SCREEN
//     1d).
//
//   - THE LADDER IS ONE DECISION, TAKEN ONCE PER WIDTH. [homeTierAt] is the only
//     place a number is compared, and [app.homeFrame] settles it beside the
//     phone tier's own — before the column is built, because the shape decides
//     what the column HOLDS and a flag settled at the draw would build one shape
//     and paint another.
//
//   - NO BORDERS, AND THE COLUMNS ARE MADE OF ALIGNMENT. The gutter is the same
//     four cells the card has always been separated by ([homeGutter]), drawn on
//     every row whether or not the column to its left had anything to say, so
//     each column's edge is a straight line the eye can find. This surface draws
//     no rules but the two the frame already had.

// ── the ladder ──────────────────────────────────────────────────────────────

// homeTier is which of home's two shapes a frame is wide enough for. The
// phone's own inbox is a third shape and is decided by [layoutTier], which is
// the whole program's breakpoint table; this ladder is home's own, and the two
// are settled in the same breath ([app.homeFrame]).
type homeTier int

const (
	// homeTierList is home at every ordinary width: the switcher takes the whole
	// frame and there is no card at all (SCREEN 1a and 1c).
	homeTierList homeTier = iota
	// homeTierCard is the wide frame: the list keeps everything it wants and the
	// card stands beside it, acting rather than previewing (SCREEN 1d).
	homeTierCard
)

// homeCardCol is what a card needs to stay a card. Under it the bands start
// giving their clauses up to each other, and a card that cannot say a whole
// sentence is a preview nobody reads.
const homeCardCol = 36

// homeSwitchName is the longest conversation NAME this list undertakes to draw
// whole beside its facts. Seventy cells is a sentence of a title — far past what
// a session names itself and past what most people type — and the law it serves
// is rowfit.go's first: the identity is whole before any fact gets a cell, so
// what this number buys is that the drop ladder never has to reach the title.
const homeSwitchName = 70

// homeSwitchTail is what a row spends on everything BUT its name at that
// reading: the mark and the two cells after it, then a project tag and an age,
// each carrying the one space [switcherTailWidth] puts in front of a fact.
const homeSwitchTail = 1 + 2 + (1 + 18) + (1 + 3)

// homeSwitchFull is the width at which the flat list stops wanting cells — the
// baseline SCREEN 1a is drawn at, where a row can carry its mark, its name, its
// project tag and its age with none of them giving way ([switcherPaintRow] drops
// them in that order below it).
//
// THE NOTE IS NOT IN THAT LIST, AND THAT IS THE WHOLE OF THIS NUMBER. It was
// 120, which was this same reading WITH the row's note on it — the `asks: May I
// re-run the typecheck job…` clause — and that put [homeCardMin] at a hundred
// and sixty, so no ordinary window ever drew a card. The arithmetic was honest
// and the input was not: the note is THE FACT THE CARD EXISTS TO CARRY, and
// reserving the list's width for it meant the layout held cells back from the
// card in order to draw, in the list, the sentence the card would have drawn
// properly. A 120- and a 140-column window got neither.
//
// So the note is not what the list is measured at. It is ranked first out of the
// row ([switcherPaintRow]'s ladder drops it before the project tag and the age),
// which is the mechanism for this: past [homeCardMin] the note gives way and the
// card takes it up, and the name, the tag and the age are untouched at every
// width the tier exists at.
const homeSwitchFull = homeSwitchName + homeSwitchTail

// homeCardMin is the width at which a card appears beside the list — WHICH IS
// THE SUM OF THE TWO COLUMNS AND THE GUTTER, and not a number chosen next to
// them. A hundred and thirty-six cells is where the list has everything it asks
// for AND a card still fits; the tier begins exactly there because that is what
// the parts add up to, and it moves by itself the day one of them changes.
//
// THE CARD IS NOT PAID FOR OUT OF THE LIST. Below this the list would have to
// give up a fact its own ladder ranks above the note — the project tag, the age,
// or the name itself — to make room for a card, which is the layout arguing with
// itself. The NOTE is a different matter and is exactly what changes hands here:
// see [homeSwitchFull].
const homeCardMin = homeSwitchFull + homeGutter + homeCardCol

// homeCardCap is the widest the card is drawn.
//
// IT WAS FORTY-EIGHT AND THE CARD WAS DEMONSTRABLY CUT OFF THERE. On a
// two-hundred-column terminal the list was holding a hundred and forty-eight
// cells — twenty-eight more than it asks for ([homeSwitchFull]) — while the card
// beside it drew `→ verbs: put it away, new chat here, open folde…` and cut the
// sentence under a failed task mid-clause. A card that ends in an ellipsis over
// the thing it was about to offer is not a card with all the room it will ever
// ask for, whatever the number next to it said.
//
// THE GIVE-WAY RULE IS THE ARITHMETIC AND NOT THE CAP. The list keeps HALF of
// every cell past [homeCardMin] ([homeColumns]), so its share is
// `width/2 + 40` and is at [homeSwitchFull] or above at every width the tier
// exists at — by construction, at any cap. Raising this number therefore never
// takes a cell from the list; it only decides how many of the card's OWN half it
// is allowed to keep. Fifty-six is a comfortable measure for the clauses the
// card actually draws, it is reached at two hundred columns where the list still
// holds a hundred and forty-four, and past it the cells do more good in the list
// — which is the same argument [homeSwitchFull] makes from the other end.
const homeCardCap = 56

// homeTierAt is the whole ladder, and it is the ONE place a width is compared.
func homeTierAt(width int) homeTier {
	if width >= homeCardMin {
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

// wide reports that the frame holds a card beside the list.
func (h *homeView) wide() bool { return h.tier >= homeTierCard }

// ── where the line list starts ──────────────────────────────────────────────

// placesTop is the first row of the list A CURSOR MAY STAND ON, and
// [homeNoLine] on a list with no such row at all. It is where home opens
// ([homeView.openAt]) and it is the row `↑` walks off to reach the tab bar
// (pages.go's [app.barReach] asks the place for it as the first of its stops).
func (h *homeView) placesTop() int {
	for at := range h.lines {
		if h.lines[at].stop() {
			return at
		}
	}
	return homeNoLine
}

// ── opening ─────────────────────────────────────────────────────────────────

// openAt is where the cursor stands the moment home appears.
//
// HOME OPENS ON THE CONVERSATION THIS WINDOW IS HOLDING — the row esc drops
// back into — with the cursor visibly on it. It opened AT REST for a wave
// (nothing highlighted, the machine's card on the right), and the resting frame
// failed the first thing a person asks of any screen with a keyboard on it:
// where am I. So the selection is on screen from the first frame, and there is
// no state left in which it is not: `↑` off the top row now walks onto the TAB
// BAR (pages.go's [barCursor]) rather than onto no row at all.
//
// The row it lands on is this window's own conversation rather than the top of
// the list, because the top row can be another window's — a selection that
// opened on a refusal would make enter mean nothing on the first keystroke.
// A window whose conversation is not on the list (a memory-only session, an
// empty machine) falls to the first row a cursor may stand on, and to line zero
// when the list has no such row at all — which is the sentence saying the
// machine is empty, and is exactly where a person should be looking.
func (h *homeView) openAt(file string) {
	h.point(file)
	if _, ok := h.focusedLine(); ok {
		return
	}
	if at := h.placesTop(); at != homeNoLine {
		h.cursor = at
		return
	}
	h.cursor = h.clamp(0)
}

// ── the window ──────────────────────────────────────────────────────────────

// homeWindow settles which rows the column shows, and it is asked once per frame
// before anything is drawn.
func (a *app) homeWindow(room int) {
	h := &a.home
	h.top = listTop(h.cursor, h.top, len(h.lines), room)
}

// ── the body ────────────────────────────────────────────────────────────────

// homeLeft is everything to the left of the card: the one list home draws.
//
// It stays a function of its own because [app.homeBody] pads whatever comes back
// to `width` and hangs the card off it, and the shape of that contract is what
// let the zones' column exist for a wave and stop existing without the card, the
// gutter, the hit map or the pointer learning that it had.
func (a *app) homeLeft(width, room int, pal palette) []homeDrawn {
	return a.homeList(width, room, pal)
}
