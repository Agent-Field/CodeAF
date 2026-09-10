package tui3

// THE HEAD: ONE SET OF ROWS OVER EVERY FRAME.
//
//	aforge                     2 want you · 1 moving · $0.14 / $20.00 · thu 9:49am
//	 Home   Parsing the logs   Porting the picker
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
// A ROOM KEEPS ITS TRAIL UNDER THE STRIP. Inside a node's page the trail and its
// facts close the head off themselves, so the conversation takes the head down
// to the strip and lays the room's own rows under it in place of the rule and
// the blank (chattabs.go's [app.headSealHeight]).

// headRows is the head, drawn at `width` in `pal`, with `middle` on its second
// row. It is always [placeHeadRows] rows; a frame that draws fewer — a room,
// which draws its trail where the rule would be — takes a prefix of it, so the
// rows a frame draws and the rows it charges are one count.
func (a *app) headRows(width int, middle string, pal palette) []string {
	// HOME'S LINE LEAVES ITS COUNTS TO THE PANELS UNDER IT, and every other
	// frame's carries them (pulse.go's [pulseBudget]).
	mode := pulseWhole
	if a.at(pageHome) {
		mode = pulseBudget
	}
	return []string{a.pulseLine(width, pal, mode), middle, pal.dim(rule(width)), ""}
}
