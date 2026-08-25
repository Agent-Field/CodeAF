package session

// READING THE USAGE LEDGER: three questions and one control.
//
// Screen 2c asks three things of the same rows and nothing else: which days,
// which models, and what the money was FOR. Screen 3d says how a person moves
// between them — two arrow axes, `shift+←→` for which window and `shift+↑↓` for
// how coarse — so that no letter is spent on time.
//
// Everything here is a PURE FUNCTION over lines a page already has. Nothing in
// this file opens a file, takes a lock or looks at a clock that was not handed
// to it: the read is [UsageCache.Read]'s and the drawing is the page's, and
// arithmetic that sits between them must be callable on a three-second beat
// without anybody having to think about it.
//
// THE EMPTINESS LAW IS THE PAGE'S TO APPLY, NOT THIS FILE'S, and the split is
// deliberate. [UsageByDay] returns a bucket for every day in the window
// INCLUDING the days nothing was spent, because a sparkline that silently
// dropped its quiet days would draw a fortnight as a week and read as busier
// than the fortnight was. Those zero buckets are honest data; what a page must
// not do is print `$0.00` under them. The two are different rules and this one
// is the reader's half.

import (
	"sort"
	"strings"
	"time"
)

// ── the window ──────────────────────────────────────────────────────────────

// UsageGrain is how coarse a window's buckets are: the `shift+↑↓` axis.
type UsageGrain string

const (
	// GrainDay is the ground rung and the one every window starts on.
	GrainDay UsageGrain = "day"
	// GrainWeek buckets from MONDAY, because a person's working week starts
	// there and a sparkline whose bars each straddle two weeks is a sparkline
	// about nothing.
	GrainWeek UsageGrain = "week"
	// GrainMonth is the coarsest rung. There is deliberately no year: a window
	// of years is a question about a machine older than this program.
	GrainMonth UsageGrain = "month"
)

// UsageWindow is WHICH stretch of time a spend page is showing and HOW COARSE
// its buckets are — the two dimensions screen 3d puts on the two arrow axes.
//
// From and To are both INCLUSIVE and both name a BUCKET rather than an instant:
// they are normalized to the first moment of the bucket they fall in
// ([UsageWindow.Normalized]), so a window is a whole number of days, weeks or
// months and never a fortnight and a half. Every method here answers a
// normalized window, so a page can hold one in a field and press arrows at it
// forever without it drifting off the calendar.
//
// The zero UsageWindow covers nothing and is what a page holds before it has
// decided; [LastDays] is where an ordinary one comes from.
type UsageWindow struct {
	From  time.Time
	To    time.Time
	Grain UsageGrain
}

// LastDays is the window a spend page opens on: the last n days ending today,
// bucketed by day. It is a constructor rather than a literal at the call site
// because "the last fourteen days" has an edge case in it — today counts as one
// of them — and a page that got that wrong would be off by a day forever.
func LastDays(now time.Time, days int) UsageWindow {
	if days < 1 {
		days = 1
	}
	end := usageBucketStart(now, GrainDay)
	return UsageWindow{From: end.AddDate(0, 0, -(days - 1)), To: end, Grain: GrainDay}
}

// Normalized is this window with its grain settled and its ends moved onto
// bucket boundaries. Every other method answers one, so callers rarely need it;
// it is exported because a page building a window from a person's own dates does.
func (w UsageWindow) Normalized() UsageWindow {
	if w.Grain != GrainWeek && w.Grain != GrainMonth {
		w.Grain = GrainDay
	}
	if w.From.IsZero() || w.To.IsZero() {
		return w
	}
	if w.To.Before(w.From) {
		w.From, w.To = w.To, w.From
	}
	w.From = usageBucketStart(w.From, w.Grain)
	w.To = usageBucketStart(w.To, w.Grain)
	return w
}

// Buckets is how many bars this window draws — the count that stays the same
// when the grain changes, which is what makes [UsageWindow.Coarser] a zoom
// rather than a jump to somewhere else.
//
// It is bounded by [usageBucketCap]: a window wider than that is a window
// nobody asked for through the arrows, and counting it out one bucket at a time
// would be a loop with a person's clock in it.
func (w UsageWindow) Buckets() int {
	w = w.Normalized()
	if w.From.IsZero() || w.To.IsZero() {
		return 0
	}
	count := 0
	for at := w.From; !at.After(w.To) && count < usageBucketCap; at = usageBucketNext(at, w.Grain) {
		count++
	}
	return count
}

