package tui3

func init() {
	registerHomeBand(homeBand{name: "gone", order: bandOrderGone,
		kinds: []bandKind{bandKindSession, bandKindProject}, draw: drawGoneBand})
}

// drawGoneBand is the card's half of the thing home knows BEFORE anything is
// pressed: this project's folder is not on the disk any more.
//
// IT SITS ABOVE THE STATE BAND, directly under the place line, which is the
// highest anything on this column goes. Everything below it — what the
// conversation is doing, the tasks it ran, the files it produced, what it cost —
// is an account of work you would go and continue, and this is the line that
// says you cannot. A frame too short for the whole card drops bands from the
// BOTTOM ([homeBands]), so being first is also the only way to be sure this one
// is drawn at all.
//
// IT IS DIM AND NOT ACCENTED. `open in another window` is the sentence this is
// modelled on, in the same voice for the same reason: it is a fact about a door,
// not a thing anybody has to do — and there is nothing on this card that
// pressing a key would fix.
func drawGoneBand(a *app, ctx bandContext) []string {
	// The subject's dir is a PATH on both kinds — a row's recorded project
	// directory, or the project's own — which is exactly the key the reading is
	// held under ([homeView.readGone]).
	if !a.homeGone(ctx.subject.dir) {
		return nil
	}
	// The path itself is NOT repeated here. The place line two rows above is
	// already the whole of it, and the card would be saying one directory twice
	// in three lines. The refusal on home's message line carries the path
	// because that line has no place line over it.
	return bandClauses(ctx.width, 0, ctx.pal.dim, homeGoneWord)
}
