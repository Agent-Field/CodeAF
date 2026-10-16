package session

// ── THE LEDGER, ASKED A QUESTION: one filter, exact totals, a page of rows ──
//
// [ReadUsage] answers every line since an instant, held in one slice, which is
// right for a page that sums the last two days and wrong for a person asking
// what a year cost: a year of calls is millions of rows, and the slice is all of
// them at once. This file is the other door. It READS THE FILE AS A STREAM —
// one bounded line at a time — and keeps only what the answer needs: the
// totals, the runs the range contains, and the one page of rows the caller
// asked for.
//
// THE GRAIN IS SAID, NEVER IMPLIED. A row of this ledger is one of two things:
//
//   - a RUN: every call made by one immutable execution ([UsageLine.Run]),
//     added up into one row whatever models it used;
//   - a CALL: one ledger line that carries no run, kept as its own row.
//
// A line without a run is an older build's line, an auxiliary call made beside
// the turn, or a receipt that arrived late. Nothing on it says which execution
// it belonged to, so it is NEVER folded into a run and NEVER folded with other
// such lines into a pretend "legacy" run: each one is its own record, counted
// apart from the runs, and its money is in every total.
//
// MONEY NOBODY COULD PRICE IS NOT FREE. A line whose figure is zero is a line
// nobody could price ([UsageLine.USD]), so a row counts those apart and a row
// made only of them has no cost at all rather than a cost of nothing.
//
// IT IS LINEAR AND SAYS SO. Every question reads the file from its first byte;
// there is no index and no cache, so nothing grows with the history except the
// time a scan takes. What it HOLDS is bounded: the runs in the range, under an
// explicit budget ([UsageLedgerBudget]) that refuses — asking for a shorter
// range — rather than answering with less than the range holds; and a page,
// chosen from them by a buffer of twice its size rather than by sorting them
// all. The budget is an ESTIMATE of the runs' weight, used to refuse early; it
// is not a promise about the process's resident memory.
//
// A FIGURE THAT CANNOT BE ADDED UP IS REFUSED, NEVER WRAPPED. A ledger line with
// negative tokens, or sums that would overflow an integer or leave the finite
// floats, is an answer this reader cannot give truthfully; it says so
// ([ErrUsageLedgerFigures]) rather than answering a wrapped count or a cost of
// infinity that no reply could carry.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The two grains a ledger row can have.
const (
	UsageGrainRun  = "run"
	UsageGrainCall = "call"
)

// The page sizes a ledger question may ask for: the default when it names
// none, and the most it may name.
const (
	UsageLedgerPageDefault = 200
	UsageLedgerPageMax     = 500
)

// usageScanSlots is how many full ledger scans may run at once in this
// process. A scan reads the whole file, so a third one beside two is a third
// pass over the same disk that answers nobody sooner; it is refused at once,
// with [ErrUsageScanBusy], rather than queued behind the others.
var usageScanSlots = make(chan struct{}, 2)

var (
	// ErrUsageScanBusy is a scan refused because enough are already running.
	ErrUsageScanBusy = errors.New("the spending ledger is already being read; try again in a moment")
	// ErrUsageLedgerTooLarge is a range holding more runs than the budget lets
	// one answer hold. It is refused rather than truncated.
	ErrUsageLedgerTooLarge = errors.New("this range holds too many runs to answer at once; ask for a shorter date range")
	// ErrUsageCursor is a cursor this query did not make.
	ErrUsageCursor = errors.New("the cursor does not belong to this query; start again without one")
	// ErrUsageLedgerChanged is a cursor whose ledger was replaced, cut
	// shorter or rewritten since the first page was read: continuing would
	// page through a different file as though it were the same one.
	ErrUsageLedgerChanged = errors.New("the spending ledger was replaced or rewritten since the first page; start again without a cursor")
	// ErrUsageLedgerFigures is a range whose figures cannot be added up
	// truthfully: a negative count, or sums past what a number can hold.
	ErrUsageLedgerFigures = errors.New("this range holds usage figures too large or invalid to add up; ask for a shorter date range or a narrower filter")
	// ErrUsageQuery is a query that cannot be asked as written.
	ErrUsageQuery = errors.New("the spending query is not valid")
)

