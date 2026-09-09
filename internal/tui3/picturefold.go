package tui3

// pictureCompactRows keeps an unsolicited image smaller than a paragraph. Both
// sent attachments and image tools use this cap, independent of terminal width.
const pictureCompactRows = 3

// pictureRowBudget is shared by both transcript image doors. It bounds rendering
// before pixels are sampled, rather than cropping a completed picture.
func pictureRowBudget(open bool) int {
	if open {
		return pictureRowsMax
	}
	return pictureCompactRows
}

// pictureDoor exists on the same terminal rungs as its preview. Missing remote
// bytes do not remove the control; the mirror can arrive after the first paint.
func (a *app) pictureDoor(e *entry, width int) bool {
	return e.kind == entryUser && len(e.pictures) > 0 && a.pal.paintsPictures() && userBodyCols(width) >= pictureColsMin
}

func (a *app) pictureFoldLine(e *entry, width int) string {
	word := "expand images"
	if e.picturesOpen {
		word = "collapse images"
	}
	return a.pal.dim(fit(userLead+bandFoldMark(a.pal, !e.picturesOpen)+" "+word+railSep+"alt+i"+railSep+"low-resolution preview", width))
}

// togglePicturesAt invalidates both row caches because task pages retain their
// own assembled transcript independently of the conversation.
func (a *app) togglePicturesAt(i int) {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || !a.pictureDoor(&es[i], a.bodyWidth()) {
		return
	}
	e := &es[i]
	e.picturesOpen, e.stale = !e.picturesOpen, true
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// toggleVisiblePictures chooses the last visible attachment control, so the key
// acts on the conversation the person is reading rather than a hidden message.
func (a *app) toggleVisiblePictures() {
	for y := a.bodyTop() + a.viewHeight() - 1; y >= a.bodyTop(); y-- {
		if r, ok := a.rowAt(y); ok && r.hit == hitPictures {
			a.togglePicturesAt(r.entry)
			return
		}
	}
}
