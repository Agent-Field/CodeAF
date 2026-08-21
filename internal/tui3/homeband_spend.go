package tui3

func init() {
	registerHomeBand(homeBand{name: "spend", order: bandOrderSpend, draw: drawSpendBand})
}

// drawSpendBand preserves the session arithmetic already held by the world.
// The standing seam has no ledger reader, so it does not invent a weekly run
// count or reach around that seam into the store.
func drawSpendBand(_ *app, ctx bandContext) []string {
	facts := homeFacts(ctx.subject.row, ctx.now)
	if facts == "" {
		return nil
	}
	return []string{ctx.pal.dim(fit(facts, ctx.width))}
}
