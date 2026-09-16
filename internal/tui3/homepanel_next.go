package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// nextPanel is `next up`: the reminders and routines this machine will act on,
// soonest first and the rules that simply hold at the end — the order and the
// `in 20h` / `mon 8:30` / `holds` clause the project card's band always drew
// (homeband_nextup.go's [standByNextDue] and [standWhenClause]), now for the
// whole machine. Every row opens the standing place, where the orders are kept.
type nextPanel struct{ homePanelBase }

func (nextPanel) rows(in *homeGridInput) homePanelRows {
	views := nextActive(in)
	standByNextDue(views)
	shown := min(in.cap(panelNext), len(views))
	lines := make([]homeLine, 0, shown)
	for _, view := range views[:shown] {
		item := view.Item
		lines = append(lines, homeLine{kind: homeLedger, project: pageStanding.word(), dir: item.ID,
			view: view, item: item, cell: &homeCell{panel: panelNext,
				title: strings.TrimSpace(item.Words), right: standWhenClause(item, in.now),
				// WHAT IT FOUND, under the cursor. The row's right-hand clause is
				// the clock; this is what the order did the last time it woke
				// ([nextUpSaid]), and it is drawn only for the row being read
				// ([homeDescLines]).
				grows: in.desc, sub: nextUpSaidOn(in, view)}})
	}
	return homePanelRows{lines: lines, more: len(views) - shown}
}

// nextActive is every standing item that is still keeping its appointment, once
// each however many projects' bands hold it, in the world's project order so
// that two items due at the same moment always draw in the same order.
func nextActive(in *homeGridInput) []StandingItemView {
	var views []StandingItemView
	seen := map[string]bool{}
	for _, project := range in.world.Projects {
		for _, view := range in.items[project.Dir] {
			if view.Item.Status != standing.StatusActive || seen[view.Item.ID] {
				continue
			}
			seen[view.Item.ID] = true
			views = append(views, view)
		}
	}
	return views
}

// nextUpSaidOn is the sentence the description column draws for a `next up`
// row, and nothing on a frame with no such column.
func nextUpSaidOn(in *homeGridInput, view StandingItemView) string {
	if !in.desc {
		return ""
	}
	return nextUpSaid(view, in.now)
}

// nextUpSaid is THE FINDING, NOT THE CLOCK. The margin already says when the
// order next wakes, and the title is the person's own words, which usually
// carry the cadence too (`sweep the repo every morning at nine…`); so a
// description that said `every morning at nine` was the third copy of one fact
// on one row (owner, 2026-09-15: "these seem redundant"). What the row has NOT
// said is what happened the last time the order woke, and that is what a
// person deciding whether to open it wants: a watch's last finding — the line
// it found, or that it found nothing, which for a watch is the most common
// finding and a real one ([standRollup] says the same) — and a reminder's or
// routine's last outcome. An order stopped on a person says so first, and one
// in the middle of a pass says what the pass is doing, in the card's own words.
//
// THE EMPTINESS LAW REACHES IT: an order that has never woken has no finding
// and draws nothing, rather than a schedule the row has already said.
func nextUpSaid(view StandingItemView, now time.Time) string {
	item := view.Item
	switch {
	case item.NeedsPerson != "":
		return tierYourCallWord + tierReasonSep + item.NeedsPerson
	case view.Running:
		return standRunWord(view, now)
	}
	switch item.When.Kind {
	case standing.WhenHold:
		return ""
	case standing.WhenProbe, standing.WhenFile, standing.WhenIdle:
		if item.LastChecked.IsZero() {
			return ""
		}
		if found := switcherFirstLine(item.LastCheckLine); found != "" {
			return found
		}
		return nextUpFoundNothingWord
	}
	if item.LastFired.IsZero() {
		return ""
	}
	return switcherFirstLine(item.LastOutcome)
}

// nextUpFoundNothingWord is a watch that looked and found nothing, said as a
// sentence because it stands alone in the description column — the rollup's
// bare `nothing` reads as a gap there rather than as the finding it is.
const nextUpFoundNothingWord = "found nothing"
