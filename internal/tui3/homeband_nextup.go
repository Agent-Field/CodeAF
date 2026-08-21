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
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].Item.NextDue, views[j].Item.NextDue
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return !a.IsZero() && a.Before(b)
	})
	var rows []string
	for _, view := range views {
		item := view.Item
		if item.Status != standing.StatusActive {
			continue
		}
		when := strings.TrimSpace(item.When.Words)
		if !item.NextDue.IsZero() {
			when = "in " + nextUpAge(item.NextDue.Sub(ctx.now))
		} else if item.When.Kind == standing.WhenProbe && !item.LastChecked.IsZero() {
			when = "checked " + sinceAt(item.LastChecked, ctx.now) + " ago"
		}
		rows = append(rows, ctx.pal.muted(fit(joinDot("◦ "+strings.TrimSpace(item.Words), when), ctx.width)))
	}
	return a.bandFold(ctx, "nextup", rows, 2, "items")
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
