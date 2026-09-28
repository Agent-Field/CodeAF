package tui3

// THE HEAD: ONE SET OF ROWS OVER EVERY FRAME.
//
//	 >● codeaf   home  teams  chats  sessions  spend  settings   2 want you · $1.20  thu 10:31pm
//
//	   ● harbor ▾   ◆ Manager ×   Refactor the rail… ×   +   ▦ All
//	───────────────────────────────────────────────────────────────────────────────────────────
//
// The wordmark with the places after it and the machine's pulse on the far
// end (topnav.go) is row zero on every page, and it never moves. Under it
// sits an air row ([navAirRow]): the breathing room that keeps the places'
// words from pressing on the strip and the rule. The strip of chats
// (chattabs.go) is the next row ONLY while a conversation is in front: a
// chat, a room inside one, the grid of open tabs, the run's work tab. On a
// place the strip is not drawn, so the head is the nav, its air row, the rule
// and a blank ([placeHeadRows]) and the body starts one row higher. In a chat
// the strip sits between the air row and the rule ([chatHeadRows]).
//
// THE STRIP USED TO BE DRAWN ON EVERY PAGE, including places, where it repeated
// the teams rail and home's sessions and offered a jump from a place to one
// chat that is not a journey anyone takes. `chats` on row zero, `alt+k` and
// home's sessions list are the ways between them (owner, 2026-09-25).//
// THE PLACES AND THE STRIP USED TO SHARE ROW ONE AND TAKE TURNS ON IT. A place
// drew its bar there and a conversation drew its strip there, so the words a
// hand was reaching for changed under it on every walk (owner, 2026-09-24).
// Row zero is the places on every page. The strip, when it is drawn, is only
// the chats.
//
// THE TWO FRAMES SHARE ONE HEAD BECAUSE A PERSON WALKS BETWEEN THEM ALL DAY.
// The conversation's head used to be two to four rows of its own with no pulse,
// padded by two ladders of floors, so `esc` from home to a chat moved the strip
// up a row and took the machine's vital signs off the screen (DESIGN.md's law
// 11). One function draws the head for every frame.
//
// A ROOM WEARS THE WHOLE HEAD TOO, and its trail and facts are the first rows
// under it, where a place's heading is (PLACES-AUDIT.md, lane K).

// headRows is the head, drawn at `width` in `pal`. While a conversation is in
// front, `strip` (the frame's [app.tabsRow]) is the row under the air row and
// the head is [chatHeadRows] rows. On a place `strip` is ignored and the head
// is [placeHeadRows]: the nav, its air row, the rule, a blank. A frame that
// draws fewer, a
// terminal under the strip's floors, takes a prefix of it, so the rows a frame
// draws and the rows it charges are one count.
//
// THE STRIP IS HANDED IN RATHER THAN LAID OUT HERE because the conversation
// lays it out first to learn whether it has a head at all, and laying the
// strip out twice a frame would spend the scroll's allocation budget on a row
// that did not change (PERF.md's scroll law). A place does not lay it out at
// all: an undrawn strip that still owned hit spans would take a click meant
// for the page.
func (a *app) headRows(width int, strip string, pal palette) []string {
	// WHILE A TEAM IS SHOWN THE RULE IS DRAWN IN ITS COLOUR, so every frame
	// says the strip above it is narrowed (teams.go). On a place there is no
	// strip, and the rule stays dim.
	ruleInk := pal.dim
	if a.stripInHead() {
		if sp, ok := a.teamActive(); ok {
			if ink := pal.teamInk(sp.HueSpec()); ink != nil {
				ruleInk = ink
			}
		}
	}
	// THE NAV IS ON ROW ZERO AND THE POINTER IS TOLD SO HERE, by the one
	// function every frame's head goes through: a press arrives as a row of
	// the terminal, and the only honest way to know the nav is on it is to
	// record it where it was drawn ([app.navPress]).
	a.tabRow = navRow
	nav := a.navLine(width, pal)
	line := ruleInk(rule(width))
	if !a.stripInHead() {
		a.chatTabHits = nil
		a.wall.chip, a.wall.door = hudSpan{}, hudSpan{}
		return []string{nav, "", line, ""}
	}
	return []string{nav, "", strip, line, ""}
}

// stripInHead reports whether this frame draws the chat strip. A conversation
// in front does, a room inside one does, and so do the grid and the work tab,
// which are the chats with something drawn over them. A place does not, and
// neither does the teams page's hosted pane: that pane is drawn as a chat so
// its rows answer, then the place's own head is put over it (teamspagehost.go),
// and a strip in the pane would be a second strip the place does not show.
func (a *app) stripInHead() bool {
	return !a.pageShowing() && !a.tp.forwarding
}
