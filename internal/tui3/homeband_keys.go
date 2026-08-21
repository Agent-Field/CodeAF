package tui3

func init() {
	registerHomeBand(homeBand{name: "keys", order: bandOrderKeys,
		kinds: []bandKind{bandKindSession, bandKindItem}, draw: drawKeysBand})
}

// drawKeysBand is the quiet legend at the foot of every actionable card.
func drawKeysBand(_ *app, ctx bandContext) []string {
	var clauses []string
	switch ctx.subject.kind {
	case bandKindSession:
		clauses = []string{"enter open", "n new chat here", "o open folder", "y copy path", "m more"}
	case bandKindItem:
		clauses = []string{"enter open where it was asked", "p pause", "s stop", "m more"}
	default:
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, clauses...)
}