// usageBucketCap bounds every loop over a window's buckets. A sparkline is a
// few dozen cells wide; four hundred is far past any window an arrow key can
// reach in a sitting, and it is here so that a window built from a bad date can
// never turn a draw into a walk through the calendar.
const usageBucketCap = 400

// Step moves the window by its OWN LENGTH — `shift+→` once is the next
// fortnight, not the next day. Paging is what the arrows on screen 3d do: the
// label between them is the reading and the control at once, so a press has to
// change the reading by a whole one of it.
//
// n is how many windows, and it may be negative. A zero window is unmoved,
// because there is nothing there to move.
func (w UsageWindow) Step(n int) UsageWindow {
	w = w.Normalized()
	span := w.Buckets()
	if span == 0 || n == 0 {
		return w
	}
	w.From = usageBucketAdd(w.From, w.Grain, n*span)
	w.To = usageBucketAdd(w.To, w.Grain, n*span)
	return w
}

// Coarser is `shift+↑`: one rung up, KEEPING THE BUCKET COUNT — a fortnight of
// days becomes a fortnight of weeks, which is what screen 3d's own caption says
// the key does. The window's END is what holds still, because the end is where
// a person is looking; the start walks back to make room.
//
// A window already on months answers itself. There is no year rung
// ([GrainMonth] says why).
func (w UsageWindow) Coarser() UsageWindow { return w.regrain(usageCoarser(w.Normalized().Grain)) }

// Finer is `shift+↓`, the exact inverse of [UsageWindow.Coarser], and a window
// already on days answers itself.
func (w UsageWindow) Finer() UsageWindow { return w.regrain(usageFiner(w.Normalized().Grain)) }

// regrain re-buckets the window at a new grain, holding its end and its bucket
// count. It is one function so that the two keys can never come to disagree
// about what a zoom is: `shift+↑` then `shift+↓` lands on the window it started
// from, up to the rounding the calendar itself imposes.
func (w UsageWindow) regrain(grain UsageGrain) UsageWindow {
	w = w.Normalized()
	if grain == w.Grain || w.From.IsZero() || w.To.IsZero() {
		return w
	}
	span := w.Buckets()
	if span < 1 {
		span = 1
	}
	end := usageBucketStart(w.To, grain)
	return UsageWindow{From: usageBucketAdd(end, grain, -(span - 1)), To: end, Grain: grain}
}

// Holds says whether an instant falls inside this window — the whole of the
// last bucket included, which is the part a caller gets wrong on its own: `To`
// names a bucket's FIRST moment, so a window ending today has to hold this
// afternoon.
func (w UsageWindow) Holds(at time.Time) bool {
	w = w.Normalized()
	if w.From.IsZero() || w.To.IsZero() || at.IsZero() {
		return false
	}
	return !at.Before(w.From) && at.Before(usageBucketNext(w.To, w.Grain))
}

// Label is the window said out loud — "aug 12 – aug 25" — and it is the control
// and the reading at once, drawn between the two arrows.
//
// LOWERCASE MONTH, EN DASH, no year while the window sits inside one. That is
// screen 3d's own spelling, and it is a different question from the one
// internal/tui3's `sinceAt` answers with "2 Jan": that is an AGE falling back to
// a date, and this is the edge of a window somebody is steering. A window that
// straddles new year spells the year on both ends rather than on one, because a
// range with a year at one end reads as a typo.
//
// A window of one bucket is one date and no dash. A zero window is "" — the
// emptiness law, so a page that has not chosen a window yet draws no arrows.
func (w UsageWindow) Label() string {
	w = w.Normalized()
	if w.From.IsZero() || w.To.IsZero() {
		return ""
	}
	if w.From.Equal(w.To) {
		return usageDateWord(w.From, false)
	}
	sameYear := w.From.Year() == w.To.Year()
	return usageDateWord(w.From, !sameYear) + " – " + usageDateWord(w.To, !sameYear)
}

// usageDateWord is one end of a label: "aug 12", or "aug 12 2025" where the year
// has to be said. Lowercased in one place so the two ends cannot differ.
func usageDateWord(at time.Time, withYear bool) string {
	if withYear {
		return strings.ToLower(at.Format("Jan 2 2006"))
	}
	return strings.ToLower(at.Format("Jan 2"))
}

// ── the calendar arithmetic the window rests on ─────────────────────────────

