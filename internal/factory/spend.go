package factory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DaySpend is the runner's figure for today. paid is what this process
// recorded today, by item id. An item in paid counts that figure instead of
// its whole stream, even when the figure is zero. A nil map means the caller
// has no ledger of its own, so an item that moved, started or ended today
// counts its whole stream spend. Triage reads are not added.
func DaySpend(items []Item, paid map[int]float64, now time.Time) float64 {
	y, mo, d := now.Date()
	isToday := func(t time.Time) bool {
		if t.IsZero() {
			return false
		}
		ty, tm, td := t.In(now.Location()).Date()
		return ty == y && tm == mo && td == d
	}
	var sum float64
	for _, it := range items {
		if it.Stream == nil {
			continue
		}
		if usd, ok := paid[it.ID]; ok {
			sum += usd
		} else if isToday(it.Changed) || isToday(it.Stream.Started) || isToday(it.Stream.Ended) {
			sum += it.Stream.Spent
		}
	}
	return sum
}

// JobSpent is what the job has spent, in dollars: its run's spend while it
// has a stream, which holds what its steps and its chat spent on the run, and
// what its chat spent before any run otherwise ([Item.ChatSpent]).
func JobSpent(it Item) float64 {
	if it.Stream != nil {
		return it.Stream.Spent
	}
	return it.ChatSpent
}

// AddChatSpend books usd that the job's own conversation spent where the
// job's money is: on its stream while it has one, and as ChatSpent while it
// has none. A cost of nothing, or less, is not booked.
func AddChatSpend(it *Item, usd float64) {
	if it == nil || usd <= 0 {
		return
	}
	if it.Stream != nil {
		it.Stream.Spent += usd
		return
	}
	it.ChatSpent += usd
}

// shiftSince is the handover a real floor can fill from its items. Spent is
// only streams that started after the window opened. SpentToday is [DaySpend]
// with no process ledger, so a stream that started earlier still counts today
// when the item moved today.
func shiftSince(items []Item, started, now time.Time) Shift {
	sh := Shift{Since: started, SpentToday: DaySpend(items, nil, now)}
	for _, it := range items {
		if it.Created.After(started) {
			sh.Arrived++
			sh.noteHour(it.Created, now)
		}
		if it.State == StateShipped {
			if at := shippedAt(it); at.After(started) {
				sh.Shipped++
				sh.Shipping = append(sh.Shipping, it.Ref())
				sh.noteHour(at, now)
			}
		}
		if at, ok := askedAt(it, started); ok {
			sh.Asked++
			sh.noteHour(at, now)
		}
		if n, at := handledSince(it, started); n > 0 {
			sh.Handled += n
			sh.noteHour(at, now)
		}
		if it.Stream != nil && it.Stream.Spent > 0 && it.Stream.Started.After(started) {
			sh.Spent += it.Stream.Spent
		}
	}
	sh.MergedToday, sh.MergedWeek = MergedCounts(items, now)
	return sh
}

// MergedCounts is how many pull requests this factory opened that GitHub has
// merged, by merged_at: today, and this calendar week starting Monday, both
// in now's location. A merge with no time is not counted. A future time is
// not counted.
func MergedCounts(items []Item, now time.Time) (today, week int) {
	if now.IsZero() {
		return 0, 0
	}
	loc := now.Location()
	y, m, d := now.In(loc).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	weekStart := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	for _, it := range items {
		pr := it.PullRequest
		if pr == nil || pr.State != PRMerged || pr.MergedAt.IsZero() {
			continue
		}
		at := pr.MergedAt.In(loc)
		if at.After(now) || at.Before(weekStart) {
			continue
		}
		week++
		if !at.Before(day) {
			today++
		}
	}
	return today, week
}

func (sh *Shift) noteHour(t, now time.Time) {
	if t.IsZero() || t.Before(now.Add(-24*time.Hour)) || t.After(now.Add(time.Minute)) {
		return
	}
	sh.Hours[t.In(now.Location()).Hour()]++
}

func shippedAt(it Item) time.Time {
	if it.Stream != nil && !it.Stream.Ended.IsZero() {
		return it.Stream.Ended
	}
	return it.Changed
}

// askedAt is an open question that arrived during the shift. Landed proof
// still carries a question and is not an ask.
func askedAt(it Item, started time.Time) (time.Time, bool) {
	if it.State == StateLanded || (it.State != StateNeedsYou && it.Question == "") {
		return time.Time{}, false
	}
	at := it.Changed
	if it.Asking != nil && !it.Asking.At.IsZero() {
		at = it.Asking.At
	}
	if !at.After(started) {
		return time.Time{}, false
	}
	return at, true
}

// handledSince counts answers and sign-offs written after the window opened.
// The words are the ones the runner stores: a yes, a no, a noted reply, and
// the two sign-off activities.
func handledSince(it Item, started time.Time) (int, time.Time) {
	var n int
	var at time.Time
	if it.Stream != nil {
		for _, line := range it.Stream.Log {
			if !line.At.After(started) || line.Tone != "said" {
				continue
			}
			text := line.Text
			if text == "yes · carrying on" || text == "no" || strings.HasPrefix(text, "noted:") {
				n++
				at = line.At
			}
		}
	}
	for _, ev := range it.Activity {
		if !ev.At.After(started) {
			continue
		}
		if ev.What == "signed off" || ev.What == "signed off with changes" {
			n++
			if ev.At.After(at) {
				at = ev.At
			}
		}
	}
	return n, at
}

// SpendDaysMax is the most calendar days one spend read may span.
const SpendDaysMax = 366

// SpendRange reads from and to, both YYYY-MM-DD and inclusive, as days in loc:
// start is from's midnight and end the midnight after to. A missing or
// malformed day, to before from, or more than [SpendDaysMax] days is refused
// in words a person can act on.
func SpendRange(from, to string, loc *time.Location) (start, end time.Time, err error) {
	start, err = time.ParseInLocation(time.DateOnly, from, loc)
	if err != nil {
		return start, end, errors.New("from is a day, as 2026-10-01")
	}
	last, err := time.ParseInLocation(time.DateOnly, to, loc)
	if err != nil {
		return start, end, errors.New("to is a day, as 2026-10-31")
	}
	if last.Before(start) {
		return start, end, errors.New("to is before from")
	}
	if start.AddDate(0, 0, SpendDaysMax).Before(last.AddDate(0, 0, 1)) {
		return start, end, fmt.Errorf("a spend read covers at most %d days", SpendDaysMax)
	}
	return start, last.AddDate(0, 0, 1), nil
}

// SpentAt is the instant a job's whole spend ([JobSpent]) is placed at: its
// run's start, or its creation when it has no run that started.
func SpentAt(it Item) time.Time {
	if it.Stream != nil && !it.Stream.Started.IsZero() {
		return it.Stream.Started
	}
	return it.Created
}
