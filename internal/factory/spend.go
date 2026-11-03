package factory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DaySpend is the runner's figure for today. paid is what this process
// recorded today, by item id. An item in paid counts that figure instead of
// its whole stream, even when the figure is zero. Any other item that moved,
// started or ended today counts its whole stream spend, which can be more than
// today's: the rail would rather stop early than miss money a restart forgot.
// Triage reads are not added.
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

// JobLifetimeSpent is all known spending on this job, including earlier runs.
func JobLifetimeSpent(it Item) float64 {
	return it.SpentBefore + JobSpent(it)
}

// ClearJobRun keeps the money before clearing the run and its pre-run chat.
// Calling it again cannot book the same money twice.
func ClearJobRun(it *Item) {
	it.SpentBefore += it.ChatSpent
	if it.Stream != nil {
		it.SpentBefore += it.Stream.Spent
	}
	for i := range it.ReportReceipts {
		it.ReportReceipts[i].Archived = true
	}
	it.Stream = nil
	it.ChatSpent = 0
	it.ChatModels = nil
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
// only streams that started after the window opened. SpentToday is
// [placedToday].
func shiftSince(items []Item, started, now time.Time) Shift {
	sh := Shift{Since: started, SpentToday: placedToday(items, now)}
	for _, it := range items {
		if it.Created.After(started) {
			sh.Arrived++
			sh.noteHour(it.Created, now)
		}
		if it.State == StateShipped {
			if at := ShippedAt(it); at.After(started) {
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

// placedToday is the spend placed ([SpentIn]) on now's calendar day, in now's
// location. A job keeps one total, so a job that began on an earlier day counts
// nothing today, even if it spent today: this figure can be low but never claims
// more than today's spend. Triage reads are not added.
func placedToday(items []Item, now time.Time) float64 {
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	var sum float64
	for _, it := range items {
		if usd, in := SpentIn(it, start, end); in {
			sum += usd
		}
	}
	return sum
}

// DailySpent is what items spent on runs whose job last changed today, in
// now's location: the day's spend a floor shows, whoever is reading it.
func DailySpent(items []Item, now time.Time) float64 {
	y, m, d := now.Date()
	var spent float64
	for _, it := range items {
		if it.Stream == nil {
			continue
		}
		if cy, cm, cd := it.Changed.In(now.Location()).Date(); cy == y && cm == m && cd == d {
			spent += it.Stream.Spent
		}
	}
	return spent
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

// ShippedAt is when a shipped job finished: its run's end, or its last
// change when the run kept no end.
func ShippedAt(it Item) time.Time {
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

// LedgerName is how the spend ledger names the job a call was spent for:
// `owner/name#number`, lower-cased like every repository key, the same on
// this machine's floor and on a worker's mirror of the job. A CI item's
// reference is the bare word "ci", so it is named by its floor id as well
// (`owner/name#ci-<id>`): two CI jobs of one repository are two jobs.
func LedgerName(it Item) string {
	ref := it.Ref()
	if it.Kind == KindCI {
		ref = "#ci-" + itoa(it.ID)
	}
	return strings.ToLower(RepoOf(it)) + ref
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

// SpentIn is the job's whole spend ([JobLifetimeSpent]) when it is placed ([SpentAt])
// in [start, end), and false when it spent nothing or is placed outside. It is
// the one placement rule every spend-by-day read counts by.
func SpentIn(it Item, start, end time.Time) (float64, bool) {
	usd, at := JobLifetimeSpent(it), SpentAt(it)
	return usd, usd > 0 && !at.Before(start) && at.Before(end)
}

// ModelUse is one model's priced calls on a job. It is recorded as each call
// is priced. A job from before the field has none, and its spend is not
// assigned to Item.Model or a step's model.
type ModelUse struct {
	Model  string  `json:"model"`
	USD    float64 `json:"usd"`
	Calls  int     `json:"calls,omitempty"`
	Input  int64   `json:"input,omitempty"`
	Output int64   `json:"output,omitempty"`
}

// AddModelUse books one priced call on the model that answered, beside the
// dollars [AddChatSpend] or the run already added. An empty model or a cost
// of nothing is not a split: the dollars stay untied. The row lives on the
// run while there is one, and on the chat's split before any run.
func AddModelUse(it *Item, model string, usd float64, calls int, in, out int64) {
	model = strings.TrimSpace(model)
	if it == nil || model == "" || usd <= 0 {
		return
	}
	if calls < 0 {
		calls = 0
	}
	if in < 0 {
		in = 0
	}
	if out < 0 {
		out = 0
	}
	row := ModelUse{Model: model, USD: usd, Calls: calls, Input: in, Output: out}
	if it.Stream != nil {
		it.Stream.Models = putModelUse(it.Stream.Models, row)
		return
	}
	it.ChatModels = putModelUse(it.ChatModels, row)
}

// ModelUses is the model split of the job's spend: the run's while it has
// one, and the chat's before any run.
func ModelUses(it Item) []ModelUse {
	if it.Stream != nil {
		return it.Stream.Models
	}
	return it.ChatModels
}

// JoinModelUses adds b's rows onto a copy of a, one row per model.
func JoinModelUses(a, b []ModelUse) []ModelUse {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := append([]ModelUse(nil), a...)
	for _, row := range b {
		out = putModelUse(out, row)
	}
	return out
}

func putModelUse(rows []ModelUse, row ModelUse) []ModelUse {
	if row.Model == "" || (row.USD <= 0 && row.Calls <= 0 && row.Input <= 0 && row.Output <= 0) {
		return rows
	}
	for i := range rows {
		if rows[i].Model != row.Model {
			continue
		}
		rows[i].USD += row.USD
		rows[i].Calls += row.Calls
		rows[i].Input += row.Input
		rows[i].Output += row.Output
		return rows
	}
	return append(rows, row)
}

// SpentAt is the instant a job's whole spend ([JobSpent]) is placed at: its
// run's start, or its creation when it has no run that started.
func SpentAt(it Item) time.Time {
	if it.Stream != nil && !it.Stream.Started.IsZero() {
		return it.Stream.Started
	}
	return it.Created
}

// AddMemorySpend books optional learning in the total and identifies its share.
func AddMemorySpend(it *Item, usd float64) {
	if it == nil || usd <= 0 {
		return
	}
	AddChatSpend(it, usd)
	it.MemorySpent += usd
}

// StepCost sums the worker's round receipts. Historical jobs without a table
// retain their last reported spend; reading never rewrites saved rows.
func (p Phase) StepCost() float64 {
	if p.RoundCosts != nil {
		var total float64
		for _, spent := range p.RoundCosts {
			total += spent
		}
		return total
	}
	if p.Result != nil {
		return p.Result.Spent
	}
	return 0
}

// RecordRoundSpend records a worker's cumulative receipt for one logical round.
func (p *Phase) RecordRoundSpend(round int, spent float64) {
	// A historical result has no complete receipt table. Keep its legacy
	// last-result display until a fresh run; inventing rows would misattribute
	// pending or interrupted work.
	if p.RoundCosts == nil && p.Result != nil {
		return
	}
	if p.RoundCosts == nil {
		p.RoundCosts = make(map[int]float64)
	}
	p.RoundCosts[max(1, round)] = spent
}
