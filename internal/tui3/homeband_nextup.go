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
// has one, and the rules that never wake at the very end.
//
// A HOLD IS NOT IN THE QUEUE AT ALL, and that is the third tier's whole reason.
// This band's question is "what happens next", and a rule has no next — it is
// simply true, and it will still be true after everything above it has gone off.
// Sorted among the appointments it would take the front of the band on the
// strength of having no appointment, which is soonest-first saying something
// that is not merely uninteresting but false. Behind them it reads as what it
// is: the standing conditions under the schedule. AMONG THEMSELVES THE NEWEST
// LEADS, because the one made this morning is the one somebody is still thinking
// about, and none of them has any other fact to sort on.
//
// It is one function because two bands say it — this one about a project, the
// machine's watchlist about everywhere (homeband_watchlist.go) — and two
// spellings of one order is two things to keep in step.
func standByNextDue(views []StandingItemView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].Item, views[j].Item
		holdA, holdB := a.When.Kind == standing.WhenHold, b.When.Kind == standing.WhenHold
		if holdA != holdB {
			return !holdA
		}
		if holdA {
			return a.Created.After(b.Created)
		}
		if a.NextDue.IsZero() != b.NextDue.IsZero() {
			return !a.NextDue.IsZero()
		}
		return !a.NextDue.IsZero() && a.NextDue.Before(b.NextDue)
	})
}

// standWhenClause is WHEN a standing thing will next be true, in one clause:
//
//	in 2h          it has an appointment and this is how far off it is
//	mon 8am        it has a cadence and no appointment anybody could work out
//	checked 4m ago it is looked at on a clock rather than being due
//	holds          it never wakes: it is a rule, and it is already true
//
// THE APPOINTMENT OUTRANKS THE CADENCE, because "in 2h" is the thing a person
// is deciding with and "every weekday at 9" is how it got there. It is shared by
// the project band and the machine's watchlist for [standByNextDue]'s reason.
func standWhenClause(item standing.Item, now time.Time) string {
	// AND A RULE IS ASKED BEFORE THE CLOCK IS. A hold has no NextDue and never
	// will, so every clause below it would fall through to whatever cadence words
	// it happens to carry — which is nothing, and a row with a blank tail says
	// less about a standing condition than the one word that is true of it.
	if item.When.Kind == standing.WhenHold {
		return standHoldsWord
	}
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
