package tui3

// The two overlays the router promoted to places, and what promotion costs.
//
// The standing list and the memory list were ≤12-row overlays drawn UNDER the
// draft, inside the conversation's chrome. As places they take the frame whole,
// which buys them the pulse, the tab bar, the composer and one key grammar — and
// costs them the two things being modal had bought:
//
//   - THEIR BARE LETTERS. `p` `s` `n` on standing and `u` `e` on memory were
//     bare because "no draft is under this list for a letter to fall through
//     into"; a place has a composer, so every letter belongs to it and the verbs
//     moved onto the `→` strip (verbstrip.go). Memory gains a real fix from
//     that: `u` was matched ahead of the filter, so the letter could not be
//     TYPED, and a search for a word containing a `u` lost it.
//   - `tab`. It cycled memory's scope; it is the way to the next place now, and
//     the scope moved to `alt+s`, which is the class a view belongs to.
//
// Both keep their bodies exactly as they drew them. What changed is the frame
// around them and the keyboard, which is the whole of what this wave claimed.

// placeHeadRows is how many rows every place spends before its body: the pulse,
// the tab bar, the rule, and the blank under it (pages.go's [placeFrame]).
//
// IT IS A CONSTANT AND THE POINTER DEPENDS ON IT. A press arrives as a row of
// the terminal and has to become a row of the body, and the only honest way to
// subtract the head is to have exactly one number for how tall the head is —
// which is also why the tab bar took the blank row home used to draw rather than
// being added under it. A fifth head row is a change to this constant and to
// nothing else.
const placeHeadRows = 4

// standPageFrame draws the standing place: the list it always drew, in the frame
// every place is drawn in.
func (a *app) standPageFrame(width, height int) ([]string, []int, int, int) {
	if !a.standPage.reading.now.IsZero() {
		return a.standingReadingFrame(width, height)
	}
	return placeFrame(a, width, height, -1, func(width, room int) []placeRow[int] {
		body := a.standPage.draw(width, room, a.pal, a.standPageHover(), a.now())
		rows := make([]placeRow[int], 0, room)
		for i, text := range body {
			at := -1
			if i < len(a.standPage.owner) {
				at = a.standPage.owner[i]
			}
			rows = append(rows, placeRow[int]{text: text, hit: at})
		}
		for len(rows) < room {
			rows = append(rows, placeRow[int]{text: "", hit: -1})
		}
		return rows
	})
}

// standPageHover is the row under the pointer, and it is -1 until the place
// grows a hover map of its own. The overlay read it off the chrome's marks, and
// the chrome does not draw this list any more; a hover resolved against a map
// nobody writes would be a highlight on whatever row it used to be.
func (a *app) standPageHover() int { return -1 }

// memoryFrame draws the memory place.
func (a *app) memoryFrame(width, height int) ([]string, []int, int, int) {
	return placeFrame(a, width, height, -1, func(width, room int) []placeRow[int] {
		body := a.memPanel.rows(width, room, a.pal, -1)
		rows := make([]placeRow[int], 0, room)
		for _, text := range body {
			rows = append(rows, placeRow[int]{text: text, hit: -1})
		}
		for len(rows) < room {
			rows = append(rows, placeRow[int]{text: "", hit: -1})
		}
		return rows
	})
}

// placeNote is the one line a place says about what it is holding, drawn under
// the rule and above the composer (pages.go's [placeFrame] states the law).
//
// It is where the counts, the filter lines and an open editor's label go — the
// facts that are about the WHOLE body rather than about the row under the
// cursor, and that would be a lie if they scrolled with it.
func (a *app) placeNote(width int) []string {
	pal := a.pal
	switch a.page {
	case pageTasks:
		if !a.taskSheet.open || a.taskSheet.detailOn {
			return nil
		}
		items := a.taskSheetItems()
		note := []string{" " + pal.dim(fit(a.taskSheetTally(items), width-2))}
		if a.taskSheetFiltering() {
			// WHAT WAS TYPED HAS TO BE ON SCREEN. A list that has lost rows for a
			// reason a reader cannot see is a list that has lost them for no
			// reason at all.
			note = append(note, " "+pal.dim(fit(a.taskSheetFilterLine(items), width-2)))
		}
		return note
	case pageSettings:
		if !a.sheet.open {
			return nil
		}
		switch {
		case a.sheet.sel != nil:
			return []string{" " + pal.dim(fit(a.sheet.sel.label, width-2))}
		case a.sheet.edit != nil:
			// THE LABEL IS THE NOTE AND THE VALUE IS THE COMPOSER. The panel used
			// to draw both on one line of its own foot; under the router the box a
			// person is typing in is THE composer, so what is left here is the one
			// thing the box cannot say — which setting this is.
			return []string{" " + pal.dim(fit(a.sheet.edit.label, width-2))}
		case a.sheet.msg != "":
			return []string{" " + pal.bad(fit(a.sheet.msg, width-2))}
		}
		return []string{" " + pal.dim(fit(a.sheet.footNote(), width-2))}
	case pageMemory:
		if a.memPanel.open && a.memPanel.footer != "" {
			return []string{" " + pal.dim(fit(a.memPanel.footer, width-2))}
		}
	}
	return nil
}
