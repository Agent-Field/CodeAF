package session

import (
	"testing"
	"time"
)

func spendLine(t *testing.T, day string, model, role string, usd float64) UsageLine {
	t.Helper()
	at := usageAt(t, day)
	return UsageLine{At: at, Day: at.Format(usageDayLayout), Model: model, Role: role, Calls: 1, Input: 100, Output: 20, USD: usd}
}

// THE QUIET DAYS ARE THE POINT. A sparkline that dropped them would draw a
// fortnight as ten bars and read as busier than the fortnight was.
func TestUsageByDayKeepsTheDaysNothingWasSpentOn(t *testing.T) {
	lines := []UsageLine{
		spendLine(t, "2026-08-12 09:00", "a", "", 1),
		spendLine(t, "2026-08-12 17:00", "a", "", 2),
		spendLine(t, "2026-08-14 09:00", "b", "", 4),
	}
	window := UsageWindow{From: usageAt(t, "2026-08-12 00:00"), To: usageAt(t, "2026-08-15 00:00"), Grain: GrainDay}
	series := UsageByDay(lines, window)
	if len(series) != 4 {
		t.Fatalf("the series has %d buckets, want one per day of the window: %+v", len(series), series)
	}
	if series[0].USD != 3 || series[0].Calls != 2 {
		t.Fatalf("aug 12 came to %+v, want both of its lines", series[0])
	}
	if series[1].USD != 0 || series[1].Calls != 0 {
		t.Fatalf("aug 13 was quiet and reads as %+v", series[1])
	}
	if series[2].USD != 4 {
		t.Fatalf("aug 14 came to %+v", series[2])
	}
	if series[3].USD != 0 {
		t.Fatalf("aug 15 was quiet and reads as %+v", series[3])
	}
	if series[0].Label != "aug 12" {
		t.Fatalf("the first bucket is labelled %q", series[0].Label)
	}
	if series[0].Tokens != 240 {
		t.Fatalf("aug 12 holds %d tokens, want input plus output of both lines", series[0].Tokens)
	}
}

// Money from outside a window piled onto its first bar would be an answer to a
// question nobody asked.
func TestUsageByDayIgnoresWhatFallsOutsideTheWindow(t *testing.T) {
	lines := []UsageLine{
		spendLine(t, "2026-08-10 09:00", "a", "", 9),
		spendLine(t, "2026-08-13 09:00", "a", "", 1),
		spendLine(t, "2026-08-20 09:00", "a", "", 9),
	}
	window := UsageWindow{From: usageAt(t, "2026-08-12 00:00"), To: usageAt(t, "2026-08-14 00:00"), Grain: GrainDay}
	total := 0.0
	for _, bucket := range UsageByDay(lines, window) {
		total += bucket.USD
	}
	if total != 1 {
		t.Fatalf("the window totals %v, want only the line inside it", total)
	}
}

// The LAST bucket has to hold its own whole day: To names a bucket's first
// moment, and a test against it alone would cut this afternoon off.
func TestTheLastBucketHoldsItsWholeDay(t *testing.T) {
	window := LastDays(usageAt(t, "2026-08-25 13:11"), 14)
	if got := window.Buckets(); got != 14 {
		t.Fatalf("last 14 days has %d buckets", got)
	}
	if !window.Holds(usageAt(t, "2026-08-25 23:59")) {
		t.Fatal("the last evening of the window falls outside it")
	}
	if window.Holds(usageAt(t, "2026-08-26 00:00")) {
		t.Fatal("tomorrow falls inside a window that ends today")
	}
	if window.Holds(usageAt(t, "2026-08-11 23:59")) {
		t.Fatal("the evening before the window falls inside it")
	}
	if !window.Holds(usageAt(t, "2026-08-12 00:00")) {
		t.Fatal("the window's own first moment falls outside it")
	}
	series := UsageByDay([]UsageLine{spendLine(t, "2026-08-25 23:30", "a", "", 5)}, window)
	if series[len(series)-1].USD != 5 {
		t.Fatalf("a late-evening line missed the last bucket: %+v", series[len(series)-1])
	}
}

