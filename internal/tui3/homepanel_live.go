package tui3

// ── THE THREE LIVE PANELS' SHARED RULES ─────────────────────────────────────
//
// `needs you`, `running` and `since you left` are the panels whose rows come and
// go while a person watches (docs/design/home-mission-control/DESIGN.md §3
// P1–P3). They hold to the same two rules — a panel keeps four rows and folds
// the rest behind one door, and a row names what enter will do when that is not
// simply "open it" — so the rules are written once, here, and each panel's own
// file says only what it holds.

// homeLiveFoldAt is how many rows a live panel draws before `N more · <place>`
// (law 9: nothing grows). It is the ceiling at rest; a short terminal squeezes
// the panel further, in priority order ([squeezeColumn]).
const homeLiveFoldAt = 4

// homeLiveFold is a panel's lines cut at [homeLiveFoldAt], with the count of
// what the fold stands for.
func homeLiveFold(lines []homeLine) homePanelRows {
	out := homePanelRows{lines: lines}
	if len(lines) > homeLiveFoldAt {
		out.lines, out.more = lines[:homeLiveFoldAt], len(lines)-homeLiveFoldAt
	}
	return out
}

// homeLiveMargin puts a row's facts at its right: its clock, and — when enter
// will do something other than open it — THE DOOR WORD, which takes the margin
// and is never dropped ([homeCell.hold]) while the clock beside it gives way
// first. The folder is gone, the conversation is on its way here, another
// window is holding it (and, under the cursor, `enter brings it here`): the
// same words and the same order the list of conversations uses
// ([switcherMarginWord], homepanel_recent.go).
//
// `here` IS NOT A DOOR WORD ON THESE PANELS. It marks this window's own row in
// the list of conversations; on a panel about questions or work it would be a
// tag on rows in this window's own folder, which the ruling retired (DESIGN §1,
// "What is retired").
func homeLiveMargin(cell *homeCell, row switcherRow, clock string) {
	cell.right = clock
	word := switcherMarginWord(row)
	if word == row.age || word == homeHereWord {
		return
	}
	cell.tag, cell.right, cell.hold = clock, word, true
	if row.door && word == homeHeldShort {
		cell.door = takeoverHeldDoorWord
	}
}

// cellKey is which of a conversation's pieces of work a grid row names, and ""
// for every row that stands for the conversation as a whole ([homeCell.key]).
func (l homeLine) cellKey() string {
	if l.cell == nil {
		return ""
	}
	return l.cell.key
}
