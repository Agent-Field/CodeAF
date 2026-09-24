package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// nextPanel is `standing`: the reminders and routines this machine will act on,
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
		// A ROW IS THE PERSON'S OWN WORDS AND NOTHING AT ITS RIGHT (owner,
		// 2026-09-15). The clock used to stand at the margin — `in 2h`, `in
		// now`, `holds` — beside a title that usually said the schedule too;
		// the row's time is said ONCE, in its description, in the one form its
		// kind of order has ([nextUpSaid]), under the cursor.
		//
		// AND THE DESCRIPTION IS THERE AT EVERY WIDTH. With nothing at the
		// right, the sentence is the only place the row says what kind of order
		// it is and when it acts; a frame too narrow for the description column
		// draws it as the second line under the cursor's row, the way a `where
		// you were` row's description is drawn there, rather than showing a bare
		// title with no time on it at all (review of #1046).
		lines = append(lines, homeLine{kind: homeLedger, project: pageStanding.word(), dir: item.ID,
			view: view, item: item, cell: &homeCell{panel: panelNext,
				title: strings.TrimSpace(item.Words),
				grows: true, sub: nextUpSaid(view, in.now)}})
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
			// AN ORDER STOPPED ON A PERSON IS NOT COMING UP, IT IS STOPPED. It is
			// a row of `needs you` already, with its question on it, and a second
			// row here would say one thing twice across the columns (owner,
			// 2026-09-15). It comes back the moment the person answers.
			if view.Item.Status != standing.StatusActive || view.Item.NeedsPerson != "" || seen[view.Item.ID] {
				continue
			}
			seen[view.Item.ID] = true
			views = append(views, view)
		}
	}
	return views
}

// nextUpSaid is ONE SENTENCE PER KIND OF ORDER, and the only place a row says
// its time (owner, 2026-09-15: "communicate time information clearly and in a
// standardized way appropriate for each type — one way for each"). The row's
// title is the person's own words; this is the machine's account of the same
// order, in a fixed shape a person learns once:
//
//	reminder · goes off tomorrow 9:00am
//	routine · every morning at nine · next tomorrow 9:00am · last: done, two branches landed
//	watch · every five minutes · last looked 3m ago · found: the last five runs are green
//	rule · always
//
// The kind word comes first because the panel holds four kinds under one
// heading and nothing else on the row says which this is. A reminder has no
// cadence and no last time — it retires in the pass that fires it — so its
// sentence is the one moment it goes off. A routine says its cadence in the
// person's words, when the next one is, and what the last one came to. A watch
// says how often it looks, when it last looked, and what it found — `found
// nothing` being the commonest finding and a real one ([standRollup] says the
// same). A rule has no clock at all; it is its own words, or `holds`. An order
// in the middle of a pass says what the pass is doing instead of its clock.
//
// THE EMPTINESS LAW REACHES EVERY CLAUSE: a routine that has never fired has no
// `last:`, a watch that has never looked has no `last looked`, and a clause
// with nothing behind it is not drawn as a blank.
func nextUpSaid(view StandingItemView, now time.Time) string {
	item := view.Item
	if view.Running {
		return rowClauses(nextUpKindWord(item), standRunWord(view, now))
	}
	words := strings.TrimSpace(item.When.Words)
	switch item.When.Kind {
	case standing.WhenHold:
		if words == "" {
			words = standHoldsWord
		}
		return rowClauses(nextUpKindWord(item), words)
	case standing.WhenProbe, standing.WhenFile, standing.WhenIdle:
		looked, found := "", ""
		if !item.LastChecked.IsZero() {
			looked = nextUpLastLookedWord + sinceAt(item.LastChecked, now) + " ago"
			found = nextUpFoundNothingWord
			if line := switcherFirstLine(item.LastCheckLine); line != "" {
				found = nextUpFoundWord + line
			}
		}
		return rowClauses(nextUpKindWord(item), words, looked, found)
	case standing.WhenAt:
		return rowClauses(nextUpKindWord(item), nextUpGoesOffWord+homeClockAt(item.NextDue, now))
	}
	next, last := "", ""
	if !item.NextDue.IsZero() {
		next = nextUpNextWord + homeClockAt(item.NextDue, now)
	}
	if !item.LastFired.IsZero() {
		if outcome := switcherFirstLine(item.LastOutcome); outcome != "" {
			last = nextUpLastWord + outcome
		}
	}
	return rowClauses(nextUpKindWord(item), words, next, last)
}

// The fixed words of a `standing` row's sentence, spelled once.
const (
	nextUpGoesOffWord      = "goes off "
	nextUpNextWord         = "next "
	nextUpLastWord         = "last: "
	nextUpLastLookedWord   = "last looked "
	nextUpFoundWord        = "found: "
	nextUpFoundNothingWord = "found nothing"
)

// nextUpKindWord is the one word for what kind of order a row is: the four the
// standing store distinguishes, in the person's own vocabulary for them.
func nextUpKindWord(item standing.Item) string {
	switch item.When.Kind {
	case standing.WhenAt:
		return "reminder"
	case standing.WhenEvery:
		return "routine"
	case standing.WhenProbe, standing.WhenFile, standing.WhenIdle:
		return "watch"
	case standing.WhenHold:
		return "rule"
	}
	return ""
}

// homeClockAt is a moment said the way a person says one: `today 6:00pm`,
// `tomorrow 9:00am`, `mon 9:00am` inside the week, `21 sep 9:00am` beyond it,
// and `now` for a moment already here. The clock is the pulse's twelve-hour
// clock with its am or pm ([pulseClock] says why never a bare `9:41`).
func homeClockAt(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if !at.After(now) {
		return "now"
	}
	clock := strings.ToLower(at.Format("3:04pm"))
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	days := int(day(at).Sub(day(now)).Hours() / 24)
	switch {
	case days == 0:
		return "today " + clock
	case days == 1:
		return "tomorrow " + clock
	case days < 7:
		return strings.ToLower(at.Format("Mon")) + " " + clock
	}
	return strings.ToLower(at.Format("2 Jan")) + " " + clock
}