// UsageQuery is one question to the ledger. Every field is optional and every
// filter is EXACT — the value as the ledger wrote it, after trimming — because
// a filter that matched loosely would make an export say something different
// from the preview it was saved from.
type UsageQuery struct {
	// From and To are the instants a line's [UsageLine.At] must fall in,
	// [From, To). A zero bound is open. [UsageDayRange] makes them from local
	// calendar days.
	From, To time.Time
	// Model is the provider's model id.
	Model string
	// Kind and Subject are what the money was spent on, classified as
	// [UsageBySubject] classifies it: Kind is one of [SubjectTask],
	// [SubjectAutomation], [SubjectConversation], and Subject — which needs a
	// Kind — is that thing's id.
	Kind, Subject string
	// The rest match one ledger field each.
	Session, Root, Task, Automation, Run, Workspace string
}

// UsageLedgerBudget bounds what one answer may hold beside its page.
type UsageLedgerBudget struct {
	// MaxRuns is the most distinct runs a range may hold.
	MaxRuns int
	// MaxBytes is an estimate of the memory those runs may take.
	MaxBytes int64
}

// DefaultUsageLedgerBudget is the budget a caller with no reason to choose
// another uses. A run row is a few hundred bytes, so this is tens of megabytes
// at the very most, and far more runs than a year of ordinary use makes.
var DefaultUsageLedgerBudget = UsageLedgerBudget{MaxRuns: 100_000, MaxBytes: 64 << 20}

// UsageEntry is one row of the ledger at its stated grain.
type UsageEntry struct {
	// Grain is [UsageGrainRun] or [UsageGrainCall].
	Grain string
	// Key is the row's stable identity: "run:" and the run's id, or "call:" and
	// the line's byte offset in the ledger, which never moves because the
	// ledger is only ever appended to.
	Key string
	// Run is the execution's id, and empty on a call row.
	Run string
	// Offset is a call row's byte offset, and -1 on a run row.
	Offset int64
	// Start and End are the earliest and latest call instants in the row.
	Start, End time.Time
	// Kind and Subject are what the money was spent on ([UsageBySubject]'s
	// classification). On a run row these, and the ids below, are set only
	// where every call in the run agrees; a field the calls disagree on is
	// left empty rather than chosen.
	Kind, Subject                              string
	Session, Root, Task, Automation, Workspace string
	// Models are the distinct model ids that answered, sorted; a call that
	// named no model is counted in UnnamedModel instead.
	Models       []string
	UnnamedModel int
	// Records is ledger lines; Calls is provider requests, where a line that
	// does not say counts as the one request every line is at least.
	Records, Calls int
	Input, Output  int64
	// USD is the sum of the priced lines. Unpriced counts the lines with no
	// figure, and Unbilled those marked as a missing receipt (also unpriced).
	USD                float64
	Unpriced, Unbilled int

	bytes   int64
	emitted bool
	// modelSet is Models as a set once a row has met more than
	// [usageModelSlice] of them. UNTIL A ROW IS FINISHED ([UsageEntry.finished])
	// Models is in the order the models were met: inserting each one in sorted
	// place would be quadratic in a row with a hundred thousand of them.
	modelSet map[string]struct{}
}

// usageModelSlice is how many distinct models a row looks for by walking its
// list; past it, a set. Ordinary runs use one or two.
const usageModelSlice = 8

// usageModelBytes is the estimate one distinct model adds to its row beside
// its own bytes: a slice slot and, past [usageModelSlice], a set entry.
const usageModelBytes = 64

