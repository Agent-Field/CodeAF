package tui3

// THE PROJECT CARD'S SECOND BAND: WHAT IS KEEPING AN EYE ON THIS PROJECT.
//
// A project is not only its conversations. It is also the things a
// conversation left behind that go on working after the window is closed
// (docs/AMBIENT.md) — a reminder, a watch, a rule, an overnight job — and on a
// card about the whole project those belong beside the chats rather than under
// a screen of their own.
//
// THE ROWS ARE THE LEFT COLUMN'S OWN ROWS AGAIN. [standGlyph] and
// [standRollup] are what home's item row is made of (homestanding.go), so an
// item wearing `▲ needs your look` on the left cannot wear a cadence on the
// right. The glyph and the rollup are one claim made twice and it is made in
// one place.
//
// AND IT READS THE CACHE, NEVER THE STORE. A band is drawn on every frame the
// cursor rests on a row (homebands.go's laws), and the store is a directory of
// documents; [app.readStandBands] already walks it once per reading of the
// world and leaves the answer on [homeView.items], keyed by the project's
// bucket. Asking [app.standItems] here would be a second walk of the same
// directory on every keystroke, for the same answer.
//
// It folds at [homeItemsShown] — the same three the left column's own band
// folds at, and behind the same words — and it ends with one dim line saying
// what all of them have done in the last week:
//
//	4 runs this week · $0.06

import (
	"strings"
)

func init() {
	registerHomeBand(homeBand{
		name:  "projectstanding",
		order: bandOrderNextUp,
		kinds: []bandKind{bandKindProject},
		draw:  drawProjectStandingBand,
	})
}

// projectItemsWord is the plural noun this band's fold line uses, and it is
// [homeItemsFoldWord]'s own words rather than a second spelling of them: the
// left column's door says `…2 more keeping an eye` and this one has to say the
// same, because they are two views of one band and a person who folded one goes
// looking for the other by its words.
var projectItemsWord = strings.TrimSpace(strings.TrimPrefix(homeItemsFoldWord, " more"))

func drawProjectStandingBand(a *app, ctx bandContext) []string {
	project, ok := bandProjectOf(ctx.subject)
	if !ok {
		return nil
	}
	views := a.home.items[project.Dir]
	if len(views) == 0 {
		// A PROJECT WITH NOTHING STANDING DRAWS NOTHING. The emptiness law, and
		// the same call the status line's segment makes: a band that
		// permanently read "nothing keeping an eye" would be a permanent
		// reminder of the absence of a thing.
		return nil
	}
	groups := make([][]string, 0, len(views))
	for _, view := range views {
		label := standGlyph(view.Item, view.Running, view.News, ctx.pal.ascii) +
			" " + strings.TrimSpace(view.Item.Words)
		groups = append(groups, projectCardRows(label, standRollup(view, ctx.now), ctx.width, ctx.pal))
	}
	rows := a.bandFoldPacked(ctx, "projectstanding", groups, homeItemsShown, projectItemsWord)
	// AND WHAT THEY HAVE ACTUALLY DONE THIS WEEK, under the list rather than on
	// the rows: it is one fact about the whole band ([standWeekFacts] sums the
	// ledger over these items' ids), and a per-row copy of it would push the
	// cadence off every line to say the same thing five times.
	//
	// It sits BELOW the fold line on purpose. The count is about every item this
	// project has, including the ones behind the fold, so a line drawn above it
	// would read as being about the three that are visible.
	if week := standWeekFacts(a.standWeek(ctx.now), standIDs(views), false); week != "" {
		rows = append(rows, ctx.pal.dim(fit(week, ctx.width)))
	}
	return rows
}

// standIDs is the ids of one band's items, which is what the ledger is summed
// over.
func standIDs(views []StandingItemView) []string {
	ids := make([]string, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.Item.ID)
	}
	return ids
}
