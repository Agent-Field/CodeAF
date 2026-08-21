package tui3

func init() {
	registerHomeBand(homeBand{name: "keys", order: bandOrderKeys,
		kinds: []bandKind{bandKindSession, bandKindItem}, draw: drawKeysBand})
}

// drawKeysBand is the quiet legend at the foot of every actionable card.
//
// A LEGEND ONLY EVER NAMES KEYS THAT WORK. A conversation whose project folder
// has been deleted keeps `y` and `m` — copying a path is a string and folding a
// band is what is already on screen, and neither asks the disk for anything —
// and loses `enter`, `n` and `o`, which all want that folder: `enter` opens a
// conversation rooted in it, `n` starts one there, `o` hands it to the machine's
// file manager. All three refuse, and a card that advertised them would be
// spending its last line inviting three keystrokes it has already decided
// against. So the sentence takes their place, which is also the answer to "why
// are there only two keys here".
func drawKeysBand(a *app, ctx bandContext) []string {
	var clauses []string
	switch ctx.subject.kind {
	case bandKindSession:
		if a.homeGone(ctx.subject.dir) {
			clauses = []string{homeGoneWord, "y copy path", "m more"}
			break
		}
		clauses = []string{"enter open", "n new chat here", "o open folder", "y copy path", "m more"}
	case bandKindItem:
		clauses = []string{"enter open where it was asked", "p pause", "s stop", "m more"}
	default:
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, clauses...)
}
