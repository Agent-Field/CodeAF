package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

func init() {
	registerHomeBand(homeBand{name: "nextup", order: bandOrderNextUp,
		kinds: []bandKind{bandKindSession, bandKindProject}, draw: drawNextUpBand})
}

func drawNextUpBand(a *app, ctx bandContext) []string {
	key := ctx.subject.dir
	if ctx.subject.kind == bandKindSession {
		for _, project := range ctx.subject.world.Projects {
			if project.Path == ctx.subject.row.Workspace {
				key = project.Dir
				break
			}
		}
	}
	views := append([]StandingItemView(nil), a.home.items[key]...)
	standByNextDue(views)
	var groups [][]string
	for _, view := range views {
		item := view.Item
		if item.Status != standing.StatusActive {
			continue
		}
		groups = append(groups, bandSidesWithSeparator(ctx.width, 2, standWordsFloor, "· ",
			"◦ "+strings.TrimSpace(item.Words), standWhenClause(item, ctx.now),
			ctx.pal.muted, ctx.pal.dim))
	}
	return a.bandFoldPacked(ctx, "nextup", groups, 2, "items")
}

// standByNextDue is the order both bands that list standing things read them in:
// SOONEST FIRST, with everything that has no appointment after everything that
// has one.
//
// It is one function because two bands say it — this one about a project, the
// machine's watchlist about everywhere (homeband_watchlist.go) — and two
// spellings of one order is two things to keep in step.
func standByNextDue(views []StandingItemView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].Item.NextDue, views[j].Item.NextDue
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return !a.IsZero() && a.Before(b)
	})
}

// standWhenClause is WHEN a standing thing will next be true, in one clause:
//
//	in 2h          it has an appointment and this is how far off it is
//	mon 8am        it has a cadence and no appointment anybody could work out
//	checked 4m ago it is looked at on a clock rather than being due
//
// THE APPOINTMENT OUTRANKS THE CADENCE, because "in 2h" is the thing a person
// is deciding with and "every weekday at 9" is how it got there. It is shared by
// the project band and the machine's watchlist for [standByNextDue]'s reason.
func standWhenClause(item standing.Item, now time.Time) string {
	if !item.NextDue.IsZero() {
		return "in " + nextUpAge(item.NextDue.Sub(now))
	}
	if item.When.Kind == standing.WhenProbe && !item.LastChecked.IsZero() {
		return "checked " + sinceAt(item.LastChecked, now) + " ago"
	}
	return strings.TrimSpace(item.When.Words)
}

func nextUpAge(d time.Duration) string {
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	if d < 24*time.Hour {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}
