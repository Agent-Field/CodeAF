package tui3

// ── THE FOLDERS A CONVERSATION IS ALSO ABOUT ────────────────────────────────
//
// The row says one name and counts the rest (homefolders.go); this is where the
// rest are named, and it is the only surface on home that lists them. The card
// is the right place for the whole set and the row is not, for the reason the
// column's second law states: NOTHING ON THIS COLUMN MAY GROW WITH THE DATA, so
// a conversation about nine folders draws three and a fold line, and a row is a
// fixed shape whatever it is about.
//
// IT SITS BESIDE THE REPOSITORY BAND ON PURPOSE. That one says where the folder
// the conversation STANDS IN has got to; this one says which other folders it
// turned out to be about. They are the two halves of "where is this work", and
// a person reading down the card meets them together.

func init() {
	registerHomeBand(homeBand{name: "folders", order: bandOrderFolders, draw: drawFoldersBand})
}

// homeFoldersShown is how many folders the band names before it folds. Three is
// the deliverables band's own allowance and the same judgement: enough that the
// ordinary card is complete, few enough that no card is mostly paths.
const homeFoldersShown = 3

func drawFoldersBand(a *app, ctx bandContext) []string {
	folders := homeFolders(ctx.subject.row)
	if len(folders) == 0 {
		return nil
	}
	groups := make([][]string, 0, len(folders))
	for _, folder := range folders {
		// THE BAND IS A PLACE, SO THE BAND IS A DOOR (pathlink.go), exactly as
		// the card's own place line is. The path is cut from the LEFT because
		// the basename at its end is what tells one folder from another.
		shown := fitLeft(shortPath(folder.Path, a.tilde, 0), ctx.width)
		groups = append(groups, []string{ctx.pal.dim(a.pathLink(folder.Path, shown))})
	}
	rows := a.bandFoldPacked(ctx, "folders", groups, homeFoldersShown, "folders")
	// THE CAPTION GOES OVER THE WHOLE BAND, in the row's own words, so that a
	// column of paths is never a list a person has to guess the heading of
	// (homeband_work.go makes the same move for the same reason).
	return append([]string{ctx.pal.dim(fit(homeAlsoWord, ctx.width))}, rows...)
}