// THE DAY IS THE WRITER'S AND NOT THE READER'S. A process in Toronto records a
// call at 23:30 as august 25; read an hour east — over ssh, in a container, in a
// test — its timestamp falls on august 26, and a series that re-derived the day
// from that timestamp would charge the money to a day the writer never had. The
// row carries its day for exactly this reason.
func TestUsageByDayChargesTheDayTheWriterWroteDown(t *testing.T) {
	// The row as a reader one zone east of the writer sees it: the stamp has
	// already turned over, the written day has not.
	line := UsageLine{At: usageAt(t, "2026-08-26 00:30"), Day: "2026-08-25",
		Model: "opus-4.1", Calls: 1, Input: 100, Output: 20, USD: 7}
	window := UsageWindow{From: usageAt(t, "2026-08-24 00:00"), To: usageAt(t, "2026-08-26 00:00"), Grain: GrainDay}
	series := UsageByDay([]UsageLine{line}, window)
	if len(series) != 3 {
		t.Fatalf("the series has %d buckets, want one per day: %+v", len(series), series)
	}
	if series[1].USD != 7 || series[1].Label != "aug 25" {
		t.Fatalf("the writer's own day came to %+v, want the whole $7 on aug 25", series[1])
	}
	if series[2].USD != 0 {
		t.Fatalf("aug 26 was charged %v for a call the writer booked on the 25th", series[2].USD)
	}

	// A row that carries no day at all — an older ledger, a line written by
	// hand — still has its timestamp, read locally, and nothing better.
	bare := UsageLine{At: usageAt(t, "2026-08-26 00:30"), Model: "opus-4.1", Calls: 1, USD: 3}
	series = UsageByDay([]UsageLine{bare}, window)
	if series[2].USD != 3 {
		t.Fatalf("a row with no day of its own landed on %+v, want its own timestamp's day", series)
	}
}

func TestUsageLineDayExposesTheWriterRecordedDayToWindowReaders(t *testing.T) {
	line := UsageLine{At: usageAt(t, "2026-08-26 00:30"), Day: "2026-08-25"}
	if got := UsageLineDay(line); !got.Equal(usageAt(t, "2026-08-25 00:00")) {
		t.Fatalf("the row resolves to %v, want the writer's august 25", got)
	}
}

// WEEKS AND MONTHS BUCKET BY THAT SAME DAY. A sunday's spending recorded in
// Toronto belongs to the week that sunday closes, whoever is reading it and
// whatever their clock says the stamp is.
func TestAWeekBucketsByTheWrittenDayToo(t *testing.T) {
	// Sunday the 23rd where it was spent; monday the 24th where it is read.
	line := UsageLine{At: usageAt(t, "2026-08-24 00:30"), Day: "2026-08-23",
		Model: "a", Calls: 1, Input: 100, Output: 20, USD: 5}
	window := UsageWindow{From: usageAt(t, "2026-08-19 00:00"), To: usageAt(t, "2026-08-25 00:00"), Grain: GrainWeek}
	series := UsageByDay([]UsageLine{line}, window)
	if len(series) != 2 {
		t.Fatalf("the window holds %d weeks, want 2: %+v", len(series), series)
	}
	if series[0].USD != 5 {
		t.Fatalf("the week of aug 17 came to %v, want the sunday that closes it", series[0].USD)
	}
	if series[1].USD != 0 {
		t.Fatalf("the week of aug 24 was charged %v for the sunday before it", series[1].USD)
	}
}

// The label is the control and the reading at once, so its spelling is a fact
// worth pinning: lowercase month, en dash, no year inside one.
func TestTheWindowLabelIsSpelledTheWayTheScreenSpellsIt(t *testing.T) {
	window := UsageWindow{From: usageAt(t, "2026-08-12 00:00"), To: usageAt(t, "2026-08-25 00:00"), Grain: GrainDay}
	if got := window.Label(); got != "aug 12 – aug 25" {
		t.Fatalf("the label is %q", got)
	}
	one := UsageWindow{From: usageAt(t, "2026-08-25 00:00"), To: usageAt(t, "2026-08-25 00:00"), Grain: GrainDay}
	if got := one.Label(); got != "aug 25" {
		t.Fatalf("a one-day window is labelled %q", got)
	}
	// A range with the year on one end alone reads as a typo, so both wear it.
	newYear := UsageWindow{From: usageAt(t, "2025-12-28 00:00"), To: usageAt(t, "2026-01-10 00:00"), Grain: GrainDay}
	if got := newYear.Label(); got != "dec 28 2025 – jan 10 2026" {
		t.Fatalf("a window across new year is labelled %q", got)
	}
	if got := (UsageWindow{}).Label(); got != "" {
		t.Fatalf("a window of nothing is labelled %q, want nothing at all", got)
	}
}

