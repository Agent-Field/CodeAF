package tui3

// THE HEAD: ONE SET OF ROWS OVER EVERY FRAME.
//
//	 >● codeaf   home  teams  chats  sessions  spend  settings   2 want you · $1.20  thu 10:31pm
//	   ● harbor ▾   ◆ Manager ×   Refactor the rail… ×   +   ▦ All
//	───────────────────────────────────────────────────────────────────────────────────────────
//
// The wordmark with the places after it and the machine's pulse on the far
// end (topnav.go), then the strip of chats (chattabs.go), then the rule, then
// a blank. Four rows ([placeHeadRows]), THE SAME FOUR ON EVERY PAGE: a place,
// a conversation, a room inside one, the grid of open tabs. The places are on
// row zero ([navRow]) and the strip is on row one ([tabStripRow]) wherever
// either is drawn, so walking between pages moves nothing a person has found.
//
// THE PLACES AND THE STRIP USED TO SHARE ROW ONE AND TAKE TURNS ON IT. A place
// drew its bar there and a conversation drew its strip there, so the words a
// hand was reaching for changed under it on every walk, the open chats
// vanished the moment a person stepped onto a place, and the way home was a
// `home` piece on the strip that duplicated a word of the bar (owner,
// 2026-09-24). Each row now says one thing on every page: where you can go,
// and which chats this window has.
//
// THE TWO FRAMES SHARE ONE HEAD BECAUSE A PERSON WALKS BETWEEN THEM ALL DAY.
// The conversation's head used to be two to four rows of its own with no pulse,
// padded by two ladders of floors, so `esc` from home to a chat moved the strip
// up a row and took the machine's vital signs off the screen (DESIGN.md's law
// 11). One function draws the head for every frame.
//
// A ROOM WEARS THE WHOLE HEAD TOO, and its trail and facts are the first rows
// under it, where a place's heading is (PLACES-AUDIT.md, lane K).

// headRows is the head, drawn at `width` in `pal`, with `strip` (the frame's
// [app.tabsRow]) on its second row. It is always [placeHeadRows] rows; a frame
// that draws fewer, a terminal under the strip's floors, takes a prefix of it,
// so the rows a frame draws and the rows it charges are one count.
//
// THE STRIP IS HANDED IN RATHER THAN LAID OUT HERE because the conversation
// lays it out first to learn whether it has a head at all, and laying the
// strip out twice a frame would spend the scroll's allocation budget on a row
// that did not change (PERF.md's scroll law).
func (a *app) headRows(width int, strip string, pal palette) []string {
	// WHILE A TEAM IS SHOWN THE RULE IS DRAWN IN ITS COLOUR, so every frame
	// says the strip above it is narrowed (teams.go).
	ruleInk := pal.dim
	if sp, ok := a.teamActive(); ok {
		if ink := pal.teamInk(sp.HueSpec()); ink != nil {
			ruleInk = ink
		}
	}
	// THE NAV IS ON ROW ZERO AND THE POINTER IS TOLD SO HERE, by the one
	// function every frame's head goes through: a press arrives as a row of
	// the terminal, and the only honest way to know the nav is on it is to
	// record it where it was drawn ([app.navPress]).
	a.tabRow = navRow
	return []string{a.navLine(width, pal), strip, ruleInk(rule(width)), ""}
}
