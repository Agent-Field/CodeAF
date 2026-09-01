package tui3

// ── THE CARD'S RHYTHM, AND ITS THREE ROLES ──────────────────────────────────
//
// The right column of home is a stack of bands (homebands.go), and every one of
// them used to be separated from its neighbour by exactly one blank row. That is
// a rhythm with no information in it: the address under the title, the bill
// under the work and the verbs under the bill all stood the same distance apart,
// so a card of seven bands read as seven facts in a heap rather than as four
// readings of one conversation. The owner met it as "the right side preview —
// make the spacing a bit more wide… it seems small".
//
// SO THE CARD HAS GROUPS, AND THE GAP IS WHAT SAYS SO. Four readings, in the
// order somebody asks them:
//
//	identity    what this is called, and where it lives
//	activity    what it is stopped on, what it ran, what it made
//	economics   what it cost
//	verbs       what can be done with it
//
// ONE GAP LAW, APPLIED EVERYWHERE ON THE CARD. Bands inside a group stand
// [homeCardGap] apart; groups stand [homeCardGroupGap] apart. There is no third
// gap, and in particular there is no tighter one: two lines that want to be
// closer than a blank row are ONE BAND rather than two — which is exactly what
// the title and the address are, because the address is the title's second line
// and was never a fact in its own right.
//
// AND AIR IS THE FIRST THING GIVEN UP. A frame too short for the card at the
// group rhythm is drawn at the flat one before a single band is dropped, and
// only a frame too short for THAT loses bands from the bottom
// ([homeCardStack]). Whitespace is the cheapest thing on this card and a fact is
// the dearest, so they go in that order.

// homeCardGap is the blank rows between two bands of one group, and
// homeCardGroupGap the blank rows between two groups. They are the card's whole
// spacing vocabulary and they are written down once: a number spelled at each
// band is a number that drifts the day somebody adds a band.
const (
	homeCardGap      = 1
	homeCardGroupGap = 2
)

// homeCardPlaceRow is which row of a card the place line is: the SECOND, and
// never further down.
//
// The address is the title's own second line — identity is one band and not two
// — and a short frame drops bands from the bottom without ever touching the
// first, so neither the rhythm nor the room a card is given can move it. The
// rule is written down once here rather than as a `2` spelled at each of the
// tests that assert it, because a number spelled three times is a number that
// drifts.
const homeCardPlaceRow = 1

// cardGroup is which of the card's four readings a band belongs to. The order
// of the constants is the order they are read in, top to bottom.
type cardGroup uint8

const (
	cardGroupIdentity cardGroup = iota
	cardGroupActivity
	cardGroupEconomics
	cardGroupVerbs
)

// cardBand is one band's painted rows and the reading it belongs to.
type cardBand struct {
	rows  []string
	group cardGroup
}

// cardBandsOf files a run of painted bands under one group, dropping the empty
// ones on the way — the emptiness law, applied where the card is assembled
// rather than at each of the seven call sites.
func cardBandsOf(group cardGroup, bands ...[]string) []cardBand {
	out := make([]cardBand, 0, len(bands))
	for _, rows := range bands {
		if len(rows) > 0 {
			out = append(out, cardBand{rows: rows, group: group})
		}
	}
	return out
}

// homeCardStack assembles a card out of grouped bands, in the room it was given.
//
// IT GIVES UP AIR BEFORE IT GIVES UP A FACT, and it never truncates a band. The
// three steps are the whole law: the group rhythm if it fits, the flat rhythm if
// it does not, and then whole bands off the bottom until it does. The first band
// is not up for negotiation — a card that dropped its own title would be a
// preview that cannot say what it is previewing.
func homeCardStack(bands []cardBand, room int) []string {
	kept := make([]cardBand, 0, len(bands))
	for _, band := range bands {
		if len(band.rows) > 0 {
			kept = append(kept, band)
		}
	}
	if rows := layCardBands(kept, true); len(rows) <= room {
		return rows
	}
	for len(kept) > 1 {
		if rows := layCardBands(kept, false); len(rows) <= room {
			return rows
		}
		kept = kept[:len(kept)-1]
	}
	rows := layCardBands(kept, false)
	if len(rows) > room {
		rows = rows[:room]
	}
	return rows
}

// layCardBands puts the gaps in. `grouped` is the rhythm with the group breaks
// in it; without it every gap is [homeCardGap], which is the card exactly as it
// was drawn before the groups existed.
func layCardBands(bands []cardBand, grouped bool) []string {
	var out []string
	for at, band := range bands {
		if at > 0 {
			gap := homeCardGap
			if grouped && band.group != bands[at-1].group {
				gap = homeCardGroupGap
			}
			for i := 0; i < gap; i++ {
				out = append(out, "")
			}
		}
		out = append(out, band.rows...)
	}
	return out
}

// homeCardHeading is how the card says what is UNDER it — `it is stopped on
// you`, `work`, `made for you`.
//
// THE SIGNPOSTS ARE ONE ROLE ABOVE THE FACTS. Every word under the title used to
// be dim, the headings with them, so the only thing on the card the eye could
// find was the title and everything below it was one grey field. A heading is
// not a fact; it is the label on a group of them, and it wears [palette.muted] —
// one reading rung over the dim body and nowhere near the ink of the title,
// which stays the brightest text on the screen and the bridge to the highlighted
// row across the gutter.
//
// WEIGHT IS MADE OF COLOUR AND NEVER OF DECORATION. There is no bold under the
// title on this card, no underline anywhere on this surface, and no new hue: the
// three roles are the three rungs the ramp already has.
func homeCardHeading(word string, width int, pal palette) string {
	return pal.muted(fit(word, width))
}
