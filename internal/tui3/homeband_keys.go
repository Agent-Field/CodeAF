package tui3

func init() {
	registerHomeBand(homeBand{name: "keys", order: bandOrderKeys,
		kinds: []bandKind{bandKindSession, bandKindItem}, draw: drawKeysBand})
}

// drawKeysBand is the quiet legend at the foot of every actionable card.
func drawKeysBand(_ *app, ctx bandContext) []string {
	var words string
	switch ctx.subject.kind {
	case bandKindSession:
		words = "enter open · n new chat here · o open folder · y copy path · m more"
	case bandKindItem:
		words = "enter open where it was asked · p pause · s stop · m more"
	default:
		return nil
	}
	return []string{ctx.pal.dim(fit(words, ctx.width))}
}
