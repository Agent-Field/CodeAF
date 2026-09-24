package tui3

// THE HEAD: ONE SET OF ROWS OVER EVERY FRAME.
//
//	codeaf                     2 want you · 1 moving · $0.14 / $20.00 · thu 9:49am
//	 home   Parsing the logs   Porting the picker
//	──────────────────────────────────────────────────────────────────────────────
//
// The pulse, then the row that says where you are — the bar of places on a
// place, the strip of chats in a conversation — then the rule, then a blank.
// Four rows ([placeHeadRows]), the bar and the strip on the same one
// ([placeTabRow]).
//
// THE TWO FRAMES SHARE ONE HEAD BECAUSE A PERSON WALKS BETWEEN THEM ALL DAY.
// The conversation's head used to be two to four rows of its own with no pulse,
// padded by two ladders of floors, so `esc` from home to a chat moved the strip
// up a row and took the machine's vital signs off the screen — the one line
// that says two things are waiting on you went away exactly where you were
// least likely to go and look for them (DESIGN.md's law 11). One function now
// draws the head for both, and the middle row is the only thing a frame hands
// it.
//
// A ROOM WEARS THE WHOLE HEAD TOO, and its trail and facts are the first rows
// under it — the room's own heading, where a place's heading is. Inside a
// node's page they used to take the place of the rule and the blank, which put
// the rule a row lower in a room than in the conversation it opened from, and
// two rows lower in the roomy layout: walking into a task moved the one line a
// person's eye uses to find where the head ends (PLACES-AUDIT.md, lane K).

// headRows is the head, drawn at `width` in `pal`, with `middle` on its second
// row. It is always [placeHeadRows] rows; a frame that draws fewer — a terminal
// under the strip's floors, which draws none of it — takes a prefix of it, so
// the rows a frame draws and the rows it charges are one count.
func (a *app) headRows(width int, middle string, pal palette) []string {
	// HOME'S LINE LEAVES ITS COUNTS TO THE PANELS UNDER IT, and every other
	// frame's carries them (pulse.go's [pulseBudget]).
	mode := pulseWhole
	if a.at(pageHome) {
		mode = pulseBudget
	}
	return []string{a.pulseLine(width, pal, mode), middle, pal.dim(rule(width)), ""}
}