// `shift+→` once is the next fortnight, not the next day: the label between the
// arrows is the reading, so a press has to move it by a whole one of itself.
func TestSteppingMovesTheWindowByItsOwnLength(t *testing.T) {
	window := LastDays(usageAt(t, "2026-08-25 13:11"), 14)
	back := window.Step(-1)
	if got := back.Label(); got != "jul 29 – aug 11" {
		t.Fatalf("one window back is %q", got)
	}
	if got := back.Buckets(); got != 14 {
		t.Fatalf("stepping changed the window's length to %d", got)
	}
	if got := back.Step(1).Label(); got != window.Label() {
		t.Fatalf("back then forward landed on %q, want %q", got, window.Label())
	}
}

// A fortnight of days becomes a fortnight of WEEKS — the bucket count is what
// holds still, which is what makes the key a zoom rather than a jump.
func TestCoarserKeepsTheBucketCountAndFinerPutsItBack(t *testing.T) {
	window := LastDays(usageAt(t, "2026-08-25 13:11"), 14)
	weeks := window.Coarser()
	if weeks.Grain != GrainWeek {
		t.Fatalf("one step up is grain %q", weeks.Grain)
	}
	if got := weeks.Buckets(); got != 14 {
		t.Fatalf("a fortnight of days became %d weeks, want 14", got)
	}
	// A week bucket starts on Monday: aug 25 2026 is a Tuesday, so the window's
	// last bar is the week of aug 24.
	if weeks.To.Weekday() != time.Monday {
		t.Fatalf("a week bucket starts on %s", weeks.To.Weekday())
	}
	months := weeks.Coarser()
	if months.Grain != GrainMonth || months.Buckets() != 14 {
		t.Fatalf("two steps up is %q with %d buckets", months.Grain, months.Buckets())
	}
	if months.To.Day() != 1 {
		t.Fatalf("a month bucket starts on the %d", months.To.Day())
	}
	if got := months.Coarser().Grain; got != GrainMonth {
		t.Fatalf("there is a rung above months: %q", got)
	}
	if got := weeks.Finer(); got.Grain != GrainDay || got.Buckets() != 14 {
		t.Fatalf("one step down is %q with %d buckets", got.Grain, got.Buckets())
	}
	if got := window.Finer().Grain; got != GrainDay {
		t.Fatalf("there is a rung below days: %q", got)
	}
}

// A week's bars each have to be one week — a bucket straddling two of them is a
// bar about nothing.
func TestAWeekGrainWindowBucketsFromMonday(t *testing.T) {
	lines := []UsageLine{
		spendLine(t, "2026-08-17 09:00", "a", "", 1), // monday
		spendLine(t, "2026-08-23 22:00", "a", "", 2), // the sunday that closes it
		spendLine(t, "2026-08-24 09:00", "a", "", 4), // the next monday
	}
	window := UsageWindow{From: usageAt(t, "2026-08-19 00:00"), To: usageAt(t, "2026-08-25 00:00"), Grain: GrainWeek}
	series := UsageByDay(lines, window)
	if len(series) != 2 {
		t.Fatalf("the window holds %d weeks, want 2: %+v", len(series), series)
	}
	if series[0].USD != 3 {
		t.Fatalf("the first week came to %v, want monday and its sunday together", series[0].USD)
	}
	if series[1].USD != 4 {
		t.Fatalf("the second week came to %v", series[1].USD)
	}
}