// usageBucketStart is the first LOCAL moment of the bucket an instant falls in.
// Local, throughout: a person's day starts when their day starts, and a ledger
// keyed to UTC would draw their evening onto tomorrow's bar.
func usageBucketStart(at time.Time, grain UsageGrain) time.Time {
	at = at.Local()
	switch grain {
	case GrainWeek:
		// Go's week starts on Sunday and a working week does not, so Monday is
		// found by walking back the offset rather than by trusting Weekday's
		// numbering.
		back := (int(at.Weekday()) + 6) % 7
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
		return day.AddDate(0, 0, -back)
	case GrainMonth:
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, at.Location())
	}
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

// usageBucketAdd moves n buckets from a bucket's start, and re-anchors: adding
// a month to the 31st lands on a day that may not exist, and AddDate's own
// normalization would silently slide the window into the next month.
func usageBucketAdd(at time.Time, grain UsageGrain, n int) time.Time {
	switch grain {
	case GrainWeek:
		return usageBucketStart(at.AddDate(0, 0, 7*n), grain)
	case GrainMonth:
		return usageBucketStart(time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, at.Location()).AddDate(0, n, 0), grain)
	}
	// A DAY IS NOT TWENTY-FOUR HOURS. AddDate walks the calendar rather than the
	// clock, so a window stepped across a daylight-saving boundary still lands on
	// midnight rather than an hour either side of it.
	return usageBucketStart(at.AddDate(0, 0, n), grain)
}

func usageBucketNext(at time.Time, grain UsageGrain) time.Time {
	return usageBucketAdd(at, grain, 1)
}

func usageCoarser(grain UsageGrain) UsageGrain {
	if grain == GrainDay {
		return GrainWeek
	}
	if grain == GrainWeek {
		return GrainMonth
	}
	return GrainMonth
}

func usageFiner(grain UsageGrain) UsageGrain {
	if grain == GrainMonth {
		return GrainWeek
	}
	if grain == GrainWeek {
		return GrainDay
	}
	return GrainDay
}

// ── which days ──────────────────────────────────────────────────────────────

// DaySpend is one bar of the sparkline: a bucket, and what was spent in it.
//
// It is called a DAY because that is what it is at the grain every window starts
// on and the word a person uses for a bar; at week or month grain the same
// struct is a week or a month and [DaySpend.Label] says which.
type DaySpend struct {
	// At is the bucket's first local moment — its identity, and what a page sorts
	// or seeks on.
	At time.Time
	// Label is the bucket said out loud, in [UsageWindow.Label]'s spelling:
	// "aug 12". A week's label is the Monday it starts on; a month's is its
	// first.
	Label string
	Calls int
	// Tokens is input plus output as one sum, which is the figure every surface
	// in this codebase draws ([TaskIndexEntry.Tokens] states the law).
	Tokens int
	USD    float64
}

// UsageByDay buckets lines into the window's own buckets, in order, WITH THE
// EMPTY ONES PRESENT.
//
// The zero buckets are the point. A fortnight with four quiet days in it is a
// fortnight, and a series that dropped them would draw ten bars where fourteen
// belong — every quiet stretch compressed away and every busy one made to look
// continuous. So a bucket nothing landed in is a bucket with zero in it, and
// what a PAGE does with that zero is the page's business (this file's header
// says why the emptiness law splits here).
//
// Lines outside the window are ignored rather than clamped into its ends: a
// window is a question about a stretch of time, and money from outside it piled
// onto the first bar would be an answer to a different one.
//
// ── WHICH DAY A ROW BELONGS TO ──
//
// THE WRITER'S DAY, ALWAYS — [UsageLine.Day], the local calendar day the process
// that made the call was standing in. It is written into the row for exactly
// this reason ([usageDayLayout] says so): a machine in Toronto records a call at
// 23:30 as August 25, and a reader that re-derived the day from the timestamp
// would charge it to August 26 the moment anybody read the ledger under a
// different TZ — over ssh, in a container, in a test. A day that moves depending
// on who is asking is not a day.
//
// WEEKS AND MONTHS BUCKET BY THAT SAME DAY, parsed as a LOCAL date and taken to
// the Monday or the first of the month around it. Said plainly, because it is a
// real consequence rather than a detail: a viewer in another zone sees the
// WRITER'S days, grouped by the READER'S calendar — which is right, since the
// only thing a week or a month can be here is a set of whole days, and the days
// were settled where the money was spent.
//
// A row with no Day on it — an older ledger, a hand-written line — falls back to
// its timestamp read locally, which is the best that can be said about it.
func UsageByDay(lines []UsageLine, window UsageWindow) []DaySpend {
	window = window.Normalized()
	if window.From.IsZero() || window.To.IsZero() {
		return nil
	}
	// The buckets are laid out FIRST and filled second, which is what makes the
	// quiet ones present without a second pass to find the gaps.
	var series []DaySpend
	at := window.From
	for count := 0; !at.After(window.To) && count < usageBucketCap; count++ {
		series = append(series, DaySpend{At: at, Label: usageDateWord(at, false)})
		at = usageBucketNext(at, window.Grain)
	}
	if len(series) == 0 {
		return nil
	}
	// `at` now names the first bucket PAST the window, which is the exclusive
	// upper bound a line is tested against — including the whole of the last
	// bucket, which a test against To alone would cut off at its first moment.
	end := at
	for _, line := range lines {
		// The row's own day, at its first local moment — so the window test and
		// the bucket search are asking the same question, and both of them are
		// asking it about the day the writer recorded.
		when := usageLineDay(line)
		if when.Before(window.From) || !when.Before(end) {
			continue
		}
		start := usageBucketStart(when, window.Grain)
		// A binary search would be the same answer at a fraction of the reading;
		// the series is at most [usageBucketCap] long and this runs on a beat,
		// so the plain walk stays.
		for i := range series {
			if series[i].At.Equal(start) {
				series[i].Calls += line.Calls
				series[i].Tokens += line.Input + line.Output
				series[i].USD += line.USD
				break
			}
		}
	}
	return series
}

