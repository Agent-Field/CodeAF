package tui3

func init() {
	registerHomeBand(homeBand{name: "keys", order: bandOrderKeys,
		kinds: []bandKind{bandKindSession, bandKindItem}, draw: drawKeysBand})
}

// drawKeysBand is the quiet legend at the foot of every actionable card.
//
// A LEGEND ONLY EVER NAMES KEYS THAT WORK. A conversation whose project folder
// has been deleted keeps `ctrl+y` and `→` — copying a path is a string and
// folding a band is what is already on screen, and neither asks the disk for
// anything — and loses `enter`, `ctrl+t` and `ctrl+o`, which all want that
// folder: `enter` opens a conversation rooted in it, `ctrl+t` starts one
// there, `ctrl+o` hands it to the machine's file manager. All three refuse,
// and a card that advertised them would be spending its last line inviting
// three keystrokes it has already decided against. So the sentence takes their
// place, which is also the answer to "why are there only two keys here".
//
// AND EVERY KEY HERE IS A CHORD OR AN ARROW, never a bare letter: home's box
// takes every letter, always (home.go's [app.homeKey]), so a legend that named
// one would be advertising a keystroke the box is about to eat.
func drawKeysBand(a *app, ctx bandContext) []string {
	var clauses []string
	switch ctx.subject.kind {
	case bandKindSession:
		if a.homeGone(ctx.subject.dir) {
			clauses = []string{homeGoneWord, "ctrl+y copy path", "→ more"}
			break
		}
		aside := "ctrl+e put away"
		if ctx.subject.row.Archived {
			aside = "ctrl+e bring back"
		}
		clauses = []string{"enter open", "ctrl+t new chat here", "ctrl+o open folder", "ctrl+y copy path", aside, "→ more"}
	case bandKindItem:
		clauses = []string{"enter open where it was asked", "ctrl+e pause", "ctrl+x stop", "→ more"}
	default:
		return nil
	}
	// The legend is written in the hint grammar, so the keys step up into the
	// data hue and the verbs stay dim — a key–value pair the eye can split,
	// instead of one flat sentence (payload.go's [paintHint]).
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return bandClauses(ctx.width, 0, keys, clauses...)
}
