package tui3

import "strings"

func init() {
	registerHomeBand(homeBand{name: "spend", order: bandOrderSpend, draw: drawSpendBand})
}

// drawSpendBand is the dim arithmetic under a conversation's card:
//
//	spent $1.25 · 34k tokens · last active 12m
//
// IT COUNTS THE TALKING AND THE WORK THE TALKING STARTED, in one figure,
// because that is what a person means by "what did this conversation cost".
// [homeFacts] does the adding and states where each half is written down; this
// band only lays the clauses out and drops the whole line when there is not one
// true fact on it (the emptiness law).
func drawSpendBand(_ *app, ctx bandContext) []string {
	facts := homeFacts(ctx.subject.row, ctx.now)
	if facts == "" {
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, strings.Split(facts, " · ")...)
}