// usageLineDay is the first local moment of the day a row belongs to: the day
// the WRITER wrote down, parsed as a local date, and the row's own timestamp
// where it says nothing.
//
// It is one function rather than an expression at each site so that the window
// test and the bucket search in [UsageByDay] cannot come to disagree about which
// day a row is on — which would silently drop a row into no bucket at all.
func usageLineDay(line UsageLine) time.Time {
	if day := strings.TrimSpace(line.Day); day != "" {
		if at, err := time.ParseInLocation(usageDayLayout, day, time.Local); err == nil {
			return at
		}
	}
	return usageBucketStart(line.At, GrainDay)
}

// UsageTotals is what a whole stretch came to: the header line's three figures.
// It is a function rather than a sum a page writes itself so that the page and
// the bars can never disagree about what "last 14 days · $34.10 · 41.2M tokens"
// is a total of.
func UsageTotals(lines []UsageLine) DaySpend {
	var total DaySpend
	for _, line := range lines {
		total.Calls += line.Calls
		total.Tokens += line.Input + line.Output
		total.USD += line.USD
	}
	return total
}

// ── which models ────────────────────────────────────────────────────────────

// ModelSpend is one row of "what ran it": a model, the role it named itself
// under, and what that pairing cost.
type ModelSpend struct {
	// Model is the provider's own id, and empty where a line named none. A page
	// makes the pretty name; this is the join key.
	Model string
	// Role is the auxiliary name a call gave itself — "title", "taskname",
	// "intake" — and EMPTY FOR ALMOST EVERY ROW, including every turn a person
	// took. It is not one of the five router slots and a column headed with
	// those words would be a lie ([UsageLine.Role] says why).
	Role   string
	Calls  int
	Tokens int
	USD    float64
}

// UsageByModel groups by the pair (model, role), dearest first.
//
// THE PAIR AND NOT THE MODEL ALONE, because the same model answers a turn and
// names a session, and a page that showed one row for it could not tell somebody
// that a third of their naming bill is on a model they meant to be cheap. Rows
// where nobody named a role collapse into one row per model, which is what
// almost the whole file is.
//
// Ties break on calls, then on the names, so two runs over one ledger draw the
// same table in the same order.
func UsageByModel(lines []UsageLine) []ModelSpend {
	type key struct{ model, role string }
	totals := map[key]*ModelSpend{}
	for _, line := range lines {
		id := key{strings.TrimSpace(line.Model), strings.TrimSpace(line.Role)}
		row := totals[id]
		if row == nil {
			row = &ModelSpend{Model: id.model, Role: id.role}
			totals[id] = row
		}
		row.Calls += line.Calls
		row.Tokens += line.Input + line.Output
		row.USD += line.USD
	}
	rows := make([]ModelSpend, 0, len(totals))
	for _, row := range totals {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		switch {
		case rows[i].USD != rows[j].USD:
			return rows[i].USD > rows[j].USD
		case rows[i].Calls != rows[j].Calls:
			return rows[i].Calls > rows[j].Calls
		case rows[i].Model != rows[j].Model:
			return rows[i].Model < rows[j].Model
		}
		return rows[i].Role < rows[j].Role
	})
	return rows
}