// finished is the row as it is handed out: its models sorted in a slice of
// their own, and no set.
func (e UsageEntry) finished() UsageEntry {
	models := slices.Clone(e.Models)
	slices.Sort(models)
	e.Models, e.modelSet = models, nil
	return e
}

// Priced reports whether any line in the row carried a figure: a row with
// none has no cost, which is not the same as a cost of nothing.
func (e UsageEntry) Priced() bool { return e.Records > e.Unpriced }

// UsageLedgerTotals is the whole filtered range, exactly, whatever the page.
type UsageLedgerTotals struct {
	// Entries is the rows the range holds: Runs plus UnknownRunRecords.
	Entries int
	// Runs is distinct executions; UnknownRunRecords is lines that carry none,
	// each its own row.
	Runs, UnknownRunRecords int
	Records, Calls          int
	Input, Output           int64
	// USD is every priced line; RunUSD and UnknownRunUSD are its two halves.
	USD, RunUSD, UnknownRunUSD float64
	Unpriced, Unbilled         int
}

// UsageScanStats is what the reader passed over to give its answer.
type UsageScanStats struct {
	// Bytes is the whole lines read; a line still being written is not one.
	Bytes int64
	// Malformed is lines that did not parse or had no instant; Oversized is
	// lines longer than the reader will hold. Both are skipped, as
	// [ReadUsage] skips them.
	Malformed, Oversized int
}

// UsageLedgerPage is one question's answer.
type UsageLedgerPage struct {
	Totals  UsageLedgerTotals
	Entries []UsageEntry
	// Next is the cursor for the rows after these, and empty on the last page.
	Next  string
	Stats UsageScanStats
}

// UsageDayRange is the instants of the local calendar days from and to, both
// included: midnight of from up to midnight after to, in loc. It is computed
// from the calendar rather than by adding hours, so a day that is 23 or 25
// hours long is still exactly that day.
func UsageDayRange(from, to string, loc *time.Location) (time.Time, time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	start, err := time.ParseInLocation(usageDayLayout, strings.TrimSpace(from), loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from is a day, as 2026-10-15", ErrUsageQuery)
	}
	last, err := time.ParseInLocation(usageDayLayout, strings.TrimSpace(to), loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to is a day, as 2026-10-15", ErrUsageQuery)
	}
	if last.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to is before from", ErrUsageQuery)
	}
	end := time.Date(last.Year(), last.Month(), last.Day()+1, 0, 0, 0, 0, loc)
	return start, end, nil
}

