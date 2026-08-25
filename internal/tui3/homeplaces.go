package tui3

import "strings"

// ── TYPING OFFERS PLACES TOO (SCREEN 1g) ────────────────────────────────────
//
// The tab bar teaches and the keyboard is faster. Somebody who has learned that
// `standing` is a place should be able to type `sta` and go there, out of the
// same box they use to find a conversation — and nobody should have to be told
// the places exist twice.
//
// A PLACE RANKS FIRST WHEN THE WORDS MATCH, which in this column's drop-up means
// nearest the box: the matches are read UPWARD from the box the words were typed
// into ([homeView.buildWorld] states the law), so "first" is the row directly
// above `ask here`. It wears `▸` — the collapsed mark, because a place opens
// into more — and says `a place` in the right margin, so a row that is not a
// conversation never has to be guessed at.
//
// IT IS [homeRank]'s OWN MACHINERY AND NOT A SECOND RANKER. The scoring is the
// same prefix-and-word walk the conversations are scored with; what a place has
// instead of a title is one lowercase word, which is exactly the shape that walk
// is best at.

// homePlace is one of the seven places, offered because what was typed matches
// its name. It is appended outside the [homeRowKind] iota block for the reason
// the three lanes before it were: a constant in the middle of that block is a
// merge conflict with every lane that also added one.
const homePlace homeRowKind = 240

// placeRowWord is the right margin of an offered place, and it is the whole of
// what that margin has to say: WHAT KIND OF THING THIS ROW IS. A count beside it
// would be a second claim the row cannot keep — the places' counts are the tab
// bar's, and they are about what CHANGED rather than about what is in there.
const placeRowWord = "a place"

// placeMatches is which places what was typed offers, best first.
//
// THE QUERY IS THE WHOLE WORD OR THE HEAD OF IT, and nothing looser. A place is
// one lowercase word with no title to search inside, so "scattered letters"
// matching — which earns a conversation its place in the list — would offer
// `settings` for the letters of "set the timer" and put a room nobody asked for
// at the top of the results. A prefix is the honest reading of a name.
func placeMatches(query string) []page {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	// A QUERY WITH A SPACE IN IT IS A SENTENCE, NOT A NAME. "standing up a watch"
	// is somebody describing work, and offering them the standing place for the
	// first word would be the surface answering a question it was not asked.
	if strings.ContainsAny(query, " /~") {
		return nil
	}
	var found []page
	for _, id := range pages() {
		if strings.HasPrefix(id.word(), query) {
			found = append(found, id)
		}
	}
	return found
}

// placeLines is the offered places as rows of the column, in drop-up order: the
// best match LAST, so it lands nearest the box.
func (h *homeView) placeLines(query string) []homeLine {
	found := placeMatches(query)
	if len(found) == 0 {
		return nil
	}
	lines := make([]homeLine, 0, len(found))
	// EXACTLY THE MOCKUP'S ORDER, TURNED OVER. `pages` is the tab bar's order and
	// a prefix walk keeps it, so the earliest place in the bar is the best match;
	// the drop-up reads upward, so it is appended last.
	for i := len(found) - 1; i >= 0; i-- {
		lines = append(lines, homeLine{kind: homePlace, project: found[i].word()})
	}
	return lines
}

// placeOf is which place a row is offering, and whether it is offering one.
func placeOf(line homeLine) (page, bool) {
	if line.kind != homePlace {
		return 0, false
	}
	return parsePageWord(line.project)
}

// homePlaceRow draws one offered place: the collapsed mark, the word, and what
// kind of thing this is out at the right margin.
//
// THE MARK IS `▸` AND IT IS A DELIBERATE SECOND USE OF IT. Everywhere else on
// this surface `▸` means "there is more behind this line" — a folded project, a
// quiet tail, a band. A place is more behind a line, so the shape is doing the
// job it always did rather than being borrowed for a new one.
func (a *app) homePlaceRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	label := bandFoldGlyph + " " + line.project
	return overlayRow(label, placeRowWord, at == h.cursor, false, at == h.hover, width, pal)
}