// ONE ROW PER MODEL, and the per-call role word does not split it.
//
// This used to group by the pair (model, role) so that a page could say which
// slice of a model's bill was naming. SCREEN 2c asks the other question — the
// role a model was BOUND to, which is a settings fact rather than a call fact —
// so the arithmetic here answers per model and the page makes the join. Three
// opus lines, one of them named `title` by the call that made it, are one row.
func TestUsageByModelGroupsOneRowPerModelDearestFirst(t *testing.T) {
	lines := []UsageLine{
		spendLine(t, "2026-08-25 09:00", "opus-4.1", "", 10),
		spendLine(t, "2026-08-25 10:00", "opus-4.1", "", 11),
		spendLine(t, "2026-08-25 11:00", "opus-4.1", "title", 1),
		spendLine(t, "2026-08-25 12:00", "haiku-4.5", "", 30),
	}
	rows := UsageByModel(lines)
	if len(rows) != 2 {
		t.Fatalf("grouped into %d rows, want one per model: %+v", len(rows), rows)
	}
	if rows[0].Model != "haiku-4.5" || rows[0].USD != 30 {
		t.Fatalf("the dearest row is %+v", rows[0])
	}
	if rows[1].Model != "opus-4.1" || rows[1].USD != 22 || rows[1].Calls != 3 {
		t.Fatalf("the opus row is %+v — the `title` call belongs in it", rows[1])
	}
	if rows[1].Tokens != 360 {
		t.Fatalf("the opus row holds %d tokens", rows[1].Tokens)
	}
}

// Three kinds of thing money is ever spent on, and a task's rows collapse into
// one row for the task rather than one per call.
func TestUsageBySubjectSaysWhatTheMoneyWasFor(t *testing.T) {
	conversation := spendLine(t, "2026-08-25 09:00", "a", "", 1)
	conversation.Session = "sess-1"
	first := spendLine(t, "2026-08-25 10:00", "a", "", 3)
	first.Session, first.Task = "sess-1", "7"
	second := spendLine(t, "2026-08-25 11:00", "a", "", 4)
	second.Session, second.Task = "sess-1", "7"
	promise := spendLine(t, "2026-08-25 12:00", "a", "", 2)
	promise.Session, promise.Standing, promise.Task = "sess-run-a", "item-6am", "1"
	again := spendLine(t, "2026-08-26 12:00", "a", "", 2)
	again.Session, again.Standing, again.Task = "sess-run-b", "item-6am", "1"

	rows := UsageBySubject([]UsageLine{conversation, first, second, promise, again})
	if len(rows) != 3 {
		t.Fatalf("grouped into %d rows, want the task, the promise and the conversation: %+v", len(rows), rows)
	}
	if rows[0].Kind != SubjectTask || rows[0].ID != "7" || rows[0].USD != 7 {
		t.Fatalf("the dearest row is %+v", rows[0])
	}
	if rows[0].Label != "a task" || rows[0].Session != "sess-1" {
		t.Fatalf("the task row is %+v", rows[0])
	}
	// TWO FIRINGS, ONE PROMISE. A row per run folder would be a log, not an
	// answer to "what was this for".
	if rows[1].Kind != SubjectStanding || rows[1].ID != "item-6am" || rows[1].USD != 4 {
		t.Fatalf("the standing row is %+v", rows[1])
	}
	if rows[1].Label != "standing" || rows[1].Session != "" {
		t.Fatalf("the standing row named a run folder: %+v", rows[1])
	}
	if rows[2].Kind != SubjectConversation || rows[2].ID != "sess-1" || rows[2].Label != "a conversation" {
		t.Fatalf("the conversation row is %+v", rows[2])
	}
}

// No machinery word may reach a screen through the one function that spells
// these three.
func TestTheSubjectWordsAreWordsAPersonUses(t *testing.T) {
	for kind, want := range map[string]string{
		SubjectTask:         "a task",
		SubjectStanding:     "standing",
		SubjectConversation: "a conversation",
		"":                  "",
		"errand":            "",
	} {
		if got := UsageSubjectWord(kind); got != want {
			t.Fatalf("UsageSubjectWord(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestUsageTotalsAddsWhatTheBarsAdd(t *testing.T) {
	lines := []UsageLine{
		spendLine(t, "2026-08-25 09:00", "a", "", 1.5),
		spendLine(t, "2026-08-25 10:00", "b", "", 2.5),
	}
	total := UsageTotals(lines)
	if total.USD != 4 || total.Calls != 2 || total.Tokens != 240 {
		t.Fatalf("the totals are %+v", total)
	}
}