// normalized trims every filter and checks the query can be asked.
func (q UsageQuery) normalized() (UsageQuery, error) {
	for _, field := range []*string{&q.Model, &q.Kind, &q.Subject, &q.Session, &q.Root, &q.Task, &q.Automation, &q.Run, &q.Workspace} {
		*field = strings.TrimSpace(*field)
	}
	switch q.Kind {
	case "", SubjectTask, SubjectAutomation, SubjectConversation:
	default:
		return q, fmt.Errorf("%w: kind is task, automation or conversation", ErrUsageQuery)
	}
	if q.Subject != "" && q.Kind == "" {
		return q, fmt.Errorf("%w: a subject needs its kind", ErrUsageQuery)
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.To.After(q.From) {
		return q, fmt.Errorf("%w: the range is empty", ErrUsageQuery)
	}
	return q, nil
}

// fingerprint is the query, hashed, so a cursor can say which query made it.
func (q UsageQuery) fingerprint() string {
	h := fnv.New64a()
	for _, part := range []string{
		strconv.FormatInt(q.From.UnixNano(), 10), strconv.FormatInt(q.To.UnixNano(), 10),
		q.Model, q.Kind, q.Subject, q.Session, q.Root, q.Task, q.Automation, q.Run, q.Workspace,
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// usageSubject is what a line's money was spent on, by [UsageBySubject]'s
// precedence: automation (or the standing item it replaced), then task, then
// the conversation the work was rooted in, then the line's own conversation.
// A test holds the two to the same answer on every fixture.
func usageSubject(line UsageLine) (kind, id string) {
	switch {
	case strings.TrimSpace(line.Automation) != "":
		return SubjectAutomation, strings.TrimSpace(line.Automation)
	case strings.TrimSpace(line.Standing) != "":
		return SubjectAutomation, strings.TrimSpace(line.Standing)
	case strings.TrimSpace(line.Task) != "":
		return SubjectTask, strings.TrimSpace(line.Task)
	case strings.TrimSpace(line.Root) != "":
		return SubjectConversation, strings.TrimSpace(line.Root)
	case strings.TrimSpace(line.Session) != "":
		return SubjectConversation, strings.TrimSpace(line.Session)
	}
	return "", ""
}

// matches is the one filter every reader of a query applies.
func (q UsageQuery) matches(line UsageLine) bool {
	if !q.From.IsZero() && line.At.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && !line.At.Before(q.To) {
		return false
	}
	exact := func(want, have string) bool { return want == "" || want == strings.TrimSpace(have) }
	if !exact(q.Model, line.Model) || !exact(q.Session, line.Session) || !exact(q.Root, line.Root) ||
		!exact(q.Task, line.Task) || !exact(q.Automation, line.Automation) || !exact(q.Run, line.Run) ||
		!exact(q.Workspace, line.Workspace) {
		return false
	}
	if q.Kind != "" {
		kind, id := usageSubject(line)
		if kind != q.Kind || (q.Subject != "" && id != q.Subject) {
			return false
		}
	}
	return true
}

// ── reading the file a bounded line at a time ───────────────────────────────

// scanUsageLines calls fn with every whole, parseable line of r and its byte
// offset, reading no further than limit bytes when limit is not negative.
//
// IT HOLDS ONE LINE AT A TIME AND NEVER MORE THAN [usageLineBytes] OF IT. A line
// longer than that is read past in pieces and skipped, but its bytes are still
// counted as consumed — [scanUsage]'s rule, so one absurd row costs its own
// record and never the rows after it. A last line with no newline is a write in
// flight and is neither parsed nor counted, as there.
func scanUsageLines(r io.Reader, limit int64, fn func(offset int64, line UsageLine) error) (UsageScanStats, error) {
	var stats UsageScanStats
	if limit >= 0 {
		r = io.LimitReader(r, limit)
	}
	buffered := bufio.NewReaderSize(r, usageLineBytes)
	for {
		chunk, err := buffered.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Longer than the reader will hold: read past the rest of it.
			size := int64(len(chunk))
			for errors.Is(err, bufio.ErrBufferFull) {
				chunk, err = buffered.ReadSlice('\n')
				size += int64(len(chunk))
			}
			if err != nil {
				return stats, usageTailError(err)
			}
			stats.Bytes += size
			stats.Oversized++
			continue
		}
		if err != nil {
			return stats, usageTailError(err)
		}
		offset := stats.Bytes
		stats.Bytes += int64(len(chunk))
		var line UsageLine
		if json.Unmarshal(bytes.TrimSpace(chunk), &line) != nil || line.At.IsZero() {
			stats.Malformed++
			continue
		}
		if err := fn(offset, line); err != nil {
			return stats, err
		}
	}
}

// usageTailError is the end of the reading: the end of the file (with or
// without a partial line after the last whole one) is no error at all.
func usageTailError(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// openUsageScan takes a scan slot and opens the ledger. A ledger that is not
// there is a machine that has spent nothing, and answers a nil file.
func openUsageScan(path string) (*os.File, func(), error) {
	select {
	case usageScanSlots <- struct{}{}:
	default:
		return nil, nil, ErrUsageScanBusy
	}
	release := func() { <-usageScanSlots }
	if strings.TrimSpace(path) == "" {
		return nil, release, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, release, nil
	}
	if err != nil {
		release()
		return nil, nil, err
	}
	return file, func() { file.Close(); release() }, nil
}

// ── folding lines into rows ─────────────────────────────────────────────────

// usageRunBytes is the estimate of one run row's own weight, before its
// strings: the struct, its map slot, its key.
const usageRunBytes = 256

// add folds one line into the row.
func (e *UsageEntry) add(line UsageLine) {
	kind, subject := usageSubject(line)
	first := e.Records == 0
	agree := func(have *string, value string) {
		if first {
			*have = value
		} else if *have != value {
			*have = ""
		}
	}
	agree(&e.Kind, kind)
	agree(&e.Subject, subject)
	agree(&e.Session, strings.TrimSpace(line.Session))
	agree(&e.Root, strings.TrimSpace(line.Root))
	agree(&e.Task, strings.TrimSpace(line.Task))
	agree(&e.Automation, strings.TrimSpace(line.Automation))
	agree(&e.Workspace, strings.TrimSpace(line.Workspace))
	if first || line.At.Before(e.Start) {
		e.Start = line.At
	}
	if first || line.At.After(e.End) {
		e.End = line.At
	}
	if model := strings.TrimSpace(line.Model); model == "" {
		e.UnnamedModel++
	} else if !e.hasModel(model) {
		e.Models = append(e.Models, model)
		if e.modelSet != nil {
			e.modelSet[model] = struct{}{}
		} else if len(e.Models) > usageModelSlice {
			e.modelSet = make(map[string]struct{}, len(e.Models)*2)
			for _, m := range e.Models {
				e.modelSet[m] = struct{}{}
			}
		}
		e.bytes += int64(len(model)) + usageModelBytes
	}
	e.Records++
	e.Calls += max(line.Calls, 1)
	e.Input += int64(line.Input)
	e.Output += int64(line.Output)
	if line.USD > 0 {
		e.USD += line.USD
	} else {
		e.Unpriced++
	}
	if line.Unbilled {
		e.Unbilled++
	}
	if first {
		e.bytes += usageRunBytes + int64(len(e.Key)+len(e.Session)+len(e.Root)+len(e.Task)+len(e.Automation)+len(e.Workspace)+len(e.Subject))
	}
}

// hasModel reports whether the row has already met model.
func (e *UsageEntry) hasModel(model string) bool {
	if e.modelSet != nil {
		_, ok := e.modelSet[model]
		return ok
	}
	return slices.Contains(e.Models, model)
}

// count adds one line to the totals, refusing a figure it cannot add.
//
// THE TOTALS ARE CHECKED AND THE ROWS ARE NOT, because every figure a row adds
// is non-negative once this has passed and a row is a part of the totals: a
// sum the totals could hold, every row's share of it can too.
func (t *UsageLedgerTotals) count(line UsageLine) error {
	calls := max(line.Calls, 1)
	if line.Input < 0 || line.Output < 0 || math.IsNaN(line.USD) || math.IsInf(line.USD, 0) ||
		t.Records == math.MaxInt || t.Calls > math.MaxInt-calls ||
		t.Input > math.MaxInt64-int64(line.Input) || t.Output > math.MaxInt64-int64(line.Output) {
		return ErrUsageLedgerFigures
	}
	if line.USD > 0 && math.IsInf(t.USD+line.USD, 0) {
		return ErrUsageLedgerFigures
	}
	t.Records++
	t.Calls += calls
	t.Input += int64(line.Input)
	t.Output += int64(line.Output)
	if line.USD > 0 {
		t.USD += line.USD
		if line.Run != "" {
			t.RunUSD += line.USD
		} else {
			t.UnknownRunUSD += line.USD
		}
	} else {
		t.Unpriced++
	}
	if line.Unbilled {
		t.Unbilled++
	}
	if line.Run == "" {
		t.UnknownRunRecords++
	}
	return nil
}

func usageCallEntry(offset int64, line UsageLine) UsageEntry {
	e := UsageEntry{Grain: UsageGrainCall, Key: fmt.Sprintf("call:%016d", offset), Offset: offset}
	e.add(line)
	return e
}

// usageRuns folds the runs a scan meets, under the budget.
type usageRuns struct {
	budget UsageLedgerBudget
	rows   map[string]*UsageEntry
	bytes  int64
}

func newUsageRuns(budget UsageLedgerBudget) *usageRuns {
	if budget.MaxRuns <= 0 {
		budget.MaxRuns = DefaultUsageLedgerBudget.MaxRuns
	}
	if budget.MaxBytes <= 0 {
		budget.MaxBytes = DefaultUsageLedgerBudget.MaxBytes
	}
	return &usageRuns{budget: budget, rows: map[string]*UsageEntry{}}
}

func (r *usageRuns) add(line UsageLine) error {
	row := r.rows[line.Run]
	if row == nil {
		if len(r.rows) >= r.budget.MaxRuns {
			return fmt.Errorf("%w (more than %d runs)", ErrUsageLedgerTooLarge, r.budget.MaxRuns)
		}
		row = &UsageEntry{Grain: UsageGrainRun, Key: "run:" + line.Run, Run: line.Run, Offset: -1}
		r.rows[line.Run] = row
	}
	before := row.bytes
	row.add(line)
	r.bytes += row.bytes - before
	if r.bytes > r.budget.MaxBytes {
		return fmt.Errorf("%w (more than %d bytes of runs)", ErrUsageLedgerTooLarge, r.budget.MaxBytes)
	}
	return nil
}

// ── the page ────────────────────────────────────────────────────────────────

// usageBefore is the page order: newest start first, then key, so two rows
// that started in the same instant still have one order every page agrees on.
func usageBefore(a, b UsageEntry) bool {
	if !a.Start.Equal(b.Start) {
		return a.Start.After(b.Start)
	}
	return a.Key < b.Key
}

// usageCursor is where a page ended — the last row's start and key, and the
// query that made it — and WHICH BYTES the first page read: the length of the
// ledger it read (always a line boundary) and a hash of that prefix's ends.
type usageCursor struct {
	V     int    `json:"v"`
	Start int64  `json:"s"`
	Key   string `json:"k"`
	Query string `json:"q"`
	Floor int64  `json:"n"`
	Ends  string `json:"h"`
}

// usageCursorVersion is the cursor's shape; an older shape is refused.
const usageCursorVersion = 2

// usageEndBytes is how much of each end of the read prefix a cursor's hash
// covers: enough to tell another file, and a fixed cost however long the
// ledger is.
const usageEndBytes = 4096

// usageReading is the prefix of the ledger one paging reads, said by a cursor.
type usageReading struct {
	floor int64
	ends  string
}

// usagePrefixEnds hashes the first and last [usageEndBytes] of the first floor
// bytes of file. It reads at fixed offsets, so a scan's position is untouched
// and an append made meanwhile is not read.
func usagePrefixEnds(file *os.File, floor int64) (string, error) {
	h := fnv.New64a()
	if file != nil && floor > 0 {
		buf := make([]byte, min(floor, usageEndBytes))
		for _, at := range []int64{0, floor - int64(len(buf))} {
			if _, err := file.ReadAt(buf, at); err != nil {
				return "", err
			}
			h.Write(buf)
		}
	}
	return strconv.FormatInt(floor, 36) + "." + strconv.FormatUint(h.Sum64(), 36), nil
}

func encodeUsageCursor(q UsageQuery, last UsageEntry, reading usageReading) string {
	raw, _ := json.Marshal(usageCursor{V: usageCursorVersion, Start: last.Start.UnixNano(), Key: last.Key, Query: q.fingerprint(), Floor: reading.floor, Ends: reading.ends})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeUsageCursor(q UsageQuery, cursor string) (*UsageEntry, *usageReading, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return nil, nil, nil
	}
	if len(cursor) > 1024 {
		return nil, nil, ErrUsageCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, nil, ErrUsageCursor
	}
	var c usageCursor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.V != usageCursorVersion || c.Key == "" || c.Query != q.fingerprint() || c.Floor < 0 || c.Ends == "" {
		return nil, nil, ErrUsageCursor
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, ErrUsageCursor
	}
	if !strings.HasPrefix(c.Key, "run:") && !strings.HasPrefix(c.Key, "call:") {
		return nil, nil, ErrUsageCursor
	}
	return &UsageEntry{Start: time.Unix(0, c.Start), Key: c.Key}, &usageReading{floor: c.Floor, ends: c.Ends}, nil
}

// QueryUsageLedger answers one page of the ledger at path: the totals of
// everything the query matches, exactly, and up to limit rows after cursor in
// page order (newest first). A limit of zero is the default page; one past the
// maximum is refused.
//
// THE TOTALS DO NOT DEPEND ON THE PAGE. They are summed from every matching
// line on the way past, so a page of one and a page of five hundred say the
// same totals, and paging to the end visits exactly Totals.Entries rows.
//
// EVERY PAGE OF ONE PAGING READS THE SAME BYTES. The first page reads the
// whole ledger and its cursor carries how far that was; a later page reads
// that prefix again and no further. So the ledger growing meanwhile — the one
// change it ever has — leaves every page and every total exactly as the first
// page said them, and what was recorded since is the next paging's, from no
// cursor. A ledger shorter than that prefix, or whose prefix no longer has the
// same first and last bytes, is another file (rotated, restored, rewritten)
// and the cursor is refused with [ErrUsageLedgerChanged] rather than
// continued through it. A rewrite that keeps the length and both ends is not
// seen; nothing in codeaf rewrites the ledger.
func QueryUsageLedger(path string, q UsageQuery, cursor string, limit int, budget UsageLedgerBudget) (UsageLedgerPage, error) {
	q, err := q.normalized()
	if err != nil {
		return UsageLedgerPage{}, err
	}
	switch {
	case limit == 0:
		limit = UsageLedgerPageDefault
	case limit < 0 || limit > UsageLedgerPageMax:
		return UsageLedgerPage{}, fmt.Errorf("%w: limit is 1 to %d", ErrUsageQuery, UsageLedgerPageMax)
	}
	after, reading, err := decodeUsageCursor(q, cursor)
	if err != nil {
		return UsageLedgerPage{}, err
	}
	file, done, err := openUsageScan(path)
	if err != nil {
		return UsageLedgerPage{}, err
	}
	defer done()

	readLimit := int64(-1)
	if reading != nil {
		if file == nil && reading.floor > 0 {
			return UsageLedgerPage{}, ErrUsageLedgerChanged
		}
		if file != nil {
			info, err := file.Stat()
			if err != nil {
				return UsageLedgerPage{}, err
			}
			if info.Size() < reading.floor {
				return UsageLedgerPage{}, ErrUsageLedgerChanged
			}
		}
		ends, err := usagePrefixEnds(file, reading.floor)
		if err != nil {
			return UsageLedgerPage{}, err
		}
		if ends != reading.ends {
			return UsageLedgerPage{}, ErrUsageLedgerChanged
		}
		readLimit = reading.floor
	}

	var page UsageLedgerPage
	runs := newUsageRuns(budget)
	// THE PAGE IS CHOSEN, NOT SORTED OUT OF EVERYTHING. Only the best limit+1
	// rows after the cursor are held: a buffer trimmed back whenever it
	// doubles, so a range of a million unattributed calls costs a page's worth
	// of memory — and the runs, once the scan has added them up, pass through
	// the same buffer rather than being copied out of their map and sorted.
	var best []UsageEntry
	remaining := 0
	keep := limit + 1
	trim := func() {
		sort.Slice(best, func(i, j int) bool { return usageBefore(best[i], best[j]) })
		if len(best) > keep {
			clear(best[keep:])
			best = best[:keep]
		}
	}
	offer := func(entry UsageEntry) {
		if after != nil && !usageBefore(*after, entry) {
			return
		}
		remaining++
		best = append(best, entry)
		if len(best) >= 2*keep {
			trim()
		}
	}
	if file != nil {
		page.Stats, err = scanUsageLines(file, readLimit, func(offset int64, line UsageLine) error {
			line.Run = strings.TrimSpace(line.Run)
			if !q.matches(line) {
				return nil
			}
			if err := page.Totals.count(line); err != nil {
				return err
			}
			if line.Run != "" {
				return runs.add(line)
			}
			offer(usageCallEntry(offset, line))
			return nil
		})
		if err != nil {
			return UsageLedgerPage{}, err
		}
	}
	page.Totals.Runs = len(runs.rows)
	page.Totals.Entries = page.Totals.Runs + page.Totals.UnknownRunRecords
	for _, row := range runs.rows {
		offer(*row)
	}
	trim()

	// The page is copied into a slice of exactly its length, so nothing the
	// caller keeps holds the buffer, the runs, or the sets they counted
	// models in.
	page.Entries = make([]UsageEntry, min(limit, len(best)))
	for i := range page.Entries {
		page.Entries[i] = best[i].finished()
	}
	if remaining > len(page.Entries) && len(page.Entries) > 0 {
		if reading == nil {
			ends, err := usagePrefixEnds(file, page.Stats.Bytes)
			if err != nil {
				return UsageLedgerPage{}, err
			}
			reading = &usageReading{floor: page.Stats.Bytes, ends: ends}
		}
		page.Next = encodeUsageCursor(q, page.Entries[len(page.Entries)-1], *reading)
	}
	return page, nil
}

// EachUsageLedgerEntry hands fn every row the query matches, in LEDGER ORDER —
// the order the calls were recorded in, a run placed at its first call — and
// answers the totals of exactly the rows it handed over.
//
// IT IS TWO PASSES OVER ONE PREFIX OF THE FILE. The first adds up the runs,
// because a run's last call may be the file's last line; the second walks the
// same bytes again — no further, though the ledger may have grown since —
// handing each unattributed call over as it is met and each run where it
// begins. So what is written is exactly what was counted, and nothing but the
// runs is ever held.
func EachUsageLedgerEntry(path string, q UsageQuery, budget UsageLedgerBudget, fn func(UsageEntry) error) (UsageLedgerTotals, UsageScanStats, error) {
	q, err := q.normalized()
	if err != nil {
		return UsageLedgerTotals{}, UsageScanStats{}, err
	}
	file, done, err := openUsageScan(path)
	if err != nil {
		return UsageLedgerTotals{}, UsageScanStats{}, err
	}
	defer done()
	var totals UsageLedgerTotals
	if file == nil {
		return totals, UsageScanStats{}, nil
	}
	runs := newUsageRuns(budget)
	stats, err := scanUsageLines(file, -1, func(_ int64, line UsageLine) error {
		line.Run = strings.TrimSpace(line.Run)
		if !q.matches(line) {
			return nil
		}
		if err := totals.count(line); err != nil {
			return err
		}
		if line.Run != "" {
			return runs.add(line)
		}
		return nil
	})
	if err != nil {
		return UsageLedgerTotals{}, stats, err
	}
	totals.Runs = len(runs.rows)
	totals.Entries = totals.Runs + totals.UnknownRunRecords
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return UsageLedgerTotals{}, stats, err
	}
	_, err = scanUsageLines(file, stats.Bytes, func(offset int64, line UsageLine) error {
		line.Run = strings.TrimSpace(line.Run)
		if !q.matches(line) {
			return nil
		}
		if line.Run == "" {
			return fn(usageCallEntry(offset, line).finished())
		}
		row := runs.rows[line.Run]
		if row == nil || row.emitted {
			return nil
		}
		row.emitted = true
		return fn(row.finished())
	})
	if err != nil {
		return UsageLedgerTotals{}, stats, err
	}
	return totals, stats, nil
}
