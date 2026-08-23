package tui3

// `keeping an eye on` — EVERY STANDING ORDER ON THE MACHINE, SOONEST FIRST.
//
// The per-project card has this band already ([drawNextUpBand]), asked of one
// project at a time. This is the same question asked of the machine, and it is
// the reason the machine card exists at all: a person sitting down wants to know
// what is still running on their behalf everywhere, not what is running in the
// folder their terminal happens to be in.
//
// TWO THINGS ARE DELIBERATELY NOT HERE. An order that is waiting on somebody is
// an ATTENTION row and belongs in `needs you` (docs/HOME-BRIDGE.md's left
// column) — the same thing drawn in two zones would be the screen asking twice
// for one decision. And a retired or paused order is not keeping an eye on
// anything, which is [app.standItems]'s own rule said again about the machine.

import "strings"

func init() {
	registerHomeBand(homeBand{
		name:  "watchlist",
		order: bandOrderWatchlist,
		kinds: []bandKind{bandKindMachine},
		draw:  drawWatchlistBand,
	})
}

// machineWatchWord is the band's heading, in the words the whole product uses
// for the thing ([homeItemsFoldWord] and /status's own line are the same three
// words). It is quoted in internal/manual/chat/home.md exactly as spelled here.
const machineWatchWord = "keeping an eye on"

// machineWatchShown is how many orders the band draws before the rest fold. Four
// is [homeShown]'s own number: this band sits at the top of a card and is the
// one a glance is for, so it gets a conversation list's allowance rather than
// the footnote allowance [homeItemsShown] gives the same rows under a project.
const machineWatchShown = 4

func drawWatchlistBand(a *app, ctx bandContext) []string {
	views := a.machineFactsAt(ctx.now).watching
	if len(views) == 0 {
		return nil
	}
	groups := make([][]string, 0, len(views))
	for _, view := range views {
		item := view.Item
		// THE MARK CARRIES THE STATE AND THE WORDS STAY CALM (homemachine.go's
		// [machineLeadInk]): the working hue on something in flight, the landed
		// hue on something with news, and the dim on everything simply waiting
		// for its time.
		glyph := standGlyph(item, view.Running, view.News, ctx.pal.ascii)
		lead := machineLeadInk(ctx.pal, glyph, machineGlyphInk(ctx.pal, view))
		// THE TITLE AND NOT THE WHOLE SENTENCE. A card thirty cells wide has room
		// for what a thing is called; [standing.Item.Title] is the store's own
		// answer to that and falls back to the person's words when nobody wrote
		// one, so this band can never invent a name.
		groups = append(groups, bandSidesWithSeparator(ctx.width, 2, standWordsFloor, "· ",
			glyph+" "+strings.TrimSpace(item.Title()), standWhenClause(item, ctx.now),
			lead, ctx.pal.dim))
	}
	rows := a.bandFoldPacked(ctx, "watchlist", groups, machineWatchShown, "keeping an eye")
	// THE HEADING IS STRUCTURE (homeband_news.go says the whole of why). The
	// coloured cells on this band are the marks — the state of each watch — and
	// the label over them says nothing about right now.
	return append([]string{ctx.pal.muted(fit(machineWatchWord, ctx.width))}, rows...)
}