// ── what it was for ─────────────────────────────────────────────────────────

// The three things money is ever spent ON, in the words a row shows.
//
// THEY ARE A CLOSED THREE BECAUSE THE LEDGER HOLDS THREE IDS. There is no
// fourth kind hiding in the file: a line was made inside a piece of work, inside
// a promise the person made, or inside a conversation they were having.
const (
	SubjectTask         = "task"
	SubjectStanding     = "standing"
	SubjectConversation = "conversation"
)

// SubjectSpend is one row of "what it was for": which thing, and what it cost.
type SubjectSpend struct {
	// Kind is one of [SubjectTask], [SubjectStanding], [SubjectConversation].
	Kind string
	// ID is the thing's own id — a node's id within its session, a standing
	// item's id, or the conversation's 16 hex. It is what a page JOINS on to get
	// a title: this file has no titles and will not invent any, so a page reads
	// the name off the task index, the standing store or the session it is
	// already holding.
	ID string
	// Session is the conversation a task's work was journaled under, so a page
	// with an ID and a Session can find the task index row that names it. It is
	// the same value as ID on a conversation row.
	Session string
	// Workspace is the project the money was spent against, and empty where the
	// line named none.
	Workspace string
	// Label is the row's LEFT WORD and nothing more: "a task", "standing", "a
	// conversation". It is deliberately not a title — see ID — and it is here so
	// that the three spellings live in one place rather than in each page that
	// draws them.
	Label  string
	Calls  int
	Tokens int
	USD    float64
}

// UsageBySubject groups the lines by what they were spent on, dearest first.
//
// A STANDING FIRING IS A STANDING FIRING AND NOT A TASK, even though it runs
// with a node's id beside it: a person recognises the promise they made months
// ago long before they recognise the run it spawned this morning, so the
// standing id wins wherever a line carries both. After that a task id wins over
// a conversation, for the same reason — the work is the thing that was asked
// for.
//
// Ties break the way [UsageByModel]'s do, so the table is stable.
func UsageBySubject(lines []UsageLine) []SubjectSpend {
	type key struct{ kind, id, session string }
	totals := map[key]*SubjectSpend{}
	for _, line := range lines {
		kind, id := SubjectConversation, strings.TrimSpace(line.Session)
		switch {
		case strings.TrimSpace(line.Standing) != "":
			kind, id = SubjectStanding, strings.TrimSpace(line.Standing)
		case strings.TrimSpace(line.Task) != "":
			kind, id = SubjectTask, strings.TrimSpace(line.Task)
		}
		at := key{kind, id, strings.TrimSpace(line.Session)}
		if kind == SubjectStanding {
			// A promise fires in a new folder every time, so grouping a standing
			// row by the session it happened in would draw one row per firing —
			// which is a log, not an answer to "what was this for".
			at.session = ""
		}
		row := totals[at]
		if row == nil {
			row = &SubjectSpend{
				Kind:      kind,
				ID:        id,
				Session:   at.session,
				Workspace: strings.TrimSpace(line.Workspace),
				Label:     UsageSubjectWord(kind),
			}
			totals[at] = row
		}
		row.Calls += line.Calls
		row.Tokens += line.Input + line.Output
		row.USD += line.USD
	}
	rows := make([]SubjectSpend, 0, len(totals))
	for _, row := range totals {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		switch {
		case rows[i].USD != rows[j].USD:
			return rows[i].USD > rows[j].USD
		case rows[i].Calls != rows[j].Calls:
			return rows[i].Calls > rows[j].Calls
		case rows[i].Kind != rows[j].Kind:
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// UsageSubjectWord is the word a row wears for what money went on, and it is the
// one place those three words are spelled — [TaskKindWord]'s law applied to the
// other axis of a spend row. An unknown kind reads as nothing rather than as
// itself, because a machine word leaking onto a screen is the failure this
// function exists to prevent.
func UsageSubjectWord(kind string) string {
	switch kind {
	case SubjectTask:
		return "a task"
	case SubjectStanding:
		return "standing"
	case SubjectConversation:
		return "a conversation"
	}
	return ""
}
